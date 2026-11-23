package clitask

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// TestCompleteRequiresRedFirstDeclaration 钉住 B2：行为类改动（含源码扩展名）没有登记
// 先失败证明时 task complete 必须阻断——3.0 里 --accept-red 可选，实测 20 个归档 change
// 只登记过 1 次，「先失败」从机制退化成了自觉。
func TestCompleteRequiresRedFirstDeclaration(t *testing.T) {
	cases := []struct {
		name    string
		st      *tasktypes.TaskState
		changed []string
		wantErr bool
	}{
		{
			name:    "行为类改动未登记先失败证明",
			st:      &tasktypes.TaskState{TaskRef: "feat/x"},
			changed: []string{"internal/cli/review.go"},
			wantErr: true,
		},
		{
			name: "已登记红判据",
			st: &tasktypes.TaskState{TaskRef: "feat/x", RedAttempts: []tasktypes.RedAttempt{
				{Run: "go test ./internal/x -run TestY", Exit: 1},
			}},
			changed: []string{"internal/cli/review.go"},
		},
		{
			name:    "显式豁免留痕",
			st:      &tasktypes.TaskState{TaskRef: "feat/x", RedFirstWaived: "回归修复，行为已被老测试覆盖"},
			changed: []string{"internal/cli/review.go"},
		},
		{
			name:    "纯文档改动不要求",
			st:      &tasktypes.TaskState{TaskRef: "feat/x"},
			changed: []string{"docs/reference/cli.md", "README.md"},
		},
		{
			name:    "空状态",
			changed: []string{"a.go"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckRedFirstDeclared(c.st, c.changed)
			if c.wantErr {
				if err == nil {
					t.Fatal("行为类改动缺先失败证明必须阻断")
				}
				if !strings.Contains(err.Error(), "先失败证明未登记") {
					t.Fatalf("错误信息应指引补登记，got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("不该阻断: %v", err)
			}
		})
	}
}
