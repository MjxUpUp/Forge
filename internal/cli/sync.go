package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Harness/forge/internal/agentbridge"
	"github.com/Harness/forge/internal/hooks"
	"github.com/Harness/forge/internal/pipeline"
	"github.com/Harness/forge/internal/protocol"
	"github.com/Harness/forge/internal/skillgen"
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
func autoSync(dir string, binaryVersion string) error {
	state, err := pipeline.LoadState(dir)
	if err != nil {
		// Can't load state — nothing to sync
		return nil
	}

	// Already synced with this version.
	// Exception: dev builds re-sync every command. The embedded hook templates
	// can change between dev builds, so version-equal would skip re-syncing and
	// leave the on-disk hooks stale (observed: .forge/hooks/ stuck at 8 files
	// while embed.go had 15).
	if state.LastSyncVersion == binaryVersion && binaryVersion != "dev" {
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

	// 3. Ensure tasks directory exists
	os.MkdirAll(filepath.Join(forgeDir, "tasks"), 0755)

	// 4. Ensure protocol.yml exists (create from defaults if missing)
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
