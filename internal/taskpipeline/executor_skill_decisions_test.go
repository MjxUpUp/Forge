package taskpipeline

// executor_skill_decisions_test.go — skillDecisionsNamesFromChanged 纯函数测试。
// 覆盖：实质内容变更命中 / decisions.md 自身排除 / 非 skill 源码前缀排除 / 混合。

import (
	"reflect"
	"strings"
	"testing"
)

func TestSkillDecisionsNamesFromChanged(t *testing.T) {
	empty := make([]string, 0) // 函数对空命中返回非 nil 空 slice（make 后未填）
	tests := []struct {
		name    string
		changed []string
		want    []string
	}{
		{
			name:    "empty input",
			changed: nil,
			want:    empty,
		},
		{
			name:    "SKILL.md triggers",
			changed: []string{"skills/code-review-gate/SKILL.md"},
			want:    []string{"code-review-gate"},
		},
		{
			name:    "scripts trigger",
			changed: []string{"skills/foo/scripts/run.sh"},
			want:    []string{"foo"},
		},
		{
			name:    "references trigger",
			changed: []string{"skills/foo/references/checklist.md"},
			want:    []string{"foo"},
		},
		{
			name:    "decisions.md excluded (recording is not a trigger)",
			changed: []string{"skills/foo/decisions.md"},
			want:    empty,
		},
		{
			name:    "non-skill source excluded by exact prefix",
			changed: []string{"internal/cli/skills_decide.go"},
			want:    empty,
		},
		{
			name:    "eval package source excluded",
			changed: []string{"internal/skillseval/runs.go"},
			want:    empty,
		},
		{
			name: "skills/ root files excluded (CONVENTIONS.md is not a skill)",
			changed: []string{"skills/CONVENTIONS.md", "skills/README.md"},
			want:  empty,
		},
		{
			name: "mixed—decisions.md, non-skill, root files filtered; real skills kept+sorted",
			changed: []string{
				"skills/foo/SKILL.md",
				"skills/foo/decisions.md",
				"skills/bar/scripts/x.sh",
				"skills/CONVENTIONS.md",
				"internal/skillseval/runs.go",
				"internal/skillsdecisions/decisions.go",
			},
			want: []string{"bar", "foo"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := skillDecisionsNamesFromChanged(tt.changed)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("skillDecisionsNamesFromChanged(%v)\n got  = %v\n want = %v", tt.changed, got, tt.want)
			}
		})
	}
}

func TestFormatSkillDecisionsAdvisory(t *testing.T) {
	got := formatSkillDecisionsAdvisory([]string{"foo", "bar"})
	// 每个 skill 名 + 对应 decide 命令都出现
	for _, want := range []string{"foo", "bar", "decide --skill foo", "decide --skill bar", "四元组", "理解 why"} {
		if !strings.Contains(got, want) {
			t.Errorf("advisory missing %q: %s", want, got)
		}
	}
	// 第三方项目归因应已从 advisory 文本清除
	for _, banned := range []string{"SkillHone", "arXiv", "借 SkillHone"} {
		if strings.Contains(got, banned) {
			t.Errorf("advisory still carries attribution %q: %s", banned, got)
		}
	}
}
