package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/impact"
)

// renderImpact 是纯函数：固定的 Report 进、固定的文本出。
func TestImpactRender(t *testing.T) {
	tests := []struct {
		name string
		rep  *impact.Report
		want string
	}{
		{
			name: "无改动",
			rep:  &impact.Report{},
			want: "没有相对 HEAD 的改动。\n",
		},
		{
			name: "只有源码无测试",
			rep: &impact.Report{
				Changed:            []string{"internal/a/a.go"},
				SourcesWithoutTest: []string{"internal/a/a.go"},
			},
			want: "改动 1 个文件\n" +
				"改了源码但没有测试文件：\n" +
				"  internal/a/a.go\n",
		},
		{
			name: "三段齐全",
			rep: &impact.Report{
				Changed:            []string{"README.md", "internal/a/a.go", "internal/b/b.go"},
				Docs:               []string{"README.md"},
				SourcesWithoutTest: []string{"internal/a/a.go"},
				SuggestedTests:     []string{"internal/b"},
			},
			want: "改动 3 个文件\n" +
				"文档（需要同步）：\n" +
				"  README.md\n" +
				"改了源码但没有测试文件：\n" +
				"  internal/a/a.go\n" +
				"建议先跑的测试目录：\n" +
				"  internal/b\n",
		},
		{
			name: "只有建议测试目录",
			rep: &impact.Report{
				Changed:        []string{"internal/b/b.go"},
				SuggestedTests: []string{"internal/b"},
			},
			want: "改动 1 个文件\n" +
				"建议先跑的测试目录：\n" +
				"  internal/b\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			renderImpact(&buf, tt.rep)
			if got := buf.String(); got != tt.want {
				t.Errorf("renderImpact 输出不符\n got: %q\nwant: %q", got, tt.want)
			}
			if tt.name == "无改动" && strings.Contains(buf.String(), "改动 0") {
				t.Errorf("无改动不应输出计数段: %q", buf.String())
			}
		})
	}
}

// 命令面约定：名字、分组、--json flag、拒绝多余参数。
func TestImpactCommandSurface(t *testing.T) {
	if impactCmd.Use != "impact" {
		t.Errorf("Use = %q, want impact", impactCmd.Use)
	}
	if impactCmd.GroupID != "pipeline" {
		t.Errorf("GroupID = %q, want pipeline", impactCmd.GroupID)
	}
	if impactCmd.Short != "按 diff 列出影响面（文档/无测试源码）" {
		t.Errorf("Short = %q", impactCmd.Short)
	}
	if impactCmd.Flags().Lookup("json") == nil {
		t.Errorf("缺少 --json flag")
	}
	if err := impactCmd.Args(impactCmd, []string{"extra"}); err == nil {
		t.Errorf("Args 应拒绝多余参数")
	}
}
