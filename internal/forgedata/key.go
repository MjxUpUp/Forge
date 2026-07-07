// Package forgedata 提供 forge 项目数据的统一路径 / key 推导。
//
// 设计目标：把当前散落的 `filepath.Join(dir, ".forge", ...)` 集中到一个真相源，
// 让项目数据 home（tasks / gates / ...）从项目级 `.forge/` 平滑迁移
// 到用户级 `~/.forge/projects/<hash12>/`。ConfigDir（protocol/CLAUDE.md
// /hooks）仍留项目级（git tracked，user-editable）。
//
// 详见 docs/plans/refactor-data-home.md。
//
// 中文字符串用 raw string（反引号）规避 Windows 输入引号腐蚀。
package forgedata

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Predefined errors
var (
	// ErrNotInGitRepo: cwd 不在任何 git repo 内
	ErrNotInGitRepo = errors.New(`forgedata: cwd is not in a git repository`)

	// ErrInvalidGitFile: `.git` file 损坏（empty / missing 'gitdir:' / NUL / looped / beyond fs root）
	ErrInvalidGitFile = errors.New(`forgedata: invalid .git file (worktree/submodule)`)
)

// FindGitRoot walks up from dir 到最近的含 `.git`（dir 或 file）祖先。找不到返 ""。
// 不依赖 forge 项目存在——只是 git repo 探测。
//
// 与 cli/hook.go:51 的 findGitRoot 同语义；不复用是因 forgedata 是基础设施包，
// 不想反向依赖 cli。等 Stage 1 接入时把 cli 那个重构为调 forge data 这个。
func FindGitRoot(dir string) string {
	d := filepath.Clean(dir)
	for {
		git := filepath.Join(d, ".git")
		if _, err := os.Stat(git); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "" // Unix "/" 或 Windows 盘根
		}
		d = parent
	}
}

// Key 推导 hash12：cwd 所在 git repo 的 `.git` common dir 的 FNV-64a hex 前 12 字符。
// 同仓库多 worktree（agent / detached / submodule）共享同一 key。
//
// 算法：
//  1. findGitRoot(cwd) —  失败 → ErrNotInGitRepo
//  2. .git 是 dir（主 worktree）→ 直接用
//     .git 是 file（worktree/submodule）→ 读 gitdir: 行 → 沿 parent 链找 .git 祖先
//  3. EvalSymlinks 后置归一（symlinked repo 同 key）
//  4. fnv-64a(... )[:12]
//
// ErrInvalidGitFile 在 .git file 损坏时返（empty / 缺 'gitdir:' / 含 NUL / 循环 / 越过 fs 根）。
func Key(cwd string) (string, error) {
	gitRoot := FindGitRoot(cwd)
	if gitRoot == "" {
		return "", ErrNotInGitRepo
	}
	absGit := filepath.Clean(filepath.Join(gitRoot, ".git"))

	info, err := os.Stat(absGit)
	if err != nil {
		return "", ErrNotInGitRepo // .git 不存在（与"非 git"等效）
	}

	var resolvedGitDir string
	if info.IsDir() {
		// 主 worktree —— .git 自身已稳定
		resolvedGitDir = absGit
	} else {
		// .git 是 file（worktree / submodule）
		resolvedGitDir, err = resolveGitFile(absGit, gitRoot)
		if err != nil {
			return "", err
		}
	}

	// EvalSymlinks 归物理路径—— symlinked repo 同 key（避免同 inode 多 key）
	if eval, err := filepath.EvalSymlinks(resolvedGitDir); err == nil {
		resolvedGitDir = eval
	}

	h := fnv.New64a()
	h.Write([]byte(filepath.Clean(resolvedGitDir)))
	return strconv.FormatUint(h.Sum64(), 16)[:12], nil
}

// resolveGitFile 解析 worktree/submodule `.git` file 的 gitdir: 行，沿 parent 链找 `.git` 祖先。
// 容错：empty / 缺 prefix / NUL / 循环 / fs 根外 全部返 ErrInvalidGitFile。
func resolveGitFile(absGitFile, gitRoot string) (string, error) {
	data, err := os.ReadFile(absGitFile)
	if err != nil {
		return "", fmt.Errorf(`%w: %s`, ErrInvalidGitFile, err.Error())
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return "", fmt.Errorf(`%w: empty .git file`, ErrInvalidGitFile)
	}
	// 第一行 "gitdir: /path/..." 或 "gitdir: ../relative"
	line := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	if !strings.HasPrefix(line, "gitdir: ") {
		return "", fmt.Errorf(`%w: missing 'gitdir:' prefix`, ErrInvalidGitFile)
	}
	relRaw := strings.TrimSpace(strings.TrimPrefix(line, "gitdir: "))
	if relRaw == `` || strings.ContainsRune(relRaw, 0) {
		return "", fmt.Errorf(`%w: invalid gitdir value`, ErrInvalidGitFile)
	}

	var target string
	if filepath.IsAbs(relRaw) {
		target = relRaw
	} else {
		target = filepath.Join(gitRoot, relRaw)
	}
	target = filepath.Clean(target)

	// 沿 parent 链找含名 `.git` 的祖先
	const safetyMax = 64
	candidate := target
	for safety := safetyMax; filepath.Base(candidate) != ".git"; safety-- {
		// 终止条件：候选已退化到空/点/根
		if candidate == `` || candidate == "." || candidate == string(filepath.Separator) || safety <= 0 {
			return "", fmt.Errorf(`%w: gitdir did not resolve to a .git ancestor: %s`, ErrInvalidGitFile, target)
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", fmt.Errorf(`%w: gitdir resolved beyond filesystem root: %s`, ErrInvalidGitFile, target)
		}
		candidate = parent
	}
	return filepath.Clean(candidate), nil
}

// RootDir 返回 `~/.forge/projects/<key>/` 路径。
//
// `FORGE_DATA_HOME` env 覆盖全局 home（test 隔离 + 高级用户覆盖）。
//
// 空 key 返 ""（caller 决定 fallback，不强默认）。
func RootDir(key string) string {
	if key == "" {
		return ""
	}
	home, err := GlobalHome()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "projects", key)
}

// GlobalHome 返全局 home。FORGE_DATA_HOME 优先（覆盖 home root），否则回落 UserHomeDir。
//
// 设计：FORGE_DATA_HOME 既管控全局 home（如 ~/.forge），所有 sub-store
// （registry/projects.json、init-suggest marker、knowledge、projects/<key>/）都用它。
// 导出供 registry/suggest 等全局 store 复用同一真相源（refactor-data-home commit E：
// 统一 FORGE_DATA_HOME，废弃 registry 旧的 FORGE_HOME env）。
func GlobalHome() (string, error) {
	if h := os.Getenv("FORGE_DATA_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".forge"), nil
}
