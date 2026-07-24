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
	// plugin 已 user-level 装时,本函数写入的 project-level hooks（GenerateSettings）是冗余的
	// （+ 旧项目 .mcp.json 的 forge server 残留,StripForgeMCPServer 清历史 init/sync 旧项目）,
	// defer 在所有 return 路径末尾统一清理。幂等:无重复时 no-op,version-equal 跳过路径也会
	// 触发（正好覆盖"plugin 在上次 sync 后才装"的迁移场景）。
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

	// 5. Clean up refactor 残留:废弃 skill + runtime state 迁 DataDir + 死文件
	cleanupDeprecatedPipelineSkill(dir)
	migrateRuntimeResidue(dir)
	cleanupLegacyDeadFiles(dir)

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

// fileExists reports whether a path exists (Stat no error). Tiny helper to keep
// the cleanup loops readable without repeating os.Stat + nil-check boilerplate.
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
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
