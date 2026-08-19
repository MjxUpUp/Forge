package agentbridge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hooks"
)

// codebuddy.go — CodeBuddy / WorkBuddy (Tencent @genie/workbuddy-desktop) agent bridge.
//
// CodeBuddy is a Claude Code-compatible coding agent whose plugin & hook model is
// field-identical to Claude Code's: .codebuddy-plugin/marketplace.json +
// plugins/<name>/.codebuddy-plugin/plugin.json + hooks/hooks.json, where hooks.json uses
// {hooks:{PreToolUse/PostToolUse/SessionStart/Stop:[{matcher,hooks:[{type:command,command}]}]}}
// — the SAME schema as hooks.ForgeHookSpec(). Hook scripts read `.tool_input.file_path`
// from stdin and block via `exit 2` + stderr (verified in CodeBuddy's own all-hooks
// marketplace: file-protection.md). So CodeBuddy needs NO stdin normalizer and NO output
// rewriting — it runs the Claude Code protocol verbatim, like opencode. Its commands DO
// carry `--agent codebuddy` (see BuildCodeBuddyHooksPayload): not for protocol reasons
// but for attribution — codebuddy has no project marker and no identity env, so the flag
// is its only way to register/stamp sessions as codebuddy.
//
// Wiring model (unlike the 8 file-writing translators): CodeBuddy's settings.json has NO
// hooks field — hooks load ONLY through an installed plugin. So Translate generates a
// self-contained "forge-local" directory marketplace under forge's global home, then
// registers it via the codebuddy CLI (`plugin marketplace add` + `plugin install --scope user`).
// User-scope plugin = install once, machine-wide, every project — same philosophy as
// kimi/codex. When the CLI is absent, Translate prints the exact manual commands and
// returns nil (assets are already on disk; the user runs the two CLI lines).
//
// Detection (ParseAgentFlag only, NOT DetectAgents): ~/.workbuddy exists iff WorkBuddy
// is installed — auto-detect would wire codebuddy on EVERY `forge init` on any machine
// with WorkBuddy installed (the zcode trap, reverted 2026-08-03: a user-level home that
// always-exists must NOT be an auto-detect signal). CodeBuddy is opt-in via
// `forge init --agents codebuddy` only. The generated .codebuddy-plugin/ is likewise NOT
// a detect signal (forge-generated files must not self-trigger wiring — the AGENTS.md
// lesson).
//
// codebuddy.go — CodeBuddy / WorkBuddy（腾讯 @genie/workbuddy-desktop）agent 桥接。
//
// CodeBuddy 是 Claude Code 兼容的 coding agent，其 plugin 与 hook 模型与 Claude Code
// 字段一致：.codebuddy-plugin/marketplace.json + plugins/<name>/.codebuddy-plugin/plugin.json
// + hooks/hooks.json，其中 hooks.json 用
// {hooks:{PreToolUse/PostToolUse/SessionStart/Stop:[{matcher,hooks:[{type:command,command}]}]}}
// ——与 hooks.ForgeHookSpec() 同 schema。hook 脚本从 stdin 读 `.tool_input.file_path`、
// 用 `exit 2` + stderr 阻断（已在 CodeBuddy 自家 all-hooks marketplace 的 file-protection.md
// 验证）。故 CodeBuddy 不需 stdin normalizer、不改输出协议——它原样跑 Claude Code
// 协议，与 opencode 同类。其命令携带 `--agent codebuddy`（见
// BuildCodeBuddyHooksPayload）：不是协议需要，而是归因需要——codebuddy 无项目标记、
// 无身份 env，该 flag 是它把会话登记/盖戳为 codebuddy 的唯一途径。
//
// 接线模型（与 8 个写文件的 translator 不同）：CodeBuddy 的 settings.json 无 hooks
// 字段——hook 只能经已安装的 plugin 加载。故 Translate 在 forge 全局 home 下生成自包含
// 的 "forge-local" directory marketplace，再经 codebuddy CLI（`plugin marketplace add` +
// `plugin install --scope user`）注册。user-scope plugin = 一次装机、全机器、所有项目
// ——与 kimi/codex 同一哲学。CLI 不在时，Translate 打印精确的手动命令并返回 nil
// （资产已在盘上；用户跑两行 CLI 即可）。
//
// 检测（仅 ParseAgentFlag，不进 DetectAgents）：~/.workbuddy 装了 WorkBuddy 就恒存在
// ——auto-detect 会让任何装有 WorkBuddy 的机器每次 `forge init` 都接 codebuddy
// （zcode 陷阱，2026-08-03 回滚：恒存在的用户级 home 不能当 auto-detect 信号）。
// CodeBuddy 仅经 `forge init --agents codebuddy` 显式接入。生成的 .codebuddy-plugin/
// 同样不作 detect 信号（forge 生成的文件不得自触发接线——AGENTS.md 教训）。

// CodeBuddyTranslator wires forge hooks into CodeBuddy/WorkBuddy via a user-scope plugin.
// See the file-header comment for the wiring model and detection rationale.
//
// CodeBuddyTranslator 经 user-scope plugin 把 forge hook 接入 CodeBuddy/WorkBuddy。
// 接线模型与检测理由见文件头注释。
type CodeBuddyTranslator struct{}

// codebuddyMarketplaceName / codebuddyPluginName are the marketplace & plugin identifiers.
// The marketplace name "forge-local" is arbitrary but must stay stable: it becomes the
// `name@marketplace` key (forge@forge-local) in WorkBuddy's settings.json/enabledPlugins
// and the known_marketplaces.json entry key. The plugin name "forge" matches the kimi
// precedent (kimiPluginName) and becomes the hooks namespace.
//
// codebuddyMarketplaceName / codebuddyPluginName 是 marketplace 与 plugin 标识。
// marketplace 名 "forge-local" 任意但须稳定：它成为 WorkBuddy settings.json/enabledPlugins
// 里的 `name@marketplace` key（forge@forge-local）与 known_marketplaces.json 条目 key。
// plugin 名 "forge" 沿用 kimi 先例（kimiPluginName），并成为 hooks 命名空间。
const (
	codebuddyMarketplaceName = "forge-local"
	codebuddyPluginName      = "forge"
)

// AgentType returns the CodeBuddy agent identifier.
//
// AgentType 返回 CodeBuddy agent 标识。
func (t *CodeBuddyTranslator) AgentType() AgentType {
	return AgentCodeBuddy
}

// CodeBuddyHooksPayload is the hooks.json schema: a single top-level "hooks" key wrapping
// the event→matcher→command map. This is ForgeHookSpec() serialized under one extra key
// — CodeBuddy's only structural deviation from Claude Code's inline-hooks plugin.json.
// encoding/json marshals map keys in sorted order, so output is deterministic without an
// explicit sort (stable across runs → golden-test friendly).
//
// CodeBuddyHooksPayload 是 hooks.json schema：单一顶层 "hooks" key 包住 event→matcher→
// command map。即 ForgeHookSpec() 多包一层 key 序列化——CodeBuddy 与 Claude Code 内联
// hooks 的 plugin.json 唯一结构差异。encoding/json 按 sorted key 序列化 map，故输出
// 无需显式 sort 即确定（跨运行稳定 → golden test 友好）。
type CodeBuddyHooksPayload struct {
	Hooks map[string][]hooks.HookMatcher `json:"hooks"`
}

// BuildCodeBuddyHooksPayload derives the hooks.json payload from hooks.ForgeHookSpec() —
// the single source of truth shared with settings.local.json, the plugin pack, and every
// other translator. CodeBuddy's hook protocol is byte-identical to Claude Code's (same
// events, same matchers, same tool names Read/Write/Edit/Bash/Skill, same exit-2 block),
// so the spec migrates with ONE rewrite: every command gains `--agent codebuddy`.
// The flag changes nothing about stdin parsing (Claude-shape needs no normalizer) or
// output (default Claude emitter) — it exists purely for ATTRIBUTION: without it,
// codebuddy fired hooks with agent=="" and its sessions were never registered or
// stamped (the fleet-wide gap found in the 2026-08 attribution audit; codebuddy also
// has no project marker, so --agent is its ONLY identity signal).
// TestCodeBuddyHooksPayload_MirrorsSpec guards the parity (spec + the one suffix).
//
// BuildCodeBuddyHooksPayload 从 hooks.ForgeHookSpec() 派生 hooks.json payload——与
// settings.local.json、plugin pack 及其他 translator 共享的单一真相源。CodeBuddy 的
// hook 协议与 Claude Code 字节一致（同 event、同 matcher、同工具名 Read/Write/Edit/
// Bash/Skill、同 exit-2 阻断），故 spec 迁移只做一处改写：每条命令加 `--agent
// codebuddy`。该 flag 对 stdin 解析（Claude 形无需 normalizer）与输出（默认 Claude
// emitter）都无影响——它纯为归因存在：没有它，codebuddy 以 agent=="" 触发 hook，
// 其会话从不被登记或盖戳（2026-08 归因审计发现的全宿主缺口；codebuddy 也没有项目
// 标记，故 --agent 是它唯一的身份信号）。
// TestCodeBuddyHooksPayload_MirrorsSpec 守卫此对等（spec + 唯一后缀）。
func BuildCodeBuddyHooksPayload() CodeBuddyHooksPayload {
	spec := hooks.ForgeHookSpec()
	out := make(map[string][]hooks.HookMatcher, len(spec))
	for event, matchers := range spec {
		ms := make([]hooks.HookMatcher, len(matchers))
		for i, m := range matchers {
			entries := make([]hooks.HookEntry, len(m.Hooks))
			for j, h := range m.Hooks {
				entries[j] = hooks.HookEntry{Type: h.Type, Command: h.Command + " --agent codebuddy"}
			}
			ms[i] = hooks.HookMatcher{Matcher: m.Matcher, Hooks: entries}
		}
		out[event] = ms
	}
	return CodeBuddyHooksPayload{Hooks: out}
}

// codebuddyPluginManifest is plugins/forge/.codebuddy-plugin/plugin.json. The hooks field
// is a relative path to hooks/hooks.json (NOT an inline object like Claude Code's
// plugin.json) — verified in CodeBuddy's ppt-implement plugin. The relative path is what
// makes CodeBuddy load hooks/hooks.json; relocating the marketplace dir keeps it valid.
//
// codebuddyPluginManifest 是 plugins/forge/.codebuddy-plugin/plugin.json。hooks 字段是
// 指向 hooks/hooks.json 的相对路径（不像 Claude Code 的 plugin.json 用内联对象）——
// 已在 CodeBuddy 的 ppt-implement plugin 验证。正是这个相对路径让 CodeBuddy 加载
// hooks/hooks.json；迁移 marketplace 目录仍保持有效。
type codebuddyPluginManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Hooks       string `json:"hooks"`
}

// codebuddyMarketplaceManifest is .codebuddy-plugin/marketplace.json — the thin marketplace
// manifest pointing at plugins/forge/. Owner is OMITTED: CodeBuddy's directory-type
// marketplace auto-generates a description and does not require owner (verified in the
// user-created "experts" marketplace, which has only name/description/plugins).
//
// codebuddyMarketplaceManifest 是 .codebuddy-plugin/marketplace.json——指向 plugins/forge/
// 的薄 marketplace manifest。owner 省略：CodeBuddy 的 directory 类型 marketplace 自动生成
// description 且不要求 owner（已在用户自建的 "experts" marketplace 验证，它只有
// name/description/plugins）。
type codebuddyMarketplaceEntry struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Description string `json:"description"`
}

type codebuddyMarketplaceManifest struct {
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Plugins     []codebuddyMarketplaceEntry `json:"plugins"`
}

// CodeBuddyMarketplaceDir returns the directory under forge's global home where the
// forge-local marketplace assets live: <GlobalHome>/agents/codebuddy/forge-local/.
// Assets stay under forge's own data root (not inside ~/.workbuddy) — forge manages them,
// WorkBuddy holds only a directory pointer in its known_marketplaces.json. User-level and
// project-independent (a user-scope plugin is machine-wide), so this is NOT under the
// per-project DataDir.
//
// CodeBuddyMarketplaceDir 返回 forge 全局 home 下 forge-local marketplace 资产目录：
// <GlobalHome>/agents/codebuddy/forge-local/。资产留在 forge 自己的数据根下（不进
// ~/.workbuddy）——forge 自管理，WorkBuddy 在其 known_marketplaces.json 里只持一个
// directory 指针。用户级且项目无关（user-scope plugin 全机器生效），故不放在按项目的
// DataDir 下。
func CodeBuddyMarketplaceDir() (string, error) {
	home, err := forgedata.GlobalHome()
	if err != nil {
		return "", fmt.Errorf("codebuddy: resolve forge home: %w", err)
	}
	return filepath.Join(home, "agents", "codebuddy", codebuddyMarketplaceName), nil
}

// GenerateCodeBuddyPluginPack writes the forge-local directory marketplace under dir:
//
//	<dir>/.codebuddy-plugin/marketplace.json
//	<dir>/plugins/forge/.codebuddy-plugin/plugin.json     (hooks: ./hooks/hooks.json)
//	<dir>/plugins/forge/hooks/hooks.json                  (= ForgeHookSpec under "hooks")
//
// Idempotent: re-runs overwrite in place. Files use writeJSONIndent (2-space indent, shared
// with the claude/cursor plugin pack) for golden-test stability. description empty falls
// back to DefaultPluginDescription.
//
// GenerateCodeBuddyPluginPack 在 dir 下写 forge-local directory marketplace：
//
//	<dir>/.codebuddy-plugin/marketplace.json
//	<dir>/plugins/forge/.codebuddy-plugin/plugin.json（hooks: ./hooks/hooks.json）
//	<dir>/plugins/forge/hooks/hooks.json（= ForgeHookSpec 包在 "hooks" 下）
//
// 幂等：重跑就地覆盖。文件走 writeJSONIndent（2 空格缩进，与 claude/cursor plugin pack
// 共享）保 golden test 稳定。description 空则回落 DefaultPluginDescription。
func GenerateCodeBuddyPluginPack(dir, description string) error {
	if dir == "" {
		return fmt.Errorf("codebuddy: GenerateCodeBuddyPluginPack requires non-empty dir")
	}
	if description == "" {
		description = DefaultPluginDescription
	}
	mp := codebuddyMarketplaceManifest{
		Name:        codebuddyMarketplaceName,
		Description: "Forge plugin marketplace (local)",
		Plugins: []codebuddyMarketplaceEntry{{
			Name:        codebuddyPluginName,
			Source:      "./plugins/" + codebuddyPluginName,
			Description: description,
		}},
	}
	if err := writeJSONIndent(filepath.Join(dir, ".codebuddy-plugin", "marketplace.json"), mp); err != nil {
		return fmt.Errorf("codebuddy: write marketplace.json: %w", err)
	}
	pluginDir := filepath.Join(dir, "plugins", codebuddyPluginName)
	manifest := codebuddyPluginManifest{
		Name:        codebuddyPluginName,
		Version:     "1.0.0",
		Description: description,
		Hooks:       "./hooks/hooks.json",
	}
	if err := writeJSONIndent(filepath.Join(pluginDir, ".codebuddy-plugin", "plugin.json"), manifest); err != nil {
		return fmt.Errorf("codebuddy: write plugin.json: %w", err)
	}
	if err := writeJSONIndent(filepath.Join(pluginDir, "hooks", "hooks.json"), BuildCodeBuddyHooksPayload()); err != nil {
		return fmt.Errorf("codebuddy: write hooks.json: %w", err)
	}
	return nil
}

// Translate generates the forge-local marketplace under forge's global home, then registers
// it with WorkBuddy via the codebuddy CLI. projectDir and input are intentionally unused:
// CodeBuddy wiring is user-level and project-independent (one user-scope plugin covers
// every project), unlike project-scoped translators. When the CLI is unavailable (not on
// PATH, not in the WorkBuddy install dir), Translate prints the exact manual commands and
// returns nil — the on-disk assets are already complete, the user only needs to run the
// two CLI lines once. This keeps `forge init --agents codebuddy` non-fatal on machines
// where the codebuddy binary cannot be located automatically.
//
// Translate 在 forge 全局 home 下生成 forge-local marketplace，再经 codebuddy CLI 注册到
// WorkBuddy。projectDir 与 input 刻意不用：CodeBuddy 接线是用户级、项目无关（一个
// user-scope plugin 覆盖所有项目），与项目级 translator 不同。CLI 不可用时（PATH 和
// WorkBuddy 安装目录都找不到 codebuddy），Translate 打印精确的手动命令并返回 nil——
// 盘上资产已齐，用户只需跑两行 CLI 一次。这让 `forge init --agents codebuddy` 在
// codebuddy 二进制无法自动定位的机器上也不报错。
func (t *CodeBuddyTranslator) Translate(projectDir string, input *TranslationInput) error {
	dir, err := CodeBuddyMarketplaceDir()
	if err != nil {
		return err
	}
	if err := GenerateCodeBuddyPluginPack(dir, DefaultPluginDescription); err != nil {
		return err
	}
	run, err := FindCodeBuddyCLI()
	if err != nil {
		// CLI not found — assets are written; print the manual registration commands and
		// return nil so init stays green. The user runs these once.
		//
		// CLI 找不到——资产已写；打印手动注册命令并返回 nil，init 保持绿。用户跑一次即可。
		printCodeBuddyManualSetup(dir)
		return nil
	}
	return registerCodeBuddyPlugin(run, dir)
}

// printCodeBuddyManualSetup prints the two CLI commands the user must run, plus the
// marketplace dir, when the codebuddy binary cannot be auto-located. Goes to stdout so the
// user sees it in the forge init flow.
//
// printCodeBuddyManualSetup 在 codebuddy 二进制无法自动定位时，打印用户需手动跑的两条
// CLI 命令及 marketplace 目录。输出到 stdout，让用户在 forge init 流程里看到。
func printCodeBuddyManualSetup(dir string) {
	q := string([]rune{0x22}) // ASCII double-quote via rune — dodges [[windows-input-quote-corruption]]
	fmt.Println(`CodeBuddy/WorkBuddy: forge plugin assets written to:`)
	fmt.Println(`  ` + dir)
	wbHome, err := codebuddyWorkBuddyHome()
	if err != nil {
		wbHome = `<WorkBuddy home>`
	}
	fmt.Println(`Register manually (codebuddy CLI not auto-located).`)
	fmt.Println(`IMPORTANT: set CODEBUDDY_CONFIG_DIR so the CLI writes to the WorkBuddy app home`)
	fmt.Println(`(` + wbHome + `), NOT the CLI default ~/.codebuddy — otherwise the app never loads it.`)
	fmt.Println(`  bash:`)
	fmt.Println(`    CODEBUDDY_CONFIG_DIR=` + q + wbHome + q + ` codebuddy plugin marketplace add ` + q + dir + q + ` --name ` + codebuddyMarketplaceName)
	fmt.Println(`    CODEBUDDY_CONFIG_DIR=` + q + wbHome + q + ` codebuddy plugin install ` + codebuddyPluginName + `@` + codebuddyMarketplaceName + ` --scope user`)
	fmt.Println(`  Windows cmd:`)
	fmt.Println(`    set CODEBUDDY_CONFIG_DIR=` + wbHome)
	fmt.Println(`    codebuddy plugin marketplace add ` + q + dir + q + ` --name ` + codebuddyMarketplaceName)
	fmt.Println(`    codebuddy plugin install ` + codebuddyPluginName + `@` + codebuddyMarketplaceName + ` --scope user`)
}

// registerCodeBuddyPlugin runs `codebuddy plugin marketplace add` + `plugin install --scope user`
// via run (which may exec the CLI directly or through a node interpreter — see codebuddyRun).
// Idempotent in practice: `plugin marketplace add` on an already-registered name updates in place;
// `plugin install` on an already-installed plugin is a no-op. CLI errors are returned (and
// surface as a Warning in forge init, which never blocks on translator errors).
//
// registerCodeBuddyPlugin 经 run（可能直接 exec CLI 或经 node 解释器，见 codebuddyRun）跑
// `codebuddy plugin marketplace add` + `plugin install --scope user`。实践中幂等：对已注册名
// `plugin marketplace add` 就地更新；对已装 plugin `plugin install` 是 no-op。CLI 错误被返回
// （在 forge init 里显示为 Warning，translator 错误从不阻断）。
func registerCodeBuddyPlugin(run codebuddyRun, dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	wbHome, err := codebuddyWorkBuddyHome()
	if err != nil {
		return fmt.Errorf(`codebuddy: resolve WorkBuddy config home: %w`, err)
	}
	add := run.Command(`plugin`, `marketplace`, `add`, abs, `--name`, codebuddyMarketplaceName)
	add.Env = withCodeBuddyConfigDir(os.Environ(), wbHome)
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf(`codebuddy: plugin marketplace add failed: %w: %s`, err, strings.TrimSpace(string(out)))
	}
	install := run.Command(`plugin`, `install`,
		codebuddyPluginName+`@`+codebuddyMarketplaceName, `--scope`, `user`)
	install.Env = withCodeBuddyConfigDir(os.Environ(), wbHome)
	if out, err := install.CombinedOutput(); err != nil {
		return fmt.Errorf(`codebuddy: plugin install failed: %w: %s`, err, strings.TrimSpace(string(out)))
	}
	fmt.Println(`CodeBuddy/WorkBuddy: forge plugin installed (forge@` + codebuddyMarketplaceName + `, user scope).`)
	return nil
}

// codebuddyWorkBuddyHome returns the config directory the WorkBuddy desktop app reads plugins
// from. The app's own workbuddySettingsPath resolves to WORKBUDDY_CONFIG_DIR || ~/.workbuddy;
// forge mirrors that so the CLI is pointed at whichever home the app actually uses. This is
// the fix for config separation: the codebuddy CLI binary defaults its write-home to
// ~/.codebuddy (CODEBUDDY_CONFIG_DIR unset), while the app reads ~/.workbuddy — without
// redirecting the CLI, registration lands in ~/.codebuddy and the app never loads it (verified
// end-to-end: stdout reports success yet ~/.workbuddy is untouched).
//
// codebuddyWorkBuddyHome 返回 WorkBuddy 桌面 app 读取 plugin 的 config 目录。app 自己的
// workbuddySettingsPath 解析为 WORKBUDDY_CONFIG_DIR || ~/.workbuddy；forge 照此镜像，把 CLI 指向
// app 实际使用的 home。这是配置分离的修复：codebuddy CLI 二进制的写入 home 默认是
// ~/.codebuddy（CODEBUDDY_CONFIG_DIR 未设），而 app 读 ~/.workbuddy——不重定向 CLI，注册会落进
// ~/.codebuddy，app 从不加载（端到端验证：stdout 报成功但 ~/.workbuddy 纹丝不动）。
func codebuddyWorkBuddyHome() (string, error) {
	if v := strings.TrimSpace(os.Getenv(`WORKBUDDY_CONFIG_DIR`)); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, `.workbuddy`), nil
}

// CodeBuddyWorkBuddyHome exports codebuddyWorkBuddyHome for cross-agent environment
// auditing (forge doctor): the config home the WorkBuddy desktop app actually reads
// plugins from. Same single-source rationale as ClaudeConfigHomeDir.
//
// CodeBuddyWorkBuddyHome 导出 codebuddyWorkBuddyHome 供跨 agent 环境审计（forge doctor）
// 使用：WorkBuddy 桌面 app 实际读取 plugin 的 config home。与 ClaudeConfigHomeDir 同一
// 单一真相源理由。
func CodeBuddyWorkBuddyHome() (string, error) {
	return codebuddyWorkBuddyHome()
}

// withCodeBuddyConfigDir returns env with CODEBUDDY_CONFIG_DIR set to dir, replacing any
// existing value. Duplicate env keys' last-one-wins behavior is platform-dependent, so the
// existing entry is stripped first to make the override deterministic.
//
// withCodeBuddyConfigDir 返回设了 CODEBUDDY_CONFIG_DIR=dir 的 env，替换已有值。重复 env 键的
// last-one-wins 行为平台依赖，故先剔除已有项让覆盖确定。
func withCodeBuddyConfigDir(env []string, dir string) []string {
	const key = `CODEBUDDY_CONFIG_DIR`
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		// Exact-key strip via Cut so a sibling like CODEBUDDY_CONFIG_DIR_BACKUP survives.
		if k, _, ok := strings.Cut(kv, `=`); ok && k == key {
			continue
		}
		out = append(out, kv)
	}
	return append(out, key+`=`+dir)
}

// codebuddyRun describes how to invoke the codebuddy CLI. WorkBuddy ships the CLI as a bare
// node shebang script (bin/codebuddy, #!/usr/bin/env node) with NO .cmd/.exe shim, so on
// Windows it cannot be exec'd directly — node must be the interpreter. argv holds the
// executable plus any fixed leading args ([exe] or [node, script]); Command splices the
// subcommand after them.
//
// codebuddyRun 描述如何调用 codebuddy CLI。WorkBuddy 把 CLI 作为裸 node shebang 脚本发布
// （bin/codebuddy，#!/usr/bin/env node），无 .cmd/.exe shim，故 Windows 上无法直接 exec——
// 必须用 node 当解释器。argv 存可执行文件加固定前缀参数（[exe] 或 [node, script]），
// Command 把子命令拼在其后。
type codebuddyRun struct {
	argv []string
}

// Command builds an exec.Cmd for a codebuddy subcommand. argv[0] is the interpreter/
// executable; argv[1:] (the script path, when node-interpreted) sits before the subcommand.
//
// Command 为 codebuddy 子命令构造 exec.Cmd。argv[0] 是解释器/可执行文件；argv[1:]
// （node 解释时的脚本路径）位于子命令参数之前。
func (r codebuddyRun) Command(args ...string) *exec.Cmd {
	if len(r.argv) == 0 {
		// Defensive: a zero-value codebuddyRun means FindCodeBuddyCLI's error wasn't checked.
		// exec.Command("") yields a Cmd whose Start fails clearly rather than panicking on
		// r.argv[0]; the caller surfaces it as a registration error.
		//
		// 防御：零值 codebuddyRun 意味着 FindCodeBuddyCLI 的错误没被检查。exec.Command("") 产出
		// 的 Cmd 其 Start 会明确失败，而非在 r.argv[0] 上 panic；调用方将其报为注册错误。
		return exec.Command(``, args...)
	}
	full := make([]string, 0, len(r.argv)+len(args))
	full = append(full, r.argv[1:]...)
	full = append(full, args...)
	return exec.Command(r.argv[0], full...)
}

// isWindowsExecutable reports whether a file's extension is one Windows can execute directly
// via CreateProcess (PATHEXT subset). A bare node shebang script (no extension) is NOT —
// exec would fail with "executable file not found in %PATH%" (the bug this guard prevents).
//
// isWindowsExecutable 判断文件扩展名是否 Windows 可经 CreateProcess 直接执行（PATHEXT 子集）。
// 裸 node shebang 脚本（无扩展名）不算——exec 会报 "executable file not found in %PATH%"
// （此守卫防的正是该 bug）。
func isWindowsExecutable(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case `.exe`, `.cmd`, `.bat`, `.com`:
		return true
	}
	return false
}

// FindCodeBuddyCLI locates how to invoke the codebuddy (alias cbc) CLI. Search order:
//  1. PATH (codebuddy, then cbc) — exec.LookPath honors PATHEXT on Windows, so a hit is
//     directly executable (machines where the CLI was npm-global-installed or WorkBuddy
//     added bin to PATH).
//  2. WorkBuddy install dir — the CLI ships unpacked at
//     <install>/resources/app.asar.unpacked/cli/bin/codebuddy. A .exe/.cmd shim (if present)
//     is preferred; otherwise the bare node script is run via the system node interpreter
//     (WorkBuddy ships codebuddy as #!/usr/bin/env node with NO shim — direct exec fails on
//     Windows, so node must interpret it).
//
// Returns an error only when no invocation is possible (caller prints manual setup): the
// script is present but node is not on PATH, or nothing is found at all.
//
// FindCodeBuddyCLI 定位如何调用 codebuddy（别名 cbc）CLI。搜索顺序：PATH（codebuddy 后 cbc）
// ——exec.LookPath 在 Windows 遵守 PATHEXT，命中即直接可执行（CLI 经 npm 全局装或 WorkBuddy
// 把 bin 加入 PATH 的机器）；WorkBuddy 安装目录——CLI unpacked 在 <install>/resources/
// app.asar.unpacked/cli/bin/codebuddy。有 .exe/.cmd shim（若有）优先；否则裸 node 脚本经
// 系统 node 解释器跑（WorkBuddy 把 codebuddy 发布为 #!/usr/bin/env node 且无 shim——Windows
// 直接 exec 会失败，须 node 解释）。仅当无法调用时返回错误（调用方打印手动设置）：脚本在但
// node 不在 PATH，或压根找不到。
func FindCodeBuddyCLI() (codebuddyRun, error) {
	for _, name := range []string{`codebuddy`, `cbc`} {
		if p, err := exec.LookPath(name); err == nil {
			return codebuddyRun{argv: []string{p}}, nil
		}
	}
	var script string
	for _, p := range codebuddyCLIInstallCandidates() {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		// A directly-executable shim wins immediately.
		//
		// 直接可执行的 shim 立即胜出。
		if runtime.GOOS != "windows" || isWindowsExecutable(p) {
			return codebuddyRun{argv: []string{p}}, nil
		}
		// Bare script (no PATHEXT extension): remember it as a node-interpreter candidate.
		//
		// 裸脚本（无 PATHEXT 扩展名）：记为 node 解释器候选。
		if script == "" {
			script = p
		}
	}
	if script != "" {
		if node, err := exec.LookPath(`node`); err == nil {
			return codebuddyRun{argv: []string{node, script}}, nil
		}
	}
	return codebuddyRun{}, fmt.Errorf(`codebuddy CLI not found on PATH or in WorkBuddy install dir (script present but node interpreter unavailable)`)
}

// codebuddyCLIInstallCandidates returns the conventional WorkBuddy desktop install paths
// where the codebuddy CLI ships unpacked. Windows-first; macOS/Linux best-effort. These
// are PROBES (stat-checked + filtered by the caller): on Windows each base yields .exe,
// .cmd, and the bare script — FindCodeBuddyCLI prefers a directly-executable shim and falls
// back to running the bare script via node.
//
// codebuddyCLIInstallCandidates 返回 WorkBuddy 桌面安装的常规路径，codebuddy CLI unpacked
// 在此。Windows 优先；macOS/Linux 尽力。这些是探测（调用方 stat 检查 + 过滤）：Windows 每个
// base 产出 .exe、.cmd 与裸脚本——FindCodeBuddyCLI 优先直接可执行的 shim，回落经 node 跑裸脚本。
func codebuddyCLIInstallCandidates() []string {
	rel := filepath.Join("resources", "app.asar.unpacked", "cli", "bin", "codebuddy")
	var cands []string
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		for _, base := range []string{
			filepath.Join(home, `AppData`, `Local`, `Programs`, `WorkBuddy`),
			filepath.Join(home, `AppData`, `Local`, `WorkBuddy`),
			`C:\Program Files\WorkBuddy`,
			`D:\Program Files\WorkBuddy`,
		} {
			cands = append(cands,
				filepath.Join(base, rel+`.exe`),
				filepath.Join(base, rel+`.cmd`),
				filepath.Join(base, rel),
			)
		}
	} else {
		cands = append(cands,
			filepath.Join(`/Applications`, `WorkBuddy.app`, `Contents`, `Resources`, rel),
			filepath.Join(home, `.local`, `share`, `WorkBuddy`, rel),
		)
	}
	return cands
}
