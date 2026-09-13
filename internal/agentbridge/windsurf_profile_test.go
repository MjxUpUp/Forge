package agentbridge

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// TestWindsurfWiring_LiteProfileDropsAdvisory 钉死 windsurf 出口过滤的 lite
// 路径（审查 m4）：lite 档下 pre_write_code 组只留白名单内 hook——advisory
// （assertion-check/skill-trigger/conventions-write 等）不进接线；核心
// （freeze-guard/task-guard）保留。镜像测试（standard）钉全量，本测试钉 lite。
func TestWindsurfWiring_LiteProfileDropsAdvisory(t *testing.T) {
	t.Setenv("FORGE_PROFILE", "lite")
	raw := buildWindsurfHooks()["hooks"].(map[string][]windsurfHookEntry)
	preWrite, ok := raw["pre_write_code"]
	if !ok || len(preWrite) == 0 {
		t.Fatal("lite 下 pre_write_code 应仍有核心守卫（task-guard 等）")
	}
	for _, e := range preWrite {
		name := strings.SplitN(e.Command, " ", 4)[2]
		if !hooks.ProfileAllowsHook(name, hooks.ProfileLite) {
			t.Errorf("lite 接线混入白名单外 hook %q", name)
		}
	}
	joined := map[string]bool{}
	for _, e := range preWrite {
		joined[strings.SplitN(e.Command, " ", 4)[2]] = true
	}
	if !joined["task-guard"] || !joined["freeze-guard"] {
		t.Errorf("lite 下 task-guard/freeze-guard 应保留，got %v", joined)
	}
	if joined["assertion-check"] || joined["skill-trigger"] || joined["conventions-write"] {
		t.Errorf("lite 下 advisory hook 应被裁掉，got %v", joined)
	}
}
