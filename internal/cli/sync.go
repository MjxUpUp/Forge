package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/skillgen"
	"github.com/spf13/cobra"
)

// autoSync keeps .forge/ files (hooks/settings/SKILL.md) in sync with the
// current binary version. Runs before every command except init.
//
// sync rules:
//   - .forge/hooks/*.sh  → always overwritten from embed templates
//   - .claude/settings.local.json → always regenerated
//   - .claude/skills/forge-quality/SKILL.md → always regenerated from protocol.yml
//   - .claude/CLAUDE.md → update Forge-managed sections
//   - .forge/protocol.yml → created from defaults if missing, never overwritten
//   - .forge/.sync-version → stamped with the current binary version (no-op detection)
//
// autoSync 确保 .forge/ 文件（hooks/settings/SKILL.md）与当前 binary version 一致。
// 除 init 外每条命令前都跑。
//
// sync 规则：
//   - .forge/hooks/*.sh  → 始终用 embed 模板覆盖
//   - .claude/settings.local.json → 始终重生
//   - .claude/skills/forge-quality/SKILL.md → 始终从 protocol.yml 重生
//   - .claude/CLAUDE.md → 更新 Forge 管理的 section
//   - .forge/protocol.yml → 缺失则从默认值创建，绝不覆盖
//   - .forge/.sync-version → 盖当前 binary version 戳（no-op 检测）
func autoSync(dir string, binaryVersion string, force bool) error {
	// When the plugin is installed at user level, the project-level hooks
	// (GenerateSettings) written by this function are redundant (plus the
	// forge-server residue in legacy .mcp.json of old projects, cleaned by
	// StripForgeMCPServer for historical init/sync of old projects). defer
	// consolidates cleanup at the end of every return path. Idempotent: no-op
	// when nothing duplicates, and the version-equal skip path also triggers it
	// (covers the migration case where the plugin was installed after the last
	// sync).
	//
	// plugin 已 user-level 装时,本函数写入的 project-level hooks（GenerateSettings）是冗余的
	// （+ 旧项目 .mcp.json 的 forge server 残留,StripForgeMCPServer 清历史 init/sync 旧项目）,
	// defer 在所有 return 路径末尾统一清理。幂等:无重复时 no-op,version-equal 跳过路径也会
	// 触发（正好覆盖"plugin 在上次 sync 后才装"的迁移场景）。
	defer dedupeProjectLevelIfPlugin(dir)

	// The .sync-version stamp drives no-op detection (replacing the deleted
	// state.LastSyncVersion—state.json is no longer generated after project-level
	// pipeline removal). Three conditions force a sync even when versions match:
	// the force flag / stamp missing or mismatched / stale hook binding inside
	// settings.local.json (legacy task-verify wrongly bound to PostToolUse).
	//
	// .sync-version stamp 判 no-op（取代已删除的 state.LastSyncVersion——项目级管道
	// 删除后 state.json 不再生成）。Three conditions force a sync even when versions
	// match：force flag / stamp 缺失或不匹配 / settings.local.json 内 stale hook binding
	// （旧版 task-verify 误绑 PostToolUse）。
	stampPath := filepath.Join(dir, ".forge", ".sync-version")
	if !force && binaryVersion != "dev" {
		if stamp, err := os.ReadFile(stampPath); err == nil &&
			strings.TrimSpace(string(stamp)) == binaryVersion &&
			!settingsHasStaleBinding(dir) {
			return nil
		}
	}

	forgeDir := filepath.Join(dir, ".forge")

	// 1. sync hook scripts
	//
	// 1. sync hook 脚本
	if err := hooks.WriteHookTemplates(forgeDir); err != nil {
		return fmt.Errorf("auto-sync: failed to update hooks: %w", err)
	}

	// 2. Sync settings.local.json—only when the plugin is not user-level
	//    installed. When the plugin is installed, user-level plugin.json
	//    already registers ForgeHookSpec machine-wide; writing project-level
	//    hooks is redundant and creates a fragile write-then-immediately-strip
	//    pattern—any interruption between GenerateSettings and the deferred
	//    dedupeProjectLevelIfPlugin leaves a corrupted file.
	//    dedupeProjectLevelIfPlugin still runs via defer to clean legacy hooks
	//    from older forge versions.
	//
	// 2. sync settings.local.json——仅在 plugin 未 user-level 安装时。
	//    plugin 已装时，user-level plugin.json 已全机注册 ForgeHookSpec；写 project-level
	//    hooks 冗余且制造脆弱的「先写后立即 strip」模式——GenerateSettings 与 defer
	//    dedupeProjectLevelIfPlugin 之间任何中断都会留下损坏文件。dedupeProjectLevelIfPlugin
	//    仍经 defer 跑以清旧版 forge 的 legacy hooks。
	if !hooks.IsClaudePluginInstalled() {
		if err := hooks.GenerateSettings(dir); err != nil {
			return fmt.Errorf("auto-sync: failed to update settings: %w", err)
		}
	}

	// 3. Ensure protocol.yml exists (create from defaults if missing)
	//
	// 3. 确保 protocol.yml 存在（缺失则从默认值创建）
	proto, err := protocol.Load(dir)
	if err != nil {
		proto = protocol.DefaultProtocol()
		if err := protocol.Save(dir, proto); err != nil {
			fmt.Fprintf(os.Stderr, "auto-sync warning: failed to create protocol.yml: %v\n", err)
		}
	}

	// 4. Sync the quality SKILL.md
	//
	// 4. 同步 quality SKILL.md
	if err := skillgen.GenerateQualitySkill(dir, proto); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to regenerate quality skill: %v\n", err)
	}

	// 5. Clean up refactor residue: deprecated skills + runtime-state migration to DataDir + dead files
	//
	// 5. Clean up refactor 残留:废弃 skill + runtime state 迁 DataDir + 死文件
	cleanupDeprecatedPipelineSkill(dir)
	migrateRuntimeResidue(dir)
	cleanupLegacyDeadFiles(dir)

	// 6. Update CLAUDE.md
	//
	// 6. 更新 CLAUDE.md
	if err := skillgen.GenerateClaudeMD(dir); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to update CLAUDE.md: %v\n", err)
	}

	// 7. Update the project-root AGENTS.md (cross-agent instruction source)
	//
	// 7. 更新 project-root AGENTS.md（跨 agent 指令源）
	if err := skillgen.GenerateAgentsMD(dir); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to update AGENTS.md: %v\n", err)
	}

	// 8. Sync the agent bridge (translate for every detected agent)
	//
	// 8. sync agent bridge（为所有检测到的 agent 翻译）
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

	// 9. Update the .sync-version stamp
	//
	// 9. 更新 .sync-version 戳
	if err := os.WriteFile(stampPath, []byte(binaryVersion), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to write sync stamp: %v\n", err)
	}

	return nil
}

// cleanupDeprecatedPipelineSkill removes the deprecated forge-pipeline skill
// directory. After the project-level pipeline was removed, the
// .claude/skills/forge-pipeline/ and .agents/skills/forge-pipeline/ left by
// older forge versions still let agents read stale content (describing the
// deleted forge gate/pipeline commands). autoSync cleans them on every call,
// idempotent. The generator generator.go is deleted, so it never regenerates.
//
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

// migrateRuntimeResidue moves runtime-state residue left under .forge/ by
// older versions into DataDir. refactor-data-home relocated runtime state
// from project-level .forge/ to user-level DataDir, but residue accumulated
// by older versions (checklog archives / tasks / gates / sessions / experience
// etc.) is not moved automatically—after upgrade, hundreds of runtime files
// still pile up under .forge/. autoSync runs this once per version change
// (overall no-op when .sync-version is equal), idempotent (already-migrated
// entries are left alone), slimming .forge/ back to a pure config directory.
//
// It reuses the forgedata.MigrateProject whitelist (runtimeDirs / runtimeFiles
// / runtimeGlobs). Default semantics: when DataDir already has the same name,
// skip and keep src (no overwrite, no data loss); when DataDir does not, move
// the whole tree there (the .forge/ copy disappears but DataDir has the copy,
// equivalent to migration). RemoveSrcOnConflict is deliberately not
// introduced—to prevent old quarantine data on the skip path (user-code copies
// isolated by file-sentinel) from being overwritten and lost. Project
// construction failure (non-git project / missing .forge) returns silently,
// so autoSync is not blocked.
//
// migrateRuntimeResidue 把 .forge/ 下老版本积累的 runtime state 残留迁到 DataDir。
// refactor-data-home 把 runtime state 从项目级 .forge/ 迁到用户级 DataDir，但老版本
// 已积累的残留（checklog 归档/tasks/gates/sessions/experience 等）不会自动搬——升级后
// .forge/ 里仍堆着几百个 runtime 文件。autoSync 每次版本变化时跑一次（.sync-version
// 相等时整体 no-op），幂等（已迁的不再动），让 .forge/ 瘦身为纯配置目录。
//
// 复用 forgedata.MigrateProject 的白名单（runtimeDirs/runtimeFiles/runtimeGlobs），
// 默认语义：DataDir 已有同名的 skip 保留 src（不覆盖、不丢数据）；DataDir 没有的整树
// 搬过去（.forge/ 那份消失但 DataDir 已有副本，等同迁移）。不引入 RemoveSrcOnConflict
// ——防止 skip 路径下 quarantine 老隔离数据（file-sentinel 隔离的用户代码副本）被覆盖式删除丢失。
// 构造 Project 失败（非 git 项目/.forge 缺失）静默返回，autoSync 不阻塞。
func migrateRuntimeResidue(dir string) {
	p, err := forgedata.ProjectFor(dir)
	if err != nil {
		return
	}
	if _, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{}); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: migrate runtime residue: %v\n", err)
	}
}

// cleanupLegacyDeadFiles removes dead files left under .forge/ after feature
// removal:
//   - pipeline.yml / state.json: project-level pipeline (forge gate/pipeline)
//     was removed (refactor/remove-project-pipeline); the config + state files
//     generated by older init are no longer read or written by forge.
//   - session-health-*.last: the session-health hook was removed as noise
//     (post v0.22); its throttle stamps (session-scoped <sid> variants) are
//     orphaned, nothing reads or writes them.
//
// autoSync removes them when detected, idempotent. It follows the
// cleanupDeprecatedPipelineSkill pattern (Stat/Glob hit → RemoveAll).
//
// cleanupLegacyDeadFiles 删除功能删除后残留在 .forge/ 的死文件：
//   - pipeline.yml/state.json：项目级管道（forge gate/pipeline）已删（refactor/
//     remove-project-pipeline），老版本 init 生成的配置 + 状态文件，forge 不再读写。
//   - session-health-*.last：session-health hook 已作为噪声移除（v0.22 后），其节流
//     stamp 残留（session-scoped <sid> 变体），无人读写。
//
// autoSync 检测到则删，幂等。沿用 cleanupDeprecatedPipelineSkill 的模式（Stat/Glob
// 命中则 RemoveAll）。
func cleanupLegacyDeadFiles(dir string) {
	forge := filepath.Join(dir, ".forge")
	for _, name := range []string{`pipeline.yml`, `state.json`} {
		if p := filepath.Join(forge, name); fileExists(p) {
			os.RemoveAll(p)
		}
	}
	matches, _ := filepath.Glob(filepath.Join(forge, "session-health-*.last"))
	for _, p := range matches {
		os.RemoveAll(p)
	}
}

// fileExists reports whether the path exists (Stat returns no error). A small
// helper that keeps cleanup loops readable, without repeating the
// os.Stat + nil-check boilerplate.
//
// fileExists 报告路径是否存在（Stat 无错）。小 helper 让 cleanup 循环可读，
// 无需重复 os.Stat + nil-check 样板。
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// settingsHasStaleBinding reports whether .claude/settings.local.json binds
// any Forge hook to an event the current generator no longer emits—this is
// the fingerprint of older Forge settings files. Concrete case: task-verify
// was bound under PostToolUse with a wide matcher (Bash|Read|Glob|Skill|Agent),
// firing on almost every tool call—with no throttling on the hook itself,
// heavy use produced runaway invocations (100+/session). The current
// generator only binds task-verify to Stop, so any binding elsewhere is stale
// and must be cleared by regeneration.
//
// settingsHasStaleBinding 报告 .claude/settings.local.json 是否把某个 Forge hook
// 绑到了当前 generator 不再 emit 的事件——这是旧版 Forge settings 文件的指纹。具体案例：
// task-verify 曾绑在 PostToolUse 下用宽 matcher（Bash|Read|Glob|Skill|Agent），
// 几乎每次工具调用都触发——在 hook 自身 throttle 缺失下，重度使用项目里产生失控调用
// （100+/session）。当前 generator 只把 task-verify 绑到 Stop，因此别处的任何绑定都是
// stale，必须重生清除。
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
	// task-verify may only live under Stop. Bindings under other events
	// (PostToolUse / PreToolUse) are legacy residue that re-fires on every
	// tool call.
	//
	// task-verify 只能在 Stop 下。其他事件（PostToolUse/PreToolUse）下的绑定是 legacy
	// 残留，每次 tool call 都会重复触发。
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
