package skillsdist

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// mkSkillMD 在 canonical 下建一个最小 SKILL.md（仅 frontmatter，checkRequires 不跑
// audit 所以极简即可，只要 skillsfm.Parse 能解出 name/requires）。
func mkSkillMD(t *testing.T, canonical, name, requires string) {
	t.Helper()
	dir := filepath.Join(canonical, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf(`mkdir %s: %v`, dir, err)
	}
	var content string
	if requires == "" {
		content = fmt.Sprintf(noReqTpl, name)
	} else {
		content = fmt.Sprintf(withReqTpl, name, requires)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatalf(`write %s: %v`, name, err)
	}
}

const withReqTpl = `---
name: %s
requires: %s
---
body
`

const noReqTpl = `---
name: %s
---
body
`

// linked 是 helper：构造一个"成功落到目标"的 TargetResult（action=linked）。
func linked() TargetResult { return TargetResult{Action: actLinked} }

func TestParseRequires(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{``, nil},
		{`   `, nil},
		{`a`, []string{`a`}},
		{`a, b`, []string{`a`, `b`}},
		{`a,b,c`, []string{`a`, `b`, `c`}},
		{` a , b , `, []string{`a`, `b`}},
		{`code-review-gate, doc-generator`, []string{`code-review-gate`, `doc-generator`}},
	}
	for _, tc := range cases {
		got := parseRequires(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf(`parseRequires(%q) = %v, want %v`, tc.in, got, tc.want)
		}
	}
}

func TestCheckRequires(t *testing.T) {
	cases := []struct {
		name         string
		setup        func(t *testing.T, dir string)
		results      []SkillInstallResult
		wantCount    int
		wantContains []string // 每个 wantContains 子串都应出现在某条 warning 中
	}{
		{
			name: `依赖满足：两者同装，无警告`,
			setup: func(t *testing.T, dir string) {
				mkSkillMD(t, dir, `alpha`, `beta`)
				mkSkillMD(t, dir, `beta`, ``)
			},
			results: []SkillInstallResult{
				{Name: `alpha`, Targets: []TargetResult{linked()}},
				{Name: `beta`, Targets: []TargetResult{linked()}},
			},
			wantCount: 0,
		},
		{
			name: `依赖在 canonical 但未同装（单装断链场景）→ 未同装警告`,
			setup: func(t *testing.T, dir string) {
				mkSkillMD(t, dir, `alpha`, `beta`)
				mkSkillMD(t, dir, `beta`, ``)
			},
			results: []SkillInstallResult{
				{Name: `alpha`, Targets: []TargetResult{linked()}},
			},
			wantCount:    1,
			wantContains: []string{`alpha`, `beta`, `未同装`},
		},
		{
			name: `依赖不在 canonical（requires 声明无效）→ 无效警告`,
			setup: func(t *testing.T, dir string) {
				mkSkillMD(t, dir, `alpha`, `ghost`)
			},
			results: []SkillInstallResult{
				{Name: `alpha`, Targets: []TargetResult{linked()}},
			},
			wantCount:    1,
			wantContains: []string{`alpha`, `ghost`, `不在 canonical`},
		},
		{
			name: `多依赖逗号分隔，其一未同装 → 仅该依赖告警`,
			setup: func(t *testing.T, dir string) {
				mkSkillMD(t, dir, `alpha`, `beta, gamma`)
				mkSkillMD(t, dir, `beta`, ``)
				mkSkillMD(t, dir, `gamma`, ``)
			},
			results: []SkillInstallResult{
				{Name: `alpha`, Targets: []TargetResult{linked()}},
				{Name: `beta`, Targets: []TargetResult{linked()}},
			},
			wantCount:    1,
			wantContains: []string{`gamma`, `未同装`},
		},
		{
			name: `blocked 的 skill 跳过（未成功装，不检查其依赖）`,
			setup: func(t *testing.T, dir string) {
				mkSkillMD(t, dir, `alpha`, `beta`)
			},
			results: []SkillInstallResult{
				{Name: `alpha`, Targets: []TargetResult{{Action: `blocked`}}},
			},
			wantCount: 0,
		},
		{
			name: `无 requires 字段的 skill 不产生警告`,
			setup: func(t *testing.T, dir string) {
				mkSkillMD(t, dir, `alpha`, ``)
			},
			results: []SkillInstallResult{
				{Name: `alpha`, Targets: []TargetResult{linked()}},
			},
			wantCount: 0,
		},
		{
			name: `aborted 的 skill 跳过（drift-policy=abort 触发，不检查依赖）`,
			setup: func(t *testing.T, dir string) {
				mkSkillMD(t, dir, `alpha`, `beta`)
				mkSkillMD(t, dir, `beta`, ``)
			},
			results: []SkillInstallResult{
				{Name: `alpha`, Targets: []TargetResult{{Action: actAborted}}},
				{Name: `beta`, Targets: []TargetResult{linked()}},
			},
			wantCount: 0,
		},
		{
			name: `reserved 的 skill 跳过（forge-pipeline/quality 等保留名）`,
			setup: func(t *testing.T, dir string) {
				mkSkillMD(t, dir, `forge-pipeline`, `beta`)
			},
			results: []SkillInstallResult{
				{Name: `forge-pipeline`, Targets: []TargetResult{{Action: actReserved}}},
			},
			wantCount: 0,
		},
		{
			name: `--skip-quality 归零：Pass=false + action=linked 仍检查依赖（用 action 而非 Pass 的核心归零测试）`,
			setup: func(t *testing.T, dir string) {
				mkSkillMD(t, dir, `alpha`, `beta`)
				mkSkillMD(t, dir, `beta`, ``)
			},
			results: []SkillInstallResult{
				{Name: `alpha`, Pass: false, Targets: []TargetResult{linked()}},
			},
			wantCount:    1,
			wantContains: []string{`alpha`, `beta`, `未同装`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			warns := checkRequires(dir, tc.results)
			if len(warns) != tc.wantCount {
				t.Errorf(`want %d warns, got %d: %v`, tc.wantCount, len(warns), warns)
			}
			joined := strings.Join(warns, "\n")
			for _, sub := range tc.wantContains {
				if !strings.Contains(joined, sub) {
					t.Errorf(`warnings 缺少子串 %q: %v`, sub, warns)
				}
			}
		})
	}
}
