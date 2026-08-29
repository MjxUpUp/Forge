package taskpipeline

// executor_skill_decisions_test.go — skill-decisions 检查测试（B 组件 advisory→guardrail）。
// 覆盖：blocking/advisory 分类纯函数 + skillDecisionsRecorded（base..HEAD 新增判定 + fail-open
// 各态）+ advisory 文案。

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSkillDecisionsBlockingAffected(t *testing.T) {
	tests := []struct {
		name    string
		changed []string
		want    []string
	}{
		{"SKILL.md triggers", []string{"skills/foo/SKILL.md"}, []string{"foo"}},
		{"scripts not blocking", []string{"skills/foo/scripts/x.sh"}, []string{}},
		{"references not blocking", []string{"skills/foo/references/c.md"}, []string{}},
		{"decisions.md not blocking", []string{"skills/foo/decisions.md"}, []string{}},
		{"non-skill source excluded", []string{"internal/cli/skills_decide.go"}, []string{}},
		{"skills root excluded", []string{"skills/CONVENTIONS.md"}, []string{}},
		{"mixed picks SKILL.md only + sorted", []string{"skills/foo/SKILL.md", "skills/foo/scripts/x.sh", "skills/bar/SKILL.md", "skills/baz/decisions.md"}, []string{"bar", "foo"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := skillDecisionsBlockingAffected(tt.changed)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("blocking got=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestSkillDecisionsAdvisoryAffected(t *testing.T) {
	tests := []struct {
		name    string
		changed []string
		want    []string
	}{
		{"scripts advisory", []string{"skills/foo/scripts/x.sh"}, []string{"foo"}},
		{"references advisory", []string{"skills/foo/references/c.md"}, []string{"foo"}},
		{"SKILL.md not advisory (covered by blocking)", []string{"skills/foo/SKILL.md"}, []string{}},
		{"subdir SKILL.md advisory (non-canonical)", []string{"skills/foo/archive/SKILL.md"}, []string{"foo"}},
		{"subdir SKILL.md + scripts same skill", []string{"skills/foo/archive/SKILL.md", "skills/foo/scripts/x.sh"}, []string{"foo"}},
		{"decisions.md excluded", []string{"skills/foo/decisions.md"}, []string{}},
		{"same skill SKILL+scripts → advisory excludes (blocking covers foo)", []string{"skills/foo/SKILL.md", "skills/foo/scripts/x.sh"}, []string{}},
		{"mixed: foo blocking, bar advisory", []string{"skills/foo/SKILL.md", "skills/bar/scripts/x.sh"}, []string{"bar"}},
		{"non-skill excluded", []string{"internal/skillseval/runs.go"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := skillDecisionsAdvisoryAffected(tt.changed)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("advisory got=%v want=%v", got, tt.want)
			}
		})
	}
}

// TestSkillDecisionsRecorded 钉住「记决策」判定锚点：decisions.md 在 base..HEAD 间新增
// `## [d-` 条目 = 记了。覆盖：未新增/新增/首次创建（base 无）/base 空 fail-open/base 不可达
// fail-open。fail-open 对齐 review snapshot「可达则严、不可达则松」。
func TestSkillDecisionsRecorded(t *testing.T) {
	dir := t.TempDir()
	sdecGit(t, dir, "init")
	sdecGit(t, dir, "config", "user.email", "test@example.com")
	sdecGit(t, dir, "config", "user.name", "Test")
	sdecWrite(t, dir, "README.md", "init\n")
	sdecGit(t, dir, "add", "-A")
	sdecGit(t, dir, "commit", "-m", "init")

	skill := "myskill"
	// base 含 1 条 decisions.md。
	sdecWrite(t, dir, "skills/"+skill+"/decisions.md", "## [d-base1] accept\nbase 决策\n")
	sdecGit(t, dir, "add", "-A")
	sdecGit(t, dir, "commit", "-m", "base decisions")
	base := sdecHead(t, dir)

	// case 1: 当前==base（1 条未改）→ 未记。
	if rec, fo := skillDecisionsRecorded(dir, base, skill); fo || rec {
		t.Errorf("未新增应 recorded=false failopen=false, got rec=%v fo=%v", rec, fo)
	}
	// case 2: 当前新增第 2 条（工作区未提交，模拟 agent 记了但没 commit）→ 记了。
	sdecWrite(t, dir, "skills/"+skill+"/decisions.md", "## [d-base1] accept\n## [d-new1] reject\n新决策\n")
	if rec, fo := skillDecisionsRecorded(dir, base, skill); fo || !rec {
		t.Errorf("新增第2条应 recorded=true failopen=false, got rec=%v fo=%v", rec, fo)
	}
	// case 3: 另一 skill 首次创建 decisions.md（base 时无该文件）→ 记了。
	skill2 := "newskill"
	sdecWrite(t, dir, "skills/"+skill2+"/decisions.md", "## [d-first] accept\n首次决策\n")
	if rec, fo := skillDecisionsRecorded(dir, base, skill2); fo || !rec {
		t.Errorf("首次创建 decisions.md 应 recorded=true, got rec=%v fo=%v", rec, fo)
	}
	// case 4: base 空（老 state 无 HeadCommit）→ fail-open。
	if _, fo := skillDecisionsRecorded(dir, "", skill); !fo {
		t.Errorf("base 空应 fail-open")
	}
	// case 5: base commit 不可达（amend/rebase 改写历史）→ fail-open。
	if _, fo := skillDecisionsRecorded(dir, "deadbeefnotarealcommit123", skill); !fo {
		t.Errorf("base 不可达应 fail-open")
	}
}

func TestFormatSkillDecisionsAdvisory(t *testing.T) {
	got := formatSkillDecisionsAdvisory([]string{"foo", "bar"})
	for _, want := range []string{"foo", "bar", "decide --skill foo", "decide --skill bar", "辅助资源", "四元组", "理解 why"} {
		if !strings.Contains(got, want) {
			t.Errorf("advisory missing %q: %s", want, got)
		}
	}
	for _, banned := range []string{"SkillHone", "arXiv"} {
		if strings.Contains(got, banned) {
			t.Errorf("advisory still carries attribution %q: %s", banned, got)
		}
	}
}

// sdecGit runs a git command in dir, fatal on failure。
func sdecGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// sdecWrite writes a file under dir, creating parent dirs。
func sdecWrite(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// sdecHead returns current HEAD commit hash。
func sdecHead(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}
