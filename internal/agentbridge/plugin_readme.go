package agentbridge

import (
	_ "embed"
	"fmt"
	"io/fs"
	"path"

	"github.com/MjxUpUp/Forge/skills"
	skillsforge "github.com/MjxUpUp/Forge/skills-forge"
)

// pluginReadmeTemplate 是静态的三步首体验 plugin README，从真实 .md 文件 embed
// （与 ts_shared.go 的 forge_spawn.ts 同一先例），替代 strings.Builder 长链：
// 运行时插值有两处——六条安装命令里的 repo slug（%[1]s，一个操作数引用六次）
// 与内嵌 skill 数量（%[2]d——渲染时从 skills.FS 现数，宣传数字永不与 embed 漂移，
// review P3-1）。
//
// 从 builder 版本继承的契约（由 TestPluginPack_Readme / TestPluginPack_NoCurlyQuotes 守卫）：
//   - 诚实能力边界：plugin 接用户级 hooks；项目登记由 init-suggest 自动接管覆盖
//     plugin 用户（step 3 + caveat 段明示，不宣传超出 hook 实际行为的
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

// embeddedSkillCount 返回内嵌 skill 总数（中立 skills.FS + forge 原生 skillsforge.FS
// 的含 SKILL.md 顶层目录——与 writePluginSkills 分发、测试计数的同一真相源）。插值进
// plugin README，宣传数字在渲染时跟踪 embed，而非硬编码静默腐烂（历史数量 30→32→37→38→49）。
func embeddedSkillCount() int {
	n := 0
	for _, lib := range []fs.FS{skills.FS, skillsforge.FS} {
		entries, err := fs.ReadDir(lib, ".")
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if _, serr := fs.Stat(lib, path.Join(e.Name(), "SKILL.md")); serr == nil {
				n++
			}
		}
	}
	return n
}

// pluginReadme 返回插值 repo slug（安装命令）与内嵌 skill 数（skills 段）后的
// plugin README。RepoSlug 为空时由 writePluginReadme 提供默认 slug。
func pluginReadme(repoSlug string) string {
	return fmt.Sprintf(pluginReadmeTemplate, repoSlug, embeddedSkillCount())
}
