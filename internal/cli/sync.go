package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hookdispatch"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/skillgen"
	"github.com/spf13/cobra"
)

// autoSync 确保 forge 资产与当前 binary version 一致。除 init 外每条命令前都跑。
// user-level-assets 重构后全部资产在用户级（规则见上）。
func autoSync(dir string, binaryVersion string, force bool) error {
	// plugin 已 user-level 装时，项目级 hook 重复（遗留 settings.local.json）+
	// 旧 .mcp.json forge server 残留在此清理；幂等，版本相等跳过路径也会触发。
	defer dedupeProjectLevelIfPlugin(dir)

	// .sync-version 戳（现在在 DataDir）判 no-op。三种情况版本相等也强制 sync：
	// force flag / stamp 缺失或不匹配 / 遗留项目 settings.local.json 内 stale hook 绑定。
	dataDir := forgedata.DataDirFor(dir)
	stampPath := filepath.Join(dataDir, ".sync-version")
	if !force && binaryVersion != "dev" {
		if stamp, err := os.ReadFile(stampPath); err == nil &&
			strings.TrimSpace(string(stamp)) == binaryVersion &&
			!settingsHasStaleBinding(dir) {
			return nil
		}
	}

	// 1. sync hook 脚本参考副本（运行时执行嵌入内容，副本仅供查看）。deploy stamp
	//    必须先落盘：file-sentinel 的 CONFIG 分支只在它的 grace 窗口内豁免
	//    .forge/hooks/ drift——没有它，被监控 Bash 命令 hook 链上的 forge 子进程
	//    重写 hooks 会被当未授权改写 quarantine（2026-08-02 事故）。stamp 失败仅
	//    告警：缺失只退回事故前的严格 quarantine，不阻塞 sync。
	if err := hooks.WriteHookDeployStamp(dataDir, hookdispatch.ProjectTagFor(dir)); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to write hook deploy stamp: %v\n", err)
	}
	if err := hooks.WriteHookTemplates(dataDir); err != nil {
		return fmt.Errorf("auto-sync: failed to update hooks: %w", err)
	}

	// 2. 用户级 claude settings.json——仅在 plugin 未 user-level 安装时
	//    （plugin.json 已全机器注册 ForgeHookSpec）。
	if !hooks.IsClaudePluginInstalled() {
		if err := hooks.GenerateUserSettings(); err != nil {
			return fmt.Errorf("auto-sync: failed to update user-level settings: %w", err)
		}
	}

	// 3. 确保 protocol.yml 存在（缺失则从默认值创建；项目级覆盖恒优先，绝不动；
	//    损坏文件先备份到一边再写默认——绝不静默覆盖）
	if err := protocol.EnsureDefault(dir); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to ensure protocol.yml: %v\n", err)
	}
	proto, err := protocol.Load(dir)
	if err != nil {
		proto = protocol.DefaultProtocol()
	}

	// 4. 同步用户级 quality SKILL.md
	if err := skillgen.GenerateUserQualitySkill(proto); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to regenerate quality skill: %v\n", err)
	}

	// 5. 清理重构残留：废弃 skill + runtime state 迁 DataDir + 死文件 +
	//    遗留项目级 forge 写入
	cleanupDeprecatedPipelineSkill(dir)
	migrateRuntimeResidue(dir)
	cleanupLegacyDeadFiles(dir)
	stripProjectLevelForgeAssets(dir)

	// 6. 更新用户级 CLAUDE.md / AGENTS.md 的 forge 段（备份+追加）
	if err := skillgen.GenerateUserClaudeMD(); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to update user-level CLAUDE.md: %v\n", err)
	}
	if err := skillgen.GenerateUserAgentsMD(); err != nil {
		fmt.Fprintf(os.Stderr, "auto-sync warning: failed to update user-level AGENTS.md: %v\n", err)
	}

	// 7. sync agent bridge（用户级，为所有检测到的 agent）
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

	// 8. 更新 .sync-version 戳
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
// Project 构造失败（git 元数据损坏等推导错误）静默返回，autoSync 不阻塞；
// 非 git 项目经 PathKey 正常迁移（DataDir 始终用户级）。
func migrateRuntimeResidue(dir string) {
	p, err := forgedata.ProjectFor(dir)
	if err != nil {
		return
	}
	res, err := forgedata.MigrateProject(p, forgedata.MigrateOptions{})
	// 信任边界（2026-08-15）：刚从可提交 .forge/ 提升的 task 文件携带攻击者可书写的门禁/信任
	// 信号——落地即刻剥离（见 migrate_sanitize.go）。MigrateProject 带部分结果返回 error 时
	// （tasks 已迁移，后续条目失败）同样必须清洗——无论本次运行怎么结束，已提升的文件都在
	// 受信 DataDir 里，而重跑会 SKIP 该次迁移且永不清洗。仅当本次 run 实际 MOVE 了 tasks 目录
	// 才触发；skip 路径（DataDir 已有 tasks）没有引入任何外来内容。
	var sanErr error
	if res != nil {
		_, sanErr = sanitizeAfterMigration(p.Root, res.Moved)
	}
	if err != nil {
		// 部分迁移路径：tasks 目录刚搬完、清洗也可能随即失败——只告警迁移错误会把清洗告警
		// 吞掉（pending 标记保其可重试，但用户必须看见；2026-08-16 复审）。两个都报。
		fmt.Fprintf(os.Stderr, "auto-sync warning: migrate runtime residue: %v\n", err)
		if sanErr != nil {
			fmt.Fprintf(os.Stderr, "auto-sync warning: 迁移了 tasks 但外来门禁信号清洗也失败（hostile task 状态可能在 DataDir）: %v\n"+
				"  修复根因后运行 forge migrate 重试清洗（pending 标记已记录，后续 autoSync 也会自动重试）\n", sanErr)
		}
		return
	}
	if sanErr != nil {
		// autoSync 不能因清洗失败把每条 forge 命令都砖死，但也不能沉默（2026-08-15 审查的
		// fail-closed 精神）：响亮 stderr + 明确修复指引。sanitizeAfterMigration 已留下
		// pending 标记，之后的 migrate/autoSync 即便 tasks 迁移本身会 SKIP（dst 已存在）也
		// 会自动重试清洗。
		fmt.Fprintf(os.Stderr, "auto-sync warning: 迁移了 tasks 但外来门禁信号清洗失败（hostile task 状态可能在 DataDir）: %v\n"+
			"  修复根因后运行 forge migrate 重试清洗（pending 标记已记录，后续 autoSync 也会自动重试）\n", sanErr)
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

// fileExists 报告路径是否存在（Stat 无错）。小 helper 让 cleanup 循环可读，
// 无需重复 os.Stat + nil-check 样板。
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

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
	Short: "同步 forge 资产到当前二进制版本（用户级）",
	Long: `手动触发 forge 资产自动同步：用户级 hooks / settings / SKILL.md / CLAUDE.md + 存量项目级残留清理。

每次 forge 命令前已自动同步（版本变化或检测到脏绑定时触发）。此命令用于：
  - 升级后强制刷新全部资产
  - 遗留项目级配置被旧版本污染（如 task-verify 误绑 PostToolUse）时手动修复

--force 跳过版本检查，无条件重生成。

注意：本命令同步的是 forge 自身资产（版本升级），不迁移项目数据——跨机器
导出/导入用 forge project export/import（见 forge project --help）。`,
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
