package agentbridge

import (
	"strings"
	"testing"
)

// TestWindsurfWiring_LiteProfileKeepsBatchRunner：W0.2 batch 推广后，windsurf
// 名册全部收敛为 batch 单入口条目——lite 档下 batch 条目必须保留（它是分派
// 运行器，逐 hook 裁剪由 inner RunHook 档位门生效），advisory per-hook 条目
// 不应存在。
func TestWindsurfWiring_LiteProfileKeepsBatchRunner(t *testing.T) {
	t.Setenv("FORGE_PROFILE", "lite")
	raw := buildWindsurfHooks()["hooks"].(map[string][]windsurfHookEntry)
	total := 0
	for _, entries := range raw {
		for _, e := range entries {
			total++
			if !strings.HasPrefix(e.Command, "forge hook batch --event") {
				t.Errorf("lite 接线应只剩 batch 单入口条目，got %q", e.Command)
			}
		}
	}
	if total == 0 {
		t.Fatal("lite 接线不应为空——batch 分派运行器必须在场")
	}
}
