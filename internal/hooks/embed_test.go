package hooks

import (
	"strings"
	"testing"
)

// TestAutoCompileHook_InjectsCompileFixLoop 钉死 dogfood 4.2：auto-compile hook 触及源码时
// 的 advisory 不只提醒自检编译，还点名 compile-fix-loop skill——编译报错时 agent 不靠自觉
// 调用 skill，hook stdout（AdditionalContext）直接指引加载。复刻 code-review-gate 路径。
func TestAutoCompileHook_InjectsCompileFixLoop(t *testing.T) {
	for _, want := range []string{"compile-fix-loop", "/compile-fix-loop"} {
		if !strings.Contains(AutoCompileHook, want) {
			t.Errorf("AutoCompileHook 应注入 compile-fix-loop skill 指引（含 %q），got:\n%s", want, AutoCompileHook)
		}
	}
}
