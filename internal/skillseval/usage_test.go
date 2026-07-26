package skillseval

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/toolusage"
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
		{`{"args":"x"}`, ""},                     // 无 skill 字段
		{`not json`, ""},                         // 非 JSON
		{"", ""},                                 // 空
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

// TestSkillCountsFromToollog_ArchiveSurvives: forge task start calls toolusage.Clear to archive
// active toollog to toollog-<ts>.jsonl. SkillCountsFromToollog must read across archives (LoadAllAll),
// otherwise historical task Skill calls are lost after archiving — archive blind spot is a prerequisite for cross-task analysis.
//
// TestSkillCountsFromToollog_ArchiveSurvives：forge task start 调 toolusage.Clear 归档
// active toollog 到 toollog-<ts>.jsonl。SkillCountsFromToollog 必须跨归档读（LoadAllAll），
// 否则归档后历史任务的 Skill 调用全丢——归档盲区是跨任务分析前提。
func TestSkillCountsFromToollog_ArchiveSurvives(t *testing.T) {
	root := t.TempDir()
	recordSkillCall(t, root, "old-skill", "t1")
	recordSkillCall(t, root, "old-skill", "t1")
	// Simulate forge task start archiving: Clear archives active toollog then empties it
	//
	// 模拟 forge task start 归档：Clear 把 active toollog 归档后清空
	if err := toolusage.Clear(root); err != nil {
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
