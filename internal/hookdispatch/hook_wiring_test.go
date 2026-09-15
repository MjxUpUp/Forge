package hookdispatch

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// TestHookWiring_EveryNameResolvable 钉住接线完备性：ForgeHookSpec 里引用的
// 每个 hook 名都必须可解析——要么有 bash embed 脚本，要么在 isInProcessHook
// 名册。漏登记的形态是运行时「unknown hook」直接 exit 1（2026-09-15 验收
// 实锤 doc-lint：分发分支存在但入口名册漏登记，v1.61.0 装机接线全死——
// 单测只打分发函数本身，绕过了 RunHook 入口，故全绿）。
func TestHookWiring_EveryNameResolvable(t *testing.T) {
	for _, groups := range hooks.ForgeHookSpec() {
		for _, g := range groups {
			for _, h := range g.Hooks {
				name := strings.TrimPrefix(h.Command, "forge hook ")
				if name == h.Command {
					continue // 非 `forge hook <name>` 形态（如 forge gate），另有契约
				}
				if _, isScript := hooks.EmbeddedContent(name); !isScript && !isInProcessHook(name) {
					t.Errorf("hook %q 已接线但不可解析（无 embed 脚本且不在 isInProcessHook 名册）——运行时会报 unknown hook", name)
				}
			}
		}
	}
}
