// Package projectroot resolves the forge project root from the current working
// directory (the directory containing the .forge/ subdirectory).
//
// Centralizes the "walk up from cwd to find .forge/" logic in one place to
// avoid cross-package duplication (originally extracted from cli/root.go and
// the now-removed mcpserver/server.go; mcpserver removed on 2026-07-24).
//
// Package projectroot 从当前工作目录解析 forge project root
// （即包含 .forge/ 子目录的目录）。
//
// 把"从 cwd 向上 walk up 找 .forge/"的逻辑集中在一处，
// 避免跨包重复（最初从 cli/root.go 以及现已移除的 mcpserver/server.go
// 中抽取；mcpserver 于 2026-07-24 移除）。
package projectroot

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// FindProject resolves cwd → *forgedata.Project (three roots: GitRoot / DataDir
// / ConfigDir).
//
// This is the main entry of the dual-root architecture
// (docs/plans/refactor-data-home.md):
//   - GitRoot   = git working-tree root (the basis for git -C operations)
//   - DataDir   = ~/.forge/projects/<hash12>/ (runtime state: state.json/tasks/
//                 gates/...)
//   - ConfigDir = <gitroot>/.forge/ (project config: pipeline.yml/protocol.yml/
//                 CLAUDE.md/hooks/)
//
// Difference from the old Find: Find returns only the single root 「directory
// containing .forge」; FindProject returns three roots, and the caller picks by
// purpose (runtime state → DataDir, config → ConfigDir, git ops → GitRoot).
//
// The global home ~/.forge is naturally excluded: forgedata.ProjectFor requires
// cwd to be inside a git repo, and findForgeConfigDir's walk-up stops at the
// gitRoot boundary—~/.forge is not within any project git repo's gitRoot
// subtree (unless the user makes home itself a git repo, an extreme edge case).
//
// FindProject 解析 cwd → *forgedata.Project（三根：GitRoot / DataDir / ConfigDir）。
//
// 这是双根架构（docs/plans/refactor-data-home.md）的主入口：
//   - GitRoot   = git working tree 根（git -C 操作基准）
//   - DataDir   = ~/.forge/projects/<hash12>/ （runtime state：state.json/tasks/gates/...）
//   - ConfigDir = <gitroot>/.forge/ （项目配置：pipeline.yml/protocol.yml/CLAUDE.md/hooks/）
//
// 与旧 Find 的区别：Find 只返回"含 .forge 的目录"单根；FindProject 返回三根，
// caller 按用途取（runtime state 用 DataDir，config 用 ConfigDir，git 操作用 GitRoot）。
//
// ~/.forge 全局 home 天然被排除：forgedata.ProjectFor 要求 cwd 在 git repo 内，
// 且 findForgeConfigDir walk-up 不超过 gitRoot 边界——~/.forge 不在任何项目 git repo
// 的 gitRoot 子树内（除非用户把 home 本身设成 git repo，属极边界异常）。
func FindProject() (*forgedata.Project, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return forgedata.ProjectFor(cwd)
}

// Find walks up from the current working directory to find the nearest
// directory containing a project .forge/ subdirectory. Returns the project
// root; returns an error when cwd is not inside a forge project.
//
// Keeps the legacy walk-up implementation (does not delegate to FindProject):
// FindProject requires cwd to be inside a git repo (forgedata.Key failure is
// an error), but Find historically supports non-git projects (anything with
// .forge/, e.g. the task-nongit case). The two differ semantically and
// coexist until all callers migrate.
//
// ~/.forge/ under the user home is a GLOBAL state store (hooks, skills,
// per-project runtime state under projects/<key>/), not a project root.
// Excluding it makes running forge from a non-project directory under home
// (e.g. ~/Downloads) report "not in a forge project" rather than mistaking
// home for the project root. A real project's .forge/ is always closer to cwd
// than home, so this exclusion never shadows a legitimate project.
//
// Find 从当前工作目录向上查找最近的、含项目 .forge/ 子目录的目录。
// 返回 project root；cwd 不在 forge project 内时返回 error。
//
// 保留旧 walk-up 实现（不委托 FindProject）：FindProject 要求 cwd 在 git repo 内
// （forgedata.Key 失败即报错），但 Find 历史上支持非 git 项目（只要有 .forge/，
// 如 task-nongit 场景）。两者语义不同，共存到全部 caller 迁移完毕。
//
// 用户 home 目录下的 ~/.forge/ 是 GLOBAL state store（hooks、skills、
// projects/<key>/ 下的 per-project runtime state），而非 project root。
// 把它排除，使在 home 下非项目目录（如 ~/Downloads）运行 forge 时
// 报 not in a forge project，而不是把 home 误作 project root。
// 真实项目的 .forge/ 总是比 home 离 cwd 更近，故此排除不会遮蔽合法项目。
func Find() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	homeDir, _ := os.UserHomeDir() // 全局状态目录约定在 home/.forge；解析失败则不排除（退化原行为）
	for {
		if isProjectRoot(dir, homeDir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not in a forge project (no .forge/ directory found)")
		}
		dir = parent
	}
}

// isProjectRoot reports whether dir holds a project .forge/ directory: dir
// must contain .forge/ and must not be the user home (~/.forge/ under home is
// a global state store, indistinguishable from a project-level .forge/ by name
// or content—both carry checklog.jsonl/toollog.jsonl; location is the only
// clean discriminator).
//
// isProjectRoot 报告 dir 是否持有一个项目 .forge/ 目录：dir 必须含 .forge/
// 且不得是用户 home（home 下的 ~/.forge/ 是 global state store，从名字或
// 内容上都与项目级 .forge/ 无法区分——两者都带 checklog.jsonl/toollog.jsonl；
// 位置是唯一干净的判别依据）。
func isProjectRoot(dir, homeDir string) bool {
	if info, err := os.Stat(filepath.Join(dir, ".forge")); err != nil || !info.IsDir() {
		return false
	}
	if homeDir != "" && samePath(dir, homeDir) {
		return false
	}
	return true
}

// samePath reports whether a and b point to the same filesystem path. Uses
// os.SameFile (device+inode), staying robust across case-insensitivity,
// symlinks, and separator/style differences (Git Bash form /c/Users vs Windows
// form C:\Users). Falls back to cleaned lexical compare when either path
// cannot be stat-ed.
//
// samePath 报告 a 与 b 是否指向同一文件系统路径。用 os.SameFile
// （device+inode），可跨大小写不敏感、symlink、分隔符/风格差异
// （Git Bash 形如 /c/Users 对 Windows 形如 C:\Users）保持稳健。
// 任一路径无法 stat 时回退到 cleaned lexical compare。
func samePath(a, b string) bool {
	ia, ea := os.Stat(a)
	ib, eb := os.Stat(b)
	if ea == nil && eb == nil {
		return os.SameFile(ia, ib)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
