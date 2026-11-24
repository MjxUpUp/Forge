package ci

// 本测试守护"发版/CI 链不调用已删命令"。core-only（4.0）删掉了 20+ 个命令组，
// 但 workflow 里的 run: 步骤不会编译报错——它只会在 CI 里 runtime 失败，或者在
// 最坏情况下（退出码被忽略）静默空跑。这里把"workflow 引用了已删命令"变成
// 编译期就能发现的失败。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// removedForgeCommands 是 core-only 之后不再存在的顶层命令组。
// 新增删除时同步这里：workflow 里再出现它们即红。
var removedForgeCommands = []string{
	"act", "bridge", "config", "conventions", "dashboard", "enforcement",
	"eval", "gate", "harness", "health", "migrate", "node", "project",
	"registry", "selfcheck", "skill", "skills", "trace", "trust", "update",
	"verify", "workspace", "worktree",
}

var removedForgeCommandRe = regexp.MustCompile(
	`(?:^|[\s/])forge\s+(` + strings.Join(removedForgeCommands, "|") + `)\b`)

// TestWorkflowsUseOnlyLiveCommands 断言 .github/workflows/*.yml 的每一行
// 非注释内容都不调用已删命令。注释里提到旧命令是允许的（那是历史说明）。
func TestWorkflowsUseOnlyLiveCommands(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatalf("列 workflow 失败: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("没找到 .github/workflows/*.yml——测试路径假设失效")
	}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", p, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			m := removedForgeCommandRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			t.Errorf("%s:%d 调用了已随 core-only 删除的命令 %q: %s", p, i+1, "forge "+m[1], trimmed)
		}
	}
}
