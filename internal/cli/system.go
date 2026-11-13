package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

func runSystemStatus() error {
	// forge 数据根走单一真相源（FORGE_DATA_HOME 优先，refactor-data-home commit E）；
	// ~/.claude 的孤儿 hook 检查仍需真实用户 home（2026-09 代码普查 R4：原先两处
	// UserHomeDir+".forge" 直拼让 FORGE_DATA_HOME 逃生舱口在本命令失灵）。
	forgeRoot, err := forgedata.GlobalHome()
	if err != nil {
		return fmt.Errorf("system: resolve forge data home: %w", err)
	}
	// ~/.claude 的旧 hook 检查是只读 advisory（缺目录静默跳过），UserHomeDir 失败
	// 忽略之——与 forgeRoot 的显式报错不对称是刻意的：前者只影响一条 warning。
	home, _ := os.UserHomeDir()
	errors := 0
	warnings := 0

	fmt.Println("=== System Health Check ===")
	fmt.Println()

	checkGlobalForge(forgeRoot, &errors, &warnings)
	checkForgeInPath(&warnings)
	checkOrphanHooks(home, &warnings)

	fmt.Println()
	fmt.Printf("Results: %d error(s), %d warning(s)\n", errors, warnings)

	if errors > 0 {
		return fmt.Errorf("system health check failed with %d error(s)", errors)
	}
	return nil
}

// checkGlobalForge 检查 forge 全局数据根（forgedata.GlobalHome 的结果，默认
// ~/.forge，受 FORGE_DATA_HOME 覆盖）。
func checkGlobalForge(forgeRoot string, errors, warnings *int) {
	if _, err := os.Stat(forgeRoot); os.IsNotExist(err) {
		fmt.Printf("  %s not found — run 'forge init' in a project first\n", forgeRoot)
		*errors++
		return
	}
	fmt.Printf("  %s exists\n", forgeRoot)

	// refactor-data-home 后的真实布局：runtime state 在 <root>/projects/<key>/
	// （per-project DataDir）。阶段 D 删掉 skills 分发后 skills-cache 不再由任何命令创建，
	// 不再检查（否则是一条永远修不好的 warning）。
	// 之前检查的目录（pipeline-templates/hooks/bin）无任何代码创建——每次运行
	// 都报 3 条永远修不好的 warning。
	for _, sub := range []struct {
		name string
		hint string
	}{
		{"projects", "run 'forge task start' in a project to create its runtime-state dir"},
	} {
		path := filepath.Join(forgeRoot, sub.name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf("  %s/ missing — %s\n", path, sub.hint)
			*warnings++
		}
	}
}

func checkForgeInPath(warnings *int) {
	if _, err := exec.LookPath("forge"); err != nil {
		fmt.Println("  forge not in PATH — 'forge' commands（task/change/gate 等）在脚本/子进程里无法解析")
		fmt.Println("     Fix: reinstall forge (npm i -g / go install) or add its install dir to PATH")
		*warnings++
	} else {
		fmt.Println("  forge in PATH")
	}
}

// checkOrphanHooks 只读提示 ~/.claude/hooks/ 下未被 settings.json 引用的旧 hook 脚本。
//
// 阶段 D 起 forge 不再安装 hook，也没有 uninstall 清理入口：这些 .sh 只可能来自
// 更早版本的 forge（或用户自己的其他工具）。因此这里只作 advisory——发现即提示
// 可手工清理，绝不计入 error，否则任何装过旧版 forge 的机器上
// `forge status --system` 会恒红，把一个「历史残留」报成「当前故障」。
func checkOrphanHooks(home string, warnings *int) {
	hooksDir := filepath.Join(home, ".claude", "hooks")
	if _, err := os.Stat(hooksDir); os.IsNotExist(err) {
		return
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	settingsData, err := os.ReadFile(settingsPath)
	if err != nil {
		*warnings++
		fmt.Println("  ~/.claude/hooks/ 存在但 settings.json 读不到 — 无法判断旧 hook 是否仍被引用（forge 已不安装 hook，可手工清理该目录）")
		return
	}
	settingsStr := string(settingsData)

	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		// 与上面 settings.json 同策略：hooks 目录读不了记 warning，不静默当成零 orphan。
		*warnings++
		fmt.Printf("  ~/.claude/hooks/ unreadable — old-hook advisory skipped: %v\n", err)
		return
	}
	orphans := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sh") {
			continue
		}
		if !strings.Contains(settingsStr, e.Name()) {
			if orphans == 0 {
				fmt.Println("  advisory: 发现旧版 hook 残留（阶段 D 起 forge 不再安装 hook；仅提示，可手工删除）：")
			}
			fmt.Printf("    ~/.claude/hooks/%s 未被 settings.json 引用\n", e.Name())
			orphans++
		}
	}
	if orphans > 0 {
		*warnings++
	}
}
