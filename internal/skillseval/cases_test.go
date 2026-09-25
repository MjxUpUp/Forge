package skillseval

import (
	"encoding/json"
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

// writeGoldenSet 在 <dir>/golden/<skill>/cases.json 写一份策展黄金集。
func writeGoldenSet(t *testing.T, dir, skill string, cases []EvalCase) {
	t.Helper()
	gd := filepath.Join(dir, "golden", skill)
	mustWrite(t, os.MkdirAll(gd, 0755))
	set := CaseSet{Skill: skill, Cases: cases}
	data, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, os.WriteFile(filepath.Join(gd, "cases.json"), data, 0644))
}

// TestLoadCases_GoldenPriorityMerge pins the merge contract: golden cases come
// first, derived cases supplement uncovered IDs, and on ID conflict the golden
// case wins.
//
// TestLoadCases_GoldenPriorityMerge 钉住合并契约：golden 在前、派生补充未覆盖 ID、
// 同 ID golden 胜出。
func TestLoadCases_GoldenPriorityMerge(t *testing.T) {
	canonical := t.TempDir()
	dir := t.TempDir()
	writeSkill(t, canonical, "my-skill", testDesc)
	derived, _ := EvalCases(canonical, "my-skill")
	mustWrite(t, SaveCases(dir, "my-skill", derived))

	curated := []EvalCase{
		{ID: "g-my-skill-t1", Skill: "my-skill", Kind: KindTrigger, Prompt: "人工改写正例", Target: "my-skill", Origin: OriginCurated},
		// Same ID as a derived case — the golden one must win.
		{ID: derived[0].ID, Skill: "my-skill", Kind: KindTrigger, Prompt: "golden 覆盖派生", Target: "my-skill", Origin: OriginCurated},
	}
	writeGoldenSet(t, dir, "my-skill", curated)

	loaded, err := LoadCases(dir, "my-skill")
	if err != nil {
		t.Fatal(err)
	}
	// 2 curated (one shadowing a derived ID) + remaining derived.
	wantLen := 2 + len(derived) - 1
	if len(loaded) != wantLen {
		t.Fatalf("merged len=%d want %d", len(loaded), wantLen)
	}
	if loaded[0].ID != "g-my-skill-t1" || loaded[0].Origin != OriginCurated {
		t.Fatalf("golden case must lead: %+v", loaded[0])
	}
	byID := map[string]EvalCase{}
	for _, c := range loaded {
		byID[c.ID] = c
	}
	if byID[derived[0].ID].Prompt != "golden 覆盖派生" {
		t.Fatalf("ID conflict: golden must win, got prompt %q", byID[derived[0].ID].Prompt)
	}
	// Derived DescHash still surfaces on the merged set (submit stale-check relies on it).
	set, err := LoadCaseSet(dir, "my-skill")
	if err != nil {
		t.Fatal(err)
	}
	if set.DescHash != DescHash(testDesc) {
		t.Fatalf("merged DescHash=%s want derived %s", set.DescHash, DescHash(testDesc))
	}
}

// TestLoadCases_GoldenOnly pins that a golden-only skill loads with an empty
// DescHash.
//
// TestLoadCases_GoldenOnly 钉住纯 golden 集可加载且 DescHash 为空——策展 case 锚定
// 真实话语而非 description，不会因 description 变更过期。
func TestLoadCases_GoldenOnly(t *testing.T) {
	dir := t.TempDir()
	writeGoldenSet(t, dir, "solo", []EvalCase{
		{ID: "g-solo-t1", Skill: "solo", Kind: KindTrigger, Prompt: "正例", Target: "solo", Origin: OriginCurated},
	})
	set, err := LoadCaseSet(dir, "solo")
	if err != nil {
		t.Fatal(err)
	}
	if set == nil || len(set.Cases) != 1 {
		t.Fatalf("golden-only set: %+v", set)
	}
	if set.DescHash != "" {
		t.Fatalf("golden-only DescHash=%q want empty", set.DescHash)
	}
}

// TestExportedStoragePaths pins the exported RunsFile / BaselinesFile wrappers:
// they must resolve to the same layout the internal writers use
// (runs/<skill>.jsonl, baselines.json).
//
// TestExportedStoragePaths 钉住导出的 RunsFile / BaselinesFile 包装：它们必须解析到
// 内部写入方使用的同一布局（runs/<skill>.jsonl、baselines.json）——外部消费者
// （dashboard 缓存）按这些路径做 mtime 指纹失效判断，此处布局漂移会静默破坏其缓存正确性。
func TestExportedStoragePaths(t *testing.T) {
	dir := t.TempDir()
	if got, want := RunsFile(dir, "alpha"), runsFile(dir, "alpha"); got != want {
		t.Errorf("RunsFile = %q, want %q", got, want)
	}
	if got, want := BaselinesFile(dir), baselinesFile(dir); got != want {
		t.Errorf("BaselinesFile = %q, want %q", got, want)
	}
	if got, want := RunsFile(dir, "alpha"), filepath.Join(dir, "runs", "alpha.jsonl"); got != want {
		t.Errorf("RunsFile 布局 = %q, want %q", got, want)
	}
	if got, want := BaselinesFile(dir), filepath.Join(dir, "baselines.json"); got != want {
		t.Errorf("BaselinesFile 布局 = %q, want %q", got, want)
	}
}
