package skilltrigger

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTriggers_ValidJSON(t *testing.T) {
	ts := ParseTriggers(`[{"event":"UserPromptSubmit","keywords":["测试","test"],"when":"condX","cooldown":30}]`)
	if len(ts) != 1 {
		t.Fatalf("len=%d want 1", len(ts))
	}
	if ts[0].Event != "UserPromptSubmit" || len(ts[0].Keywords) != 2 || ts[0].Cooldown != 30 {
		t.Errorf("unexpected trigger: %+v", ts[0])
	}
}

func TestParseTriggers_InvalidJSON(t *testing.T) {
	if ts := ParseTriggers(`[{bad json`); ts != nil {
		t.Errorf("invalid JSON should yield nil, got %v", ts)
	}
}

func TestParseTriggers_Empty(t *testing.T) {
	if ts := ParseTriggers("  "); ts != nil {
		t.Errorf("empty should yield nil, got %v", ts)
	}
}

func TestParseTriggers_EmptyArray(t *testing.T) {
	if ts := ParseTriggers("[]"); len(ts) != 0 {
		t.Errorf("empty array should yield no triggers, got %v", ts)
	}
}

func writeSkill(t *testing.T, root, dir, content string) {
	t.Helper()
	p := filepath.Join(root, dir)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAll(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "with-triggers", "---\nname: with-triggers\nmetadata:\n  triggers: '[{\"event\":\"UserPromptSubmit\",\"keywords\":[\"k\"]}]'\n---\nbody\n")
	writeSkill(t, root, "no-triggers", "---\nname: no-triggers\n---\nbody\n")
	writeSkill(t, root, "declared-empty", "---\nname: declared-empty\nmetadata:\n  triggers: '[]'\n---\nbody\n")
	writeSkill(t, root, "bad-json", "---\nname: bad-json\nmetadata:\n  triggers: '[{bad'\n---\nbody\n")
	writeSkill(t, root, "code-review-gate", "---\nname: code-review-gate\nmetadata:\n  triggers: '[{\"event\":\"Stop\"}]'\n---\nbody\n")
	writeSkill(t, root, "fallback-name", "---\nmetadata:\n  triggers: '[{\"event\":\"Stop\"}]'\n---\nbody\n")

	got := LoadAll(root)
	by := map[string]SkillTriggers{}
	for _, st := range got {
		by[st.Skill] = st
	}
	if _, ok := by["with-triggers"]; !ok {
		t.Errorf("missing with-triggers; got %v", by)
	}
	if _, ok := by["fallback-name"]; !ok {
		t.Errorf("skill without frontmatter name should fall back to dir name; got %v", by)
	}
	for _, gone := range []string{"no-triggers", "declared-empty", "bad-json", "code-review-gate"} {
		if _, ok := by[gone]; ok {
			t.Errorf("%s should be skipped", gone)
		}
	}
	if len(got) != 2 {
		t.Errorf("len=%d want 2", len(got))
	}
}

func TestLoadAll_EmptyDir(t *testing.T) {
	if got := LoadAll(""); got != nil {
		t.Errorf("empty dir should yield nil, got %v", got)
	}
	if got := LoadAll(filepath.Join(t.TempDir(), "missing")); got != nil {
		t.Errorf("missing dir should yield nil, got %v", got)
	}
}
