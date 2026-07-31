package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runSystemStatus() error {
	home, _ := os.UserHomeDir()
	errors := 0
	warnings := 0

	fmt.Println("=== System Health Check ===")
	fmt.Println()

	checkGlobalForge(home, &errors, &warnings)
	checkLarkCLI(&errors, &warnings)
	checkForgeInPath(&errors, &warnings)
	checkOrphanHooks(home, &errors, &warnings)
	checkSkillsManifest(home, &errors, &warnings)

	fmt.Println()
	fmt.Printf("Results: %d error(s), %d warning(s)\n", errors, warnings)

	if errors > 0 {
		return fmt.Errorf("system health check failed with %d error(s)", errors)
	}
	return nil
}

func checkGlobalForge(home string, errors, warnings *int) {
	forgeDir := filepath.Join(home, ".forge")
	if _, err := os.Stat(forgeDir); os.IsNotExist(err) {
		fmt.Println("  ~/.forge/ not found — run 'forge init' in a project first")
		*errors++
		return
	}
	fmt.Println("  ~/.forge/ exists")

	// Real layout after refactor-data-home: runtime state lives in
	// ~/.forge/projects/<key>/ (per-project DataDir); embedded skills unpack to
	// ~/.forge/skills-cache/. The previously checked dirs (pipeline-templates/
	// hooks/bin) were never created by any code path — every run reported 3
	// unfixable warnings.
	//
	// refactor-data-home 后的真实布局：runtime state 在 ~/.forge/projects/<key>/
	// （per-project DataDir）；embedded skill 解包到 ~/.forge/skills-cache/。
	// 之前检查的目录（pipeline-templates/hooks/bin）无任何代码创建——每次运行
	// 都报 3 条永远修不好的 warning。
	for _, sub := range []struct {
		name string
		hint string
	}{
		{"projects", "run 'forge task start' in a project to create its runtime-state dir"},
		{"skills-cache", "run 'forge skills install' to unpack the embedded skill library"},
	} {
		path := filepath.Join(forgeDir, sub.name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf("  ~/.forge/%s/ missing — %s\n", sub.name, sub.hint)
			*warnings++
		}
	}
}

func checkLarkCLI(errors, warnings *int) {
	if _, err := exec.LookPath("lark-cli"); err != nil {
		fmt.Println("  lark-cli not in PATH — feishu auto-publish disabled")
		*warnings++
	} else {
		fmt.Println("  lark-cli available")
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
		// Same policy as settings.json above: an unreadable hooks dir is a
		// warning, not silently zero orphans.
		//
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

// checkSkillsManifest checks ~/.forge/skills-manifest.json (snapshot from the last forge skills install).
//
// checkSkillsManifest 检查 ~/.forge/skills-manifest.json（上次 forge skills install 的快照）。
func checkSkillsManifest(home string, errors, warnings *int) {
	mfPath := filepath.Join(home, ".forge", "skills-manifest.json")
	data, err := os.ReadFile(mfPath)
	if err != nil {
		fmt.Println("  ~/.forge/skills-manifest.json not found — run 'forge skills install' to distribute skill library")
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
		fmt.Printf("  ~/.forge/skills-manifest.json corrupt: %v\n", err)
		*errors++
		return
	}
	fmt.Printf("  Skills manifest: %d skill (%d pass), canonical=%s, generated %s\n",
		m.Stats.Total, m.Stats.Pass, m.CanonicalRoot, m.GeneratedAt)
}
