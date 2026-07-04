package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/pipeline"
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
//   - .claude/skills/forge-pipeline/SKILL.md → always regenerate from pipeline.yml
//   - .claude/skills/forge-quality/SKILL.md → always regenerate from protocol.yml
//   - .claude/CLAUDE.md → update Forge-managed section
//   - .forge/protocol.yml → create from defaults if missing, never overwrite
//   - .forge/pipeline.yml → never touched (user may have customized)
//   - .forge/state.json → only update last_sync_version
func autoSync(dir string, binaryVersion string, force bool) error {
	state, stateErr := pipeline.LoadState(dir)

	// Decide whether this is a no-op. Version-equal + non-dev + clean → skip.
	// Three conditions force a sync even when versions match:
	//  1. force flag (explicit `forge sync --force`)
	//  2. state.json missing/corrupt — old code returned nil here, which left
	//     stale settings.local.json (e.g. legacy PostToolUse matchers) unhealed
	//     forever. Fall through to regenerate the state-independent artifacts.
	//  3. settings.local.json carries a stale hook binding from an older Forge
	//     version (task-verify mis-bound to PostToolUse with a wide matcher,
	//     firing on nearly every tool call and producing runaway invocations
	//     in real heavy-use projects).
	if !force && stateErr == nil && state != nil &&
		state.LastSyncVersion == binaryVersion && binaryVersion != "dev" &&
		!settingsHasStaleBinding(dir) {
		return nil
	}

	forgeDir := filepath.Join(dir, ".forge")

	// 1. Sync hook scripts
	if err := hooks.WriteHookTemplates(forgeDir); err != nil {
		return fmt.Errorf("auto-sync: failed to update hooks: %w", err)
	}

	// 2. Sync settings.local.json
	if err := hooks.GenerateSettings(dir); err != nil {
		return fmt.Errorf("auto-sync: failed to update settings: %w", err)
	}

	// Without a usable state (missing/corrupt state.json) we can't safely run
	// the state-dependent steps below (mode-based protocol defaults,
	// snapshot-inferred gates, last_sync_version write). Heal the
	// state-independent artifacts — hooks + settings, the ones that carry stale
	// hook bindings — and stop here rather than guessing mode.
	if state == nil {
		return nil
	}

	// 3. Ensure protocol.yml exists (create from defaults if missing)
	proto, err := protocol.Load(dir)
	if err != nil {
		// protocol.yml missing — create from defaults using pipeline mode
		proto = protocol.DefaultProtocol(state.Mode)
		if err := protocol.Save(dir, proto); err != nil {
			fmt.Fprintf(os.Stderr, "auto-sync warning: failed to create protocol.yml: %v\n", err)
		}
	}

	// 5. Sync pipeline SKILL.md
	p, err := pipeline.Load(dir)
	if err == nil {
		var inferredIDs []string
		if state.Snapshot != nil {
			inferredIDs = state.Snapshot.InferredGates
		}
		if err := skillgen.GenerateSkill(dir, p, inferredIDs); err != nil {
			fmt.Fprintf(os.Stderr, "auto-sync warning: failed to regenerate skill: %v\n", err)
		}

		// 6. Sync quality SKILL.md
		if err := skillgen.GenerateQualitySkill(dir, proto, p); err != nil {
			fmt.Fprintf(os.Stderr, "auto-sync warning: failed to regenerate quality skill: %v\n", err)
		}
	}

	// 7. Update CLAUDE.md
	if err := skillgen.GenerateClaudeMD(dir); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to update CLAUDE.md: %v\n", err)
	}

	// 7b. Update project-root AGENTS.md (cross-agent instruction source)
	if err := skillgen.GenerateAgentsMD(dir); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to update AGENTS.md: %v\n", err)
	}

	// 8. Sync agent bridge (translate for all detected agents)
	agents := agentbridge.DetectAgents(dir)
	if len(agents) > 0 {
		bridgeInput := &agentbridge.TranslationInput{
			Protocol:  proto,
			Pipeline:  p,
			HookNames: hooks.HookNames(),
		}
		if errs := agentbridge.TranslateForAgents(dir, agents, bridgeInput); len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "auto-sync warning: agent bridge: %v\n", e)
			}
		}
	}

	// 9. Update last_sync_version
	state.LastSyncVersion = binaryVersion
	if err := state.Save(dir); err != nil {
		return fmt.Errorf("auto-sync: failed to update state: %w", err)
	}

	return nil
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
