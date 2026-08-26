// feed.go — multi-source event merger for the pulse panel: TaskState (task-start + gate),
// checklog (skill-trigger), and act conclusions merge into one time-descending event stream.
// Read-only: every source is loaded through the existing store read paths, nothing is written.
//
// feed.go —— pulse 面板的多源事件归并器：TaskState（task-start + gate）、checklog
// （skill-trigger）、act 结论归并成一条时间降序事件流。只读：所有源都走现有 store 的
// 读路径加载，不做任何写操作。
package dashboard

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// Feed event kinds / severities — the wire contract the frontend consumes.
//
// Feed 事件 kind / severity——前端消费的线上契约。
const (
	FeedKindTaskStart    = "task-start"
	FeedKindGate         = "gate"
	FeedKindSkillTrigger = "skill-trigger"
	FeedKindConclusion   = "conclusion"

	FeedSeverityOK   = "ok"
	FeedSeverityWarn = "warn"
	FeedSeverityFail = "fail"
	FeedSeverityInfo = "info"
)

// defaultFeedLimit caps the feed response so polling never ships a huge body.
//
// defaultFeedLimit 截断 feed 响应，轮询永不发出大包。
const defaultFeedLimit = 200

// FeedEvent is one merged stream event. Field names are the frontend contract.
// SessionID is deliberately absent — defense-in-depth: localhost + Host check, but never
// serialize session identifiers.
//
// FeedEvent 是归并流的一条事件。字段名即前端契约。刻意无 SessionID——纵深防御：
// localhost + Host 校验，但绝不序列化 session 标识。
type FeedEvent struct {
	Time     time.Time `json:"time"`
	Kind     string    `json:"kind"`    // "task-start" | "gate" | "skill-trigger" | "conclusion"
	Project  string    `json:"project"` // 项目名（projectName 末两段）
	TaskRef  string    `json:"taskRef"`
	Severity string    `json:"severity"` // "ok" | "warn" | "fail" | "info"
	Title    string    `json:"title"`
	Detail   string    `json:"detail,omitempty"`
	Gate     string    `json:"gate,omitempty"`   // gate 事件: implement/verify/complete
	Passed   *bool     `json:"passed,omitempty"` // gate 事件
	Commit   string    `json:"commit,omitempty"` // gate 事件 HeadCommit 短哈希
	Grade    string    `json:"grade,omitempty"`  // conclusion 事件（分数内联在 Title）
	// Node is the originating machine's node_id (multi-machine Phase 3): conclusion
	// and skill-trigger events carry the record's nodestamp; task-start carries the
	// current lease holder (who's working it). Empty on legacy unstamped records —
	// omitempty keeps the pre-multi-machine wire shape unchanged.
	//
	// Node 是来源机器的 node_id（多机器 Phase 3）：conclusion 与 skill-trigger 事件
	// 携带记录的 nodestamp；task-start 携带当前租约持有者（谁在干活）。存量无戳
	// 记录为空——omitempty 保持多机器前的线上结构不变。
	Node string `json:"node,omitempty"`
	// Skill is the structured skill name on skill-trigger events. The frontend's
	// fold-card aggregation reads this field — it must not regex-parse the name
	// back out of the display Title (title wording is free to change; this is the
	// contract). Empty when the checklog detail carries no parseable name.
	//
	// Skill 是 skill-trigger 事件上的结构化 skill 名。前端折叠卡聚合读此字段——
	// 不得从展示文案 Title 正则反解（标题措辞可改，此字段才是契约）。checklog
	// detail 无可解析名时为空。
	Skill string `json:"skill,omitempty"`
}

// FeedQuery filters AggregateFeed. Since is exclusive (Time > since) for polling
// increments; Project matches the forge key OR the display name; TaskRef scopes to one
// task; Limit 0 means defaultFeedLimit.
//
// FeedQuery 是 AggregateFeed 的过滤条件。Since 为排他（Time > since）供轮询增量；
// Project 同时匹配 forge key 与显示名；TaskRef 限定单任务；Limit 0 = 默认 200。
type FeedQuery struct {
	Since   time.Time
	Project string
	TaskRef string
	Limit   int
}

// pulseRoot is one project in scope: its root plus both identities (forge key for
// filtering, display name for attribution).
//
// pulseRoot 是范围内的一个项目：root + 两重身份（forge key 供过滤，显示名供归属）。
type pulseRoot struct {
	root string
	key  string // forge 项目 key（推导失败为 ""）
	name string // projectName(root)
}

// resolvePulseRoots expands Options into the project scope: Roots (global) when non-empty,
// otherwise the single Root (test/library fallback). Empty roots are dropped. The
// registry→Roots resolution and the empty-registry fallback live in the cli layer
// (cli/dashboard.go) — feed just consumes Options.
//
// resolvePulseRoots 把 Options 展开成项目范围：Roots 非空走全局，否则单 Root（测试/
// 库调用兜底）。空 root 丢弃。registry→Roots 的解析与空 registry 退化在 cli 层
// （cli/dashboard.go）——feed 只消费 Options。
func resolvePulseRoots(opts Options) []pulseRoot {
	roots := opts.Roots
	if len(roots) == 0 && opts.Root != "" {
		roots = []string{opts.Root}
	}
	out := make([]pulseRoot, 0, len(roots))
	for _, r := range roots {
		if r == "" {
			continue
		}
		pr := pulseRoot{root: r, name: projectName(r)}
		if proj, err := forgedata.ProjectFor(r); err == nil {
			pr.key = proj.Key
		}
		out = append(out, pr)
	}
	return out
}

// matches reports whether the query project filter selects this root (key or name).
//
// matches 报告 query 的 project 过滤是否命中本 root（key 或名）。
func (pr pulseRoot) matches(filter string) bool {
	return filter == "" || filter == pr.key || filter == pr.name
}

// FeedResult is the outcome of AggregateFeed: the merged, filtered, capped events plus
// whether the cap cut anything off. Truncated lets the client distinguish "no more
// events" from "older events were dropped" — a truncated incremental (since) poll means
// events were lost permanently unless the client refetches in full.
//
// FeedResult 是 AggregateFeed 的结果：归并、过滤、截断后的事件流 + 是否发生了截断。
// Truncated 让客户端区分「没有更多事件」与「更早事件被丢弃」——增量（since）轮询
// 若被截断意味着事件已永久丢失，客户端须全量重拉。
type FeedResult struct {
	Events    []FeedEvent
	Truncated bool
}

// AggregateFeed merges all event sources across the projects in scope into one
// time-descending stream, then applies the query filters (project / taskRef / since /
// limit). Source data comes from sharedPulseCache (fingerprint-gated — unchanged files
// are not re-parsed); projections still run fresh each call because zombie/severity are
// time-dependent. Per-source read failures (checklog / act) are skipped non-fatally —
// one broken source must not blank the whole panel; ListTaskStates errors propagate
// (→ HTTP 500). Empty data returns an empty non-nil slice so JSON serializes [] rather
// than null.
//
// AggregateFeed 把范围内各项目的全部事件源归并成一条时间降序流，再应用查询过滤
// （project / taskRef / since / limit）。源数据来自 sharedPulseCache（指纹门控——
// 文件未变不重解析）；投影仍每次现算，因僵尸/severity 是时间相关的。单源读失败
// （checklog / act）跳过不致命——一个坏源不应让整面板空白；ListTaskStates 错误上抛
// （→ HTTP 500）。空数据返回非 nil 空切片，JSON 序列化为 [] 而非 null。
func AggregateFeed(opts Options, now time.Time, q FeedQuery) (FeedResult, error) {
	events := []FeedEvent{}
	for _, pr := range resolvePulseRoots(opts) {
		if !pr.matches(q.Project) {
			continue
		}
		d, err := sharedPulseCache.projectData(pr)
		if err != nil {
			return FeedResult{}, err
		}
		events = append(events, feedForProject(pr, d, now)...)
	}
	if q.TaskRef != "" {
		events = slices.DeleteFunc(events, func(e FeedEvent) bool { return e.TaskRef != q.TaskRef })
	}
	if !q.Since.IsZero() {
		events = slices.DeleteFunc(events, func(e FeedEvent) bool { return !e.Time.After(q.Since) })
	}
	// Most recent first; stable so same-time events keep source order (task-start before
	// its gates before the conclusion).
	//
	// 最近在前；稳定排序使同刻事件保持来源序（task-start 先于其 gate 先于结论）。
	slices.SortStableFunc(events, func(a, b FeedEvent) int {
		return b.Time.Compare(a.Time)
	})
	limit := q.Limit
	if limit <= 0 {
		limit = defaultFeedLimit
	}
	truncated := len(events) > limit
	if truncated {
		events = events[:limit]
	}
	return FeedResult{Events: events, Truncated: truncated}, nil
}

// feedForProject projects the cached sources of one project into events.
//
// feedForProject 把单项目的缓存源投影成事件。
func feedForProject(pr pulseRoot, d *projectData, now time.Time) []FeedEvent {
	var events []FeedEvent

	for _, s := range d.states {
		events = append(events, taskStartEvent(pr, s, now))
		events = append(events, gateEvents(pr, s)...)
	}

	for _, e := range d.checkEntries {
		if e.Check != checklog.CheckSkillTrigger {
			continue
		}
		name := checklog.SkillFromTriggerDetail(e.Detail)
		title := "skill 触发"
		if name != "" {
			title = "skill 触发: " + name
		}
		events = append(events, FeedEvent{
			Time: e.RecordedAt, Kind: FeedKindSkillTrigger, Project: pr.name,
			TaskRef: e.TaskRef, Severity: FeedSeverityInfo,
			Title: title, Detail: e.Detail,
			Node:  e.NodeID, // 事件打戳（nodestamp）的机器归因
			Skill: name,     // 结构化 skill 名：前端折叠卡聚合约契，反解 title 文案会随措辞静默失效
		})
	}

	for _, c := range d.conclusions {
		events = append(events, conclusionEvent(pr, c))
	}
	return events
}

// taskStartEvent projects TaskState.StartedAt into a task-start event: in-progress tasks
// are info (title carries origin tool + gate progress), zombies escalate to warn with the
// stall duration in the title, completed tasks are ok.
//
// taskStartEvent 把 TaskState.StartedAt 投影成 task-start 事件：进行中为 info（标题带
// origin tool + gate 进度），僵尸升级为 warn 且标题标注停滞时长，已完成为 ok。
func taskStartEvent(pr pulseRoot, s *taskpipeline.TaskState, now time.Time) FeedEvent {
	ev := FeedEvent{
		Time: s.StartedAt, Kind: FeedKindTaskStart, Project: pr.name, TaskRef: s.TaskRef,
	}
	if s.Lease.ActiveAt(now) {
		ev.Node = s.Lease.HolderNode // 当前有效租约的持有者（谁在干活；过期即不显示，与 LeaseStatus 同一条「过期即自由」规则）
	}
	if s.IsComplete() {
		ev.Severity = FeedSeverityOK
		ev.Title = s.TaskRef + " 已完成"
		return ev
	}
	ev.Severity = FeedSeverityInfo
	var title strings.Builder
	title.WriteString(s.TaskRef + " 进行中")
	if !s.IsGeneric() {
		fmt.Fprintf(&title, " · gate %d/%d", len(s.CompletedGates()), len(taskpipeline.DefaultGates()))
	}
	if s.OriginTool != "" {
		title.WriteString(" · via " + s.OriginTool)
	}
	if zombie, _ := taskpipeline.IsZombie(pr.root, s, now); zombie {
		ev.Severity = FeedSeverityWarn
		fmt.Fprintf(&title, " · 僵尸 %s", formatStallAge(stallAge(pr.root, s, now)))
	}
	ev.Title = title.String()
	return ev
}

// stallAge returns the longest measured stall among the zombie checks (0 when the signal
// is repeat-abandon, which carries no timestamp).
//
// stallAge 返回各僵尸检查中量到的最长停滞时长（反复回收类信号无时间戳时为 0）。
func stallAge(root string, s *taskpipeline.TaskState, now time.Time) time.Duration {
	var age time.Duration
	if ok, a := taskpipeline.IsOfferedZombie(s, now); ok && a > age {
		age = a
	}
	if ok, a := taskpipeline.IsClaimedStale(root, s, now); ok && a > age {
		age = a
	}
	if ok, a := taskpipeline.IsInputReqStale(root, s, now); ok && a > age {
		age = a
	}
	if age == 0 {
		age = now.Sub(s.StartedAt) // 无时间戳信号（abandoned_count≥2）退化用存活时长
	}
	return age
}

// formatStallAge renders a stall duration compactly (8d / 3h / 45m).
//
// formatStallAge 把停滞时长紧凑渲染（8d / 3h / 45m）。
func formatStallAge(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}

// gateEvents projects each History entry into a gate event: pass→ok / fail→fail, the gate
// id stripped of the task- prefix (implement/verify/complete), HeadCommit shortened, and a
// retry note in Detail when the same gate was attempted before.
//
// gateEvents 把每条 History 投影成 gate 事件：过→ok / 败→fail，gate id 剥掉 task-
// 前缀（implement/verify/complete），HeadCommit 截短，同 gate 有前次尝试时 Detail 带
// retry 信息。
func gateEvents(pr pulseRoot, s *taskpipeline.TaskState) []FeedEvent {
	events := make([]FeedEvent, 0, len(s.History))
	attempts := map[string]int{}
	for _, h := range s.History {
		attempts[h.Gate]++
		passed := h.Passed
		ev := FeedEvent{
			Time: h.CompletedAt, Kind: FeedKindGate, Project: pr.name, TaskRef: s.TaskRef,
			Gate:   strings.TrimPrefix(h.Gate, "task-"),
			Passed: &passed,
			Commit: shortCommit(h.HeadCommit),
			Title:  fmt.Sprintf("%s · %s %s", s.TaskRef, strings.TrimPrefix(h.Gate, "task-"), gateVerdict(h.Passed)),
		}
		if h.Passed {
			ev.Severity = FeedSeverityOK
		} else {
			ev.Severity = FeedSeverityFail
		}
		if n := attempts[h.Gate]; n > 1 {
			ev.Detail = fmt.Sprintf("第 %d 次尝试（重试）", n)
		}
		events = append(events, ev)
	}
	return events
}

func gateVerdict(passed bool) string {
	if passed {
		return "通过"
	}
	return "失败"
}

// shortCommit trims a full hash to the conventional 7-char short form.
//
// shortCommit 把完整哈希截成惯例的 7 位短形式。
func shortCommit(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// conclusionEvent projects an act conclusion: severity maps from grade (A/B→ok, C→info,
// D→warn, F→fail), Detail carries evidence strength + det/claim counts + acceptance x/y.
//
// conclusionEvent 投影 act 结论：severity 按 grade 映射（A/B→ok、C→info、D→warn、
// F→fail），Detail 带证据强度 + det/claim 数 + 验收 x/y。
func conclusionEvent(pr pulseRoot, c act.Conclusion) FeedEvent {
	score := int(c.Score + 0.5) // 四舍五入到 int，内联进标题（前端不另读分数字段）
	return FeedEvent{
		Time: c.CompletedAt, Kind: FeedKindConclusion, Project: pr.name, TaskRef: c.TaskRef,
		Severity: gradeSeverity(c.Grade),
		Title:    fmt.Sprintf("%s 完成 · %s %d 分", c.TaskRef, c.Grade, score),
		Detail: fmt.Sprintf("证据 %s · det=%d claim=%d · 验收 %d/%d",
			c.Strength, c.Deterministic, c.AgentClaim, c.AcceptancePass, c.AcceptanceTotal),
		Grade: c.Grade,
		Node:  c.NodeID, // 结论落章机器
	}
}

// gradeSeverity maps a letter grade to a feed severity; unknown/empty grades stay info.
//
// gradeSeverity 把字母 grade 映射成 feed severity；未知/空 grade 保持 info。
func gradeSeverity(grade string) string {
	switch grade {
	case "A", "B":
		return FeedSeverityOK
	case "C":
		return FeedSeverityInfo
	case "D":
		return FeedSeverityWarn
	case "F":
		return FeedSeverityFail
	default:
		return FeedSeverityInfo
	}
}
