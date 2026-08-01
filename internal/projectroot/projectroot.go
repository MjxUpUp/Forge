// Package projectroot resolves the forge project root from the current working
// directory.
//
// After the user-level-assets refactor, the anchor of "is this a forge project" is
// the GLOBAL REGISTRY (~/.forge/projects.json), not a project-level .forge/ marker:
// forge init writes nothing into the project by default. Resolution order:
//  1. registry.IsMember(cwd) — git-key match (worktree-safe) or non-git path-prefix match
//  2. legacy fallback: walk up for a project-level .forge/ (projects init'd by older
//     forge versions) — on hit, self-heal by registering the project
//  3. otherwise: ErrNoForgeConfig
//
// Centralizes the "which project am I in" logic in one place to
// avoid cross-package duplication (originally extracted from cli/root.go and
// the now-removed mcpserver/server.go; mcpserver removed on 2026-07-24).
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

// FindProject resolves cwd → *forgedata.Project (Key / Root / GitRoot / DataDir /
// ConfigDir). Membership is judged via the global registry (with the legacy .forge/
// walk-up as fallback); the Project itself is derived by forgedata.ProjectFor
// (ConfigDir = <root>/.forge/ when it exists, else DataDir).
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

// Find resolves the current working directory to the forge project root. Returns
// forgedata.ErrNoForgeConfig when cwd is not inside a forge project.
//
// ~/.forge/ under the user home is a GLOBAL state store, not a project root — the
// legacy walk-up excludes it (a real project's .forge/ is always closer to cwd than
// home, so this exclusion never shadows a legitimate project).
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

	// 1. Registry membership (the post-refactor anchor).
	//
	// 1. 注册表成员资格（重构后的锚点）。
	if root, ok := registry.IsMember(cwd); ok {
		return root, nil
	}

	// 2. Legacy fallback: walk up for a project-level .forge/ (projects init'd
	//    before the user-level-assets refactor). Self-heal: register the project so
	//    subsequent resolutions hit the fast path (and the entry gains a key).
	//
	// 2. 遗留兜底：向上 walk 找项目级 .forge/（user-level-assets 重构前 init 的
	//    项目）。自愈：登记该项目，后续解析走快路径（条目同时获得 key）。
	if root, ok := legacyFind(cwd); ok {
		_ = registry.Add(root) // 自愈登记，失败不阻断
		return root, nil
	}

	return "", forgedata.ErrNoForgeConfig
}

// legacyFind walks up from dir to find the nearest directory containing a project
// .forge/ subdirectory. Two boundaries prevent mistaking the GLOBAL STORE for a
// project marker:
//  1. stop at the effective user home (os.UserHomeDir) — ~/.forge under home is the
//     global store, never a project;
//  2. skip any .forge/ that CONTAINS projects.json — that is the global registry
//     file, so the .forge is the global store itself. This second check is what saves
//     HOME-overridden environments (tests): the walk can reach the REAL ~/.forge
//     above the fake home, where the effective-home comparison no longer helps
//     (observed: forge status in a temp dir resolving to "Project: Administrator").
//
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
			if _, err := os.Stat(filepath.Join(candidate, "projects.json")); err == nil {
				return "", false // 全局 store（内含全局注册表），不是项目标记
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

// samePath reports whether a and b point to the same filesystem path. Uses
// os.SameFile (device+inode), staying robust across case-insensitivity,
// symlinks, and separator/style differences (Git Bash form /c/Users vs Windows
// form C:\Users). Falls back to cleaned lexical compare when either path
// cannot be stat-ed.
//
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
