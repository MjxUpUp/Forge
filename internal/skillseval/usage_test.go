package skillseval

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWrite(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadUsage(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "skill-usage.jsonl")
	content := `{"type":"skill-load","skill":"a"}
{"type":"other","skill":"b"}
{"type":"skill-load","skill":"a"}
not-json-line
{"type":"skill-load","skill":"c"}
`
	mustWrite(t, os.WriteFile(logPath, []byte(content), 0644))
	counts, total, err := LoadUsage(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total=%d want 3", total)
	}
	if counts["a"] != 2 {
		t.Fatalf("a=%d want 2", counts["a"])
	}
	if counts["c"] != 1 {
		t.Fatalf("c=%d want 1", counts["c"])
	}
	if _, ok := counts["b"]; ok {
		t.Fatal("b（非 skill-load）不应计入")
	}
}

func TestLoadUsage_MissingFile(t *testing.T) {
	counts, total, err := LoadUsage(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil {
		t.Fatalf("缺失文件应返回空而非错误: %v", err)
	}
	if total != 0 || len(counts) != 0 {
		t.Fatal("want empty counts/total")
	}
}

func TestAnalyzeUsage(t *testing.T) {
	canonical := t.TempDir()
	for _, n := range []string{"triggered", "never-used"} {
		sd := filepath.Join(canonical, n)
		mustWrite(t, os.MkdirAll(sd, 0755))
		mustWrite(t, os.WriteFile(filepath.Join(sd, "SKILL.md"),
			[]byte("---\nname: "+n+"\ndescription: d\n---\n\nbody\n"), 0644))
	}
	logPath := filepath.Join(t.TempDir(), "skill-usage.jsonl")
	mustWrite(t, os.WriteFile(logPath, []byte(`{"type":"skill-load","skill":"triggered"}
{"type":"skill-load","skill":"triggered"}
`), 0644))

	rep, err := AnalyzeUsage(canonical, logPath)
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

// TestAnalyzeUsage_FiltersGhostSkills：日志含 canonical 已删的"幽灵技能"时，HotSkills/UsedSkills
// 必须过滤掉——与 NeverTriggered（仅 canonical）对称。否则 used_skills 会 > total_skills，
// hot_skills 里混入已不存在的名字（原实现把 counts 全量塞进 hot）。
func TestAnalyzeUsage_FiltersGhostSkills(t *testing.T) {
	canonical := t.TempDir()
	sd := filepath.Join(canonical, "real-skill")
	mustWrite(t, os.MkdirAll(sd, 0755))
	mustWrite(t, os.WriteFile(filepath.Join(sd, "SKILL.md"),
		[]byte("---\nname: real-skill\ndescription: d\n---\n\nbody\n"), 0644))
	logPath := filepath.Join(t.TempDir(), "skill-usage.jsonl")
	// real-skill（canonical 存在）+ ghost-skill（日志残留，canonical 已删）
	mustWrite(t, os.WriteFile(logPath, []byte(`{"type":"skill-load","skill":"real-skill"}
{"type":"skill-load","skill":"ghost-skill"}
{"type":"skill-load","skill":"ghost-skill"}
`), 0644))

	rep, err := AnalyzeUsage(canonical, logPath)
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
