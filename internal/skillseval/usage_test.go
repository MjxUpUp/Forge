package skillseval

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/MjxUpUp/Forge/internal/util"
)

func mustWrite(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// makeCanonicalSkill creates a skill in the canonical directory (directory name = frontmatter name).
//
// makeCanonicalSkill 在 canonical 目录造一个 skill（目录名 = frontmatter name）。
func makeCanonicalSkill(t *testing.T, canonical, name string) {
	t.Helper()
	sd := filepath.Join(canonical, name)
	mustWrite(t, os.MkdirAll(sd, 0755))
	mustWrite(t, os.WriteFile(filepath.Join(sd, "SKILL.md"),
		[]byte(fmt.Sprintf("---\nname: %s\ndescription: d\n---\n\nbody\n", name)), 0644))
}

// recordSkillCall writes a Skill tool call to toollog.jsonl via toolusage.Record.
// Goes through the real collection layer (DataDirFor(root)/toollog.jsonl) for closed-loop verification of
// SkillCountsFromToollog's read path and new data source.
//
// recordSkillCall 经 toolusage.Record 写一条 Skill 工具调用到 toollog.jsonl。
// 走真实采集层（DataDirFor(root)/toollog.jsonl），闭环验证 SkillCountsFromToollog
// 读取的路径与新数据源。
func recordSkillCall(t *testing.T, root, skillName, taskRef string) {
	t.Helper()
	mustWrite(t, toolusage.Record(root, &toolusage.ToolCall{
		ToolName:  "Skill",
		ToolInput: fmt.Sprintf(`{"skill":%q}`, skillName),
		TaskRef:   taskRef,
	}))
}

func recordRead(t *testing.T, root, taskRef string) {
	t.Helper()
	mustWrite(t, toolusage.Record(root, &toolusage.ToolCall{
		ToolName: "Read",
		TaskRef:  taskRef,
	}))
}

func TestExtractSkillName(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"skill":"foo"}`, "foo"},
		{`{"skill":" foo ","args":"x"}`, "foo"}, // TrimSpace
		{`{"args":"x"}`, ""},                    // 无 skill 字段
		{`not json`, ""},                        // 非 JSON
		{"", ""},                                // 空
	}
	for _, c := range cases {
		if got := ExtractSkillName(c.in); got != c.want {
			t.Errorf("ExtractSkillName(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestSkillCountsFromToollog(t *testing.T) {
	root := t.TempDir()
	recordSkillCall(t, root, "alpha", "t1")
	recordSkillCall(t, root, "alpha", "t1")
	recordSkillCall(t, root, "beta", "t2")
	recordRead(t, root, "t1") // 非 Skill，不计

	counts, total, err := SkillCountsFromToollog(root)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total=%d want 3（只计 Skill 调用，Read 排除）", total)
	}
	if counts["alpha"] != 2 || counts["beta"] != 1 {
		t.Fatalf("counts=%v want alpha:2 beta:1", counts)
	}
}

func TestSkillCountsFromToollog_Empty(t *testing.T) {
	root := t.TempDir() // 无 toollog 文件
	counts, total, err := SkillCountsFromToollog(root)
	if err != nil {
		t.Fatalf("无 toollog 应返回空而非错误: %v", err)
	}
	if total != 0 || len(counts) != 0 {
		t.Fatal("want empty")
	}
}

func TestAnalyzeUsage(t *testing.T) {
	canonical := t.TempDir()
	makeCanonicalSkill(t, canonical, "triggered")
	makeCanonicalSkill(t, canonical, "never-used")
	root := t.TempDir()
	recordSkillCall(t, root, "triggered", "t1")
	recordSkillCall(t, root, "triggered", "t2")

	rep, err := AnalyzeUsage(root, canonical)
	if err != nil {
		t.Fatal(err)
	}
	if rep.TotalEvents != 2 {
		t.Fatalf("total=%d want 2", rep.TotalEvents)
	}
	if rep.TotalSkills != 2 {
		t.Fatalf("total_skills=%d want 2", rep.TotalSkills)
	}
	if rep.UsedSkills != 1 {
		t.Fatalf("used=%d want 1", rep.UsedSkills)
	}
	if len(rep.NeverTriggered) != 1 || rep.NeverTriggered[0] != "never-used" {
		t.Fatalf("never=%v want [never-used]", rep.NeverTriggered)
	}
	if len(rep.HotSkills) != 1 || rep.HotSkills[0].Name != "triggered" || rep.HotSkills[0].Count != 2 {
		t.Fatalf("hot=%v want triggered:2", rep.HotSkills)
	}
}

// TestAnalyzeUsage_FiltersGhostSkills: toollog retains ghost skills already removed from canonical,
// HotSkills/UsedSkills must filter them — symmetric with NeverTriggered (canonical only).
// Verifies ghost filtering logic still holds after data source switched to toollog.
//
// TestAnalyzeUsage_FiltersGhostSkills：toollog 残留 canonical 已删的「幽灵技能」，
// HotSkills/UsedSkills 必须过滤——与 NeverTriggered（仅 canonical）对称。
// 验证数据源切到 toollog 后幽灵过滤逻辑仍成立。
func TestAnalyzeUsage_FiltersGhostSkills(t *testing.T) {
	canonical := t.TempDir()
	makeCanonicalSkill(t, canonical, "real-skill")
	root := t.TempDir()
	recordSkillCall(t, root, "real-skill", "t1")
	recordSkillCall(t, root, "ghost-skill", "t2") // 幽灵：toollog 有，canonical 无
	recordSkillCall(t, root, "ghost-skill", "t3")

	rep, err := AnalyzeUsage(root, canonical)
	if err != nil {
		t.Fatal(err)
	}
	if rep.UsedSkills != 1 {
		t.Fatalf("UsedSkills=%d want 1（幽灵 ghost-skill 不计入 canonical 使用集）", rep.UsedSkills)
	}
	if rep.TotalEvents != 3 {
		t.Fatalf("TotalEvents=%d want 3（原始事件数不变，只是幽灵不归入 canonical）", rep.TotalEvents)
	}
	for _, h := range rep.HotSkills {
		if h.Name == "ghost-skill" {
			t.Fatalf("幽灵 ghost-skill 不应进 HotSkills: %v", rep.HotSkills)
		}
	}
}

// TestSkillCountsFromToollog_ArchiveSurvives: the archived toollog-<ts>.jsonl
// shape is what historical task-start archival produced. SkillCountsFromToollog
// must read across archives (LoadAllAll), otherwise historical task Skill calls
// are lost after archiving — archive blind spot is a prerequisite for
// cross-task analysis. (The old toolusage.Clear helper used here was deleted as
// dead code; the archive is simulated by a direct rename — the same on-disk
// shape.)
//
// TestSkillCountsFromToollog_ArchiveSurvives：toollog-<ts>.jsonl 归档形态即历史
// task-start 归档产出的形态。SkillCountsFromToollog 必须跨归档读（LoadAllAll），
// 否则归档后历史任务的 Skill 调用全丢——归档盲区是跨任务分析前提。（此处原先
// 用的 toolusage.Clear 助手已作死代码删除；归档改为直接 rename 模拟——同样的
// 磁盘形态。）
func TestSkillCountsFromToollog_ArchiveSurvives(t *testing.T) {
	root := t.TempDir()
	recordSkillCall(t, root, "old-skill", "t1")
	recordSkillCall(t, root, "old-skill", "t1")
	// Simulate task-start archiving: rename the active toollog to a timestamped
	// archive, then let new calls start a fresh active file.
	//
	// 模拟 task-start 归档：把 active toollog 重命名为带时间戳的归档，
	// 新调用随后落在全新的 active 文件里。
	dir := forgedata.DataDirFor(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "toollog.jsonl"), util.ArchivedName(dir, "toollog", time.Now())); err != nil {
		t.Fatal(err)
	}
	// New task active toollog
	//
	// 新任务 active toollog
	recordSkillCall(t, root, "new-skill", "t2")

	counts, total, err := SkillCountsFromToollog(root)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total=%d want 3（归档的 2 + active 的 1，跨归档累加）", total)
	}
	if counts["old-skill"] != 2 {
		t.Fatalf("old-skill=%d want 2（归档后仍应读到，否则跨任务分析降级）", counts["old-skill"])
	}
	if counts["new-skill"] != 1 {
		t.Fatalf("new-skill=%d want 1", counts["new-skill"])
	}
}

// recordSkillTrigger writes a CheckSkillTrigger entry to checklog via the real Record path, mirroring what
// cli/recordSkillTriggerHits produces in production. Both go through the single source of truth
// checklog.DetailForSkillTrigger — this helper no longer hand-mirrors the format string (minor-1: a hand-mirrored
// format could drift from the reader checklog.SkillFromTriggerDetail and silently drop passive signals).
//
// recordSkillTrigger 经真实 Record 路径写一条 CheckSkillTrigger 到 checklog，镜像 cli/recordSkillTriggerHits
// 在生产的产出。两者都走唯一真相源 checklog.DetailForSkillTrigger——本 helper 不再手工镜像格式串
// （minor-1：手工镜像格式可能漂离读取方 checklog.SkillFromTriggerDetail、静默丢失被动信号）。
func recordSkillTrigger(t *testing.T, root, skillName, taskRef string) {
	t.Helper()
	mustWrite(t, checklog.Record(root, &checklog.Entry{
		Check:   checklog.CheckSkillTrigger,
		Passed:  true,
		Checked: true,
		TaskRef: taskRef,
		Detail:  checklog.DetailForSkillTrigger(skillName, `UserPromptSubmit`, `coding_intent`),
	}))
}

func TestSkillCountsFromChecklog(t *testing.T) {
	root := t.TempDir()
	recordSkillTrigger(t, root, "alpha", "t1")
	recordSkillTrigger(t, root, "alpha", "t1")
	recordSkillTrigger(t, root, "beta", "t2")
	// 非 CheckSkillTrigger 条目不计入被动触发统计。
	//
	// non-CheckSkillTrigger entries do not count toward passive-trigger stats.
	mustWrite(t, checklog.Record(root, &checklog.Entry{Check: checklog.CheckAutoCompile, Passed: true, Detail: "x"}))

	counts, total, err := SkillCountsFromChecklog(root)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total=%d want 3（只计 CheckSkillTrigger）", total)
	}
	if counts["alpha"] != 2 || counts["beta"] != 1 {
		t.Fatalf("counts=%v want alpha:2 beta:1", counts)
	}
}

// TestAnalyzeUsage_MergesPassiveTriggers: a skill that fires passively (skill-trigger) but is never explicitly
// called (no Skill tool event) must NOT appear in NeverTriggered — merging active + passive closes the
// undertrigger false-positive (the dogfood 0-trigger blind spot on the usage side). Its Count is the passive total.
//
// TestAnalyzeUsage_MergesPassiveTriggers：一个被动触发（skill-trigger）但从未显式调用（无 Skill 工具事件）
// 的 skill 不得进 NeverTriggered——合并主动+被动闭合 undertrigger 假阳性（usage 侧的 dogfood 0 触发盲区）。
// 其 Count 为被动总数。
func TestAnalyzeUsage_MergesPassiveTriggers(t *testing.T) {
	canonical := t.TempDir()
	makeCanonicalSkill(t, canonical, "passive-only")
	makeCanonicalSkill(t, canonical, "never-used")
	root := t.TempDir()
	// passive-only 只被 skill-trigger 触发，无 Skill 工具调用。
	//
	// passive-only only fires via skill-trigger, never via the Skill tool.
	recordSkillTrigger(t, root, "passive-only", "t1")
	recordSkillTrigger(t, root, "passive-only", "t2")

	rep, err := AnalyzeUsage(root, canonical)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range rep.NeverTriggered {
		if n == "passive-only" {
			t.Fatalf("passive-only 不应在 NeverTriggered（被动触发过）：%v", rep.NeverTriggered)
		}
	}
	if len(rep.NeverTriggered) != 1 || rep.NeverTriggered[0] != "never-used" {
		t.Fatalf("never=%v want [never-used]", rep.NeverTriggered)
	}
	var found bool
	for _, h := range rep.HotSkills {
		if h.Name == "passive-only" {
			found = true
			if h.Count != 2 {
				t.Fatalf("passive-only Count=%d want 2（被动触发合并）", h.Count)
			}
		}
	}
	if !found {
		t.Fatalf("passive-only 应在 HotSkills（被动触发计入触达）：%v", rep.HotSkills)
	}
	if rep.UsedSkills != 1 {
		t.Fatalf("UsedSkills=%d want 1（passive-only）", rep.UsedSkills)
	}
	if rep.TotalEvents != 2 {
		t.Fatalf("TotalEvents=%d want 2（被动 2 + 主动 0）", rep.TotalEvents)
	}
}
