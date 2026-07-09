package taskpipeline

import (
	"strings"
	"testing"
)

// TestFormatMissing_InjectsTestDiscipline 钉死 dogfood 4.2/2.1：task-verify 测试缺失 advisory
// 不再教 escape（"To bypass: FORGE_TEST_COVERAGE=disable" 当头条，DevWorkbench 330 次把 disable
// 写进 task plan）。改为注入 test-discipline skill 指引（复刻 code-review-gate hook 驱动路径）。
// escape 降级为末尾审计脚注（env 仍可用，但不再当主指令教学）。
func TestFormatMissing_InjectsTestDiscipline(t *testing.T) {
	// 单文件分支
	single := formatMissing([]string{"internal/foo/bar.go"})
	if !strings.Contains(single, "test-discipline") {
		t.Errorf("formatMissing 应注入 test-discipline skill 指引，got:\n%s", single)
	}
	if !strings.Contains(single, "/test-discipline") {
		t.Errorf("formatMissing 应给出 skill 加载入口（/test-discipline），got:\n%s", single)
	}
	// escape 不再当头条教学指令
	if strings.Contains(single, "To bypass for this task") {
		t.Errorf("escape 教学文案应撤掉（不再当头条），got:\n%s", single)
	}
	// escape env 仍作审计脚注保留（诚实：env 真实存在，不藏）
	if !strings.Contains(single, "FORGE_TEST_COVERAGE=disable") {
		t.Errorf("escape env 应作审计脚注保留（env 真实可用），got:\n%s", single)
	}
	if !strings.Contains(single, "forge trace") {
		t.Errorf("escape 脚注应说明审计可追溯（forge trace），got:\n%s", single)
	}

	// 多文件分支（>5）走另一 format 分支，同样注入
	multi := formatMissing([]string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go"})
	if !strings.Contains(multi, "test-discipline") {
		t.Errorf("多文件分支也应注入 test-discipline，got:\n%s", multi)
	}
	if strings.Contains(multi, "To bypass for this task") {
		t.Errorf("多文件分支也应撤 escape 头条，got:\n%s", multi)
	}
}
