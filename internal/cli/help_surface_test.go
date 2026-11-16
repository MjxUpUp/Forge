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

// 入口路由（R0）的钉子：task-first 模型与 change phase/clarify 必须在帮助与
// reference 里说同一件事——help 是 agent 最常读的入口，口径漂移等于行为漂移。
func TestHelpSurface_EntryRouting(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot, "docs", "reference", "cli.md"))
	if err != nil {
		t.Fatalf("read cli.md: %v", err)
	}
	for _, sub := range []string{"change phase", "change clarify", "--kind task", "ready_changes"} {
		if !strings.Contains(string(body), sub) {
			t.Errorf("docs/reference/cli.md 缺入口路由说明：%q", sub)
		}
	}
}

// dogfood 修复的文档钉子：这四句是 agent 照抄的说明书，缺了就会踩同一个坑。
func TestCliReference_DogfoodFixesPinned(t *testing.T) {
	root := repoRoot
	data, err := os.ReadFile(filepath.Join(root, "docs", "reference", "cli.md"))
	if err != nil {
		t.Fatalf("read cli.md: %v", err)
	}
	doc := string(data)
	for _, want := range []string{
		"落后远端先给 `git pull --ff-only`",
		"ready 的 change 直接给 `forge task start --change <id>`",
		"按空白切分后直接 exec",
		"不带 `--ref` 会报 no active task",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("cli.md 缺少 dogfood 修复的说明：%q", want)
		}
	}
}
