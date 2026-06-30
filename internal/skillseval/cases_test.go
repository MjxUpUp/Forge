package skillseval

import (
	"os"
	"path/filepath"
	"testing"
)

// testDesc 含 Use when（两个 trigger，or 分隔）+ SKIP（一个 skip），均 >3 rune。
const testDesc = "Use when: 编写 React 组件 or 实现前端布局 SKIP: 选择技术栈"

// writeSkill 在 canonical 下造一个带 SKILL.md 的 skill 目录。
func writeSkill(t *testing.T, canonical, name, desc string) {
	t.Helper()
	sd := filepath.Join(canonical, name)
	mustWrite(t, os.MkdirAll(sd, 0755))
	mustWrite(t, os.WriteFile(filepath.Join(sd, "SKILL.md"),
		[]byte("---\nname: "+name+"\ndescription: "+desc+"\n---\n\nbody\n"), 0644))
}

func TestEvalCases_DerivesTriggersAndSkips(t *testing.T) {
	canonical := t.TempDir()
	writeSkill(t, canonical, "my-skill", testDesc)
	cases, err := EvalCases(canonical, "my-skill")
	if err != nil {
		t.Fatal(err)
	}
	var trig, not int
	for _, c := range cases {
		if c.Skill != "my-skill" {
			t.Fatalf("skill=%s want my-skill", c.Skill)
		}
		switch c.Kind {
		case KindTrigger:
			trig++
			if c.Target != "my-skill" {
				t.Fatalf("trigger target=%s want my-skill", c.Target)
			}
		case KindNotTrigger:
			not++
			if c.Target != "" {
				t.Fatalf("not-trigger target=%q want empty", c.Target)
			}
		}
	}
	if trig != 2 {
		t.Fatalf("trigger cases=%d want 2", trig)
	}
	if not != 1 {
		t.Fatalf("not-trigger cases=%d want 1", not)
	}
}

func TestEvalCases_IDStableAcrossRuns(t *testing.T) {
	canonical := t.TempDir()
	writeSkill(t, canonical, "my-skill", testDesc)
	a, _ := EvalCases(canonical, "my-skill")
	b, _ := EvalCases(canonical, "my-skill")
	if len(a) != len(b) {
		t.Fatalf("len a=%d b=%d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("case %d ID unstable: %s vs %s", i, a[i].ID, b[i].ID)
		}
		if a[i].ID == "" {
			t.Fatalf("case %d has empty ID", i)
		}
	}
}

func TestEvalCases_DescHashMatchesDescription(t *testing.T) {
	canonical := t.TempDir()
	writeSkill(t, canonical, "my-skill", testDesc)
	cases, _ := EvalCases(canonical, "my-skill")
	want := DescHash(testDesc)
	for _, c := range cases {
		if c.DescHash != want {
			t.Fatalf("case DescHash=%s want %s", c.DescHash, want)
		}
	}
}

func TestSaveLoadCases_RoundTrip(t *testing.T) {
	canonical := t.TempDir()
	dir := t.TempDir()
	writeSkill(t, canonical, "my-skill", testDesc)
	cases, _ := EvalCases(canonical, "my-skill")
	mustWrite(t, SaveCases(dir, "my-skill", cases))

	loaded, err := LoadCases(dir, "my-skill")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != len(cases) {
		t.Fatalf("roundtrip len=%d want %d", len(loaded), len(cases))
	}
	want := map[string]string{}
	for _, c := range cases {
		want[c.ID] = c.Prompt
	}
	for _, c := range loaded {
		if want[c.ID] != c.Prompt {
			t.Fatalf("roundtrip prompt mismatch for %s: %q vs %q", c.ID, want[c.ID], c.Prompt)
		}
	}
}

func TestLoadCases_MissingFile(t *testing.T) {
	loaded, err := LoadCases(t.TempDir(), "nope")
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if loaded != nil {
		t.Fatalf("want nil cases, got %v", loaded)
	}
}
