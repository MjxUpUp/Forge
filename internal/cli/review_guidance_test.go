package cli

import (
	"strings"
	"testing"
)

// TestGateGuidance_RoutesToSkills 钉死 dogfood 4.2：review-stop hook 注入的 gateGuidance
// 不只指 code-review-gate，还按问题类型路由到 A 类 skill（systematic-debugging /
// compile-fix-loop / test-discipline）。复刻 code-review-gate 的 hook 强制驱动——审查发现的
// bug 不再靠 agent 自觉调用 skill，而是 Stop additionalContext 直接点名。
func TestGateGuidance_RoutesToSkills(t *testing.T) {
	for _, want := range []string{
		"code-review-gate", // 主指引
		"systematic-debugging",
		"compile-fix-loop",
		"test-discipline",
	} {
		if !strings.Contains(gateGuidance, want) {
			t.Errorf("gateGuidance 应含问题类型路由 skill %q，got:\n%s", want, gateGuidance)
		}
	}
}
