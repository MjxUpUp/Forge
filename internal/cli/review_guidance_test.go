package cli

import (
	"strings"
	"testing"
)

// TestGateGuidance_RoutesToSkills pins dogfood 4.2: the gateGuidance injected by the review-stop hook
// points not only to code-review-gate but also routes to category-A skills by problem type (systematic-debugging /
// compile-fix-loop / test-discipline). This replicates the hook-enforced drive of code-review-gate — bugs found in
// review no longer rely on the agent voluntarily invoking skills; the Stop additionalContext names them directly.
//
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
	// Host-agnostic loading form (2026-08-25 prompt-copy fix): the slash-command
	// form (/code-review-gate) only exists in Claude Code, and a repo-relative
	// skills/... path is a 404 in user projects — both must stay out.
	//
	// 宿主无关的加载形态（2026-08-25 文案修复）：slash command 形态
	// （/code-review-gate）只在 Claude Code 存在，仓库相对 skills/... 路径在
	// 用户项目是 404——两者都不得出现。
	for _, banned := range []string{"/code-review-gate", "skills/code-review-gate/SKILL.md"} {
		if strings.Contains(gateGuidance, banned) {
			t.Errorf("gateGuidance 不得含 Claude-only slash command 或仓库相对路径 %q，got:\n%s", banned, gateGuidance)
		}
	}
}
