// Package harnessaudit computes the pre/post metrics of the A–G harness fixes from a project's own ledgers (checklog × toollog × task states × hazard events) — the reproducible replacement for the one-off audit scripts behind docs/design/harness-fixes-a-g-2026-09.md (design M).
//
// Package harnessaudit 从项目自身账本（checklog × toollog × 任务状态 × hazard 事件）算出
// A–G 修复的事前/事后指标——设计 M：把两机审计的一次性脚本变成可复算、两机同口径的命令。
// 全部判定为纯函数（Build 与各 *Metrics 不碰磁盘），`forge eval harness-audit` 只是加载与
// 渲染层；口径参数（去重窗/join 窗/跟随窗）随报告输出，回测只对同口径 JSON 作差。
package harnessaudit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/gatecmdform"
	"github.com/MjxUpUp/Forge/internal/hazard"
	"github.com/MjxUpUp/Forge/internal/skillmetrics"
	"github.com/MjxUpUp/Forge/internal/skillsfm"
	"github.com/MjxUpUp/Forge/internal/skilltrigger"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// Caliber pins the measurement windows so two machines' reports are only compared when the caliber matches (design M 风险：口径漂移会重造两机分歧).
//
// Caliber 钉住度量窗口——两机报告只在口径一致时可比（设计 M 风险：口径漂移会重造两机
// 分歧）。随 Report 序列化。
type Caliber struct {
	DedupWindow       time.Duration `json:"dedup_window"`        // toollog Pre/Post 双记去重（同 session+tool+input）
	JoinWindow        time.Duration `json:"join_window"`         // 触发→加载 / next-hint→采纳
	FollowWindow      time.Duration `json:"follow_window"`       // inline 指令→跟随动作
	DrillWindow       time.Duration `json:"drill_window"`        // skill 加载→references 下钻
	PairWindow        time.Duration `json:"pair_window"`         // PostToolUse 触发 ↔ 其工具调用的配对
	HazardDedupWindow time.Duration `json:"hazard_dedup_window"` // hazard 双投递（= hazard.EventDedupWindow，判定复用 hazard.IsDoubleDelivery）
}

// DefaultCaliber is the caliber both audits used (toollog 2s dedupe, 10-minute join, 30-minute follow, 20-minute drill, 3s pairing / hazard dedupe).
//
// DefaultCaliber 是两机审计共用的口径。
func DefaultCaliber() Caliber {
	return Caliber{
		DedupWindow:       2 * time.Second,
		JoinWindow:        skillmetrics.TriggerEngageWindow,
		FollowWindow:      30 * time.Minute,
		DrillWindow:       20 * time.Minute,
		PairWindow:        3 * time.Second,
		HazardDedupWindow: hazard.EventDedupWindow,
	}
}

// Ratio is a numerator/denominator pair with its rate (0 when the denominator is 0).
//
// Ratio 是分子/分母及其比率（分母 0 时比率 0）。
type Ratio struct {
	Num  int     `json:"num"`
	Den  int     `json:"den"`
	Rate float64 `json:"rate"`
}

func ratio(num, den int) Ratio {
	r := Ratio{Num: num, Den: den}
	if den > 0 {
		r.Rate = float64(num) / float64(den)
	}
	return r
}

// Input is everything Build needs; loaders live in the CLI so the core stays disk-free.
//
// Input 是 Build 的全部输入；加载器放 CLI，核心不碰磁盘。
type Input struct {
	Entries      []checklog.Entry
	Calls        []toolusage.ToolCall // 原始 toollog；Build 按 Caliber.DedupWindow 去重
	Tasks        []*tasktypes.TaskState
	Hazards      []hazard.Event
	RefsCritical map[string][]string // skill → 声明的 refs_critical 相对路径（设计 D）
	Version      string
	Now          time.Time
	Caliber      *Caliber // nil = DefaultCaliber
	// LoaderWarnings lists data sources the caller failed to load (degraded input); serialized so a baseline JSON is self-describing — an all-zero F/D/E section from a failed load must never pass as a real zero.
	//
	// LoaderWarnings 是调用方加载失败的数据源清单（降级输入）；随报告序列化，让基线 JSON
	// 自证——加载失败导致的 F/D/E 全零绝不能被当成真零（E1/F2 的目标恰好是 0）。
	LoaderWarnings []string
}

// Report is the JSON/table output of one audit run.
//
// Report 是一次审计的 JSON/表格输出。
type Report struct {
	GeneratedAt    time.Time         `json:"generated_at"`
	ForgeVersion   string            `json:"forge_version"`
	Caliber        Caliber           `json:"caliber"`
	Window         Window            `json:"window"`
	LoaderWarnings []string          `json:"loader_warnings,omitempty"` // 非空 = 降级输入，不得作基线
	A              SkillTriggerStats `json:"a_skill_trigger"`
	B              NextHintStats     `json:"b_next_hint"`
	C              GateCmdFormStats  `json:"c_gate_cmd_form"`
	D              RefsCriticalStats `json:"d_refs_critical"`
	E              AttributionStats  `json:"e_attribution"`
	F              HazardStats       `json:"f_hazard"`
	G              CoverageStats     `json:"g_coverage"`
}

// Window is the observed data span.
//
// Window 是观测到的数据跨度；Days 下限 1（跨度不足一天时日均量不放大）。
type Window struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	Days float64   `json:"days"`
}

// ChannelStat is hits→loaded for one hook event channel.
//
// ChannelStat 是单个 hook 事件通道的 命中→加载。
type ChannelStat struct {
	Hits   int     `json:"hits"`
	Loaded int     `json:"loaded"`
	Rate   float64 `json:"rate"`
}

// SkillTriggerStats covers A1–A4.
//
// SkillTriggerStats 覆盖 A1–A4：日均触发、按通道转化、verification-driver 精度、inline 跟随。
type SkillTriggerStats struct {
	Total         int                    `json:"total"`
	DailyTriggers float64                `json:"daily_triggers"` // A1
	ByEvent       map[string]ChannelStat `json:"by_event"`       // A2 按通道
	Conversion    Ratio                  `json:"conversion"`     // A2 总转化
	// VerificationDriverTestCmdPrecision is A3 under the caliber this ledger can support: the PostToolUse hit's paired Bash IS a test command. The design's "failing test command" half is unmeasurable from toollog (no exit codes) — the field name discloses the narrowing; the ≥80% target rebinds to this caliber (design M/A3 note).
	//
	// VerificationDriverTestCmdPrecision 是账本能支撑的 A3 口径：PostToolUse 命中配对到的 Bash
	// 是测试命令。设计原文的「失败的测试命令」一半在 toollog 里不可测（无退出码）——字段名
	// 直接披露收窄，≥80% 目标按本口径重绑（设计 M/A3 注记）。
	VerificationDriverTestCmdPrecision Ratio  `json:"verification_driver_testcmd_precision"`
	InlineFollow                       *Ratio `json:"inline_follow,omitempty"` // A4；无 inline 行 = nil（n/a）
}

// NextHintStats covers B1.
//
// NextHintStats 覆盖 B1：next-hint 行 → JoinWindow 内同命令执行。
type NextHintStats struct {
	Hints    int   `json:"hints"`
	Adoption Ratio `json:"adoption"`
}

// GateCmdFormStats covers C1–C3 (and B2/B3 shares) over Bash calls that embed a gate command.
//
// GateCmdFormStats 覆盖 C1–C3（及 B2/B3 占比）：只统计嵌有门禁子命令的 Bash 调用。
type GateCmdFormStats struct {
	Total         int   `json:"total"`
	Standalone    int   `json:"standalone"`
	AndChainOnly  int   `json:"and_chain_only"`
	MultiGate     int   `json:"multi_gate"`     // B2
	Semicolon     int   `json:"semicolon"`      // B3
	PipeTruncated int   `json:"pipe_truncated"` // C2
	GrepMasked    int   `json:"grep_masked"`
	C1            Ratio `json:"c1_semicolon_or_grep"` // 分号或 grep 掩蔽
	C2            Ratio `json:"c2_pipe_truncated"`
	C3            Ratio `json:"c3_compliant"`
}

// RefsCriticalStats covers D1 per host (origin tool) — Declared=0 means no skill has declared refs_critical yet (n/a).
//
// RefsCriticalStats 覆盖 D1（按 origin tool 分层）；Declared=0 表示尚无 skill 声明
// refs_critical（n/a）。
type RefsCriticalStats struct {
	Declared int              `json:"declared_skills"`
	Overall  Ratio            `json:"overall"`
	PerHost  map[string]Ratio `json:"per_host,omitempty"`
}

// AttributionStats covers E1–E2.
//
// AttributionStats 覆盖 E1–E2。E1 泄漏 = 落在任务证据封印之后、且不属于任务创建会话的
// 归因行（无 session 或其他会话）；创建会话自己封印后的行是 complete 仪式 / owner 续作
// （doc-gate 卡两天时 owner 修文档、重跑 verify），单列 PostSealOwnerRows 不算泄漏。按
// 会话归属而非时间宽限区分（评审：时间宽限既藏快泄漏、又拦不住慢仪式）。
type AttributionStats struct {
	PostSealRows          int            `json:"post_seal_rows"`               // E1 泄漏行 = NoSession + OtherSession（封印后 + 非创建会话）
	PostSealNoSessionRows int            `json:"post_seal_no_session_rows"`    // 泄漏中无 session 的行——E2 回填修的就是这条通道
	PostSealOtherRows     int            `json:"post_seal_other_session_rows"` // 泄漏中确属他会话的行——外来归因通道，与无 session 噪声分列（评审：97% 泄漏是无 session 行，不拆则外来通道被淹没）
	TasksWithPostSealRows int            `json:"tasks_with_post_seal_rows"`    // E1 泄漏任务数
	PostSealOwnerRows     int            `json:"post_seal_owner_rows"`         // 封印后创建会话自己的行（仪式/续作，不算泄漏）
	ProbeFlaggedRows      int            `json:"probe_flagged_rows"`           // Meta[post_seal]=true 原始计数（探针覆盖度，含仪式行）
	NoSessionShare        Ratio          `json:"no_session_share"`
	ResolvePathCounts     map[string]int `json:"resolve_path_counts"`
}

// HazardStats covers F: raw blocks, host double-deliveries, logical incidents, and how many incidents were later released/confirmed.
//
// HazardStats 覆盖 F：原始 block、宿主双投递、逻辑事件数、事后被 confirm/release 的比例。
type HazardStats struct {
	Blocks            int   `json:"blocks"`
	DoubleDeliveries  int   `json:"double_deliveries"`
	Incidents         int   `json:"incidents"`
	ReleasedIncidents Ratio `json:"released_incidents"`
}

// CoverageStats covers G: soft-gate compliance.
//
// CoverageStats 覆盖 G：软门禁听从率——coverage fail 后同任务是否出现 pass；cheat/unused
// 扫描 fail 计数。
type CoverageStats struct {
	CoverageFails        int   `json:"coverage_fails"`
	CoverageFixAfterFail Ratio `json:"coverage_fix_after_fail"`
	CheatScanFails       int   `json:"cheat_scan_fails"`
	UnusedScanFails      int   `json:"unused_scan_fails"`
}

// Build computes the whole report from in-memory ledgers.
//
// Build 从内存账本算出完整报告。
func Build(in Input) *Report {
	cal := DefaultCaliber()
	if in.Caliber != nil {
		cal = *in.Caliber
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	calls := DedupeCalls(in.Calls, cal.DedupWindow)
	win := observedWindow(in.Entries, calls)
	hostOf := hostResolver(in.Tasks)
	return &Report{
		GeneratedAt:    now,
		ForgeVersion:   in.Version,
		Caliber:        cal,
		Window:         win,
		LoaderWarnings: in.LoaderWarnings,
		A:              SkillTriggerMetrics(in.Entries, calls, win.Days, cal),
		B:              NextHintMetrics(in.Entries, calls, cal),
		C:              GateCmdFormMetrics(calls),
		D:              RefsCriticalMetrics(calls, in.RefsCritical, hostOf, cal),
		E:              AttributionMetrics(in.Entries, in.Tasks),
		F:              HazardMetrics(in.Hazards, cal),
		G:              CoverageMetrics(in.Entries),
	}
}

// DedupeCalls collapses host Pre/Post double records: same session+tool+input within window keeps the first.
//
// DedupeCalls 折叠宿主 Pre/Post 双记：同 session+tool+input 在 window 内只留首条（两机审计
// 口径：7566→4154 / 14830→11292）。输入按时间排序后处理，不改调用方切片。
func DedupeCalls(calls []toolusage.ToolCall, window time.Duration) []toolusage.ToolCall {
	sorted := make([]toolusage.ToolCall, len(calls))
	copy(sorted, calls)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Timestamp.Before(sorted[j].Timestamp) })
	type key struct{ s, t, in string }
	last := map[key]time.Time{}
	out := make([]toolusage.ToolCall, 0, len(sorted))
	for _, c := range sorted {
		k := key{c.SessionID, c.ToolName, c.ToolInput}
		if prev, ok := last[k]; ok && c.Timestamp.Sub(prev) <= window {
			continue
		}
		last[k] = c.Timestamp
		out = append(out, c)
	}
	return out
}

func observedWindow(entries []checklog.Entry, calls []toolusage.ToolCall) Window {
	var w Window
	note := func(ts time.Time) {
		if ts.IsZero() {
			return
		}
		if w.From.IsZero() || ts.Before(w.From) {
			w.From = ts
		}
		if ts.After(w.To) {
			w.To = ts
		}
	}
	for _, e := range entries {
		note(e.RecordedAt)
	}
	for _, c := range calls {
		note(c.Timestamp)
	}
	w.Days = w.To.Sub(w.From).Hours() / 24
	if w.Days < 1 {
		w.Days = 1
	}
	return w
}

// commandOf 取 Bash 调用的命令文本（tool_input 是 {"command":...} JSON；截断/非 JSON 时原样）。
func commandOf(c toolusage.ToolCall) string {
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(c.ToolInput), &in); err == nil && in.Command != "" {
		return in.Command
	}
	return c.ToolInput
}

// SkillTriggerMetrics computes A1–A4 from skill-trigger rows joined against tool calls.
//
// SkillTriggerMetrics 从 skill-trigger 行 × 工具调用算 A1–A4。加载判定复用 funnel 的
// EngagedAfter（同 session、JoinWindow 内 Read SKILL.md / Skill 调用）；A3 精度 = PostToolUse
// 触发的 verification-driver 在 PairWindow 内配对到的 Bash 是否真为测试命令（触发自己的
// IsTestCommand 定义）；A4 只对声明了 follow 匹配器的 inline 行计算。
func SkillTriggerMetrics(entries []checklog.Entry, calls []toolusage.ToolCall, days float64, cal Caliber) SkillTriggerStats {
	st := SkillTriggerStats{ByEvent: map[string]ChannelStat{}}
	bash := bashBySession(calls)
	var loaded, vdDen, vdNum, inlDen, inlNum int
	followRe := map[string]*regexp.Regexp{}
	for _, e := range entries {
		if e.Check != checklog.CheckSkillTrigger {
			continue
		}
		skill := checklog.SkillFromTriggerDetail(e.Detail)
		event := checklog.EventFromTriggerDetail(e.Detail)
		if skill == "" || event == "" {
			continue
		}
		st.Total++
		cs := st.ByEvent[event]
		cs.Hits++
		if skillmetrics.EngagedAfter(calls, e.SessionID, skill, e.RecordedAt) {
			cs.Loaded++
			loaded++
		}
		st.ByEvent[event] = cs
		if skill == "verification-driver" && event == "PostToolUse" {
			vdDen++
			if paired, ok := nearestBash(bash[e.SessionID], e.RecordedAt, cal.PairWindow); ok && skilltrigger.IsTestCommand(commandOf(paired)) {
				vdNum++
			}
		}
		if e.Meta[checklog.MetaKeyTriggerMode] == checklog.TriggerModeInline {
			pat := e.Meta[checklog.MetaKeyFollowPattern]
			if pat == "" {
				continue
			}
			re, ok := followRe[pat]
			if !ok {
				compiled, err := regexp.Compile(pat)
				if err != nil {
					continue // 声明的匹配器非法：该行不进分母（写方校验兜底）
				}
				re, followRe[pat] = compiled, compiled
			}
			inlDen++
			for _, c := range bash[e.SessionID] {
				d := c.Timestamp.Sub(e.RecordedAt)
				if d >= 0 && d <= cal.FollowWindow && re.MatchString(commandOf(c)) {
					inlNum++
					break
				}
			}
		}
	}
	for ev, cs := range st.ByEvent {
		if cs.Hits > 0 {
			cs.Rate = float64(cs.Loaded) / float64(cs.Hits)
		}
		st.ByEvent[ev] = cs
	}
	if days > 0 {
		st.DailyTriggers = float64(st.Total) / days
	}
	st.Conversion = ratio(loaded, st.Total)
	st.VerificationDriverTestCmdPrecision = ratio(vdNum, vdDen)
	if inlDen > 0 {
		r := ratio(inlNum, inlDen)
		st.InlineFollow = &r
	}
	return st
}

func bashBySession(calls []toolusage.ToolCall) map[string][]toolusage.ToolCall {
	m := map[string][]toolusage.ToolCall{}
	for _, c := range calls {
		if c.ToolName == "Bash" {
			m[c.SessionID] = append(m[c.SessionID], c)
		}
	}
	return m
}

// nearestBash 返回 at 前后 window 内时间最近的 Bash 调用。
func nearestBash(calls []toolusage.ToolCall, at time.Time, window time.Duration) (toolusage.ToolCall, bool) {
	var best toolusage.ToolCall
	bestD := window + 1
	found := false
	for _, c := range calls {
		d := c.Timestamp.Sub(at)
		if d < 0 {
			d = -d
		}
		if d <= window && d < bestD {
			best, bestD, found = c, d, true
		}
	}
	return best, found
}

// NextHintMetrics computes B1: a next-hint row is adopted when the suggested command runs in the same session within JoinWindow.
//
// NextHintMetrics 算 B1：next-hint 行的建议命令在同会话 JoinWindow 内被执行即采纳（命令
// 按空白归一后子串匹配——agent 常加 cd 前缀或 2>&1）。
func NextHintMetrics(entries []checklog.Entry, calls []toolusage.ToolCall, cal Caliber) NextHintStats {
	var st NextHintStats
	bash := bashBySession(calls)
	adopted := 0
	for _, e := range entries {
		if e.Check != checklog.CheckNextHint {
			continue
		}
		st.Hints++
		want := normalizeCmd(e.Meta[checklog.MetaKeySuggested])
		if want == "" {
			continue
		}
		for _, c := range bash[e.SessionID] {
			d := c.Timestamp.Sub(e.RecordedAt)
			if d >= 0 && d <= cal.JoinWindow && strings.Contains(normalizeCmd(commandOf(c)), want) {
				adopted++
				break
			}
		}
	}
	st.Adoption = ratio(adopted, st.Hints)
	return st
}

func normalizeCmd(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// GateCmdFormMetrics computes C1–C3 (and the B2/B3 shares) over Bash calls that embed a gate command.
//
// GateCmdFormMetrics 对嵌有门禁子命令的 Bash 调用算 C1–C3（及 B2/B3 占比）。
func GateCmdFormMetrics(calls []toolusage.ToolCall) GateCmdFormStats {
	var st GateCmdFormStats
	c1, c3 := 0, 0
	for _, c := range calls {
		if c.ToolName != "Bash" {
			continue
		}
		f := gatecmdform.Classify(commandOf(c))
		if f.Gates == 0 {
			continue
		}
		st.Total++
		if f.Standalone {
			st.Standalone++
		}
		if f.Compliant && !f.Standalone {
			st.AndChainOnly++ // 合规的 && 链——与 Standalone 加和恰为 C3 分子，三数自洽
		}
		if f.MultiGate {
			st.MultiGate++
		}
		if f.Semicolon {
			st.Semicolon++
		}
		if f.PipeTruncated {
			st.PipeTruncated++
		}
		if f.GrepMasked {
			st.GrepMasked++
		}
		if f.Semicolon || f.GrepMasked {
			c1++
		}
		if f.Compliant {
			c3++
		}
	}
	st.C1 = ratio(c1, st.Total)
	st.C2 = ratio(st.PipeTruncated, st.Total)
	st.C3 = ratio(c3, st.Total)
	return st
}

// RefsCriticalMetrics computes D1: after a load of a skill that declares refs_critical, was any declared reference read within DrillWindow in the same session — stratified by the host that owns the call's task.
//
// RefsCriticalMetrics 算 D1：声明了 refs_critical 的 skill 被加载后，同会话 DrillWindow 内是否
// Read 了任一声明的 reference——按调用所属任务的 host（origin tool）分层。未声明任何
// skill 时 Declared=0（n/a）。
func RefsCriticalMetrics(calls []toolusage.ToolCall, refs map[string][]string, hostOf func(taskRef string) string, cal Caliber) RefsCriticalStats {
	st := RefsCriticalStats{Declared: len(refs)}
	if len(refs) == 0 {
		return st
	}
	type load struct {
		at      time.Time
		session string
		skill   string
		host    string
	}
	var loads []load
	readsBySession := map[string][]toolusage.ToolCall{}
	for _, c := range calls {
		switch c.ToolName {
		case "Read":
			readsBySession[c.SessionID] = append(readsBySession[c.SessionID], c)
			p := strings.ToLower(strings.ReplaceAll(readPath(c.ToolInput), "\\", "/"))
			for skill := range refs {
				if strings.HasSuffix(p, "/"+strings.ToLower(skill)+"/skill.md") {
					loads = append(loads, load{c.Timestamp, c.SessionID, skill, hostOf(c.TaskRef)})
				}
			}
		case "Skill":
			name := skillmetrics.ExtractSkillName(c.ToolInput)
			if _, ok := refs[name]; ok {
				loads = append(loads, load{c.Timestamp, c.SessionID, name, hostOf(c.TaskRef)})
			}
		}
	}
	perHost := map[string]*[2]int{}
	num := 0
	for _, l := range loads {
		drilled := false
		for _, r := range readsBySession[l.session] {
			d := r.Timestamp.Sub(l.at)
			if d <= 0 || d > cal.DrillWindow {
				continue
			}
			p := strings.ToLower(strings.ReplaceAll(readPath(r.ToolInput), "\\", "/"))
			for _, ref := range refs[l.skill] {
				if strings.HasSuffix(p, "/"+strings.ToLower(strings.TrimPrefix(ref, "./"))) {
					drilled = true
					break
				}
			}
			if drilled {
				break
			}
		}
		if perHost[l.host] == nil {
			perHost[l.host] = &[2]int{}
		}
		perHost[l.host][1]++
		if drilled {
			perHost[l.host][0]++
			num++
		}
	}
	st.Overall = ratio(num, len(loads))
	if len(perHost) > 0 {
		st.PerHost = map[string]Ratio{}
		for h, v := range perHost {
			st.PerHost[h] = ratio(v[0], v[1])
		}
	}
	return st
}

func readPath(toolInput string) string {
	var in struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(toolInput), &in); err == nil {
		return in.FilePath
	}
	return ""
}

func hostResolver(tasks []*tasktypes.TaskState) func(string) string {
	m := map[string]string{}
	for _, t := range tasks {
		if t != nil && t.OriginTool != "" {
			m[t.TaskRef] = t.OriginTool
		}
	}
	return func(ref string) string {
		if h, ok := m[ref]; ok {
			return h
		}
		return "unknown"
	}
}

// AttributionMetrics computes E1–E2: rows landing after a task's evidence seal are split by session ownership — the task's creating session (ceremony / owner follow-up) vs any other or no session (leak) — plus the raw probe count, the no-session share, and the resolve_path distribution.
//
// AttributionMetrics 算 E1–E2：封印后落到任务名下的行按会话归属拆分——创建会话（仪式 /
// owner 续作）vs 其他会话或无会话（泄漏）；另算探针原始计数、无 session 占比与
// resolve_path 分布。封印时刻与所属会话来自 TaskState（SealedAt / SessionID）。
func AttributionMetrics(entries []checklog.Entry, tasks []*tasktypes.TaskState) AttributionStats {
	st := AttributionStats{ResolvePathCounts: map[string]int{}}
	type sealInfo struct {
		at    time.Time
		owner string
	}
	seals := map[string]sealInfo{}
	for _, t := range tasks {
		if t == nil {
			continue
		}
		if s := t.SealedAt(); !s.IsZero() {
			seals[t.TaskRef] = sealInfo{at: s, owner: t.SessionID}
		}
	}
	leakTasks := map[string]bool{}
	noSession := 0
	for _, e := range entries {
		if e.SessionID == "" {
			noSession++
		}
		if p := e.Meta[checklog.MetaKeyResolvePath]; p != "" {
			st.ResolvePathCounts[p]++
		}
		if e.Meta[checklog.MetaKeyPostSeal] == "true" {
			st.ProbeFlaggedRows++
		}
		if e.TaskRef == "" {
			continue
		}
		si, ok := seals[e.TaskRef]
		if !ok || !e.RecordedAt.After(si.at) {
			continue
		}
		if si.owner != "" && e.SessionID == si.owner {
			st.PostSealOwnerRows++
			continue
		}
		st.PostSealRows++
		if e.SessionID == "" {
			st.PostSealNoSessionRows++
		} else {
			st.PostSealOtherRows++
		}
		leakTasks[e.TaskRef] = true
	}
	st.TasksWithPostSealRows = len(leakTasks)
	st.NoSessionShare = ratio(noSession, len(entries))
	return st
}

// HazardMetrics computes F from the hazard event stream with the writer's own dedupe rule (hazard.IsDoubleDelivery against the previous event of ANY type) so pre-fix history is rebuilt exactly as the fixed writer would have recorded it.
//
// HazardMetrics 从 hazard 事件流算 F。双投递判定直接复用写侧 hazard.IsDoubleDelivery，且与
// 写侧一样只比对「上一条任意类型事件」（写侧只看文件末行）——修复前的历史按修复后写方
// 会落盘的形态重建，F2 前后对比同口径。事件 = block − 双投递；已放行 = 其指纹随后出现
// confirm/release/data。
func HazardMetrics(events []hazard.Event, cal Caliber) HazardStats {
	var st HazardStats
	sorted := make([]hazard.Event, len(events))
	copy(sorted, events)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Ts.Before(sorted[j].Ts) })
	released := map[string]bool{}
	for _, e := range sorted {
		if e.Type == hazard.EventConfirm || e.Type == hazard.EventRelease || e.Type == hazard.EventData {
			released[e.Fingerprint] = true
		}
	}
	var prev *hazard.Event
	incidentsReleased := 0
	for i := range sorted {
		e := &sorted[i]
		if e.Type == hazard.EventBlock {
			st.Blocks++
			if prev != nil && e.Fingerprint != "" && hazard.IsDoubleDelivery(*prev, *e) {
				st.DoubleDeliveries++
			} else {
				st.Incidents++
				if released[e.Fingerprint] {
					incidentsReleased++
				}
			}
		}
		prev = e
	}
	_ = cal // 窗口由 hazard.IsDoubleDelivery 内的 EventDedupWindow 决定；Caliber 字段仅作报告披露
	st.ReleasedIncidents = ratio(incidentsReleased, st.Incidents)
	return st
}

// CoverageMetrics computes G: per task, does a test-coverage-gate fail get followed by a pass; plus cheat/unused scan fail counts.
//
// CoverageMetrics 算 G：按任务，test-coverage-gate 的 fail 是否随后出现 pass（软门禁听从率）；
// 以及 cheat-scan / unused-scan 的 fail 计数。
func CoverageMetrics(entries []checklog.Entry) CoverageStats {
	var st CoverageStats
	const coverage = taskpipeline.CheckNameTestCoverage
	byTask := map[string][]checklog.Entry{}
	for _, e := range entries {
		failed := e.EffectiveLevel().IsFailure()
		switch e.Check {
		case coverage:
			byTask[e.TaskRef] = append(byTask[e.TaskRef], e)
		case checklog.CheckCheatScan:
			if failed {
				st.CheatScanFails++
			}
		case checklog.CheckUnusedScan:
			if failed {
				st.UnusedScanFails++
			}
		}
	}
	fixed := 0
	for _, rows := range byTask {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].RecordedAt.Before(rows[j].RecordedAt) })
		for i, r := range rows {
			if !r.EffectiveLevel().IsFailure() {
				continue
			}
			st.CoverageFails++
			for _, later := range rows[i+1:] {
				if later.EffectiveLevel() == checklog.LevelPass {
					fixed++
					break
				}
			}
		}
	}
	st.CoverageFixAfterFail = ratio(fixed, st.CoverageFails)
	return st
}

// LoadRefsCritical scans canonicalDir/*/SKILL.md for `metadata.refs_critical` (JSON array of relative reference paths, design D) and returns skill → paths.
//
// LoadRefsCritical 扫描 canonicalDir/*/SKILL.md 的 `metadata.refs_critical`（JSON 数组，
// reference 相对路径，设计 D），返回 skill → 路径。目录缺失或无声明返回空 map。
func LoadRefsCritical(canonicalDir string) map[string][]string {
	out := map[string][]string{}
	matches, err := filepath.Glob(filepath.Join(canonicalDir, "*", "SKILL.md"))
	if err != nil {
		return out
	}
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		fm := skillsfm.Parse(data)
		if fm == nil {
			continue
		}
		raw := fm.Metadata["refs_critical"]
		if raw == "" {
			continue
		}
		var paths []string
		if err := json.Unmarshal([]byte(raw), &paths); err != nil || len(paths) == 0 {
			continue
		}
		out[filepath.Base(filepath.Dir(m))] = paths
	}
	return out
}

// Render formats the report as the human table (JSON is the machine surface).
//
// Render 渲染人读表格（机器面走 JSON）。
func Render(r *Report) string {
	var b strings.Builder
	pct := func(x Ratio) string { return fmt.Sprintf("%d/%d = %.1f%%", x.Num, x.Den, x.Rate*100) }
	fmt.Fprintf(&b, "Harness 审计（forge %s，%s ~ %s，%.1f 天；口径 dedup=%s join=%s follow=%s drill=%s）\n",
		r.ForgeVersion, r.Window.From.Format("2006-01-02"), r.Window.To.Format("2006-01-02"), r.Window.Days,
		r.Caliber.DedupWindow, r.Caliber.JoinWindow, r.Caliber.FollowWindow, r.Caliber.DrillWindow)
	if len(r.LoaderWarnings) > 0 {
		fmt.Fprintf(&b, "⚠ 降级输入（不得作基线）：%s\n", strings.Join(r.LoaderWarnings, "；"))
	}
	fmt.Fprintf(&b, "A skill-trigger: 触发 %d（A1 日均 %.1f）  A2 总转化 %s  A3 verification-driver 配对命令为测试命令 %s（口径：不含成败，toollog 无退出码）\n",
		r.A.Total, r.A.DailyTriggers, pct(r.A.Conversion), pct(r.A.VerificationDriverTestCmdPrecision))
	events := make([]string, 0, len(r.A.ByEvent))
	for ev := range r.A.ByEvent {
		events = append(events, ev)
	}
	sort.Strings(events)
	for _, ev := range events {
		cs := r.A.ByEvent[ev]
		fmt.Fprintf(&b, "   %-18s %d/%d = %.1f%%\n", ev, cs.Loaded, cs.Hits, cs.Rate*100)
	}
	if r.A.InlineFollow != nil {
		fmt.Fprintf(&b, "   A4 inline 跟随 %s\n", pct(*r.A.InlineFollow))
	} else {
		b.WriteString("   A4 inline 跟随 n/a（无 inline 行）\n")
	}
	fmt.Fprintf(&b, "B next-hint: %d 条，B1 采纳 %s\n", r.B.Hints, pct(r.B.Adoption))
	fmt.Fprintf(&b, "C 门禁命令形态: %d 条  standalone %d  仅&& %d  B2 多门禁 %d  B3 分号 %d  管道截断 %d  grep %d\n   C1 %s  C2 %s  C3 %s\n",
		r.C.Total, r.C.Standalone, r.C.AndChainOnly, r.C.MultiGate, r.C.Semicolon, r.C.PipeTruncated, r.C.GrepMasked, pct(r.C.C1), pct(r.C.C2), pct(r.C.C3))
	if r.D.Declared == 0 {
		b.WriteString("D refs-critical: n/a（无 skill 声明 refs_critical）\n")
	} else {
		fmt.Fprintf(&b, "D refs-critical: %d skill 声明，D1 下钻 %s\n", r.D.Declared, pct(r.D.Overall))
		hosts := make([]string, 0, len(r.D.PerHost))
		for h := range r.D.PerHost {
			hosts = append(hosts, h)
		}
		sort.Strings(hosts)
		for _, h := range hosts {
			fmt.Fprintf(&b, "   %-14s %s\n", h, pct(r.D.PerHost[h]))
		}
	}
	fmt.Fprintf(&b, "E 归因: 封印后泄漏行 %d（%d 任务；无 session %d / 他会话 %d）  owner 续作行 %d  探针标记 %d  无 session 占比 %s  resolve_path %v\n",
		r.E.PostSealRows, r.E.TasksWithPostSealRows, r.E.PostSealNoSessionRows, r.E.PostSealOtherRows, r.E.PostSealOwnerRows, r.E.ProbeFlaggedRows, pct(r.E.NoSessionShare), r.E.ResolvePathCounts)
	fmt.Fprintf(&b, "F hazard: block %d  双投递 %d  事件 %d  放行 %s\n", r.F.Blocks, r.F.DoubleDeliveries, r.F.Incidents, pct(r.F.ReleasedIncidents))
	fmt.Fprintf(&b, "G 软门禁: coverage fail %d → 转 pass %s  cheat-scan fail %d  unused-scan fail %d\n",
		r.G.CoverageFails, pct(r.G.CoverageFixAfterFail), r.G.CheatScanFails, r.G.UnusedScanFails)
	return b.String()
}
