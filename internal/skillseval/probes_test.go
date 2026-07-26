package skillseval

// probes_test.go — behavior probe evaluation + loading tests. Covers judgeBehavior four prefixes,
// LoadProbes round-trip, judgeResult behavior branches.
//
// probes_test.go — behavior probe 判定 + 加载测试。覆盖 judgeBehavior 四前缀、
// LoadProbes round-trip、judgeResult behavior 分支。

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJudgeBehavior_Contains(t *testing.T) {
	cases := []struct {
		output string
		oracle string
		want   bool
	}{
		{"输出含跨层数据类型检查", "contains:跨层数据类型", true},
		{"输出含别的内容", "contains:跨层数据类型", false},
		{"", "contains:x", false},   // 空输出 → false
		{"非空输出", "", false},       // 空 oracle → false
		{"非空输出", "contains:", false}, // contains 空子串？rest="" → Contains 任何串都 true，但语义空
	}
	for _, c := range cases {
		if got := judgeBehavior(c.output, c.oracle); got != c.want {
			t.Errorf("judgeBehavior(%q, %q) = %v, want %v", c.output, c.oracle, got, c.want)
		}
	}
}

func TestJudgeBehavior_NotContains(t *testing.T) {
	if !judgeBehavior("全是中文没有英文字母", "not-contains:X") {
		t.Error("not-contains should pass when substring absent")
	}
	if judgeBehavior("output has X mark", "not-contains:X") {
		t.Error("not-contains should fail when substring present")
	}
}

func TestJudgeBehavior_Regex(t *testing.T) {
	if !judgeBehavior("score 95/A", "regex:score \\d+/[A-Z]") {
		t.Error("regex match should pass")
	}
	if judgeBehavior("no match here", "regex:score \\d+/[A-Z]") {
		t.Error("regex non-match should fail")
	}
	if judgeBehavior("x", "regex:[") {
		t.Error("bad regex should fail (not panic)")
	}
}

func TestJudgeBehavior_Exact(t *testing.T) {
	if !judgeBehavior("  yes  ", "exact:yes") {
		t.Error("exact should trim both sides and match")
	}
	if judgeBehavior("no", "exact:yes") {
		t.Error("exact mismatch should fail")
	}
}

func TestJudgeBehavior_NoPrefix_DefaultsContains(t *testing.T) {
	if !judgeBehavior("含关键词的输出", "关键词") {
		t.Error("no-prefix oracle should default to contains")
	}
}

func TestJudgeBehavior_UnknownPrefix_Fails(t *testing.T) {
	// Unknown prefix (misspelled contain:) is treated as config error → return false to fail the probe loudly,
	// not fallback to contains silent judging (rest=keyword would falsely pass on fallback, masking config error).
	//
	// 未知前缀（拼错的 contain:）视为配置错误 → return false 让 probe 失败暴露，
	// 非 fallback contains 静默判定（rest=keyword 若 fallback 会假 pass，掩盖配置错误）。
	if judgeBehavior("含 keyword 的输出", "contain:keyword") {
		t.Error("unknown prefix should fail loud (config error), not fallback to contains")
	}
}

func TestJudgeResult_BehaviorBranch(t *testing.T) {
	c := EvalCase{Kind: KindBehavior, Skill: "s", Oracle: "contains:OK"}
	if !judgeResult(c, "", "output OK here") {
		t.Error("behavior should pass when output matches oracle")
	}
	if judgeResult(c, "", "no match") {
		t.Error("behavior should fail when output misses oracle")
	}
	// behavior does not look at actualTriggered (passing skill name also does not affect judgment)
	//
	// behavior 不看 actualTriggered（传 skill 名也不影响判定）
	if judgeResult(c, "s", "no match") {
		t.Error("behavior must ignore actualTriggered")
	}
}

func TestLoadProbes_NoFile(t *testing.T) {
	cases, err := LoadProbes(t.TempDir(), "missing")
	if err != nil {
		t.Fatalf("LoadProbes missing file: %v", err)
	}
	if cases != nil {
		t.Errorf("LoadProbes missing file got %v, want nil", cases)
	}
}

func TestLoadProbes_RoundTrip(t *testing.T) {
	canonical := t.TempDir()
	skillDir := filepath.Join(canonical, "my-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	yaml := "skill: my-skill\nprobes:\n" +
		"  - id: p-cross-layer\n" +
		"    input: |\n" +
		"      检查这段代码：User.ID 是 int，API 返回 id \"123\"\n" +
		"    oracle: \"contains:跨层数据类型\"\n" +
		"    rationale: 识别 int vs string 类型漂移\n" +
		"  - id: p-sql\n" +
		"    input: |\n" +
		"      审查这段 SQL 注入风险\n" +
		"    oracle: \"regex:SELECT .+ FROM\"\n"
	if err := os.WriteFile(filepath.Join(skillDir, "probes.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cases, err := LoadProbes(canonical, "my-skill")
	if err != nil {
		t.Fatalf("LoadProbes: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("got %d probes, want 2", len(cases))
	}
	for _, c := range cases {
		if c.Kind != KindBehavior {
			t.Errorf("Kind = %q, want behavior", c.Kind)
		}
		if c.Skill != "my-skill" {
			t.Errorf("Skill = %q", c.Skill)
		}
		if c.Oracle == "" {
			t.Errorf("Oracle empty for %s", c.ID)
		}
		if c.ProbeInput == "" {
			t.Errorf("ProbeInput empty for %s", c.ID)
		}
		if c.DescHash != "" {
			t.Errorf("behavior DescHash should be empty (independent of description), got %q", c.DescHash)
		}
	}
	if cases[0].ID != "p-cross-layer" {
		t.Errorf("ID = %q, want p-cross-layer", cases[0].ID)
	}
	if cases[0].Oracle != "contains:跨层数据类型" {
		t.Errorf("Oracle = %q", cases[0].Oracle)
	}
}

func TestLoadProbes_AutoID(t *testing.T) {
	// probe without explicit id → computes stable id from input+oracle.
	//
	// probe 不声明 id → 按 input+oracle 算稳定 id。
	canonical := t.TempDir()
	skillDir := filepath.Join(canonical, "s")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	yaml := "skill: s\nprobes:\n  - input: foo\n    oracle: \"contains:bar\"\n"
	if err := os.WriteFile(filepath.Join(skillDir, "probes.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	cases, err := LoadProbes(canonical, "s")
	if err != nil {
		t.Fatalf("LoadProbes: %v", err)
	}
	if len(cases) != 1 || cases[0].ID == "" {
		t.Fatalf("auto ID not filled: %+v", cases)
	}
	// Load again, id should be stable (same input+oracle).
	//
	// 再加载一次，id 应稳定（同 input+oracle）。
	cases2, _ := LoadProbes(canonical, "s")
	if cases2[0].ID != cases[0].ID {
		t.Errorf("auto ID not stable: %q vs %q", cases[0].ID, cases2[0].ID)
	}
}
