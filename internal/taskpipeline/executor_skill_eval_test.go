package taskpipeline

// executor_skill_eval_test.go — skill-eval advisory 的纯函数测试。
// 测 skillNamesFromChanged（无 git 依赖）：skills/ 前缀精确命中、internal/ 下同名词
// 不误命中、无 case 集静默、同 skill 去重。

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// writeCaseSet 在 casesDir 下造一个空的 cases/<name>.json（仅验证存在性判定）。
func writeCaseSet(t *testing.T, casesDir, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(casesDir, "cases"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casesDir, "cases", name+".json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestSkillNamesFromChanged_HitsSkillsDirWithCaseSet：skills/foo/SKILL.md + 有 case 集 → ["foo"]。
func TestSkillNamesFromChanged_HitsSkillsDirWithCaseSet(t *testing.T) {
	casesDir := t.TempDir()
	writeCaseSet(t, casesDir, "foo")
	changed := []string{
		"skills/foo/SKILL.md",
		"README.md",
	}
	got := skillNamesFromChanged(changed, casesDir)
	want := []string{"foo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want %v", got, want)
	}
}

// TestSkillNamesFromChanged_MissesCliSkillsFiles：internal/cli/skills_*.go、
// internal/skillseval/*.go 含 "skills" 子串但非 "skills/" 前缀 → 不误命中。
// 守护 plan 强调的 testcoverage.go:67 反面坑（substring 误匹配）。
func TestSkillNamesFromChanged_MissesCliSkillsFiles(t *testing.T) {
	casesDir := t.TempDir()
	writeCaseSet(t, casesDir, "foo") // 即便有同名 case 集，也不该命中 internal 下文件
	changed := []string{
		"internal/cli/skills_install.go",
		"internal/skillseval/runs.go",
		"internal/skillscanonical/resolve.go",
	}
	got := skillNamesFromChanged(changed, casesDir)
	if len(got) != 0 {
		t.Fatalf("internal/ 下同名词源码不该命中 skill-eval，got=%v", got)
	}
}

// TestSkillNamesFromChanged_NoCaseSetSilent：改了 skills/bar/ 但没生成过 case 集 →
// 静默（无回归基准，提醒也跑不了）。
func TestSkillNamesFromChanged_NoCaseSetSilent(t *testing.T) {
	casesDir := t.TempDir() // 空，无任何 case 集
	changed := []string{"skills/bar/SKILL.md"}
	got := skillNamesFromChanged(changed, casesDir)
	if len(got) != 0 {
		t.Fatalf("无 case 集应静默，got=%v", got)
	}
}

// TestSkillNamesFromChanged_DedupesSameSkill：同 skill 多文件变更 → 去重为一条。
func TestSkillNamesFromChanged_DedupesSameSkill(t *testing.T) {
	casesDir := t.TempDir()
	writeCaseSet(t, casesDir, "foo")
	changed := []string{
		"skills/foo/SKILL.md",
		"skills/foo/references/x.md",
		"skills/foo/scripts/run.sh",
	}
	got := skillNamesFromChanged(changed, casesDir)
	want := []string{"foo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want %v（应去重）", got, want)
	}
}

// TestSkillNamesFromChanged_MultipleSkillsSorted：多 skill → 排序输出。
func TestSkillNamesFromChanged_MultipleSkillsSorted(t *testing.T) {
	casesDir := t.TempDir()
	writeCaseSet(t, casesDir, "zeta")
	writeCaseSet(t, casesDir, "alpha")
	changed := []string{
		"skills/zeta/SKILL.md",
		"skills/alpha/SKILL.md",
	}
	got := skillNamesFromChanged(changed, casesDir)
	want := []string{"alpha", "zeta"}
	if !sort.StringsAreSorted(got) || !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want sorted %v", got, want)
	}
}

// TestFormatSkillEvalAdvisory_NonEmpty：提醒含命令 + skill 名。
func TestFormatSkillEvalAdvisory_NonEmpty(t *testing.T) {
	s := formatSkillEvalAdvisory([]string{"foo"})
	if s == "" {
		t.Fatal("advisory 不应为空")
	}
	if !strings.Contains(s, "eval-report --skill foo") {
		t.Fatalf("advisory 缺回归命令: %s", s)
	}
}
