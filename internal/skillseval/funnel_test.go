package skillseval

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// mkHit 造一条 CheckSkillTrigger 条目。delivered 传 nil 表示旧条目（无送达章）。
// mkHit builds one CheckSkillTrigger entry. delivered=nil means a legacy entry (no stamp).
func mkHit(session, skill string, at time.Time, delivered *bool) checklog.Entry {
	return checklog.Entry{
		Check:      checklog.CheckSkillTrigger,
		Passed:     true,
		Checked:    true,
		SessionID:  session,
		Detail:     checklog.DetailForSkillTrigger(skill, "UserPromptSubmit", "keywords"),
		RecordedAt: at,
		Delivered:  delivered,
	}
}

// mkRead 造一条 Read 工具调用（读某 skill 的 SKILL.md）。形状契约的生产侧一半在
// TestHookToolTrackRecordsReadFilePath（tool-track 写 {"file_path":...}）——2026-08-16
// 审查 HIGH-1：两侧曾静默分叉（生产不写、测试手造），join 在真实数据上死亡而单测全绿。
func mkRead(session, path string, at time.Time) toolusage.ToolCall {
	ti, _ := json.Marshal(map[string]string{"file_path": path})
	return toolusage.ToolCall{ToolName: "Read", ToolInput: string(ti), SessionID: session, Timestamp: at}
}

// mkSkillCall 造一条 Skill 工具调用。
// mkSkillCall builds one Skill tool call.
func mkSkillCall(session, skill string, at time.Time) toolusage.ToolCall {
	ti, _ := json.Marshal(map[string]string{"skill": skill})
	return toolusage.ToolCall{ToolName: "Skill", ToolInput: string(ti), SessionID: session, Timestamp: at}
}

func boolPtr(b bool) *bool { return &b }

// TestBuildTriggerFunnel_ReadEngagement pins the join's positive path: after a hit, a same-session Read of that skill's SKILL.md (cache or source path) → Engaged.
//
// TestBuildTriggerFunnel_ReadEngagement 钉死 join 核心正向路径：命中后同 session 读该 skill
// 的 SKILL.md（cache 路径或源路径）→ Engaged。这是 0/134 盲区的解——加载信号本来就在
// toollog 里，只差这个 join。
func TestBuildTriggerFunnel_ReadEngagement(t *testing.T) {
	base := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	delivered := boolPtr(true)
	entries := []checklog.Entry{
		mkHit("s1", "test-discipline", base, delivered),
		mkHit("s2", "implementation-discipline", base, delivered),
	}
	calls := []toolusage.ToolCall{
		// cache 路径（Windows 反斜杠也要命中——归一化后后缀匹配）。
		// Cache path (Windows backslashes must match too — suffix after normalization).
		mkRead("s1", `C:\Users\x\.forge\skills-cache\embedded\test-discipline\SKILL.md`, base.Add(2*time.Minute)),
		// 源路径（unix 风格）。
		// Source path (unix style).
		mkRead("s2", "/e/Forge/skills/implementation-discipline/SKILL.md", base.Add(3*time.Minute)),
	}
	rep := BuildTriggerFunnel(entries, calls)
	if rep.TotalHits != 2 || rep.TotalEngaged != 2 {
		t.Fatalf("Hits=%d Engaged=%d, want 2/2", rep.TotalHits, rep.TotalEngaged)
	}
	for _, sf := range rep.Skills {
		if sf.Engaged != 1 {
			t.Errorf("%s Engaged=%d, want 1", sf.Name, sf.Engaged)
		}
	}
}

// TestBuildTriggerFunnel_SkillCallEngagement: an explicit Skill(<name>) call also counts; a call name differing only in case matches too (review LOW-3: aligned with the Read branch's case normalization).
//
// TestBuildTriggerFunnel_SkillCallEngagement Skill(<name>) 显式调用也算遵循；仅大小写
// 差异的调用名同样命中（审查 LOW-3：与 Read 分支的大小写归一对齐）。
func TestBuildTriggerFunnel_SkillCallEngagement(t *testing.T) {
	base := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	rep := BuildTriggerFunnel(
		[]checklog.Entry{mkHit("s1", "tdd-cycle", base, boolPtr(true))},
		[]toolusage.ToolCall{mkSkillCall("s1", "TDD-Cycle", base.Add(time.Minute))},
	)
	if rep.TotalEngaged != 1 {
		t.Fatalf("Engaged=%d, want 1（大小写变体也须命中）", rep.TotalEngaged)
	}
}

// TestBuildTriggerFunnel_NoFalseEngagement negative cases: different session / different skill / outside the window / Read BEFORE the hit — none count.
//
// TestBuildTriggerFunnel_NoFalseEngagement 反向用例：异 session / 异 skill / 窗口外 /
// 命中前的 Read 都不算遵循——归因宁缺勿滥，否则漏斗变自欺。
func TestBuildTriggerFunnel_NoFalseEngagement(t *testing.T) {
	base := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	entries := []checklog.Entry{
		mkHit("s1", "test-discipline", base, boolPtr(true)),
		mkHit("s2", "other-skill", base, boolPtr(true)),
	}
	calls := []toolusage.ToolCall{
		// 异 session。
		// Different session.
		mkRead("sX", "/e/Forge/skills/test-discipline/SKILL.md", base.Add(time.Minute)),
		// 异 skill。
		// Different skill.
		mkRead("s1", "/e/Forge/skills/compile-fix-loop/SKILL.md", base.Add(time.Minute)),
		// 窗口外（> 10min）。
		// Outside the window (> 10min).
		mkRead("s1", "/e/Forge/skills/test-discipline/SKILL.md", base.Add(11*time.Minute)),
		// 命中之前读的不算（归因方向必须向后）。
		// A read BEFORE the hit doesn't count (attribution must look forward).
		mkRead("s2", "/e/Forge/skills/other-skill/SKILL.md", base.Add(-time.Minute)),
	}
	rep := BuildTriggerFunnel(entries, calls)
	if rep.TotalEngaged != 0 {
		t.Fatalf("Engaged=%d, want 0（全部反向用例都不应计入）", rep.TotalEngaged)
	}
}

// TestBuildTriggerFunnel_PromptDedupe: same-prompt double-fire (two entries, same session and skill, within 60s) collapses to 1 hit; different sessions / beyond 60s do not.
//
// TestBuildTriggerFunnel_PromptDedupe 同 prompt 双机制命中（60s 内同 session 同 skill 两
// 条）折成 1 hit；跨 session / 超 60s 不折。2026-08-16 现场：skill-trigger advisory 与
// 强制路由同时命中同一 skill——两次命中一次遵循，不去重分母虚增。
func TestBuildTriggerFunnel_PromptDedupe(t *testing.T) {
	base := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	entries := []checklog.Entry{
		mkHit("s1", "td", base, boolPtr(true)),
		mkHit("s1", "td", base.Add(10*time.Second), boolPtr(true)), // 双机制 → 折叠
		mkHit("s1", "td", base.Add(5*time.Minute), boolPtr(true)),  // 超 60s → 新 hit
		mkHit("s2", "td", base.Add(20*time.Second), boolPtr(true)), // 异 session → 新 hit
	}
	rep := BuildTriggerFunnel(entries, nil)
	if rep.TotalHits != 3 {
		t.Fatalf("Hits=%d, want 3（双机制折叠 + 2 新 hit）", rep.TotalHits)
	}
	// 去重团的送达章取或：首条 nil（旧条目）+ 团内第二条 true → 团算已送达、不算未知。
	// Group delivery OR: first entry nil (legacy) + second true in-group → delivered, not unknown.
	entries2 := []checklog.Entry{
		mkHit("s1", "td", base, nil),
		mkHit("s1", "td", base.Add(5*time.Second), boolPtr(true)),
	}
	rep2 := BuildTriggerFunnel(entries2, nil)
	if len(rep2.Skills) != 1 || rep2.Skills[0].Delivered != 1 || rep2.Skills[0].DeliveryUnknown != 0 {
		t.Fatalf("团内或语义失败: %+v", rep2.Skills)
	}
}

// TestBuildTriggerFunnel_DeliveryBuckets three delivery buckets: true / false / nil (legacy).
//
// TestBuildTriggerFunnel_DeliveryBuckets 送达三态分桶：true / false / nil（旧条目）。
// Delivered 只计 true；false 不进任何桶（保守）；nil 进 DeliveryUnknown——诚实单列，
// 不假装已送达。
func TestBuildTriggerFunnel_DeliveryBuckets(t *testing.T) {
	base := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	entries := []checklog.Entry{
		mkHit("s1", "td", base, boolPtr(true)),
		mkHit("s2", "td", base.Add(time.Hour), boolPtr(false)), // kimi Stop 等死通道
		mkHit("s3", "td", base.Add(2*time.Hour), nil),          // 字段引入前旧条目
	}
	rep := BuildTriggerFunnel(entries, nil)
	if len(rep.Skills) != 1 {
		t.Fatalf("应聚成 1 个 skill，got %d", len(rep.Skills))
	}
	sf := rep.Skills[0]
	if sf.Hits != 3 || sf.Delivered != 1 || sf.DeliveryUnknown != 1 {
		t.Fatalf("Hits=%d Delivered=%d Unknown=%d, want 3/1/1", sf.Hits, sf.Delivered, sf.DeliveryUnknown)
	}
}

// TestBuildTriggerFunnel_EmptySessionNotEngaged: empty session (legacy) cannot be attributed — counts as a hit but never Engaged.
//
// TestBuildTriggerFunnel_EmptySessionNotEngaged 空 session（旧条目）无法归因——计 hit 但
// 永不算 Engaged。
func TestBuildTriggerFunnel_EmptySessionNotEngaged(t *testing.T) {
	base := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	rep := BuildTriggerFunnel(
		[]checklog.Entry{mkHit("", "td", base, nil)},
		[]toolusage.ToolCall{mkRead("", "/e/Forge/skills/td/SKILL.md", base.Add(time.Minute))},
	)
	if rep.TotalHits != 1 || rep.TotalEngaged != 0 {
		t.Fatalf("Hits=%d Engaged=%d, want 1/0", rep.TotalHits, rep.TotalEngaged)
	}
}

// TestReadFilePath 读路径提取的解析容错：正常 JSON / 非 JSON / 空。
// TestReadFilePath parse tolerance for the read-path extractor: valid JSON / non-JSON /
// empty.
func TestReadFilePath(t *testing.T) {
	if p := readFilePath(`{"file_path":"/a/b/SKILL.md"}`); p != "/a/b/SKILL.md" {
		t.Errorf("正常解析失败: %q", p)
	}
	if p := readFilePath("not-json"); p != "" {
		t.Errorf("非 JSON 应返空: %q", p)
	}
	if p := readFilePath(""); p != "" {
		t.Errorf("空输入应返空: %q", p)
	}
}

// funnelGoldenFixture 是一次覆盖全部判定分支的构造数据集：多 skill 多 session、
// 60s 去重窗口边界（10s 内折叠 / 61s 不折叠）、送达三态（true/false/nil）、
// engaged 的全部反向用例（异 session / 异 skill / 窗口外 / 命中前 / 空 session /
// 截断 JSON / 无关工具），外加大小写与反斜杠路径归一。供 golden 快照与索引等价
// 两个测试共用。
func funnelGoldenFixture() (entries []checklog.Entry, calls []toolusage.ToolCall) {
	base := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	entries = []checklog.Entry{
		mkHit("s1", "alpha", base, boolPtr(true)),
		mkHit("s1", "alpha", base.Add(10*time.Second), boolPtr(false)), // 同团：送达章取或
		mkHit("s1", "alpha", base.Add(5*time.Minute), nil),             // 超 60s → 新团，unknown
		mkHit("s2", "beta", base, nil),
		mkHit("s2", "beta", base.Add(61*time.Second), boolPtr(true)), // 61s > 60s → 新团
		mkHit("", "gamma", base, nil),                                // 空 session：计 hit 永不 engaged
		mkHit("s3", "alpha", base, boolPtr(true)),
		// 噪声：非 trigger check 与解析不出 skill 名的条目都必须跳过。
		// Noise: a non-trigger check and an unparseable detail must both be skipped.
		{Check: "read-before-edit", SessionID: "s1", RecordedAt: base},
		{Check: checklog.CheckSkillTrigger, SessionID: "s1", RecordedAt: base, Detail: "stop-cap advisory（无标记）"},
	}
	calls = []toolusage.ToolCall{
		mkRead("s1", "/e/Forge/skills/alpha/SKILL.md", base.Add(time.Minute)),
		mkRead("s1", `C:\Users\x\.forge\skills-cache\embedded\beta\SKILL.md`, base.Add(2*time.Minute)), // 异 skill（且属 s1 非 s2）
		mkSkillCall("s1", "ALPHA", base.Add(3*time.Minute)),                                            // 大小写变体
		mkSkillCall("s2", "beta", base.Add(time.Minute)),
		mkRead("s3", "/e/Forge/skills/alpha/SKILL.md", base.Add(11*time.Minute)), // 窗口外
		mkRead("s3", `E:\Forge\skills\ALPHA\skill.md`, base.Add(11*time.Minute)), // 窗口外（归一仍须命中路径形态）
		mkRead("s1", "/e/Forge/skills/alpha/SKILL.md", base.Add(-time.Minute)),   // 命中前
		mkRead("", "/e/Forge/skills/gamma/SKILL.md", base.Add(time.Minute)),      // 空 session 调用
		// 截断 JSON（toollog 输入 500 字符截断的生产形态）：按无信号处理。
		// Truncated JSON (the 500-char production truncation): no signal.
		{ToolName: "Read", ToolInput: `{"file_path":"/e/Forge/skills/al`, SessionID: "s1", Timestamp: base.Add(4 * time.Minute)},
		// 无关工具：永不构成 engaged 信号。
		// Unrelated tool: never an engagement signal.
		{ToolName: "Grep", ToolInput: `{"pattern":"alpha"}`, SessionID: "s1", Timestamp: base.Add(time.Minute)},
	}
	return entries, calls
}

// TestBuildTriggerFunnel_GoldenJSON pins byte-equivalent output: before vs after the engagedAfter hot-path rework (O(n×m) full rescans → session-bucketed, extract-once), this JSON must stay byte-identical.
//
// TestBuildTriggerFunnel_GoldenJSON 是输出字节等价的快照钉：重构 engagedAfter 热路径
// （O(n×m) 全量重复解析 → 按 session 分桶 + 每 call 预提取一次）前后，本 JSON 必须逐字节
// 不变。期望值在旧实现上生成并人工核对。
func TestBuildTriggerFunnel_GoldenJSON(t *testing.T) {
	entries, calls := funnelGoldenFixture()
	got, err := json.Marshal(BuildTriggerFunnel(entries, calls))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"window":600000000000,"skills":[` +
		`{"name":"alpha","hits":3,"delivered":2,"delivery_unknown":1,"engaged":1},` +
		`{"name":"beta","hits":2,"delivered":1,"delivery_unknown":1,"engaged":1},` +
		`{"name":"gamma","hits":1,"delivered":0,"delivery_unknown":1,"engaged":0}` +
		`],"total_hits":6,"total_delivered":3,"total_engaged":2}`
	if string(got) != want {
		t.Fatalf("漏斗输出与 golden 快照不符（重构改变了行为）：\n got: %s\nwant: %s", got, want)
	}
}

// TestEngagedIndexEquivalence brute-force cross-checks the indexed judgment against the per-call one over a full grid: for every (session × skill × time) combination both paths must agree.
//
// TestEngagedIndexEquivalence 把索引判定与逐条判定在全网格上暴力对拍：每个
// (session × skill × 时刻) 组合两条路径必须同真同假。这钉住分桶预提取重构不改变
// 任何单点判定——golden 快照钉聚合输出，本测试钉判定核本身。
func TestEngagedIndexEquivalence(t *testing.T) {
	_, calls := funnelGoldenFixture()
	base := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	idx := buildEngagedIndex(calls)
	sessions := []string{"", "s1", "s2", "s3", "sX"}
	skills := []string{"alpha", "beta", "gamma", "ALPHA", "missing"}
	times := []time.Time{
		base.Add(-time.Minute), base, base.Add(30 * time.Second),
		base.Add(time.Minute), base.Add(3 * time.Minute), base.Add(5 * time.Minute),
		base.Add(61 * time.Second), base.Add(9*time.Minute + 59*time.Second),
		base.Add(10 * time.Minute), base.Add(11 * time.Minute),
	}
	for _, s := range sessions {
		for _, sk := range skills {
			for _, at := range times {
				want := engagedAfter(calls, s, sk, at)
				got := idx.engagedAfter(s, sk, at)
				if got != want {
					t.Fatalf("判定分叉：session=%q skill=%q at=%v 索引=%v 逐条=%v", s, sk, at, got, want)
				}
			}
		}
	}
	// 至少要有一处为真，否则对拍退化为「两边都恒假」的自证。
	// At least one combination must be true, or the cross-check degenerates
	// into "both sides always false" and proves nothing.
	anyTrue := false
	for _, s := range sessions {
		for _, sk := range skills {
			if engagedAfter(calls, s, sk, base) {
				anyTrue = true
			}
		}
	}
	if !anyTrue {
		t.Fatal("夹具没有任何 engaged=true 的组合，等价对拍无意义")
	}
}
