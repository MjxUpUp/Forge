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
//
// mkRead builds one Read tool call (on some skill's SKILL.md). The production half of this
// shape contract is TestHookToolTrackRecordsReadFilePath (tool-track writing {"file_path":...})
// — review HIGH-1 (2026-08-16): the two halves once diverged silently (production wrote
// nothing, tests hand-marshaled), killing the join on real data while unit tests stayed green.
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

// TestBuildTriggerFunnel_ReadEngagement 钉死 join 核心正向路径：命中后同 session 读该 skill
// 的 SKILL.md（cache 路径或源路径）→ Engaged。这是 0/134 盲区的解——加载信号本来就在
// toollog 里，只差这个 join。
//
// TestBuildTriggerFunnel_ReadEngagement pins the join's positive path: after a hit, a
// same-session Read of that skill's SKILL.md (cache or source path) → Engaged. This is the
// answer to the 0/134 blind spot — the load signal was in toollog all along; only the join
// was missing.
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

// TestBuildTriggerFunnel_SkillCallEngagement Skill(<name>) 显式调用也算遵循；仅大小写
// 差异的调用名同样命中（审查 LOW-3：与 Read 分支的大小写归一对齐）。
//
// TestBuildTriggerFunnel_SkillCallEngagement: an explicit Skill(<name>) call also counts;
// a call name differing only in case matches too (review LOW-3: aligned with the Read
// branch's case normalization).
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

// TestBuildTriggerFunnel_NoFalseEngagement 反向用例：异 session / 异 skill / 窗口外 /
// 命中前的 Read 都不算遵循——归因宁缺勿滥，否则漏斗变自欺。
//
// TestBuildTriggerFunnel_NoFalseEngagement negative cases: different session / different
// skill / outside the window / Read BEFORE the hit — none count. Attribution must err on
// the side of missing, or the funnel becomes self-deception.
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

// TestBuildTriggerFunnel_PromptDedupe 同 prompt 双机制命中（60s 内同 session 同 skill 两
// 条）折成 1 hit；跨 session / 超 60s 不折。2026-08-16 现场：skill-trigger advisory 与
// 强制路由同时命中同一 skill——两次命中一次遵循，不去重分母虚增。
//
// TestBuildTriggerFunnel_PromptDedupe: same-prompt double-fire (two entries, same session
// and skill, within 60s) collapses to 1 hit; different sessions / beyond 60s do not.
// 2026-08-16 live case: skill-trigger advisory and the forced router hit the same skill at
// once — two hits, one engagement; without dedupe the denominator inflates.
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

// TestBuildTriggerFunnel_DeliveryBuckets 送达三态分桶：true / false / nil（旧条目）。
// Delivered 只计 true；false 不进任何桶（保守）；nil 进 DeliveryUnknown——诚实单列，
// 不假装已送达。
//
// TestBuildTriggerFunnel_DeliveryBuckets three delivery buckets: true / false / nil
// (legacy). Delivered counts only true; false lands in no bucket (conservative); nil goes
// to DeliveryUnknown — listed honestly, never assumed delivered.
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

// TestBuildTriggerFunnel_EmptySessionNotEngaged 空 session（旧条目）无法归因——计 hit 但
// 永不算 Engaged。
//
// TestBuildTriggerFunnel_EmptySessionNotEngaged: empty session (legacy) cannot be
// attributed — counts as a hit but never Engaged.
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
