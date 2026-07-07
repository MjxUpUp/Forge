package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/skillgen"
	"github.com/spf13/cobra"
)

// autoSync ensures .forge/ files (hooks, settings, SKILL.md) match the current
// binary version. It runs before every command except init.
//
// Sync rules:
//   - .forge/hooks/*.sh  → always overwrite with embedded templates
//   - .claude/settings.local.json → always regenerate
//   - .claude/skills/forge-quality/SKILL.md → always regenerate from protocol.yml
//   - .claude/CLAUDE.md → update Forge-managed section
//   - .forge/protocol.yml → create from defaults if missing, never overwrite
//   - .forge/.sync-version → stamp current binary version (no-op detection)
func autoSync(dir string, binaryVersion string, force bool) error {
	// plugin 已 user-level 装时,本函数写入的 project-level hooks（GenerateSettings）+
	// MCP（Translate writeClaudeMCP）是冗余的,defer 在所有 return 路径末尾统一清理。
	// 幂等:无重复时 no-op,version-equal 跳过路径也会触发（正好覆盖"plugin 在上次 sync
	// 后才装"的迁移场景）。
	defer dedupeProjectLevelIfPlugin(dir)

	// .sync-version stamp 判 no-op（取代已删除的 state.LastSyncVersion——项目级管道
	// 删除后 state.json 不再生成）。Three conditions force a sync even when versions
	// match: force flag / stamp missing-or-mismatch / stale hook binding in
	// settings.local.json (legacy task-verify mis-bound to PostToolUse).
	stampPath := filepath.Join(dir, ".forge", ".sync-version")
	if !force && binaryVersion != "dev" {
		if stamp, err := os.ReadFile(stampPath); err == nil &&
			strings.TrimSpace(string(stamp)) == binaryVersion &&
			!settingsHasStaleBinding(dir) {
			return nil
		}
	}

	forgeDir := filepath.Join(dir, ".forge")

	// 1. Sync hook scripts
	if err := hooks.WriteHookTemplates(forgeDir); err != nil {
		return fmt.Errorf("auto-sync: failed to update hooks: %w", err)
	}

	// 2. Sync settings.local.json — only when plugin is NOT user-level installed.
	//    When plugin IS installed, user-level plugin.json already registers
	//    ForgeHookSpec machine-wide; writing project-level hooks is redundant and
	//    creates a fragile "write then immediately strip" pattern where any
	//    interruption between GenerateSettings and the deferred
	//    dedupeProjectLevelIfPlugin leaves the file corrupted. dedupeProjectLevelIfPlugin
	//    still runs via defer to clean up legacy hooks from older forge versions.
	if !hooks.IsClaudePluginInstalled() {
		if err := hooks.GenerateSettings(dir); err != nil {
			return fmt.Errorf("auto-sync: failed to update settings: %w", err)
		}
	}

	// 3. Ensure protocol.yml exists (create from defaults if missing)
	proto, err := protocol.Load(dir)
	if err != nil {
		proto = protocol.DefaultProtocol()
		if err := protocol.Save(dir, proto); err != nil {
			fmt.Fprintf(os.Stderr, "auto-sync warning: failed to create protocol.yml: %v\n", err)
		}
	}

	// 4. Sync quality SKILL.md
	if err := skillgen.GenerateQualitySkill(dir, proto); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to regenerate quality skill: %v\n", err)
	}

	// 5. Clean up 废弃 forge-pipeline skill（项目级管道删除后老版本残留）
	cleanupDeprecatedPipelineSkill(dir)

	// 6. Update CLAUDE.md
	if err := skillgen.GenerateClaudeMD(dir); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to update CLAUDE.md: %v\n", err)
	}

	// 7. Update project-root AGENTS.md (cross-agent instruction source)
	if err := skillgen.GenerateAgentsMD(dir); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to update AGENTS.md: %v\n", err)
	}

	// 8. Sync agent bridge (translate for all detected agents)
	agents := agentbridge.DetectAgents(dir)
	if len(agents) > 0 {
		bridgeInput := &agentbridge.TranslationInput{
			Protocol:  proto,
			HookNames: hooks.HookNames(),
		}
		if errs := agentbridge.TranslateForAgents(dir, agents, bridgeInput); len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "auto-sync warning: agent bridge: %v\n", e)
			}
		}
	}

	// 9. Update .sync-version stamp
	if err := os.WriteFile(stampPath, []byte(binaryVersion), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to write sync stamp: %v\n", err)
	}

	return nil
}

// cleanupDeprecatedPipelineSkill 删除已废弃的 forge-pipeline skill 目录。项目级管道
// 删除后，老版本 forge 生成的 .claude/skills/forge-pipeline/ 与 .agents/skills/forge-pipeline/
// 残留会让 agent 读到过时内容（描述已删除的 forge gate/pipeline 命令）。autoSync 每次
// 调用清理，幂等。生成器 generator.go 已删，不会再生成。
func cleanupDeprecatedPipelineSkill(dir string) {
	for _, p := range []string{
		filepath.Join(dir, ".claude", "skills", "forge-pipeline"),
		filepath.Join(dir, ".agents", "skills", "forge-pipeline"),
	} {
		if _, err := os.Stat(p); err == nil {
			os.RemoveAll(p)
		}
	}
}

// settingsHasStaleBinding reports whether .claude/settings.local.json binds a
// Forge hook to an event the current generator no longer emits — the signature
// of a settings file left over from an older Forge version. The concrete case:
// task-verify was once bound under PostToolUse with a wide matcher
// (Bash|Read|Glob|Skill|Agent), firing on nearly every tool call and — without
// the hook's own throttle — producing runaway invocations (100+/session) in
// real heavy-use projects. The current generator binds task-verify only under
// Stop, so any binding for it elsewhere is stale and must be regenerated away.
func settingsHasStaleBinding(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		return false
	}
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false
	}
	// task-verify must live only under Stop. A binding under any other event
	// (PostToolUse/PreToolUse) is a legacy leftover that re-triggers it per
	// tool call.
	for event, matchers := range cfg.Hooks {
		if event == "Stop" {
			continue
		}
		for _, m := range matchers {
			for _, h := range m.Hooks {
				if strings.Contains(h.Command, "task-verify") {
					return true
				}
			}
		}
	}
	return false
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().Bool("force", false, "强制重生成 .forge/ 资产，跳过版本检查")
}

var syncCmd = &cobra.Command{
	Use:   "sync [--force]",
	Short: "同步 .forge/ 资产到当前二进制版本",
	Long: `手动触发 .forge/ 自动同步：hooks / settings.local.json / SKILL.md / CLAUDE.md。

每次 forge 命令前已自动同步（版本变化或检测到脏绑定时触发）。此命令用于：
  - 升级后强制刷新全部资产
  - settings.local.json 被旧版本污染（如 task-verify 误绑 PostToolUse）时手动修复

--force 跳过版本检查，无条件重生成。`,
	RunE: runSync,
}

func runSync(cmd *cobra.Command, args []string) error {
	dir, err := findProjectRoot()
	if err != nil {
		return err
	}
	force, _ := cmd.Flags().GetBool("force")
	if err := autoSync(dir, rootCmd.Version, force); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "forge assets synced.")
	return nil
}
