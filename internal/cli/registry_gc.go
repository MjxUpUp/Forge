package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/spf13/cobra"
)

// registry_gc.go —— `forge registry gc`：回收孤儿项目数据目录。
//
// 与 registry prune 的分工：prune 清注册表条目（projects.json 里的死路径/重复）；
// gc 收数据目录（<home>/projects/<key>/ 里不再对应任何登记项目的目录）。后者是
// T3 checklog janitor 收口了 checklog 无界增长之后的同类问题：projects/ 目录数
// 无界增长——测试隔离疏漏（2026-08 实测泄漏过 50 个夹具目录）与项目注销/移动
// 残留（init-suggest 曾留下 hooks 副本）都往里堆。
//
// 安全语义（对齐 worktree janitor 的「脏的永不删」）：
//   - 已登记项目的 key 永不触碰（registry.Keys 为准）
//   - 默认 dry-run：只报告，不动任何文件；--prune 才执行
//   - 空目录树（零文件，只剩目录骨架）直接删除——无数据可失
//   - 过期非空（最新文件 mtime > 14d 前）移入 ~/.forge/backups/gc-<时间戳>/——
//     移动不是删除，确认无需恢复前数据一直可找回
//   - 新鲜非空（14d 内有文件活动）只报告保留——未登记但活跃的项目（DataDir 按
//     cwd 纯推导，合法存在于注册表之外）靠这道门保住
//   - 扫描失败的目录只报告绝不处置
func init() {
	registryCmd.AddCommand(registryGcCmd)
	registryGcCmd.Flags().Bool("prune", false, "执行清理（默认 dry-run 只报告不改动）")
}

var registryGcCmd = &cobra.Command{
	Use:   `gc`,
	Short: `回收孤儿项目数据目录（默认演练，--prune 才动手）`,
	Long: `Reclaim orphan project data directories under the forge data home.

扫描 <data-home>/projects/ 下不在全局注册表内的数据目录并分类处置：
空目录树删除；最新文件超过 14 天的非空目录移入 ~/.forge/backups/gc-<时间戳>/
（移动而非删除，可恢复）；14 天内有活动的与已登记项目的目录永不触碰。

默认 dry-run 只报告；加 --prune 执行。与 forge registry prune 互补：
prune 清注册表死条目，gc 收数据目录残留。`,
	RunE: runRegistryGc,
}

// gcStaleAfter 与 worktree janitor 同一期（14d）：新鲜度门保住「未登记但活跃」
// 的项目数据（DataDir 按 cwd 纯推导，注册表外合法存在）。
const gcStaleAfter = 14 * 24 * time.Hour

// gcCandidate 是一个孤儿数据目录的扫描结果。kind 取 empty / stale / fresh /
// unreadable（扫描失败——只报告不处置）。
type gcCandidate struct {
	key    string
	path   string
	kind   string
	files  int
	bytes  int64
	newest time.Time
}

// scanGcCandidates 扫描 projectsDir 下不在 registered 集合内的数据目录并分类。
// 文件计数先于 stat：单文件 Info 失败仍计入 files（有东西≠空目录，宁可少删）。
func scanGcCandidates(projectsDir string, registered map[string]bool, now time.Time) ([]gcCandidate, error) {
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []gcCandidate
	for _, e := range entries {
		if !e.IsDir() || registered[e.Name()] {
			continue
		}
		c := gcCandidate{key: e.Name(), path: filepath.Join(projectsDir, e.Name())}
		walkErr := filepath.WalkDir(c.path, func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			c.files++
			if info, serr := d.Info(); serr == nil {
				c.bytes += info.Size()
				if info.ModTime().After(c.newest) {
					c.newest = info.ModTime()
				}
			}
			return nil
		})
		switch {
		case walkErr != nil:
			c.kind = "unreadable"
		case c.files == 0:
			c.kind = "empty"
		case now.Sub(c.newest) > gcStaleAfter:
			c.kind = "stale"
		default:
			c.kind = "fresh"
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out, nil
}

// moveDirToBackup 把 src 移入 backupRoot/<key>（同名冲突加 -2/-3 后缀）。同
// data-home 内移动 = 同卷 rename，原子。
func moveDirToBackup(src, backupRoot, key string) (string, error) {
	if err := os.MkdirAll(backupRoot, 0755); err != nil {
		return "", err
	}
	dest := filepath.Join(backupRoot, key)
	for i := 2; ; i++ {
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			break
		}
		dest = filepath.Join(backupRoot, fmt.Sprintf("%s-%d", key, i))
	}
	if err := os.Rename(src, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func runRegistryGc(cmd *cobra.Command, _ []string) error {
	prune, _ := cmd.Flags().GetBool("prune")
	home, err := forgedata.GlobalHome()
	if err != nil {
		return err
	}
	projectsDir := filepath.Join(home, "projects")
	registered := make(map[string]bool)
	for _, k := range registry.Keys() {
		registered[k] = true
	}
	cands, err := scanGcCandidates(projectsDir, registered, time.Now())
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	if len(cands) == 0 {
		fmt.Fprintln(w, `无孤儿数据目录。`)
		return nil
	}

	var backupRoot string
	if prune {
		backupRoot = filepath.Join(home, "backups", "gc-"+time.Now().Format("20060102-150405"))
	}
	removed, backed, kept := 0, 0, 0
	var backupMade []string
	fmt.Fprintf(w, `孤儿数据目录（不在注册表内的 projects/<key>）：%d 个%s`, len(cands), "\n")
	for _, c := range cands {
		switch c.kind {
		case "empty":
			if !prune {
				fmt.Fprintf(w, "  · %s（空目录树，将删除）\n", c.key)
				continue
			}
			if err := os.RemoveAll(c.path); err != nil {
				kept++
				fmt.Fprintf(w, "  ✗ %s 删除失败（保留）: %v\n", c.key, err)
				continue
			}
			removed++
			fmt.Fprintf(w, "  ✎ 已删除空目录 %s\n", c.key)
		case "stale":
			if !prune {
				fmt.Fprintf(w, "  · %s（过期，%d 文件 %s，最新活动 %s，将移入备份）\n",
					c.key, c.files, humanBytes(c.bytes), c.newest.Format("2006-01-02"))
				continue
			}
			dest, err := moveDirToBackup(c.path, backupRoot, c.key)
			if err != nil {
				kept++
				fmt.Fprintf(w, "  ✗ %s 移入备份失败（保留）: %v\n", c.key, err)
				continue
			}
			backed++
			backupMade = append(backupMade, dest)
			fmt.Fprintf(w, "  ⇢ 已备份 %s（%d 文件 %s）\n", c.key, c.files, humanBytes(c.bytes))
		case "fresh":
			kept++
			fmt.Fprintf(w, "  · %s（%d 文件，最新活动 %s——%d 天内有活动，保留）\n",
				c.key, c.files, c.newest.Format("2006-01-02"), int(gcStaleAfter.Hours()/24))
		default: // unreadable
			kept++
			fmt.Fprintf(w, "  ⚠ %s（扫描失败，只报告不处置）\n", c.key)
		}
	}
	fmt.Fprintln(w)
	if prune {
		fmt.Fprintf(w, "✅ 回收完成：删除空目录 %d，移入备份 %d，保留 %d。\n", removed, backed, kept)
		if len(backupMade) > 0 {
			fmt.Fprintf(w, "备份位置：%s（确认无需恢复后可手动删除）\n", backupRoot)
		}
		return nil
	}
	fmt.Fprintf(w, "演练模式（未改动任何文件）——加 --prune 执行：删除空目录 %d，移入备份 %d，保留 %d。\n", removed, backed, kept)
	return nil
}

// humanBytes 简版字节数渲染（KiB/MiB/GiB），仅供 gc 报告行内展示。
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
