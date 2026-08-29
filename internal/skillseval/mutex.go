package skillseval

// mutex.go —— 跨 skill 互斥集 eval：从 SKIP 让渡边派生 contrast case。
//
// 既有 eval 闭环（cases.go/runs.go）是单 skill 视角：trigger/not-trigger case 从不说
// 误路由的 prompt「本该去哪」。而 SKILL.md description 的 SKIP 段已声明所有权让渡，
// 形如「Rust 代码审查（用 rust-code-review）」——括号里的（用 X）/（use X) 模式即
// 有向边 A --skip--> B。互斥 case 把边变成对比断言：用 B 自己的 trigger 片段（B 域）
// 构造的 prompt 必须路由到 B（Positive）、不得路由到 A（Negative）。
//
// 判定契约（已决，勿重议）：actual == Positive 才 pass；actual == Negative 是头号混淆行
// （A 声明过的让渡恰恰被违反）；actual == 其他只是普通路由失误。mutex-report --gate 在
// 任一混淆行存在时 exit 4，BLOCKED 行走 stderr——stdout 保持纯数据通道，对齐
// skills battery --gate 契约（internal/cli/skills_battery.go）。
//
// case ID 锚定 sha1("mutex:"+A+":"+B+":"+原始trigger片段)[:12]——未渲染的片段，与
// cases.go caseID 同理：渲染规则演进不应让 ID 集体漂移、毁掉回归信号。

import (
	"bufio"
	"cmp"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/MjxUpUp/Forge/internal/skillsfm"
	"github.com/MjxUpUp/Forge/internal/util"
)

// mutexDelegateRe 从 SKIP 括号提取让渡目标：语料里中文（用 X）与英文 (use X) 两种
// 写法都存在。只捕获 skill 名——「，与门控叠加不替代」这类尾缀限定不进边目标
// （让渡对象是 X，与限定语语义无关）。
var mutexDelegateRe = regexp.MustCompile(`[（(](?:用|use)\s+([a-z0-9-]+)`)

// mutexPromptsPerEdge 每边派生的 prompt 上限。B 的 trigger 片段按提取顺序取（即
// description 自身的显著度排序）；上限让 case 规模随边数线性增长，而非随片段数膨胀。
const mutexPromptsPerEdge = 2

// mutexRunMu 保护 runs.jsonl 追加。与 runMu（单 skill runs）分离——两份日志从不
// 交错，共用一把锁只会增加假竞争。
var mutexRunMu sync.Mutex

// MutexEdge is a directed delegation edge A --skip--> B parsed from A's SKIP section.
//
// MutexEdge 是从 A 的 SKIP 段解析出的有向让渡边 A --skip--> B。
type MutexEdge struct {
	From string `json:"from"` // A：声明让渡的 skill
	To   string `json:"to"`   // B：被让渡的目标 skill
	// Fragment is the raw SKIP fragment carrying the delegation (kept for case-ID anchoring and human audit).
	//
	// Fragment 是携带让渡的原始 SKIP 片段（供 case ID 锚定与人工审计）。
	Fragment string `json:"fragment"`
}

// MutexCase is one contrast case derived from an edge: a prompt from B's domain.
//
// MutexCase 是从一条边派生的单个对比 case：B 域的 prompt。
type MutexCase struct {
	ID       string `json:"id"`       // sha1("mutex:"+A+":"+B+":"+rawFragment)[:12]
	Positive string `json:"positive"` // B：正确路由目标
	Negative string `json:"negative"` // A：头号混淆目标（让渡声明者）
	Prompt   string `json:"prompt"`   // renderTriggerPrompt 渲染后的测试 prompt
	Source   string `json:"source"`   // 生成此 case 的 B 原始 trigger 片段
}

// MutexResult is the judged result of one mutex case inside a MutexRun.
//
// MutexResult 是 MutexRun 里单个互斥 case 的判定结果。
type MutexResult struct {
	CaseID string `json:"case_id"`
	Actual string `json:"actual"` // 归一化后的实际路由 skill（"" = 未触发）
	Pass   bool   `json:"pass"`   // actual == Positive 才为 true
}

// MutexRun is one complete mutex-set run, appended to <dir>/mutex/runs.jsonl.
//
// MutexRun 是一次完整的互斥集 run，append 到 <dir>/mutex/runs.jsonl。
// 字段刻意对齐 EvalRun（RunID/Timestamp/ForgeVersion/AgentModel），跨版本/跨模型
// 可比性守卫原样沿用。
type MutexRun struct {
	RunID        string        `json:"run_id"`
	Timestamp    time.Time     `json:"timestamp"`
	ForgeVersion string        `json:"forge_version"` // 防跨版本假回归
	AgentModel   string        `json:"agent_model"`   // agent 自报，防跨模型假回归
	Results      []MutexResult `json:"results"`
}

// MutexConfusion is one first-class confusion row: the prompt routed to Negative (A) — exactly the handoff A declared it would not claim.
//
// MutexConfusion 是一条头号混淆行：prompt 路由到了 Negative（A）——恰是 A 声明过
// 会让渡的所有权。
type MutexConfusion struct {
	CaseID   string `json:"case_id"`
	Positive string `json:"positive"`
	Negative string `json:"negative"`
	Prompt   string `json:"prompt"`
	Actual   string `json:"actual"`
}

// MutexMatrixCell is one (positive, actual) aggregation bucket of the confusion matrix.
//
// MutexMatrixCell 是混淆矩阵的一个 (positive, actual) 聚合格。
type MutexMatrixCell struct {
	Positive string `json:"positive"`
	Actual   string `json:"actual"`
	Count    int    `json:"count"`
}

// MutexMatrix is the report over the latest run: per-case judgments, the aggregated (positive, actual) matrix, and the confusion list.
//
// MutexMatrix 是基于最新 run 的报告：逐 case 判定、(positive, actual) 聚合矩阵、
// 混淆清单。
type MutexMatrix struct {
	Total      int               `json:"total"`
	Passed     int               `json:"passed"`
	Results    []MutexCaseJudged `json:"results"`
	Cells      []MutexMatrixCell `json:"cells"`
	Confusions []MutexConfusion  `json:"confusions,omitempty"`
	// GateBlocked: any confusion row exists (actual == Negative somewhere).
	//
	// GateBlocked：存在任一混淆行（某处 actual == Negative）。mutex-report --gate
	// 读它做 exit 4 决策——判定在此处，os.Exit 留在 CLI 薄壳。
	GateBlocked bool `json:"gate_blocked"`
}

// MutexCaseJudged pairs a case with its judged outcome for the report view.
//
// MutexCaseJudged 把 case 与其判定结果配对，供报告视图。
type MutexCaseJudged struct {
	CaseID   string `json:"case_id"`
	Positive string `json:"positive"`
	Negative string `json:"negative"`
	Prompt   string `json:"prompt"`
	Actual   string `json:"actual"`
	Pass     bool   `json:"pass"`
}

func mutexDir(dir string) string       { return filepath.Join(dir, "mutex") }
func mutexCasesFile(dir string) string { return filepath.Join(mutexDir(dir), "cases.json") }
func mutexRunsFile(dir string) string  { return filepath.Join(mutexDir(dir), "runs.jsonl") }

// mutexCaseID 把 case ID 锚定在边 + 未渲染 trigger 片段上（漂移理由见文件头注释）。
func mutexCaseID(from, to, rawFragment string) string {
	h := sha1.Sum([]byte("mutex:" + from + ":" + to + ":" + rawFragment))
	return hex.EncodeToString(h[:])[:12]
}

// MutexEdges walks every skill under canonical, parses the SKIP section of each
// description (same skipPartRe/skipSplitRe extraction as ExtractTriggers), and collects
// delegation edges.
//
// MutexEdges 遍历 canonical 下全部 skill，解析各 description 的 SKIP 段（与
// ExtractTriggers 同一套 skipPartRe/skipSplitRe 提取），收集让渡边。目标不在
// ListSkills 结果里的边被丢弃——悬空的（用 X）引用是 description 的 bug，不是
// eval 边；对它断言等于测一个不可能触发的让渡。
func MutexEdges(canonical string) ([]MutexEdge, error) {
	names, err := skillsdist.ListSkills(canonical)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(names))
	for _, n := range names {
		known[n] = true
	}

	var edges []MutexEdge
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(canonical, name, "SKILL.md"))
		if err != nil {
			return nil, err
		}
		desc := skillsfm.Parse(data).Description
		m := skipPartRe.FindStringSubmatch(desc)
		if m == nil {
			continue
		}
		for _, part := range skipSplitRe.Split(m[1], -1) {
			p := trimTriggerPart(part)
			dm := mutexDelegateRe.FindStringSubmatch(p)
			if dm == nil {
				continue
			}
			target := dm[1]
			if target == name {
				// 自我让渡是同义反复，不是互斥边。
				continue
			}
			if !known[target] {
				// 悬空目标：丢弃该边（见函数头注释）。
				continue
			}
			edges = append(edges, MutexEdge{From: name, To: target, Fragment: p})
		}
	}

	// 按 (From, To, Fragment) 去重——同一让渡可能在 description 的不同措辞里
	// 重复出现。再按 (From, To) 排序，输出稳定、diff 友好。
	slices.SortFunc(edges, func(a, b MutexEdge) int {
		if c := cmp.Compare(a.From, b.From); c != 0 {
			return c
		}
		if c := cmp.Compare(a.To, b.To); c != 0 {
			return c
		}
		return cmp.Compare(a.Fragment, b.Fragment)
	})
	out := edges[:0]
	for i, e := range edges {
		if i > 0 && e == edges[i-1] {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// MutexCases derives contrast cases from the delegation edges: for each edge A→B, up to mutexPromptsPerEdge of B's own trigger fragments are rendered into prompts.
//
// MutexCases 从让渡边派生对比 case：每条边 A→B 取 B 自己的 trigger 片段（至多
// mutexPromptsPerEdge 个）渲染成 prompt。B 无可提取 trigger 的边跳过——prompt
// 必须来自 B 声明的域，否则「必须路由到 B」没有事实依据。
func MutexCases(canonical string) ([]MutexCase, error) {
	edges, err := MutexEdges(canonical)
	if err != nil {
		return nil, err
	}
	triggersBySkill := make(map[string][]string)
	// seen 按 case ID 去重：A 可能在多个 SKIP 片段里重复声明对 B 的同一让渡（措辞
	// 变体），而 ID 锚定 (A, B, B 的 trigger 片段)——不去重则每条变体边都会重复
	// 产出同一组 case。
	seen := make(map[string]bool)
	var cases []MutexCase
	for _, e := range edges {
		triggers, ok := triggersBySkill[e.To]
		if !ok {
			data, rerr := os.ReadFile(filepath.Join(canonical, e.To, "SKILL.md"))
			if rerr != nil {
				return nil, rerr
			}
			triggers, _ = ExtractTriggers(skillsfm.Parse(data).Description)
			triggersBySkill[e.To] = triggers
		}
		if len(triggers) > mutexPromptsPerEdge {
			triggers = triggers[:mutexPromptsPerEdge]
		}
		for _, raw := range triggers {
			id := mutexCaseID(e.From, e.To, raw)
			if seen[id] {
				continue
			}
			seen[id] = true
			cases = append(cases, MutexCase{
				ID:       id,
				Positive: e.To,
				Negative: e.From,
				Prompt:   renderTriggerPrompt(raw),
				Source:   raw,
			})
		}
	}
	return cases, nil
}

// SaveMutexCases atomically writes the case set to <dir>/mutex/cases.json (MarshalIndent, same style as cases.go).
//
// SaveMutexCases 原子写 case 集到 <dir>/mutex/cases.json（MarshalIndent，风格同
// cases.go）。空集视为无操作——不写空文件。
func SaveMutexCases(dir string, cases []MutexCase) error {
	if len(cases) == 0 {
		return nil
	}
	data, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(mutexCasesFile(dir), data, 0644)
}

// LoadMutexCases reads the case set.
//
// LoadMutexCases 读 case 集。文件不存在返回 nil,nil（从未生成过互斥 case）。
func LoadMutexCases(dir string) ([]MutexCase, error) {
	data, err := os.ReadFile(mutexCasesFile(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cases []MutexCase
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, err
	}
	return cases, nil
}

// JudgeMutexCase is the single-source mutex pass judgment: pass iff actual == Positive (case-insensitive, whitespace-trimmed — same tolerance as judgeResult).
//
// JudgeMutexCase 是互斥 pass 判定的单一真相源：actual == Positive 才 pass
// （大小写不敏感、trim 空白——与 judgeResult 同容忍度）。
func JudgeMutexCase(c MutexCase, actual string) bool {
	return strings.EqualFold(strings.TrimSpace(actual), c.Positive)
}

// RecordMutexRun processes one batch refill of mutex results: normalizes each actual (NormalizeTriggered against the canonical set), judges (JudgeMutexCase), and appends a MutexRun.
//
// RecordMutexRun 处理一次互斥结果整批回填：归一化每个 actual（对 canonical 集走
// NormalizeTriggered）、判定（JudgeMutexCase）、append 一条 MutexRun。未知 case_id
// 跳过（dispatch 后 case 集已变）；全部未知则显式报错——对齐 SubmitRun 语义，让
// agent 重新拉 case 集，而非落一条空 run。
func RecordMutexRun(dir string, cases []MutexCase, canonicalSkills []string, agentModel, forgeVersion string, raw []SubmitResult) (*MutexRun, error) {
	caseByID := make(map[string]MutexCase, len(cases))
	for _, c := range cases {
		caseByID[c.ID] = c
	}
	results := make([]MutexResult, 0, len(raw))
	for _, r := range raw {
		c, ok := caseByID[r.CaseID]
		if !ok {
			// 未知 case_id：case 集已变（重新 mutex-gen），跳过该条。
			continue
		}
		actual := NormalizeTriggered(r.ActualTriggered, canonicalSkills)
		results = append(results, MutexResult{
			CaseID: c.ID,
			Actual: actual,
			Pass:   JudgeMutexCase(c, actual),
		})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("all case_ids unknown — mutex case set regenerated since dispatch, re-fetch via 'forge skills mutex-gen'")
	}
	run := &MutexRun{
		RunID:        newRunID(),
		Timestamp:    time.Now(),
		ForgeVersion: forgeVersion,
		AgentModel:   agentModel,
		Results:      results,
	}
	if err := AppendMutexRun(dir, run); err != nil {
		return nil, fmt.Errorf("append mutex run: %w", err)
	}
	return run, nil
}

// AppendMutexRun appends a run to <dir>/mutex/runs.jsonl (thread-safe, fsync — replicates runs.go AppendRun: an append-only regression log must survive crashes and stay readable).
//
// AppendMutexRun 追加一条 run 到 <dir>/mutex/runs.jsonl（线程安全、fsync——复刻
// runs.go AppendRun：append-only 回归日志必须崩溃后可读）。
func AppendMutexRun(dir string, run *MutexRun) error {
	mutexRunMu.Lock()
	defer mutexRunMu.Unlock()
	path := mutexRunsFile(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// LoadMutexRuns reads all mutex runs (in write order).
//
// LoadMutexRuns 读所有互斥 run（按写入顺序）。文件不存在返回 nil,nil。坏行跳过；
// scanner buffer 增大（一条 run 行携带全部 case 结果，可能很长）——均复刻
// runs.go LoadRuns。
func LoadMutexRuns(dir string) ([]MutexRun, error) {
	f, err := os.Open(mutexRunsFile(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var runs []MutexRun
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var r MutexRun
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			continue // 坏行跳过
		}
		runs = append(runs, r)
	}
	return runs, scanner.Err()
}

// LatestMutexRun returns the most recent mutex run (last line of the jsonl).
//
// LatestMutexRun 返回最新一条互斥 run（jsonl 末行）。无 run 返回 nil,nil。
func LatestMutexRun(dir string) (*MutexRun, error) {
	runs, err := LoadMutexRuns(dir)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	return &runs[len(runs)-1], nil
}

// ConfusionMatrix builds the report over a run against the case set: per-case judgments, (positive, actual) aggregation, and the confusion list (actual == Negative rows).
//
// ConfusionMatrix 基于 run 与 case 集构建报告：逐 case 判定、(positive, actual)
// 聚合、混淆清单（actual == Negative 的行）。引用了不在 case 集内 ID 的结果
// （run 过期 vs case 已重新生成）跳过——矩阵必须谈论当前 case 集，而非历史。
//
// latest == nil 产出空矩阵（Total=0、GateBlocked=false）：「没检查任何东西」由
// Total==0 表达，绝不是一个 vacuous pass。
func ConfusionMatrix(latest *MutexRun, cases []MutexCase) *MutexMatrix {
	m := &MutexMatrix{}
	if latest == nil {
		return m
	}
	caseByID := make(map[string]MutexCase, len(cases))
	for _, c := range cases {
		caseByID[c.ID] = c
	}
	cellIdx := make(map[[2]string]int)
	for _, r := range latest.Results {
		c, ok := caseByID[r.CaseID]
		if !ok {
			continue
		}
		m.Total++
		if r.Pass {
			m.Passed++
		}
		m.Results = append(m.Results, MutexCaseJudged{
			CaseID:   r.CaseID,
			Positive: c.Positive,
			Negative: c.Negative,
			Prompt:   c.Prompt,
			Actual:   r.Actual,
			Pass:     r.Pass,
		})
		key := [2]string{c.Positive, r.Actual}
		if i, ok := cellIdx[key]; ok {
			m.Cells[i].Count++
		} else {
			cellIdx[key] = len(m.Cells)
			m.Cells = append(m.Cells, MutexMatrixCell{Positive: c.Positive, Actual: r.Actual, Count: 1})
		}
		if strings.EqualFold(r.Actual, c.Negative) {
			m.Confusions = append(m.Confusions, MutexConfusion{
				CaseID:   r.CaseID,
				Positive: c.Positive,
				Negative: c.Negative,
				Prompt:   c.Prompt,
				Actual:   r.Actual,
			})
		}
	}
	slices.SortFunc(m.Cells, func(a, b MutexMatrixCell) int {
		if c := cmp.Compare(a.Positive, b.Positive); c != 0 {
			return c
		}
		return cmp.Compare(a.Actual, b.Actual)
	})
	m.GateBlocked = len(m.Confusions) > 0
	return m
}
