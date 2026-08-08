package forgedata

// migrate.go performs a one-shot migration of project-level .forge/ runtime state to the user-level DataDir.
//
// migrate.go —— 旧项目级 .forge/ runtime state → 用户级 DataDir 一次性迁移。
//
// Background (refactor-data-home): runtime state (tasks/gates/checklog/toollog/act/
// sessions/quarantine/active-task-ref/.task-verify-throttle.last, etc.) moves from project-level
// <root>/.forge/ to user-level ~/.forge/projects/<key>/ (DataDir). Projects that accumulated
// runtime state with an older forge version before release run `forge migrate` after upgrade to
// move legacy .forge/ runtime state into DataDir; new runtime state is written directly to DataDir.
//
// 背景（refactor-data-home）：runtime state（tasks/gates/checklog/toollog/act/
// sessions/quarantine/active-task-ref/.task-verify-throttle.last 等）从项目级
// <root>/.forge/ 迁到用户级 ~/.forge/projects/<key>/（DataDir）。release 前已用旧
// forge 版本积累过 runtime state 的项目，升级到新版本后用 `forge migrate` 把遗留
// .forge/ runtime state 搬到 DataDir；新写的 runtime state 已直接落 DataDir。
//
// Project config (ConfigDir) is NOT migrated: hooks/ (project-config hook scripts, distinct
// from the runtime copy DataDir/hooks — zombie accessors are not involved in migration),
// protocol.yml, CLAUDE.md, and AGENTS.md remain under <root>/.forge/.
//
// 项目配置（ConfigDir）不迁：hooks/（项目配置 hook 脚本，区别于 runtime 副本
// DataDir/hooks——僵尸 accessor 不涉迁移）/protocol.yml/CLAUDE.md/AGENTS.md 仍留
// <root>/.forge/。
//
// Safety design:
//   - Allowlist (explicit runtime state names listed); never blindly migrate the whole .forge/ — prevents accidental config migration
//   - Idempotent: re-running yields empty Moved (runtime already in DataDir, .forge/ has no runtime)
//   - --dry-run previews without executing; --force overwrites existing same-named entries in DataDir (default skip)
//   - Cross-device Rename failure (project disk ≠ home disk, Windows D:→C: common) falls back to copy+remove
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

// runtimeDirs lists directories under .forge/ belonging to runtime state (migrated to DataDir with the same name).
// Based on stores actually migrated in commits B/D/E. ConfigDir config directories (hooks/) are excluded —
// hooks/ holds project-config hook scripts (ConfigHooksDir), not runtime state.
//
// runtimeDirs 是 .forge/ 下属 runtime state 的目录（迁到 DataDir 同名）。
// 基于 commit B/D/E 实际迁移的 store。ConfigDir 配置目录（hooks/）不在此列——
// hooks/ 是项目配置 hook 脚本（ConfigHooksDir），不是 runtime。
var runtimeDirs = []string{
	"tasks", "gates", "hazards",
	"act", "stamps", "sessions", "quarantine",
}

// runtimeFiles lists single files under .forge/ belonging to runtime state (no archive variants).
//
// runtimeFiles 是 .forge/ 下属 runtime state 的单文件（无归档变体）。
var runtimeFiles = []string{
	"sessions.jsonl", "session.json",
	"checklog.jsonl", "toollog.jsonl",
	".task-verify-throttle.last",
}

// runtimeGlobs lists glob patterns under .forge/ belonging to runtime state (archive/session-scoped variants).
// active-task-ref* covers legacy active-task-ref and session-scoped active-task-ref-<sid>.
// checklog-*.jsonl / toollog-*.jsonl are timestamped archives (distinct from the main files in runtimeFiles).
//
// runtimeGlobs 是 .forge/ 下属 runtime state 的 glob 模式（归档/session-scoped 变体）。
// active-task-ref* 覆盖 legacy active-task-ref + session-scoped active-task-ref-<sid>。
// checklog-*.jsonl / toollog-*.jsonl 是带时间戳的归档（区别于 runtimeFiles 的主文件）。
var runtimeGlobs = []string{
	"checklog-*.jsonl", "toollog-*.jsonl",
	"active-task-ref*",
}

// MigrateOptions controls the behavior of MigrateProject.
//
// MigrateOptions 控制 MigrateProject 行为。
type MigrateOptions struct {
	// DryRun: only classify and report what would migrate; do not actually move.
	DryRun bool // 只分类报告将迁移，不实际移动
	// Force: overwrite when DataDir already has an entry with the same name (default skip).
	Force bool // DataDir 已有同名时覆盖（默认 skip）
}

// MigrationResult records migration details for the command layer to print a report.
//
// MigrationResult 记录迁移明细，供命令层打印报告。
type MigrationResult struct {
	// Moved: entries successfully migrated to DataDir (relative names).
	Moved []string // 成功迁到 DataDir 的条目（相对名）
	// Skipped: DataDir already has the same name and Force is not set; skipped.
	Skipped []string // DataDir 已有同名且非 Force，跳过
	// Left: entries remaining in ConfigDir after migration (config + unknown); populated only when non-DryRun.
	Left []string // 迁移后 ConfigDir 剩余条目（配置 + 未知），仅非 DryRun 填
}

// MigrateProject moves runtime state under p.ConfigDir (.forge/) to p.DataDir.
// Idempotent: when runtime state is already in DataDir, .forge/ has no runtime and Moved is empty.
//
// MigrateProject 把 p.ConfigDir（.forge/）下的 runtime state 搬到 p.DataDir。
// 幂等：runtime state 已在 DataDir 时 .forge/ 无 runtime，Moved 为空。
//
// Ensure first creates DataDir (including .migration-meta.json); each store's write path is guaranteed to exist afterward.
//
// Ensure 先建 DataDir（含 .migration-meta.json），各 store 后续写入路径必然存在。
func MigrateProject(p *Project, opts MigrateOptions) (*MigrationResult, error) {
	// Zero-project-write projects have ConfigDir == DataDir: every whitelist entry's
	// src IS its dst. There is no project-level residue to migrate — and continuing
	// would be destructive: under --force migrateOne would RemoveAll(dst) (deleting the
	// live DataDir data itself) before the move, and even non-force would report a
	// misleading "skipped, use --force" hint that steers the user toward that path.
	// Return an empty result instead.
	//
	// 零项目写入项目 ConfigDir == DataDir：白名单每条 src 就是 dst。无项目级残留
	// 可迁——继续跑还是破坏性的：--force 下 migrateOne 会先 RemoveAll(dst)（把活的
	// DataDir 数据本身删掉）再移动，非 force 也会报告误导性的「已跳过，--force 覆盖」
	// 提示，把用户引向破坏路径。直接返回空结果。
	if filepath.Clean(p.ConfigDir) == filepath.Clean(p.DataDir) {
		return &MigrationResult{}, nil
	}
	if err := p.Ensure(); err != nil {
		return nil, fmt.Errorf("ensure DataDir: %w", err)
	}
	res := &MigrationResult{}

	// Explicit directories + files
	//
	// 显式目录 + 文件
	names := make([]string, 0, len(runtimeDirs)+len(runtimeFiles))
	names = append(names, runtimeDirs...)
	names = append(names, runtimeFiles...)
	for _, name := range names {
		src := filepath.Join(p.ConfigDir, name)
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				// Does not exist; skip.
				continue // 不存在，跳过
			}
			// Permission/IO errors are NOT "does not exist": silently treating them as absent
			// would drop the entry from the report while its data stays behind (or gets
			// half-migrated elsewhere). Surface the failure and abort this run.
			//
			// 权限/IO 错误不是「不存在」：当不存在静默跳过会让条目从报告凭空消失
			// （数据残留或别处半迁移）。显式报错，中止本次迁移。
			return nil, fmt.Errorf("stat %s: %w", name, err)
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

	// glob patterns (archive/session variants)
	//
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

	// Record remaining entries in ConfigDir after migration (config + unknown entries). DryRun does not execute, so leftovers are meaningless and left empty.
	//
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

// migrateOne moves src→dst, returning (actually moved, error).
// If dst already exists: skip unless Force (returns false with no error); under Force, dst is removed first then moved.
// DryRun leaves all files untouched (including dst — the pre-check prevents dry-run+force from deleting DataDir data);
// returns true meaning would migrate/overwrite, false meaning would skip.
//
// migrateOne 移动 src→dst，返回 (是否实际移动, error)。
// dst 已存在：非 Force skip（返 false 无 err），Force 先删 dst 再移。
// DryRun 时**完全不动文件**（含不删 dst——前置检查防 dry-run+force 误删 DataDir 数据），
// 返 true 表示「将迁移/覆盖」，false 表示「将 skip」。
func migrateOne(src, dst string, opts MigrateOptions) (bool, error) {
	if opts.DryRun {
		// dry-run only reports intent and touches no files (especially does not delete dst)
		// dry-run 只报告意图，不碰任何文件（尤其不删 dst）
		exists, err := statExists(dst)
		if err != nil {
			return false, fmt.Errorf("stat dst: %w", err)
		}
		if exists && !opts.Force {
			return false, nil // dst 已有且非 force → 将 skip
		}
		return true, nil // 将迁移（dst 不存在）或将覆盖（force）
	}
	exists, err := statExists(dst)
	if err != nil {
		return false, fmt.Errorf("stat dst: %w", err)
	}
	if exists {
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

// statExists classifies os.Stat: (true/false, nil) for exists / does-not-exist, and a real
// error for anything else (permission/IO/invalid-path) so callers never treat an IO failure
// as "absent" — a migration could otherwise delete a source whose destination was never
// verified, or silently drop an entry from the report.
//
// statExists 分类 os.Stat 结果：(true/false, nil) 表存在/不存在，其余错误（权限/IO/
// 非法路径）原样返回——调用方不得把 IO 失败当「不存在」，否则迁移可能在目标未验证时
// 删掉源，或让条目从报告凭空消失。
func statExists(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// moveEntry prefers os.Rename (same-device atomic, moves the whole subtree at once); on cross-device failure it
// falls back to recursive copy + remove. When the project disk differs from the home disk (D: project / C: ~/.forge),
// Rename returns a link error (EXDEV equivalent) and copyTree is the safety net.
//
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

// copyTree recursively copies src→dst (file or directory), preserving mode. Runtime state has no symlinks,
// so os.Stat (which follows) is sufficient; when a symlink points to a dir, the target contents are copied as a dir (acceptable).
//
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
