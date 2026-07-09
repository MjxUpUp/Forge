package forgedata

// migrate.go —— 旧项目级 .forge/ runtime state → 用户级 DataDir 一次性迁移。
//
// 背景（refactor-data-home）：runtime state（tasks/gates/checklog/toollog/act/
// sessions/quarantine/active-task-ref/.task-verify-throttle.last 等）从项目级
// <root>/.forge/ 迁到用户级 ~/.forge/projects/<key>/（DataDir）。release 前已用旧
// forge 版本积累过 runtime state 的项目，升级到新版本后用 `forge migrate` 把遗留
// .forge/ runtime state 搬到 DataDir；新写的 runtime state 已直接落 DataDir。
//
// 项目配置（ConfigDir）不迁：hooks/（项目配置 hook 脚本，区别于 runtime 副本
// DataDir/hooks——僵尸 accessor 不涉迁移）/protocol.yml/CLAUDE.md/AGENTS.md 仍留
// <root>/.forge/。
//
// 安全设计：
//   - 白名单（明确列出 runtime state 名），不盲目迁整个 .forge/——防误迁配置
//   - 幂等：重复跑 Moved 空（runtime 已在 DataDir，.forge/ 无 runtime）
//   - --dry-run 预览不执行；--force 覆盖 DataDir 已有同名（默认 skip）
//   - 跨设备 Rename 失败（项目盘 ≠ home 盘，Windows D:→C: 常见）fallback copy+remove

import (
	"fmt"
	"os"
	"path/filepath"
)

// runtimeDirs 是 .forge/ 下属 runtime state 的目录（迁到 DataDir 同名）。
// 基于 commit B/D/E 实际迁移的 store。ConfigDir 配置目录（hooks/）不在此列——
// hooks/ 是项目配置 hook 脚本（ConfigHooksDir），不是 runtime。
var runtimeDirs = []string{
	"tasks", "gates", "hazards",
	"act", "stamps", "sessions", "quarantine",
}

// runtimeFiles 是 .forge/ 下属 runtime state 的单文件（无归档变体）。
var runtimeFiles = []string{
	"sessions.jsonl", "session.json",
	"checklog.jsonl", "toollog.jsonl",
	".task-verify-throttle.last",
}

// runtimeGlobs 是 .forge/ 下属 runtime state 的 glob 模式（归档/session-scoped 变体）。
// active-task-ref* 覆盖 legacy active-task-ref + session-scoped active-task-ref-<sid>。
// checklog-*.jsonl / toollog-*.jsonl 是带时间戳的归档（区别于 runtimeFiles 的主文件）。
var runtimeGlobs = []string{
	"checklog-*.jsonl", "toollog-*.jsonl",
	"active-task-ref*",
}

// MigrateOptions 控制 MigrateProject 行为。
type MigrateOptions struct {
	DryRun bool // 只分类报告将迁移，不实际移动
	Force  bool // DataDir 已有同名时覆盖（默认 skip）
}

// MigrationResult 记录迁移明细，供命令层打印报告。
type MigrationResult struct {
	Moved   []string // 成功迁到 DataDir 的条目（相对名）
	Skipped []string // DataDir 已有同名且非 Force，跳过
	Left    []string // 迁移后 ConfigDir 剩余条目（配置 + 未知），仅非 DryRun 填
}

// MigrateProject 把 p.ConfigDir（.forge/）下的 runtime state 搬到 p.DataDir。
// 幂等：runtime state 已在 DataDir 时 .forge/ 无 runtime，Moved 为空。
//
// Ensure 先建 DataDir（含 .migration-meta.json），各 store 后续写入路径必然存在。
func MigrateProject(p *Project, opts MigrateOptions) (*MigrationResult, error) {
	if err := p.Ensure(); err != nil {
		return nil, fmt.Errorf("ensure DataDir: %w", err)
	}
	res := &MigrationResult{}

	// 显式目录 + 文件
	names := make([]string, 0, len(runtimeDirs)+len(runtimeFiles))
	names = append(names, runtimeDirs...)
	names = append(names, runtimeFiles...)
	for _, name := range names {
		src := filepath.Join(p.ConfigDir, name)
		if _, err := os.Stat(src); err != nil {
			continue // 不存在，跳过
		}
		moved, err := migrateOne(src, filepath.Join(p.DataDir, name), opts)
		if err != nil {
			return nil, fmt.Errorf("migrate %s: %w", name, err)
		}
		if moved {
			res.Moved = append(res.Moved, name)
		} else {
			res.Skipped = append(res.Skipped, name)
		}
	}

	// glob 模式（归档/session 变体）
	for _, pattern := range runtimeGlobs {
		matches, err := filepath.Glob(filepath.Join(p.ConfigDir, pattern))
		if err != nil {
			continue
		}
		for _, src := range matches {
			name := filepath.Base(src)
			moved, err := migrateOne(src, filepath.Join(p.DataDir, name), opts)
			if err != nil {
				return nil, fmt.Errorf("migrate %s: %w", name, err)
			}
			if moved {
				res.Moved = append(res.Moved, name)
			} else {
				res.Skipped = append(res.Skipped, name)
			}
		}
	}

	// 记录迁移后 ConfigDir 剩余（配置 + 未知条目）。DryRun 不执行，剩余无意义，不填。
	if !opts.DryRun {
		entries, err := os.ReadDir(p.ConfigDir)
		if err == nil {
			for _, e := range entries {
				res.Left = append(res.Left, e.Name())
			}
		}
	}
	return res, nil
}

// migrateOne 移动 src→dst，返回 (是否实际移动, error)。
// dst 已存在：非 Force skip（返 false 无 err），Force 先删 dst 再移。
// DryRun 时**完全不动文件**（含不删 dst——前置检查防 dry-run+force 误删 DataDir 数据），
// 返 true 表示"将迁移/覆盖"，false 表示"将 skip"。
func migrateOne(src, dst string, opts MigrateOptions) (bool, error) {
	if opts.DryRun {
		// dry-run 只报告意图，不碰任何文件（尤其不删 dst）
		if _, err := os.Stat(dst); err == nil && !opts.Force {
			return false, nil // dst 已有且非 force → 将 skip
		}
		return true, nil // 将迁移（dst 不存在）或将覆盖（force）
	}
	if _, err := os.Stat(dst); err == nil {
		if !opts.Force {
			return false, nil // skip
		}
		if err := os.RemoveAll(dst); err != nil {
			return false, fmt.Errorf("force-remove dst: %w", err)
		}
	}
	if err := moveEntry(src, dst); err != nil {
		return false, err
	}
	return true, nil
}

// moveEntry 优先 os.Rename（同设备原子，整棵子树一次移动），跨设备失败 fallback
// 递归 copy + remove。项目盘 ≠ home 盘（D: 项目 / C: ~/.forge）时 Rename 返 link
// error（EXDEV 等价），copyTree 保底。
func moveEntry(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyTree(src, dst); err != nil {
		return fmt.Errorf("copy fallback: %w", err)
	}
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("remove src after copy: %w", err)
	}
	return nil
}

// copyTree 递归复制 src→dst（文件或目录），保留模式。runtime state 无 symlink，
// 用 os.Stat（跟随）足够；遇 symlink 指向 dir 时按 dir 复制目标内容（可接受）。
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode())
}
