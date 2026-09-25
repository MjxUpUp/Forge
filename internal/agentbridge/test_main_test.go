package agentbridge

import (
	"os"
	"testing"
)

// TestMain 钉包级密闭性：档位默认 standard——防开发机 shell 导出的
// FORGE_PROFILE=lite 泄漏进接线镜像测试造成假红（W0.3；档位相关测试用
// t.Setenv 覆盖本默认）。
func TestMain(m *testing.M) {
	os.Setenv("FORGE_PROFILE", "standard")
	os.Exit(m.Run())
}
