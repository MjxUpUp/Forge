package cli

import (
	"encoding/json"
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
	home, _ := os.UserHomeDir()
	errors := 0
	warnings := 0

	fmt.Println("=== System Health Check ===")
	fmt.Println()

	checkGlobalForge(forgeRoot, &errors, &warnings)
	checkForgeInPath(&errors, &warnings)
	checkOrphanHooks(home, &errors, &warnings)
	checkSkillsManifest(forgeRoot, &errors, &warnings)

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
	// （per-project DataDir）；embedded skill 解包到 <root>/skills-cache/。
	// 之前检查的目录（pipeline-templates/hooks/bin）无任何代码创建——每次运行
	// 都报 3 条永远修不好的 warning。
	for _, sub := range []struct {
		name string
		hint string
	}{
		{"projects", "run 'forge task start' in a project to create its runtime-state dir"},
		{"skills-cache", "run 'forge skills install' to unpack the embedded skill library"},
	} {
		path := filepath.Join(forgeRoot, sub.name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf("  %s/ missing — %s\n", path, sub.hint)
			*warnings++
		}
	}
}

func checkForgeInPath(errors, warnings *int) {
	if _, err := exec.LookPath("forge"); err != nil {
		fmt.Println("  forge not in PATH — hooks that call 'forge hook' won't work")
		fmt.Println("     Fix: reinstall forge (npm i -g / go install) or add its install dir to PATH")
		*warnings++
	} else {
		fmt.Println("  forge in PATH")
	}
}

func checkOrphanHooks(home string, errors, warnings *int) {
	hooksDir := filepath.Join(home, ".claude", "hooks")
	if _, err := os.Stat(hooksDir); os.IsNotExist(err) {
		return
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	settingsData, err := os.ReadFile(settingsPath)
	if err != nil {
		*warnings++
		fmt.Println("  ~/.claude/settings.json not found — hooks may be orphaned")
		return
	}
	settingsStr := string(settingsData)

	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		// 与上面 settings.json 同策略：hooks 目录读不了记 warning，不静默当成零 orphan。
		*warnings++
		fmt.Printf("  ~/.claude/hooks/ unreadable — orphan-hook check skipped: %v\n", err)
		return
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sh") {
			continue
		}
		if !strings.Contains(settingsStr, e.Name()) {
			fmt.Printf("  ORPHAN HOOK: %s (in ~/.claude/hooks/ but not in settings.json)\n", e.Name())
			*errors++
		}
	}
}

// checkSkillsManifest 检查全局数据根下的 skills-manifest.json（上次 forge skills
// install 的快照；路径随 forgedata.GlobalHome，受 FORGE_DATA_HOME 覆盖）。
func checkSkillsManifest(forgeRoot string, errors, warnings *int) {
	mfPath := filepath.Join(forgeRoot, "skills-manifest.json")
	data, err := os.ReadFile(mfPath)
	if err != nil {
		fmt.Printf("  %s not found — run 'forge skills install' to distribute skill library\n", mfPath)
		*warnings++
		return
	}
	var m struct {
		GeneratedAt   string `json:"generated_at"`
		CanonicalRoot string `json:"canonical_root"`
		Stats         struct {
			Total int `json:"total"`
			Pass  int `json:"pass"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		fmt.Printf("  %s corrupt: %v\n", mfPath, err)
		*errors++
		return
	}
	fmt.Printf("  Skills manifest: %d skill (%d pass), canonical=%s, generated %s\n",
		m.Stats.Total, m.Stats.Pass, m.CanonicalRoot, m.GeneratedAt)
}
