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
//
// stamps 与 hazards 刻意不在列（2026-08-15 信任边界审查）：.forge/ 是可提交进 repo 的，从它
// 提升的任何内容都是攻击者可书写的——clone 一个恶意仓库即可带上被 forge 当本机信任锚的
// stamps/hazard 确认（控制高危命令的 repo 可离线推算 hazard 指纹）。hazard 确认本身是 5 分钟
// TTL 标记，迁移旧值也毫无价值。两者保持项目局部，永不迁移。
//
// 顺序是信任边界要求："tasks" 必须排最后。调用方只在 tasks 迁移实际完成（tasks ∈ Moved）
// 时对提升的 task 文件清洗外来门禁信号；tasks 排第一时，更晚条目失败会把已提升、未清洗的
// task 文件搁浅在 DataDir（重跑会 skip 该次迁移——skip 永不清洗——hostile 状态永久存活）。
// tasks 排最后使任何更早的失败在第一个 task 文件落地前中止。
var runtimeDirs = []string{
	"gates", "act", "sessions", "quarantine",
	"tasks",
}

// runtimeFiles 是 .forge/ 下属 runtime state 的单文件（无归档变体）。
//
// 已知信任残留（2026-08-16 复审）：checklog.jsonl / toollog.jsonl 从可提交的 .forge/ 逐字提升，
// 即攻击者可书写——而 work-activity（HARD 门禁）以 toollog 条目计数为本机证据，恶意仓库可
// 预先满足它。暂接受：仅 legacy 路径可能带上这些（新装从不往 .forge/ 写 toollog），且
// work-activity 本就按弱证据设计（工具调用是 advisory 级代理，非审查证明）。任务级信任信号
// 不在此残留内——它们在 tasks/*.json，走 StripForeignGateSignals。
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
//
// MigrateProject 把 p.ConfigDir（.forge/）下的 runtime state 搬到 p.DataDir。
// 幂等：runtime state 已在 DataDir 时 .forge/ 无 runtime，Moved 为空。
//
// Ensure 先建 DataDir（含 .migration-meta.json），各 store 后续写入路径必然存在。
func MigrateProject(p *Project, opts MigrateOptions) (*MigrationResult, error) {
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
			// 权限/IO 错误不是「不存在」：当不存在静默跳过会让条目从报告凭空消失
			// （数据残留或别处半迁移）。显式报错，中止本次迁移。
			return res, fmt.Errorf("stat %s: %w", name, err)
		}
		moved, err := migrateOne(src, filepath.Join(p.DataDir, name), opts)
		if err != nil {
			// 回滚「半途的 tasks 迁移」：moveEntry 跨设备 fallback（copyTree + 删源）可能
			// 半路死掉，在 DataDir 留下半个 hostile tasks 树——重跑会 SKIP（dst 已存在）且
			// 永不清洗。此刻 dst 上的任何内容都纯粹是本次的部分拷贝（非 force：dst 原不存在；
			// force：dst 已在上方被删），删除是安全的。
			if name == "tasks" {
				// 回滚守护的是信任边界（未清洗的 hostile tasks 不得残留在 dst；
				// 重跑会 SKIP 跳过它们）。回滚自身失败即不变量已破——把路径一起
				// 上抛而不是 `_ =`，否则残留静默活过每次重试。
				if rerr := os.RemoveAll(filepath.Join(p.DataDir, name)); rerr != nil {
					return res, fmt.Errorf("migrate %s 失败且回滚失败（DataDir 可能残留未清洗的 tasks，需手动删除 %s 后重跑）: %w (rollback: %v)", name, filepath.Join(p.DataDir, name), err, rerr)
				}
			}
			return res, fmt.Errorf("migrate %s: %w", name, err)
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
				// 随 error 一起返回部分结果（见上方循环）：调用方读 res.Moved 判断即便中止
				// 也是否需要清洗已提升的 task 文件。
				return res, fmt.Errorf("migrate %s: %w", name, err)
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
// 返 true 表示「将迁移/覆盖」，false 表示「将 skip」。
func migrateOne(src, dst string, opts MigrateOptions) (bool, error) {
	if opts.DryRun {
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
