package agentbridge

import (
	_ "embed"
	"fmt"
)

// pluginReadmeTemplate is the static three-step first-run plugin README, embedded from
// a real .md file (same precedent as forge_spawn.ts in ts_shared.go) instead of a
// strings.Builder chain: the only runtime interpolation is the repo slug in the six
// install-command lines (%[1]s, one operand referenced six times).
//
// pluginReadmeTemplate 是静态的三步首体验 plugin README，从真实 .md 文件 embed
// （与 ts_shared.go 的 forge_spawn.ts 同一先例），替代 strings.Builder 长链：
// 唯一的运行时插值是六条安装命令里的 repo slug（%[1]s，一个操作数引用六次）。
//
// Contracts carried over from the builder version (guarded by TestPluginPack_Readme /
// TestPluginPack_NoCurlyQuotes):
//   - Honest capability boundary: the plugin wires user-level hooks only; project
//     registration (global registry + user-level protocol/skill assets) still needs
//     forge init per project (step 3 + caveat section make this explicit — no
//     "install once, perfect everywhere" claim).
//   - Code blocks use 4-space indent; inline commands use backticks; content has no
//     curly quotes and no raw double quotes (Windows input-quote corruption guard).
//   - npm package name is @agent_forge/forge (matching npm/package.json), NOT the
//     GitHub owner slug — an earlier version wrote @mjxupup/forge, a nonexistent package.
//   - The Kimi Code table row intentionally keeps the literal MjxUpUp/Forge install URL
//     (it documents forge's own repo, not the branded spec.RepoSlug) — only the six
//     step-2 install commands follow the slug.
//
// 从 builder 版本继承的契约（由 TestPluginPack_Readme / TestPluginPack_NoCurlyQuotes 守卫）：
//   - 诚实能力边界：plugin 只接用户级 hooks；项目登记（全局注册表 + 用户级
//     协议/skill 资产）仍需每项目 forge init（step 3 + caveat 段明示，不宣传
//     "一次安装处处完美"）。
//   - 代码块用 4-space 缩进；行内命令用反引号；内容无弯引号、无裸双引号
//     （Windows 输入引号腐蚀守卫）。
//   - npm 包名是 @agent_forge/forge（与 npm/package.json 一致），不是 GitHub owner
//     slug——早期版本写过 @mjxupup/forge，指向不存在的包。
//   - Kimi Code 表格行刻意保留字面 MjxUpUp/Forge 安装 URL（它指 forge 自己的仓库，
//     非品牌化 spec.RepoSlug）——只有 step 2 的六条安装命令跟随 slug。
//
//go:embed assets/plugin_readme.md
var pluginReadmeTemplate string

// pluginReadme returns the plugin README with the repo slug interpolated into the
// install commands. writePluginReadme supplies the default slug when RepoSlug is empty.
//
// pluginReadme 返回插值 repo slug 后的 plugin README。RepoSlug 为空时由
// writePluginReadme 提供默认 slug。
func pluginReadme(repoSlug string) string {
	return fmt.Sprintf(pluginReadmeTemplate, repoSlug)
}
