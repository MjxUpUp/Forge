// Package projectroot resolves the forge project root from the current working directory.
//
// Package projectroot 从当前工作目录解析 forge project root。
//
// user-level-assets 重构后，"这是不是 forge 项目"的锚点是全局注册表
// （~/.forge/projects.json），而非项目级 .forge/ 标记：forge init 默认不向项目
// 写任何东西。解析顺序：
//  1. registry.IsMember(cwd)——git key 匹配（跨 worktree）或非 git 路径前缀匹配
//  2. 遗留兜底：向上 walk 找项目级 .forge/（老版本 forge init 的项目）——
//     命中后自愈登记
//  3. 否则：ErrNoForgeConfig
//
// 把"我在哪个项目"的判定集中在一处，避免跨包重复（最初从 cli/root.go 以及
// 现已移除的 mcpserver/server.go 中抽取；mcpserver 于 2026-07-24 移除）。
package projectroot

import (
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/registry"
)

// FindProject resolves cwd → *forgedata.Project (Key / Root / GitRoot / DataDir / ConfigDir).
//
// FindProject 解析 cwd → *forgedata.Project（Key / Root / GitRoot / DataDir /
// ConfigDir）。成员资格经全局注册表判定（遗留 .forge/ walk-up 兜底）；Project 本身
// 由 forgedata.ProjectFor 推导（ConfigDir = <root>/.forge/ 存在时，否则 DataDir）。
func FindProject() (*forgedata.Project, error) {
	root, err := Find()
	if err != nil {
		return nil, err
	}
	return forgedata.ProjectFor(root)
}

// Find resolves the current working directory to the forge project root.
//
// Find 把当前工作目录解析到 forge 项目根。cwd 不在 forge 项目内时返回
// forgedata.ErrNoForgeConfig。
//
// 用户 home 下的 ~/.forge/ 是 GLOBAL state store，不是项目根——遗留 walk-up 排除
// 它（真实项目的 .forge/ 总是比 home 离 cwd 更近，此排除不会遮蔽合法项目）。
func Find() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// 1. 注册表成员资格（重构后的锚点）。
	if root, ok := registry.IsMember(cwd); ok {
		return root, nil
	}

	// 2. 遗留兜底：向上 walk 找项目级 .forge/（user-level-assets 重构前 init 的
	//    项目）。自愈：登记该项目，后续解析走快路径（条目同时获得 key）。
	//    Project Policy Layer P1：declined 的遗留项目不自愈——退出一票否决，返回
	//    registry.ErrDeclinedProject（hook 分发对任何 Find 失败都静默放行，无需特判；
	//    CLI 侧该错误文案即面向用户的 declined 提示）。
	if root, ok := legacyFind(cwd); ok {
		if _, state := registry.State(root); state == registry.StatusDeclined {
			return "", registry.ErrDeclinedProject
		}
		_ = registry.Add(root) // 自愈登记，失败不阻断
		return root, nil
	}

	return "", forgedata.ErrNoForgeConfig
}

// legacyFind 从 dir 向上查找最近的、含项目 .forge/ 子目录的目录。两道边界防止把
// 全局 store 误认成项目标记：
//  1. 到有效用户 home（os.UserHomeDir）即停——home 下的 ~/.forge 是全局 store，
//     绝不是项目；
//  2. 跳过任何内含 projects.json 的 .forge/——projects.json 是全局注册表文件，
//     该 .forge 即全局 store 本身。第二道才是 HOME 被覆盖环境（测试）的保障：
//     walk 可能越过假 home 命中真实的 ~/.forge，此时有效 home 比较已帮不上忙
//     （实测：临时目录里 forge status 解析出 "Project: Administrator"）。
func legacyFind(dir string) (string, bool) {
	d, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	homeDir, _ := os.UserHomeDir() // 有效 home 边界；解析失败则不设（退化原行为）
	for {
		if homeDir != "" && samePath(d, homeDir) {
			return "", false // 到 home 边界停止
		}
		candidate := filepath.Join(d, ".forge")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			// 全局 store 嗅探：用户级 store（~/.forge 或 FORGE_DATA_HOME）必然带有
			// 全局独有的产物（projects.json 注册表 和/或 projects/ 按项目数据树
			// 和/或 skills-cache/），而项目 .forge/ 绝不会包含这些。三者都查是
			// 必要的：仅由 `forge data-dir` MkdirAll 创建的 store 只有 projects/
			// 还没有 projects.json（CI 实证：runner home 的 .forge 只含
			// projects/，只查 projects.json 的嗅探漏判，home 下所有临时目录
			// 都解析成 "Project: runneradmin"）。
			if isGlobalForgeStore(candidate) {
				return "", false // 全局 store，不是项目标记
			}
			return d, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		d = parent
	}
}

// samePath 报告 a 与 b 是否指向同一文件系统路径。用 os.SameFile
// （device+inode），可跨大小写不敏感、symlink、分隔符/风格差异（Git Bash
// 形如 /c/Users 对 Windows 形如 C:\Users）保持稳健。
// 任一路径无法 stat 时回退到 cleaned lexical compare。
func samePath(a, b string) bool {
	ia, ea := os.Stat(a)
	ib, eb := os.Stat(b)
	if ea == nil && eb == nil {
		return os.SameFile(ia, ib)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// globalStoreMarkers 列出只有用户级全局 store（~/.forge 或 FORGE_DATA_HOME）
// 才会含的产物：注册表文件、按项目数据树、skills 分发缓存/manifest、文件备份、
// init-suggest 标记目录。项目 .forge/ 装的是 hooks/、protocol.yml、team-mode、
// .sync-version——与这些名字无交集。
var globalStoreMarkers = []string{
	"projects.json",
	"projects",
	"skills-cache",
	"skills-manifest.json",
	"backups",
	".init-suggested",
}

// isGlobalForgeStore 按内容报告一个 .forge 目录是否用户级全局 store（而非项目
// 标记）：任一 globalStoreMarkers 条目存在即全局 store。仅由 `forge data-dir`
// MkdirAll 创建的 store 只有 projects/ 还没有 projects.json；被 skills 分发
// 碰过的 store 带 skills-cache/skills-manifest.json——查全量标记覆盖所有
// 创建者（CI 实证：runner home 的 .forge 让只查 projects.json 的嗅探漏判，
// home 下所有临时目录都解析成 "Project: runneradmin"）。
func isGlobalForgeStore(forgeDir string) bool {
	for _, m := range globalStoreMarkers {
		if _, err := os.Stat(filepath.Join(forgeDir, m)); err == nil {
			return true
		}
	}
	return false
}
