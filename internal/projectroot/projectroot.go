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
