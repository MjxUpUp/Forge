package skillseval

// runs.go is the other half of the skill eval closed-loop data layer: run records,
// normalization, judgment, regression comparison, health score, baseline.
//
// runs.go 是 skill eval 闭环数据层的另一半：run 记录、归一化、判定、回归比对、
// 健康度分、baseline。
//
// Closed-loop data flow: the agent fetches the case set via MCP → dispatches a fresh
// subagent per prompt → batch-refills which skill actually fired to forge (SubmitRun)
// → forge normalizes + judges + computes health + appends an EvalRun → eval-report
// compares latest run vs baseline for regression.
//
// 闭环数据流：agent 通过 MCP 拿 case 集 → dispatch fresh subagent 跑每个 prompt
// → 把「实际触发了哪个 skill」整批回填给 forge（SubmitRun）→ forge 归一化 + 判定
// + 算健康度 + append 一条 EvalRun → eval-report 取 latest run vs baseline 做回归。
//
// forge itself cannot spawn AI (pi lacks the claude -p auto mode), so the actual
// triggering is run by the agent while forge only normalizes / judges / compares —
// semi-automated positioning.
//
// forge 自身 spawn 不了 AI（pi 无 claude -p 自动模式），所以「实际触发」由 agent
// 跑、forge 只做归一化/判定/比对——半自动定位。

import (
	"bufio"
	"cmp"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/MjxUpUp/Forge/internal/util"
)

var runMu sync.Mutex

// CaseResult is the actual run result for a single case (judged by forge after the agent refills it).
//
// CaseResult 是单 case 的实际跑结果（agent 回填后由 forge 判定）。
type CaseResult struct {
	CaseID          string `json:"case_id"`
	Kind            string `json:"kind"`
	Prompt          string `json:"prompt"`
	Expected        string `json:"expected"`                // trigger/not-trigger/behavior
	// ActualTriggered: normalized name of the skill that actually fired ("" = did not fire).
	ActualTriggered string `json:"actual_triggered"`        // 归一化后的实际触发 skill 名（""=没触发）
	// ActualOutput (behavior kind): the actual output refilled by the agent, including the original text needed by the oracle for judgment; redacted by eval-cases/report before exposure.
	ActualOutput    string `json:"actual_output,omitempty"` // behavior 类：agent 回填的实际输出（含 oracle 判定所需原文，eval-cases/report 对外脱敏）
	Pass            bool   `json:"pass"`
	Note            string `json:"note,omitempty"` // agent 标注的异常/理由
}

// EvalRun is one complete run (a batch refill), appended to dir/runs/<skill>.jsonl.
//
// EvalRun 是一次完整 run（整批回填），append 到 dir/runs/<skill>.jsonl。
type EvalRun struct {
	RunID         string       `json:"run_id"`
	Skill         string       `json:"skill"`
	Timestamp     time.Time    `json:"timestamp"`
	// ForgeVersion: guards against cross-version false regressions.
	ForgeVersion  string       `json:"forge_version"`             // 防跨版本假回归
	// AgentModel: self-reported by the agent, guards against cross-model false regressions.
	AgentModel    string       `json:"agent_model"`               // agent 自报，防跨模型假回归
	// DescHash: fingerprint of the description at run time.
	DescHash      string       `json:"desc_hash"`                 // run 时刻 description 指纹
	// CaseSetHash: sha1 of all CaseIDs after sorting, a quick consistency check for the set.
	CaseSetHash   string       `json:"case_set_hash"`             // 所有 CaseID 排序后 sha1，集一致性快检
	// BaselineRunID: the baseline locked at run time.
	BaselineRunID string       `json:"baseline_run_id,omitempty"` // run 时刻锁定的 baseline
	Results       []CaseResult `json:"results"`
	HealthScore   float64      `json:"health_score"`
}

// SubmitResult is the raw result refilled by the agent (not yet normalized or judged).
//
// SubmitResult 是 agent 回填的原始结果（未归一化、未判定）。
type SubmitResult struct {
	CaseID          string `json:"case_id"`
	ActualTriggered string `json:"actual_triggered"`
	ActualOutput    string `json:"actual_output,omitempty"` // behavior 类：dispatch subagent 跑 ProbeInput 的输出
	Note            string `json:"note,omitempty"`
}

// Baseline records the baseline run anchored for a given skill. A baseline is a verified
// release-worthy point, set explicitly.
//
// Baseline 记录某 skill 锚定的 baseline run。baseline = 已验证可发布点，显式设定。
type Baseline struct {
	RunID string    `json:"run_id"`
	SetAt time.Time `json:"set_at"`
	SetBy string    `json:"set_by"` // "manual" / "cli" / "mcp"
}

// RegressionReport is the regression comparison result of the latest run vs baseline.
// Three states are distinct: regressions are counted only on the matched set (baseline
// pass → latest fail); new/removed do not count as regressions (case-set turnover caused
// by description rewrites is expected behavior).
//
// RegressionReport 是 latest run vs baseline 的回归比对结果。
// 三态分明：regression 只在 matched 集数（baseline pass → latest fail）；
// new/removed 不计 regression（description 改写导致 case 集换血是预期行为）。
type RegressionReport struct {
	Skill                      string       `json:"skill"`
	BaselineRun                string       `json:"baseline_run,omitempty"`
	LatestRun                  string       `json:"latest_run"`
	HasBaseline                bool         `json:"has_baseline"`
	Comparable                 bool         `json:"comparable"` // ForgeVersion/AgentModel/DescHash 是否一致
	IncomparableReason         string       `json:"incomparable_reason,omitempty"`
	Matched                    int          `json:"matched"`
	NetRegressions             int          `json:"net_regressions"` // regressions - improvements（可负=净改善）
	Regressions                []CaseResult `json:"regressions,omitempty"`
	Improvements               []CaseResult `json:"improvements,omitempty"`
	Stable                     []CaseResult `json:"stable,omitempty"`
	New                        []CaseResult `json:"new,omitempty"`
	Removed                    []CaseResult `json:"removed,omitempty"`
	TriggerPassRateBaseline    float64      `json:"trigger_pass_rate_baseline,omitempty"`
	TriggerPassRateLatest      float64      `json:"trigger_pass_rate_latest"`
	NotTriggerPassRateBaseline float64      `json:"not_trigger_pass_rate_baseline,omitempty"`
	NotTriggerPassRateLatest   float64      `json:"not_trigger_pass_rate_latest"`
	BehaviorPassRateBaseline   float64      `json:"behavior_pass_rate_baseline,omitempty"`
	BehaviorPassRateLatest     float64      `json:"behavior_pass_rate_latest"`
}

// newRunID generates a run ID of the form run-<unix>-<randhex>.
//
// newRunID 生成"run-<unix>-<randhex>"。
func newRunID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("run-%d-%s", time.Now().Unix(), hex.EncodeToString(b[:]))
}

// caseSetHash returns sha1[:12] of all sorted CaseIDs, used as a quick set-consistency check.
//
// caseSetHash 返回所有 CaseID 排序后的 sha1[:12]，用于集一致性快检。
func caseSetHash(results []CaseResult) string {
	ids := make([]string, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.CaseID)
	}
	slices.Sort(ids)
	h := sha1.Sum([]byte(strings.Join(ids, ",")))
	return hex.EncodeToString(h[:])[:12]
}

// NormalizeTriggered normalizes the agent-refilled actual trigger name: trims leading/trailing
// whitespace and CJK/ASCII punctuation + lowercases + matches canonical exactly. none semantics
// (none/无/-/n/a) normalize to "". Names that do not match canonical keep their lowercased
// original value — normalization does not guess for the agent; during judgment, trigger cases
// fail on !=skill and not-trigger cases pass on !=skill, behavior is deterministic.
//
// Stripping leading/trailing punctuation is necessary: agent refills may carry periods/quotes/
// full-width chars (lark-doc。., "lark-doc"); without stripping they would miss the canonical
// match and yield trigger fail / not-trigger pass (deterministic but wrong). Only leading/trailing
// chars are stripped, leaving hyphenated legal skill names intact.
//
// NormalizeTriggered 把 agent 回填的实际触发名归一化：trim 首尾空格与中英文标点 +
// lowercase + canonical 精确匹配。none 语义（none/无/-/n/a）归一为""。不匹配
// canonical 的保留 lowercased 原值——归一化不替 agent 猜测，判定时 trigger 类会因
// !=skill 而 fail、not-trigger 类会因 !=skill 而 pass，行为确定。
//
// strip 首尾标点是必要的：agent 回填可能带句号/引号/全角符（"lark-doc。"、
// "「lark-doc"」），不 strip 会因不匹配 canonical 而 trigger fail/not-trigger pass
// （确定但错误）。只 strip 首尾，不破坏含连字符的合法 skill 名。
func NormalizeTriggered(actual string, canonicalSkills []string) string {
	// Leading/trailing CJK/ASCII punctuation + whitespace. Trim strips repeatedly by character
	// set (any single char in the set).
	//
	// 首尾中英文标点 + 空白。Trim 按字符集（任一字符）反复剥。
	const punct = ` 	
.,;:!?()[]。；：！？（）【】「」“”‘’`
	s := strings.Trim(actual, punct)
	if s == "" {
		return ""
	}
	low := strings.ToLower(s)
	switch low {
	case "none", "n/a", "na", "无", "（无）", "(none)", "-":
		return ""
	}
	for _, name := range canonicalSkills {
		if strings.ToLower(name) == low {
			return name
		}
	}
	return low
}

// judgeResult judges whether a single result passes based on the case's Kind.
//
//	trigger kind: actual == skill (self-reference)
//	not-trigger kind: actual != skill (including empty or any other skill) — does not couple
//	with"which skill it should route to"
//	behavior kind: judgeBehavior(actualOutput, oracle) — passes only when the output satisfies
//	the oracle (see probes.go)
//
// judgeResult 按 case 的 Kind 判定单结果是否 pass。
//
//	trigger 类：actual == skill（自指）
//	not-trigger 类：actual != skill（含空、含任何其他 skill）——不耦合「该路由到哪个 skill」
//	behavior 类：judgeBehavior(actualOutput, oracle)——输出满足 oracle 才 pass（见 probes.go）
func judgeResult(c EvalCase, actualTriggered, actualOutput string) bool {
	if c.Kind == KindBehavior {
		return judgeBehavior(actualOutput, c.Oracle)
	}
	act := strings.ToLower(strings.TrimSpace(actualTriggered))
	skill := strings.ToLower(c.Skill)
	if c.Kind == KindTrigger {
		return act == skill
	}
	return act != skill
}

// HealthScore is the trigger/not-trigger weighted pass rate minus the regression penalty.
//
// When one kind has no cases, the other kind takes the full weight; regressions come from the
// degradation count vs baseline, pass 0 when there is no baseline.
//
// HealthScore 按 trigger/not-trigger 加权通过率减回归惩罚。
//
//	baseScore = 100 * (0.6*triggerAcc + 0.4*notTriggerAcc)
//	health = clamp(baseScore - 8*regressions, 0, 100)
//
// 某类无 case 时另一类占满权重；regressions 来自 vs baseline 的退化数，无 baseline 传 0。
func HealthScore(results []CaseResult, regressions int) float64 {
	var trigPass, trigTotal, notPass, notTotal int
	for _, r := range results {
		switch r.Kind {
		case KindTrigger:
			trigTotal++
			if r.Pass {
				trigPass++
			}
		case KindNotTrigger:
			notTotal++
			if r.Pass {
				notPass++
			}
		}
	}
	var base float64
	switch {
	case trigTotal == 0 && notTotal == 0:
		base = 0
	case trigTotal == 0:
		base = 100 * float64(notPass) / float64(notTotal)
	case notTotal == 0:
		base = 100 * float64(trigPass) / float64(trigTotal)
	default:
		base = 100 * (0.6*float64(trigPass)/float64(trigTotal) + 0.4*float64(notPass)/float64(notTotal))
	}
	score := base - float64(regressions*8)
	switch {
	case score < 0:
		score = 0
	case score > 100:
		score = 100
	}
	return math.Round(score*100) / 100
}

// passRates returns (triggerPassRate, notTriggerPassRate, behaviorPassRate).
// The behavior kind is tracked independently — it measures behavior quality (output satisfies
// the oracle), a separate dimension from routing health (trigger/not-trigger); therefore
// HealthScore (routing health) does not count behavior, and this function returns it separately
// for the report to display.
//
// passRates 返回 (triggerPassRate, notTriggerPassRate, behaviorPassRate)。
// behavior 类独立计——它测行为质量（输出满足 oracle），与路由健康度（trigger/not-trigger）
// 是两个维度，故 HealthScore（路由健康度）不计 behavior，这里单独返回供 report 展示。
func passRates(results []CaseResult) (trigRate, notRate, behaviorRate float64) {
	var tp, tt, np, nt, bp, bt int
	for _, r := range results {
		switch r.Kind {
		case KindTrigger:
			tt++
			if r.Pass {
				tp++
			}
		case KindNotTrigger:
			nt++
			if r.Pass {
				np++
			}
		case KindBehavior:
			bt++
			if r.Pass {
				bp++
			}
		}
	}
	if tt > 0 {
		trigRate = float64(tp) / float64(tt)
	}
	if nt > 0 {
		notRate = float64(np) / float64(nt)
	}
	if bt > 0 {
		behaviorRate = float64(bp) / float64(bt)
	}
	return
}

// CompareRuns compares latest vs baseline and produces a RegressionReport.
// When baseline==nil, HasBaseline=false and only the latest pass rates are filled in.
// Comparability: Comparable=true only when ForgeVersion/AgentModel/DescHash all match —
// otherwise"regressions"across models/versions/description changes are false regressions,
// and the report marks them incomparable.
//
// CompareRuns 比对 latest vs baseline，产出 RegressionReport。
// baseline==nil 时 HasBaseline=false，只填 latest 的 pass rate。
// 可比性：ForgeVersion/AgentModel/DescHash 三者一致才 Comparable=true——否则
// 跨模型/跨版本/改了 description 的「回归」是假回归，report 标不可比。
func CompareRuns(latest, baseline *EvalRun) *RegressionReport {
	rep := &RegressionReport{
		Skill:       latest.Skill,
		LatestRun:   latest.RunID,
		HasBaseline: baseline != nil,
	}
	rep.TriggerPassRateLatest, rep.NotTriggerPassRateLatest, rep.BehaviorPassRateLatest = passRates(latest.Results)
	if baseline == nil {
		return rep
	}
	rep.BaselineRun = baseline.RunID
	rep.TriggerPassRateBaseline, rep.NotTriggerPassRateBaseline, rep.BehaviorPassRateBaseline = passRates(baseline.Results)

	// Comparability check.
	//
	// 可比性校验。
	rep.Comparable = true
	var reasons []string
	if latest.ForgeVersion != baseline.ForgeVersion {
		rep.Comparable = false
		reasons = append(reasons, fmt.Sprintf("forge_version %s→%s", baseline.ForgeVersion, latest.ForgeVersion))
	}
	if latest.AgentModel != baseline.AgentModel {
		rep.Comparable = false
		reasons = append(reasons, fmt.Sprintf("agent_model %s→%s", baseline.AgentModel, latest.AgentModel))
	}
	if latest.DescHash != baseline.DescHash {
		rep.Comparable = false
		reasons = append(reasons, fmt.Sprintf("desc_hash %s→%s", baseline.DescHash, latest.DescHash))
	}
	rep.IncomparableReason = strings.Join(reasons, "; ")

	baseMap := make(map[string]CaseResult, len(baseline.Results))
	for _, r := range baseline.Results {
		baseMap[r.CaseID] = r
	}
	latestMap := make(map[string]CaseResult, len(latest.Results))
	for _, r := range latest.Results {
		latestMap[r.CaseID] = r
	}

	for id, l := range latestMap {
		b, ok := baseMap[id]
		if !ok {
			rep.New = append(rep.New, l)
			continue
		}
		rep.Matched++
		switch {
		case b.Pass && !l.Pass:
			rep.Regressions = append(rep.Regressions, l)
		case !b.Pass && l.Pass:
			rep.Improvements = append(rep.Improvements, l)
		default:
			rep.Stable = append(rep.Stable, l)
		}
	}
	for id, b := range baseMap {
		if _, ok := latestMap[id]; !ok {
			rep.Removed = append(rep.Removed, b)
		}
	}
	rep.NetRegressions = len(rep.Regressions) - len(rep.Improvements)

	sortResults(rep.Regressions)
	sortResults(rep.Improvements)
	sortResults(rep.Stable)
	sortResults(rep.New)
	sortResults(rep.Removed)
	return rep
}

func sortResults(rs []CaseResult) {
	slices.SortFunc(rs, func(a, b CaseResult) int { return cmp.Compare(a.CaseID, b.CaseID) })
}

// countRegressions returns the degradation count of the matched set of latest vs baseline
// (the regressions count among the three states). It is extracted so that SubmitRun (computing
// the health penalty) uses only the regressions count without touching the rest of the
// RegressionReport fields — the intent is explicit, avoiding the implicit contract
// "CompareRuns only takes len(Regressions)" being misread by maintainers.
//
// countRegressions 返回 latest vs baseline 的 matched 集退化数（三态里的 regressions
// 计数）。抽出来让 SubmitRun（算 health 惩罚）只用 regressions 计数，不触碰整张
// RegressionReport 的其余字段——意图清晰，避免「CompareRuns 只取 len(Regressions)」
// 的隐式契约被维护者误读。
func countRegressions(latest, baseline *EvalRun) int {
	if baseline == nil {
		return 0
	}
	return len(CompareRuns(latest, baseline).Regressions)
}

// SubmitRun processes one batch refill: normalize + DescHash check + judge + compute health + append.
// canonical is used to read the current SKILL.md DescHash and to normalize against the canonical set.
// Returns the newly created and persisted EvalRun.
//
// SubmitRun 处理一次整批回填：归一化 + DescHash 校验 + 判定 + 算 health + append。
// canonical 用于读当前 SKILL.md 的 DescHash 和 canonical 集归一化。
// 返回新建并已落盘的 EvalRun。
func SubmitRun(dir, canonical, skill, agentModel, forgeVersion string, raw []SubmitResult) (*EvalRun, error) {
	set, err := LoadCaseSet(dir, skill)
	if err != nil {
		return nil, fmt.Errorf("load cases: %w", err)
	}
	if set == nil || len(set.Cases) == 0 {
		return nil, fmt.Errorf("no eval cases for skill %q — run 'forge skills eval-gen --skill %s --save' first", skill, skill)
	}

	// DescHash check: the case-set fingerprint must equal the current SKILL.md fingerprint,
	// otherwise the case set is stale.
	// Pure behavior sets (DescHash empty, not anchored to description) skip this check —
	// behavior cases are maintained independently of description, and changing description
	// should not fail a behavior run.
	//
	// DescHash 校验：case 集的指纹必须等于当前 SKILL.md 的指纹，否则 case 集已过期。
	// 纯 behavior 集（DescHash 空，不锚 description）跳过此校验——behavior case
	// 独立于 description 维护，改 description 不应让 behavior run 失败。
	curDH, err := currentDescHash(canonical, skill)
	if err != nil {
		return nil, fmt.Errorf("read current SKILL.md: %w", err)
	}
	if set.DescHash != "" && curDH != set.DescHash {
		return nil, fmt.Errorf("eval cases stale: case desc_hash %s != current %s — description changed, re-run 'forge skills eval-gen --skill %s --save'", set.DescHash, curDH, skill)
	}

	// ListSkills failure is propagated, not swallowed: canonical was just read successfully
	// by currentDescHash, so a read-dir failure here is anomalous — silently degrading
	// canonicalSkills to nil would skew NormalizeTriggered for every case.
	//
	// ListSkills 失败要传播、不吞掉：canonical 刚被 currentDescHash 成功读过，
	// 此处读目录失败属异常——静默把 canonicalSkills 降级为 nil 会让所有 case 的
	// NormalizeTriggered 失真。
	canonicalSkills, err := skillsdist.ListSkills(canonical)
	if err != nil {
		return nil, fmt.Errorf("list canonical skills: %w", err)
	}
	caseByID := make(map[string]EvalCase, len(set.Cases))
	for _, c := range set.Cases {
		caseByID[c.ID] = c
	}

	results := make([]CaseResult, 0, len(raw))
	for _, r := range raw {
		c, ok := caseByID[r.CaseID]
		if !ok {
			// Unknown case_id: the case set has changed (re-run eval-gen); skip this entry.
			//
			// 未知 case_id：case 集已变（重新 eval-gen），跳过该条。
			continue
		}
		actual := NormalizeTriggered(r.ActualTriggered, canonicalSkills)
		results = append(results, CaseResult{
			CaseID:          c.ID,
			Kind:            c.Kind,
			Prompt:          c.Prompt,
			Expected:        c.Kind,
			ActualTriggered: actual,
			ActualOutput:    r.ActualOutput,
			Pass:            judgeResult(c, actual, r.ActualOutput),
			Note:            r.Note,
		})
	}
	// All case_ids unknown (case set just rebuilt, agent holds stale ids) → results empty.
	// Silent success would let the agent misread"ran successfully but all failed" and persist
	// an empty run — fail explicitly so it re-fetches the case set.
	//
	// 所有 case_id 都未知（case 集刚重建，agent 拿的是旧 id）→ results 空。静默成功
	// 会让 agent 误判「跑成功只是全挂」并落一条空 run——明确报错让它重新拿 case 集。
	if len(results) == 0 {
		return nil, fmt.Errorf("all case_ids unknown — case set regenerated since dispatch, re-fetch via forge_skill_eval_cases / eval-gen --save")
	}

	// Baseline (locked at run time).
	//
	// baseline（run 时刻锁定）。
	run := &EvalRun{
		RunID:        newRunID(),
		Skill:        skill,
		Timestamp:    time.Now(),
		ForgeVersion: forgeVersion,
		AgentModel:   agentModel,
		DescHash:     curDH,
		CaseSetHash:  caseSetHash(results),
		Results:      results,
	}
	regressions := 0
	// Distinguish "no baseline" from "baseline unreadable": a corrupt baselines.json must
	// warn on stderr, not silently masquerade as "nothing to compare" (which would make
	// the regression penalty disappear without a trace — the regression gate is only
	// trustworthy if its failures are explicit).
	//
	// 区分「无 baseline」与「baseline 不可读」：baselines.json 损坏必须 stderr 告警，
	// 不能静默伪装成「无可比 baseline」（那会让回归惩罚无痕消失——回归门禁只有
	// 失败显式时才可信）。
	if bl, berr := GetBaseline(dir, skill); berr != nil {
		fmt.Fprintf(os.Stderr, "warn: baseline 不可读（%v），本次跳过回归比对——不等同于无 baseline，请检查 %s\n", berr, baselinesFile(dir))
	} else if bl.RunID != "" {
		runs, rerr := LoadRuns(dir, skill)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "warn: runs 不可读（%v），无法与 baseline %s 比对，本次回归比对跳过\n", rerr, bl.RunID)
		} else {
			for i := range runs {
				if runs[i].RunID == bl.RunID {
					run.BaselineRunID = bl.RunID
					regressions = countRegressions(run, &runs[i])
					break
				}
			}
		}
	}
	run.HealthScore = HealthScore(results, regressions)

	if err := AppendRun(dir, skill, run); err != nil {
		return nil, fmt.Errorf("append run: %w", err)
	}
	return run, nil
}

// AppendRun appends a run to dir/runs/<skill>.jsonl (thread-safe, mirroring the checklog.Record pattern).
//
// AppendRun 追加一条 run 到 dir/runs/<skill>.jsonl（线程安全，复刻 checklog.Record 模式）。
func AppendRun(dir, skill string, run *EvalRun) error {
	runMu.Lock()
	defer runMu.Unlock()
	path := runsFile(dir, skill)
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
	// fsync: runs.jsonl is an append-only regression log; a partially written run that gets
	// truncated would be silently skipped by LoadRuns as a bad line (manifesting as
	// "submitted but report cannot find latest"). Sync to disk guarantees that a committed
	// run survives crashes and stays readable, consistent with the fsync trade-off in util.AtomicWrite.
	//
	// fsync：runs.jsonl 是 append-only 回归日志，单条 run 写到一半被截断会被
	// LoadRuns 的坏行跳过静默丢弃（表现「提交了但 report 找不到 latest」）。Sync
	// 落盘保证已提交 run 在崩溃后可读，与 util.AtomicWrite 的 fsync 取舍一致。
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

// LoadRuns reads all runs (in write order). Returns nil,nil if the file does not exist.
// Uses an enlarged scanner buffer — a run contains prompts/notes for all cases and a single
// line may be very long.
//
// LoadRuns 读所有 run（按写入顺序）。文件不存在返回 nil,nil。
// 用增大的 scanner buffer——run 含全部 case 的 prompt/note，单行可能很长。
func LoadRuns(dir, skill string) ([]EvalRun, error) {
	f, err := os.Open(runsFile(dir, skill))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var runs []EvalRun
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var r EvalRun
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			continue // 坏行跳过
		}
		runs = append(runs, r)
	}
	return runs, scanner.Err()
}

// LatestRun returns the most recent run (last line of the jsonl). Returns nil,nil if there is no run.
//
// LatestRun 返回最新一条 run（jsonl 末行）。无 run 返回 nil,nil。
func LatestRun(dir, skill string) (*EvalRun, error) {
	runs, err := LoadRuns(dir, skill)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	return &runs[len(runs)-1], nil
}

// LoadRunByID searches all runs by RunID. Returns nil,nil if not found.
//
// LoadRunByID 在所有 run 里按 RunID 查找。未找到返回 nil,nil。
func LoadRunByID(dir, skill, runID string) (*EvalRun, error) {
	runs, err := LoadRuns(dir, skill)
	if err != nil {
		return nil, err
	}
	for i := range runs {
		if runs[i].RunID == runID {
			return &runs[i], nil
		}
	}
	return nil, nil
}

// LoadBaselines reads the baseline map. Returns an empty map if the file does not exist.
//
// LoadBaselines 读 baseline 映射。文件不存在返回空 map。
func LoadBaselines(dir string) (map[string]Baseline, error) {
	data, err := os.ReadFile(baselinesFile(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Baseline{}, nil
		}
		return nil, err
	}
	var bs map[string]Baseline
	if err := json.Unmarshal(data, &bs); err != nil {
		return nil, err
	}
	if bs == nil {
		return map[string]Baseline{}, nil
	}
	return bs, nil
}

// GetBaseline returns the baseline for a skill. Returns the zero-value Baseline{} if none.
//
// GetBaseline 返回某 skill 的 baseline。无则零值 Baseline{}。
func GetBaseline(dir, skill string) (Baseline, error) {
	bs, err := LoadBaselines(dir)
	if err != nil {
		return Baseline{}, err
	}
	return bs[skill], nil
}

// SetBaseline marks the baseline run for a skill (atomically writes the whole map).
//
// Note: AtomicWrite only guarantees a single atomic write; it does not cover the
// read-modify-write of"LoadBaselines → mutate map → write" — two concurrent SetBaseline
// calls (different skills) end with the later writer winning and the earlier writer's change
// lost. Baselines are set explicitly by humans (CLI/MCP are not high-frequency concurrent),
// so the convention is to call serially; add baselineMu later if concurrency is needed.
//
// SetBaseline 标记某 skill 的 baseline run（原子写整张映射）。
//
// 注意：AtomicWrite 只保证单次写原子，不覆盖「LoadBaselines→改 map→写」的读改写——
// 两个并发 SetBaseline（不同 skill）后写者赢、前者的改动丢失。baseline 是人工显式
// 设定（CLI/MCP 不会高频并发），约定串行调用即可；若未来需并发再加 baselineMu。
func SetBaseline(dir, skill, runID, setBy string) error {
	bs, err := LoadBaselines(dir)
	if err != nil {
		return err
	}
	bs[skill] = Baseline{RunID: runID, SetAt: time.Now(), SetBy: setBy}
	data, err := json.MarshalIndent(bs, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(baselinesFile(dir), data, 0644)
}
