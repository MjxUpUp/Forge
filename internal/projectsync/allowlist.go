package projectsync

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// allowlist.go —— 项目 bundle 携带内容的单一真相源。按设计默认拒绝：只有显式
// 枚举的可移植类进入；一切机器本地文件（锚/sentinel/戳/备份）与敏感 store
// （quarantine 源码倾倒、hazard 命令行）保持在外，除非显式 include。未来文件类
// 按构造排除——forge 新增的 sentinel 或锚绝不泄进 bundle，直到被刻意加进清单。

// IncludeQuarantine / IncludeHazards 是显式选入的敏感 store。
const (
	IncludeQuarantine = `quarantine`
	IncludeHazards    = `hazards`
)

// ExportFiles walks dataDir and returns the DataDir-relative paths a bundle carries.
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

// StripNonAllowlisted removes every file under dataDir that the allowlist does not admit, returning the removed rel paths.
//
// StripNonAllowlisted 删除 dataDir 下 allowlist 不放行的每个文件，返回被删的 rel
// 路径。这是 allowlist 默认拒绝在导入侧的执行：Unpack 只校验 manifest↔tar 一致，
// 而 manifest 本身不可信（无签名）——伪造 bundle 可在清单里列
// imports.jsonl / active-task-ref-* / hooks/* / quarantine/** / hazards/**，
// datamerge 会忠实地把它们搬进活 DataDir（污染账本、劫持会话锚、绕过敏感 store
// 的 --include 门槛）。选入型 store 在此无条件剥除：v1 的 import 没有 --include
// flag，即使 bundle 诚实声明了 Includes，也是丢掉这些载荷而非换来一个未经请求
// 的敏感 store。
func StripNonAllowlisted(dataDir string) ([]string, error) {
	var removed []string
	err := filepath.WalkDir(dataDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dataDir, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		// 空目录残留无害（Dirs 的 move 分支会建目标目录），只剥文件。
		if included, _ := allowlistFile(rel); !included {
			if rmErr := os.Remove(p); rmErr != nil {
				return rmErr
			}
			removed = append(removed, rel)
		}
		return nil
	})
	return removed, err
}

// allowlistFile 分类一个 DataDir 相对路径：(纳入, 已知 store)。knownStore 仅对
// 选入型敏感 store 非空，调用方据此在显式选入时放行。
func allowlistFile(rel string) (bool, string) {
	if !strings.ContainsRune(rel, '/') {
		// DataDir 根级：精确名单 + 两个前缀族（active 与 rotated 日志）。
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
		return strings.HasSuffix(rel, `.json`), ``
	case `sessions`:
		return strings.HasSuffix(rel, `.json`), ``
	case `act`:
		return rel == `act/conclusions.jsonl`, ``
	case `stamps`:
		// stamps/hook-deploy 是 hooks/settings.go 写的 epoch+tag 机器本地部署戳。
		return rel != `stamps/hook-deploy`, ``
	case `quarantine`:
		return false, IncludeQuarantine
	case `hazards`:
		return false, IncludeHazards
	default:
		return false, ``
	}
}
