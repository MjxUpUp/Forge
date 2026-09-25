package skillseval

// dir.go — eval 数据根目录解析 + 一次性旧路径迁移。
//
// eval 闭环历史上落在 ~/.pi/research/skill-eval（沿用早期工具的路径约定，早于 forgedata
// 命名空间）。该硬编码 home join 完全绕开 GlobalHome/FORGE_DATA_HOME——无测试隔离、无
// CI 覆盖，崭新 CI runner 永远看不到仓库级 eval 数据。解析现在与其他 forge store 同链：
//
//	--dir flag（CLI，最高）> FORGE_EVAL_DIR env > <GlobalHome>/evals（~/.forge/evals）
//
// 迁移设计（经评审加固）：
//   - 哨兵标记 <root>/.migrated-from-pi 锚定「已完成」：写入后迁移永不重跑——同时消掉
//     每请求 Glob IO（pulse 每个请求都调 EvalDir）与清单复活（删掉的清单不得从旧副本
//     还魂）。删标记 = 显式重新武装迁移。
//   - 尽力而为、绝不阻塞解析：迁移失败（权限/磁盘）时不写标记（下次默认解析重试），
//     但仍返回目标目录——纯读命令（eval-report、pulse）不该死于面子工程的迁移。
//   - copy 回退走 staging 目录、成功后 rename 就位——半途 copy 的目标树绝不可能冒充
//     「已迁移」（stat(dst) 只会看到完整树）。
//   - 无锁防竞态：rename/copy 失败后复查 dst——另一进程已完成迁移视为成功而非报错。
//
// 显式/env 解析永不迁移——把 --dir 指向仓库 evals/ 时绝不能把用户数据搬进仓库。

import (
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// EnvDirName 免 CLI flag 覆盖 eval 数据根（CI / 脚本用）。
const EnvDirName = "FORGE_EVAL_DIR"

// markerName 是 eval 根内锚定「迁移已跑过」的哨兵文件。存在即跳过整个迁移步骤；
// 删除它 = 显式重新武装迁移。
const markerName = ".migrated-from-pi"

// EvalDir returns the default eval data root (no --dir flag).
//
// EvalDir 返回默认 eval 数据根（不带 --dir）。保留给无 flag 上下文的读侧消费方
// （dashboard pulse、taskpipeline advisory）作唯一入口。
func EvalDir() (string, error) {
	return ResolveDir("")
}

// ResolveDir resolves the eval data root: explicit > EnvDirName >
// <GlobalHome>/evals.
//
// ResolveDir 解析 eval 数据根：explicit > EnvDirName > <GlobalHome>/evals。
// 旧数据迁移只在默认分支发生（见文件头注释）。
func ResolveDir(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Clean(explicit), nil
	}
	if e := os.Getenv(EnvDirName); e != "" {
		return filepath.Clean(e), nil
	}
	home, err := forgedata.GlobalHome()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "evals")
	migrateLegacy(dir)
	return dir, nil
}

// legacyPaths 返回命名空间化之前的位置：skill-eval 树与其父目录（曾放 eval-*.md 清单）。
// home 解析失败时 ok=false。
func legacyPaths() (tree, checklistDir string, ok bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", false
	}
	root := filepath.Join(home, ".pi", "research")
	return filepath.Join(root, "skill-eval"), root, true
}

// migrateLegacy 把旧数据一次性迁入 target。完全尽力而为：绝不返回错误——解析不能死于
// 面子工程的迁移（失败的 pass 只是没写标记、下次重试；每一步各自幂等）。
func migrateLegacy(target string) {
	marker := filepath.Join(target, markerName)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	tree, checklistRoot, ok := legacyPaths()
	if !ok {
		return
	}
	// 树步骤先行——它必须看到不存在的 target 才会迁入（此处预建 target 会让树步骤
	// 的 stat(dst) 误读「已迁移」而跳过整个搬迁；首版评审抓出）。
	treeErr := migrateLegacyTree(tree, target)
	if err := os.MkdirAll(target, 0755); err != nil {
		return // 目标建不出来（只读 home 等）——读命令对不存在的目录自会优雅降级
	}
	clErr := migrateLegacyChecklists(checklistRoot, filepath.Join(target, "checklists"))
	if treeErr == nil && clErr == nil {
		// 只有两步都成才写标记——半途状态保持重试武装。
		_ = os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)), 0644)
	}
}

// migrateLegacyTree：无旧目录 → no-op（旧位置是普通文件也不迁——rename 它会把 eval 根
// 变成文件）；target 已存在 → no-op（绝不覆盖）；否则 rename，失败退化为 staging copy
// （成功才 rename 就位，失败整体弃置——半棵树绝不占住 target 路径）。
func migrateLegacyTree(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil || !fi.IsDir() {
		return nil
	}
	if fi, err := os.Stat(dst); err == nil {
		if fi.IsDir() {
			return nil
		}
		// dst 是普通文件（用户手建）——保留；后续 IO 错误会指向真凶而非被静默覆盖。
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// 竞态复查：两次 stat 之间另一进程可能已完成迁移——视为成功而非报错。
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	staging := dst + ".migrating"
	_ = os.RemoveAll(staging) // debris from a previously interrupted pass
	if err := copyTree(src, staging); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	if err := os.Rename(staging, dst); err != nil {
		_ = os.RemoveAll(staging)
		if _, err2 := os.Stat(dst); err2 == nil {
			return nil // lost the race to a concurrent migrator — fine
		}
		return err
	}
	return nil
}

// migrateLegacyChecklists 把旧 research 根下的 eval-*.md 复制进 dstDir。已存在文件跳过
// （绝不覆盖）；读不动的文件静默跳过——清单丢失只是面子问题，不值得让整趟失败。
func migrateLegacyChecklists(srcDir, dstDir string) error {
	matches, err := filepath.Glob(filepath.Join(srcDir, "eval-*.md"))
	if err != nil || len(matches) == 0 {
		return nil
	}
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	for _, m := range matches {
		dst := filepath.Join(dstDir, filepath.Base(m))
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return err
		}
	}
	return nil
}

// copyTree 递归复制 src 树到 dst（文件统一 0644、目录 0755——eval 数据是 JSON/JSONL/MD，
// 无可执行文件）。作为 migrateLegacyTree 的跨卷回退。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, 0644)
	})
}
