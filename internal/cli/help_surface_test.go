package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 命令面收敛的钉子：帮助文本不得再回到 hook 时代（阶段 B–D 删掉宿主层）。
// 这些字符串是 agent 读到的说明书，改动等于改行为。
func TestHelpSurface_NoHostLayerWording(t *testing.T) {
	if containsAny(rootCmd.Long, "初始化管道", "forge hook", "Stop hook") {
		t.Errorf("root 帮助仍含宿主层措辞:\n%s", rootCmd.Long)
	}
	if containsAny(reviewCmd.Long, "Stop hook") {
		t.Errorf("review 帮助仍说 Stop hook:\n%s", reviewCmd.Long)
	}
	if containsAny(reviewGateCmd.Short, "Stop hook") {
		t.Errorf("review gate Short 仍说 Stop hook: %s", reviewGateCmd.Short)
	}
}

func TestHelpSurface_ContractFirstShort(t *testing.T) {
	if verifyCmd.Short != "检查仓库契约（spec/forge.yaml）并按需跑回归" {
		t.Errorf("verify Short = %q", verifyCmd.Short)
	}
	if configCmd.Short != "兼容基线声明（compat）" {
		t.Errorf("config Short = %q", configCmd.Short)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// TestHelpSurface_CliReferenceContractWording 钉住 docs/reference/cli.md 的接线措辞：
// 阶段 D 后"hook 是否生效"已不成立，reference 必须说"本地接线"。放在 TestHelpSurface_
// 前缀下，让 change 的 #5 判据同时覆盖帮助面与 reference（见 acceptance.md）。
func TestHelpSurface_CliReferenceContractWording(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot, "docs", "reference", "cli.md"))
	if err != nil {
		t.Fatalf("read cli.md: %v", err)
	}
	if !strings.Contains(string(body), "不依赖本地接线是否生效") {
		t.Fatalf("docs/reference/cli.md 应写「不依赖本地接线是否生效」")
	}
	if strings.Contains(string(body), "不依赖本地 hook 是否生效") {
		t.Fatalf("docs/reference/cli.md 仍有 hook 时代措辞")
	}
}
