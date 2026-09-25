package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(pluginCmd)
	pluginCmd.AddCommand(pluginPackCmd)
	pluginCmd.AddCommand(pluginStatusCmd)
	pluginCmd.AddCommand(pluginDedupeCmd)
	pluginCmd.AddCommand(pluginKimiManifestCmd)
	pluginDedupeCmd.Flags().Bool("keep-empty", false, "保留 settings.local.json 文件壳（清 forge hooks 后写 {} 而非删）——自动调用（init-suggest SessionStart）传 true,手动 dedupe 不传,删空文件")
	pluginPackCmd.Flags().String("out", "", "输出目录（默认当前目录，即仓库根）")
	pluginKimiManifestCmd.Flags().Bool("write", false, "重写已提交的 .kimi-plugin/plugin.json（默认只打印+报告漂移）")
}

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "plugin marketplace 生成与管理",
}

var pluginPackCmd = &cobra.Command{
	Use:   "pack [--out dir]",
	Short: "生成多 host plugin pack（claude/codex/cursor/copilot/reasonix）",
	Long: `生成一个 plugin pack，让各 AI 编码 agent 通过自己的 plugin marketplace 一键安装 forge。

写入（默认当前目录）：
  .claude-plugin/marketplace.json   claude+codex+copilot 共享读
  .cursor-plugin/marketplace.json   cursor
  plugins/<name>/
    .claude-plugin/plugin.json      claude manifest（hooks = ForgeHookSpec，与 forge init 字节一致）
    skills/<skill>/...              内嵌 canonical skill 库（每 skill 一目录，claude plugin 按约定加载）
    reasonix-plugin.json            reasonix native manifest（flat hooks；Claude 兼容不解析 hooks，故 reasonix 需 native manifest）
    README.md                       每 host 安装命令

采用多 host 插件市场的通用模式：薄 manifest + 共享内容，单仓即 marketplace。
source 用 plugins/<name> 子目录（forge 是 Go 工具仓，须隔离源码树）。

示例：
  forge plugin pack                 在当前仓库根生成 forge 自身的 marketplace
  forge plugin pack --out ../proj   在指定目录生成`,
	RunE: runPluginPack,
}

func runPluginPack(cmd *cobra.Command, args []string) error {
	out, _ := cmd.Flags().GetString("out")
	if out == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("plugin pack: get cwd: %w", err)
		}
		out = cwd
	}
	spec := agentbridge.DefaultPluginPack(out)
	if err := agentbridge.GeneratePluginPack(spec); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "plugin pack generated at %s\n", out)
	return nil
}

// pluginStatusCmd 报告 forge plugin 是否在 user-level 已装。供脚本/hook（init-suggest
// 的 dedupe 分支）检测：exit 0 = 已装（plugin 已 user-level 接管 hooks+MCP），非零 = 未装。
var pluginStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "报告 forge plugin 是否在 user-level 已装（exit 0=已装，非零=未装）",
	Long: `检测 Claude Code 是否在 user-level 安装了 forge plugin。
读 <claude home>/plugins/installed_plugins.json，找 forge@<marketplace> 的 scope=user 条目
（claude home 优先 CLAUDE_CONFIG_DIR，fallback ~/.claude）。

供脚本/hook 检测：exit 0 = 已装（plugin 已接管 hooks+MCP，project-level 重复可清理），
非零退出 = 未装。SilenceErrors+SilenceUsage 压住 cobra 自身的 Error:/Usage 块（未装时
RunE 仍 return error，cli.Execute root.go:66 会向 stderr 再打一行——裸跑仍 stdout+stderr
两行；init-suggest.sh 用 >/dev/null 2>&1 只看 exit code 不受影响）。`,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		if hooks.IsClaudePluginInstalled() {
			fmt.Fprintln(out, "forge plugin: installed at user level")
			return nil
		}
		fmt.Fprintln(out, "forge plugin: not installed at user level")
		return fmt.Errorf("forge plugin not installed at user level")
	},
}

// pluginDedupeCmd 在 plugin 已装时一次性清理 project-level 重复的 hooks（settings.local.json）
// 与 forge MCP server（.mcp.json）。init-suggest SessionStart hook 自动调用（传 --keep-empty,
// 存量迁移且保留文件壳）,也可手动跑（默认删空文件）。幂等：无重复时 no-op（无输出，hook 据此不产生提示噪音）。
var pluginDedupeCmd = &cobra.Command{
	Use:   "dedupe [dir]",
	Short: "plugin 已装时清理项目级 + user 级重复 hooks + MCP",
	Long: `当 forge plugin 在 user-level 已装，project-level 的 .claude/settings.local.json hooks
与 .mcp.json forge server 是冗余的（Claude Code 双重加载同名 forge server / 双跑 hook）。
本命令一次性移除这两类 forge 来源的重复注册，保留用户自定义条目。

同时清理 user-level（~/.claude 或 $CLAUDE_CONFIG_DIR）settings.local.json 的 forge hooks：
plugin.json 已在 user-level 注册全部 ForgeHookSpec，此处的 forge hook 必重复（历史 global
forge init 写 home / 旧全局安装残留）。user-level 始终保留文件壳（绝不删用户全局配置），
与 --keep-empty flag 无关。

--keep-empty：清完 forge hooks 后若 [dir]/.claude/settings.local.json 只剩 forge 来源（无
用户字段），默认删整个文件；传 --keep-empty 则写 {} 保留文件壳（settings.local.json 是
gitignored 个人配置,用户常主动放置/编辑,自动调用时不删）。仅影响 project-level
settings.local.json,.mcp.json 清完仍删空。user-level 不受此 flag 影响（始终保留壳）。

仅在 plugin 已装时清理（未装时不动——project-level 是唯一来源）。幂等：无重复时 no-op。
[dir] 默认当前目录。

执行时机（两路径,review S4）：在 forge 项目内,autoSync 的 defer（每条非 init forge 命令末尾,
sync.go:33）会先静默完成 project+user 级 dedupe（dedupeProjectLevelIfPlugin）,本命令 RunE 此时
多为 no-op 无输出。本命令的独立价值在：(a) 在非 forge 项目（如 home）手动跑——autoSync 不触发
（findProjectRoot 失败,root.go:37）,RunE 是唯一清理者并给出可读输出（'cd ~ && forge plugin dedupe'
是清 user 级全局重复的常用入口）；(b) --keep-empty 显式控制项目级是否删空文件。`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPluginDedupe,
}

func runPluginDedupe(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	if !hooks.IsClaudePluginInstalled() {
		fmt.Fprintln(out, `plugin 未装，无需 dedupe（project-level 是唯一来源）`)
		return nil
	}
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve dir: %w", err)
	}
	// --keep-empty: 自动调用（init-suggest SessionStart）传 true——保留 settings.local.json
	// 文件壳（用户常主动放置/编辑,绝不静默删）;手动 dedupe 默认 false,删空文件（显式清理语义）。
	keepEmpty, _ := cmd.Flags().GetBool("keep-empty")
	hooksChanged, err := hooks.StripForgeHooks(abs, keepEmpty)
	if err != nil {
		return fmt.Errorf("strip hooks: %w", err)
	}
	mcpChanged, err := agentbridge.StripForgeMCPServer(abs)
	if err != nil {
		return fmt.Errorf("strip mcp: %w", err)
	}
	// user-level: plugin.json 在 user-level 注册全部 ForgeHookSpec → ~/.claude/settings.local.json
	// 的 forge hook 必重复（历史 global forge init 写 home / 旧全局安装残留）。Claude Code 双跑。
	// keepEmpty 固定 true（StripForgeHooksUserLevel 内部）——用户全局配置绝不删,只清 forge hooks
	// 保留壳,与 project-level keepEmpty flag 无关。
	//
	// return err（与 project 级两处一致,区别于 dedupeProjectLevelIfPlugin 的 warn）：本命令是用户
	// 专门为清理而显式跑的,失败应上报而非吞掉;autoSync defer 路径相反,降级 warn 不阻断主命令。
	userChanged, err := hooks.StripForgeHooksUserLevel()
	if err != nil {
		return fmt.Errorf("strip user-level hooks: %w", err)
	}
	if !hooksChanged && !mcpChanged && !userChanged {
		// 无重复 → no-op，不输出（init-suggest hook 据空输出不产生提示噪音）。
		return nil
	}
	if hooksChanged || mcpChanged {
		var parts []string
		if hooksChanged {
			parts = append(parts, "hooks")
		}
		if mcpChanged {
			parts = append(parts, "MCP")
		}
		fmt.Fprintln(out, `plugin 已 user-level 接管，移除项目级重复 `+strings.Join(parts, "+")+`（`+abs+`）`)
	}
	if userChanged {
		// user-level 单独提示:位置不同（全局 ~/.claude 而非项目目录）,独立一行让用户知晓全局配置被清。
		fmt.Fprintln(out, `plugin 已 user-level 接管，移除 user-level 重复 hooks（`+hooks.ClaudeHome()+`）`)
	}
	return nil
}

// pluginKimiManifestCmd 是已提交 kimi plugin manifest 的 CLI 再生成出口——2026-08-16
// 审计注记指出 Build 三件套没有生产调用方（再生成只存在于测试的 -update-kimi-plugin
// flag），ForgeHookSpec 变更意味着手工对齐 .kimi-plugin/plugin.json。包装同一三件套
// （BuildKimiPluginManifest + MarshalKimiPluginManifest，description 取共享的
// KimiPluginDescription 常量）给维护者一个一等命令；守卫测试仍是执法者，CLI 是便利。
var pluginKimiManifestCmd = &cobra.Command{
	Use:   "kimi-manifest [--write]",
	Short: "渲染/再生成 kimi plugin manifest（version 读 npm/package.json）",
	Long: `渲染 forge 的 kimi plugin manifest（.kimi-plugin/plugin.json），version 从仓库的
npm/package.json 读（单一真相源，与 release.js / 守卫测试同源），hooks 从 ForgeHookSpec
派生，description 取共享常量——与守卫测试 TestKimiPluginManifestMirrorsSpec 完全同源。

默认：manifest JSON 打到 stdout，并向 stderr 报一行 version 与已提交文件是否 in sync
（只报告，退出码恒 0——执法者是守卫测试，不是本命令）。
--write：与已提交文件逐字节比对，一致则报 in sync 不改写；不一致则重写。

仓库根发现：从 cwd 向上找含 npm/package.json 的目录（forge 仓库自身的维护命令，
在仓库内任意子目录跑均可）。`,
	RunE: runPluginKimiManifest,
}

func runPluginKimiManifest(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	root, err := findKimiManifestRoot()
	if err != nil {
		return err
	}
	version, err := readNpmPackageVersion(root)
	if err != nil {
		return err
	}
	want, err := agentbridge.MarshalKimiPluginManifest(agentbridge.BuildKimiPluginManifest(version, agentbridge.KimiPluginDescription))
	if err != nil {
		return fmt.Errorf("plugin kimi-manifest: marshal: %w", err)
	}

	manifestPath := filepath.Join(root, ".kimi-plugin", "plugin.json")
	committed, readErr := os.ReadFile(manifestPath)
	// A non-NotExist read failure (AV/editor file lock on Windows — review L-2) must NOT
	// be conflated with drift: report-only would mislabel it 已提交文件漂移, and --write
	// would overwrite bytes it never compared. Only a truly absent file is "not in sync".
	//
	// 非 NotExist 的读取失败（Windows 上杀软/编辑器文件锁——评审 L-2）绝不能与漂移
	// 混同：report-only 会误标「已提交文件漂移」，--write 会改写从未比对过的字节。
	// 只有文件真正缺席才算「不同步」。
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("plugin kimi-manifest: read committed manifest: %w", readErr)
	}
	inSync := readErr == nil && string(committed) == string(want)

	write, _ := cmd.Flags().GetBool("write")
	// --write 守卫（评审 L-1）：仓库根发现接受任何持有 npm/package.json 的祖先；要写进
	// 一个恰好有 npm/ 子目录的用户 monorepo 需要第二个 forge 仓库标记——根上有 go.mod、
	// 或已有提交的 .kimi-plugin/plugin.json。report-only 保持宽容（打印 stdout 无害）。
	if write {
		// 区分 NotExist 与其他 stat 失败（L-1 复审后续，2026-08-22）：杀软/编辑器
		// 锁住 go.mod 正是 L-2 在 manifest 读取上守卫的 Windows 场景——把它当
		// 「无 go.mod」既误拒 --write 又在错误文案里误报原因。
		if _, goModErr := os.Stat(filepath.Join(root, "go.mod")); goModErr != nil {
			if !os.IsNotExist(goModErr) {
				return fmt.Errorf("plugin kimi-manifest: stat go.mod: %w", goModErr)
			}
			if _, manifestErr := os.Stat(manifestPath); manifestErr != nil {
				if !os.IsNotExist(manifestErr) {
					return fmt.Errorf("plugin kimi-manifest: stat %s: %w", manifestPath, manifestErr)
				}
				return fmt.Errorf("plugin kimi-manifest: %s 无 go.mod 且无既有 .kimi-plugin/plugin.json——拒绝 --write（防误写非 forge 仓库），去掉 --write 只打印", root)
			}
		}
	}
	if !write {
		// 只报告：打印渲染的 manifest，stderr 一行 version+漂移。两种情况都退出 0
		// ——执法是 TestKimiPluginManifestMirrorsSpec 的职责。
		fmt.Fprint(out, string(want))
		if inSync {
			fmt.Fprintf(os.Stderr, "kimi manifest: in sync（version=%s, %s）\n", version, manifestPath)
		} else {
			fmt.Fprintf(os.Stderr, "kimi manifest: 已提交文件漂移（version=%s, %s）——forge plugin kimi-manifest --write 再生成\n", version, manifestPath)
		}
		return nil
	}

	if inSync {
		fmt.Fprintf(out, "kimi manifest: in sync，未改写（version=%s, %s）\n", version, manifestPath)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		return fmt.Errorf("plugin kimi-manifest: mkdir: %w", err)
	}
	if err := os.WriteFile(manifestPath, want, 0o644); err != nil {
		return fmt.Errorf("plugin kimi-manifest: write: %w", err)
	}
	fmt.Fprintf(out, "kimi manifest: 已重写 %s（version=%s）\n", manifestPath, version)
	return nil
}

// findKimiManifestRoot 从 cwd 向上找 forge 仓库根（持有 npm/package.json 的目录——
// version 来源）。在仓库外跑时报错并给指引：这是 forge 仓库自身的维护命令，不是
// 跑在用户仓库上的东西。
func findKimiManifestRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("plugin kimi-manifest: getwd: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "npm", "package.json")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("plugin kimi-manifest: 向上未找到 npm/package.json——请在 forge 仓库内运行（维护命令，非用户仓库命令）")
		}
		dir = parent
	}
}

// readNpmPackageVersion 从 <root>/npm/package.json 读 version 字段——scripts/release.js
// bump、守卫测试读取的同一单一真相源。
func readNpmPackageVersion(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "npm", "package.json"))
	if err != nil {
		return "", fmt.Errorf("plugin kimi-manifest: read npm/package.json: %w", err)
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", fmt.Errorf("plugin kimi-manifest: unmarshal npm/package.json: %w", err)
	}
	if pkg.Version == "" {
		return "", fmt.Errorf("plugin kimi-manifest: npm/package.json version 为空")
	}
	return pkg.Version, nil
}
