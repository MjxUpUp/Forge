package hooks

import (
	"strings"
	"testing"
)

// TestAutoCompileHook_InjectsCompileFixLoop pins dogfood 4.2: when the
// auto-compile hook touches source code, its advisory does more than remind
// the agent to self-check compilation — it names the compile-fix-loop skill.
// When compilation errors occur, rather than relying on the agent to
// voluntarily invoke the skill, the hook stdout (AdditionalContext) directly
// guides loading it. This mirrors the code-review-gate path.
//
// TestAutoCompileHook_InjectsCompileFixLoop 钉死 dogfood 4.2：auto-compile hook 触及源码时
// 的 advisory 不只提醒自检编译，还点名 compile-fix-loop skill——编译报错时 agent 不靠自觉
// 调用 skill，hook stdout（AdditionalContext）直接指引加载。复刻 code-review-gate 路径。
func TestAutoCompileHook_InjectsCompileFixLoop(t *testing.T) {
	if !strings.Contains(AutoCompileHook, "compile-fix-loop") {
		t.Errorf("AutoCompileHook 应注入 compile-fix-loop skill 指引，got:\n%s", AutoCompileHook)
	}
	// Slash-command form (/compile-fix-loop) only exists in Claude Code — for
	// kimi/copilot/codex/zcode it is dead text, so the natural-language "加载
	// <name> skill" form is used instead (valid on every host).
	//
	// slash command 形态（/compile-fix-loop）只在 Claude Code 存在——对
	// kimi/copilot/codex/zcode 是死文本，故改用自然语言「加载 <name> skill」
	// 形态（对所有宿主有效）。
	if strings.Contains(AutoCompileHook, "/compile-fix-loop") {
		t.Errorf("AutoCompileHook 不得使用 Claude-only slash command 形态（/compile-fix-loop），got:\n%s", AutoCompileHook)
	}
}
