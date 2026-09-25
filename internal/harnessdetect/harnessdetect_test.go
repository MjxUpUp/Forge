package harnessdetect

import (
	"os"
	"path/filepath"
	"testing"
)

// harnessdetect_test.go — 外来 harness 信号表（P4 让位检测）契约：高置信目录级
// 信号命中才让位（宁可漏判多问一次，不可误判错误接管——mise 原则）。

func TestDetect_NoSignal(t *testing.T) {
	root := t.TempDir()
	if sig, hit := Detect(root); hit {
		t.Fatalf(`空目录误判外来 harness: %q`, sig)
	}
}

func TestDetect_Table(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, root string)
		want  string
	}{
		{
			name: `spec-kit 标记目录 .specify`,
			setup: func(t *testing.T, root string) {
				os.MkdirAll(filepath.Join(root, `.specify`), 0755)
			},
			want: `.specify/（spec-kit）`,
		},
		{
			name: `项目级 .claude/commands 有内容`,
			setup: func(t *testing.T, root string) {
				os.MkdirAll(filepath.Join(root, `.claude`, `commands`), 0755)
				os.WriteFile(filepath.Join(root, `.claude`, `commands`, `spec.md`), []byte(`x`), 0644)
			},
			want: `.claude/commands/（自带 slash commands）`,
		},
		{
			name: `项目级 .claude/settings.json 带 hooks 键`,
			setup: func(t *testing.T, root string) {
				os.MkdirAll(filepath.Join(root, `.claude`), 0755)
				os.WriteFile(filepath.Join(root, `.claude`, `settings.json`), []byte(`{"hooks": {}}`), 0644)
			},
			want: `.claude/settings.json 含 hooks（自有 harness 接线）`,
		},
		{
			name: `.cursor/rules 有内容`,
			setup: func(t *testing.T, root string) {
				os.MkdirAll(filepath.Join(root, `.cursor`, `rules`), 0755)
				os.WriteFile(filepath.Join(root, `.cursor`, `rules`, `a.mdc`), []byte(`x`), 0644)
			},
			want: `.cursor/rules/（Cursor 规则集）`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			c.setup(t, root)
			sig, hit := Detect(root)
			if !hit {
				t.Fatalf(`应命中让位信号`)
			}
			if sig != c.want {
				t.Errorf(`signal = %q, want %q`, sig, c.want)
			}
		})
	}
}

func TestDetect_NonSignalsDoNotTrigger(t *testing.T) {
	root := t.TempDir()
	// 低置信形态：空 commands 目录、settings.json 无 hooks/permissions 键、
	// .claude 存在但只有无关文件——都不让位（漏判代价 = 多问一次，可接受）。
	os.MkdirAll(filepath.Join(root, `.claude`, `commands`), 0755) // 空
	os.MkdirAll(filepath.Join(root, `.claude`), 0755)
	os.WriteFile(filepath.Join(root, `.claude`, `settings.json`), []byte(`{"model": "x"}`), 0644)
	if sig, hit := Detect(root); hit {
		t.Fatalf(`低置信形态误判让位: %q`, sig)
	}
}
