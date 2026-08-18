package projectsync

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// allowlist.go — the single source of truth for what a project bundle carries.
// DEFAULT-DENY by design: only the explicitly enumerated portable classes enter;
// every machine-local file (anchors, sentinels, stamps, backups) and every sensitive
// store (quarantine source dumps, hazard command lines) stays out unless explicitly
// included. Future file classes are excluded by construction — a new sentinel or
// anchor added to forge never leaks into bundles until deliberately allowlisted.
//
// allowlist.go —— 项目 bundle 携带内容的单一真相源。按设计默认拒绝：只有显式
// 枚举的可移植类进入；一切机器本地文件（锚/sentinel/戳/备份）与敏感 store
// （quarantine 源码倾倒、hazard 命令行）保持在外，除非显式 include。未来文件类
// 按构造排除——forge 新增的 sentinel 或锚绝不泄进 bundle，直到被刻意加进清单。

// IncludeQuarantine / IncludeHazards are the opt-in sensitive stores.
//
// IncludeQuarantine / IncludeHazards 是显式选入的敏感 store。
const (
	IncludeQuarantine = `quarantine`
	IncludeHazards    = `hazards`
)

// ExportFiles walks dataDir and returns the slash-separated DataDir-relative paths
// that a bundle carries. extra is a list of opt-in includes (IncludeQuarantine /
// IncludeHazards); unknown values are an error (fail-closed on typos — silently
// dropping a typo'd "quarentine" would later surprise the user with a missing
// store).
//
// Portable classes:
//   - tasks/*.json        (NOT *.lock — per-task lock residue)
//   - checklog*.jsonl, toollog*.jsonl (active + rotated)
//   - sessions.jsonl, sessions/*.json
//   - act/conclusions.jsonl
//   - stamps/*            (NOT stamps/hook-deploy — machine-local epoch stamp)
//   - protocol.yml
//
// ExportFiles 遍历 dataDir，返回 bundle 携带的斜杠分隔 DataDir 相对路径。extra 是
// 选入的敏感 store（IncludeQuarantine / IncludeHazards）；未知值报错（对 typo
// fail-closed——静默丢掉拼错的 "quarentine" 会在日后以缺 store 惊吓用户）。
//
// 可移植类：
//   - tasks/*.json        （不含 *.lock——per-task 锁残留）
//   - checklog*.jsonl、toollog*.jsonl（active + rotated）
//   - sessions.jsonl、sessions/*.json
//   - act/conclusions.jsonl
//   - stamps/*            （不含 stamps/hook-deploy——机器本地 epoch 戳）
//   - protocol.yml
func ExportFiles(dataDir string, extra []string) ([]string, error) {
	includes := map[string]bool{}
	for _, e := range extra {
		switch e {
		case IncludeQuarantine, IncludeHazards:
			includes[e] = true
		default:
			return nil, fmt.Errorf(`未知的 --include 值 %q（可用：%s / %s）`, e, IncludeQuarantine, IncludeHazards)
		}
	}

	var out []string
	err := filepath.WalkDir(dataDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Never descend into the whole-dir backups — they contain superseded
			// copies of everything else and would balloon the bundle.
			//
			// 绝不进入整目录备份——它们装着其他一切文件的被替换副本，进 bundle
			// 只会膨胀。
			if d.Name() != filepath.Base(dataDir) && strings.HasPrefix(d.Name(), `.rekey-backup-`) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(dataDir, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)

		if included, store := allowlistFile(rel); included || (store != `` && includes[store]) {
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// allowlistFile classifies one DataDir-relative path: (included, optInStore).
// optInStore is non-empty only for the sensitive stores, so the caller can admit
// them when explicitly included.
//
// allowlistFile 分类一个 DataDir 相对路径：(纳入, 已知 store)。knownStore 仅对
// 选入型敏感 store 非空，调用方据此在显式选入时放行。
func allowlistFile(rel string) (bool, string) {
	if !strings.ContainsRune(rel, '/') {
		// DataDir 根级：精确名单 + 两个前缀族（active 与 rotated 日志）。
		//
		// DataDir root level: exact names + two prefix families (active and
		// rotated logs).
		switch {
		case rel == `protocol.yml`, rel == `sessions.jsonl`:
			return true, ``
		case strings.HasPrefix(rel, `checklog`) || strings.HasPrefix(rel, `toollog`):
			return strings.HasSuffix(rel, `.jsonl`), ``
		default:
			return false, ``
		}
	}
	top := rel[:strings.IndexByte(rel, '/')]
	switch top {
	case `tasks`:
		// *.json 即任务状态；*.lock 不是（per-task 锁残留）。
		//
		// *.json is a task state; *.lock is not (per-task lock residue).
		return strings.HasSuffix(rel, `.json`), ``
	case `sessions`:
		return strings.HasSuffix(rel, `.json`), ``
	case `act`:
		return rel == `act/conclusions.jsonl`, ``
	case `stamps`:
		// stamps/hook-deploy 是 hooks/settings.go 写的 epoch+tag 机器本地部署戳。
		//
		// stamps/hook-deploy is the machine-local epoch+tag deploy stamp written
		// by hooks/settings.go.
		return rel != `stamps/hook-deploy`, ``
	case `quarantine`:
		return false, IncludeQuarantine
	case `hazards`:
		return false, IncludeHazards
	default:
		return false, ``
	}
}
