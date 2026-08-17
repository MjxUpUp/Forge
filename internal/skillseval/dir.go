package skillseval

// dir.go — eval data root resolution + one-time legacy migration.
//
// The eval loop historically lived at ~/.pi/research/skill-eval (a path convention inherited from
// an earlier tool, predating the forgedata namespace). That hardcoded home join bypassed
// GlobalHome/FORGE_DATA_HOME entirely — no test isolation, no CI override, and a fresh CI runner
// could never see repo-level eval data. Resolution now follows the same priority chain as every
// other forge store:
//
//	--dir flag (CLI, highest) > FORGE_EVAL_DIR env > <GlobalHome>/evals (~/.forge/evals)
//
// The first default-resolution migrates a legacy ~/.pi/research/skill-eval tree (and the sibling
// eval-*.md checklists eval-gen --save used to drop in ~/.pi/research/) into the new root, once:
// rename when possible, copy when rename fails (legacy left in place — never deleted on the
// copy path). Explicit/env resolution NEVER migrates — pointing --dir at a repo evals/ directory
// must not move user data into the repository.
//
// dir.go — eval 数据根目录解析 + 一次性旧路径迁移。
//
// eval 闭环历史上落在 ~/.pi/research/skill-eval（沿用早期工具的路径约定，早于 forgedata
// 命名空间）。该硬编码 home join 完全绕开 GlobalHome/FORGE_DATA_HOME——无测试隔离、无
// CI 覆盖，崭新 CI runner 永远看不到仓库级 eval 数据。解析现在与其他 forge store 同链：
//
//	--dir flag（CLI，最高）> FORGE_EVAL_DIR env > <GlobalHome>/evals（~/.forge/evals）
//
// 首次默认解析会把旧的 ~/.pi/research/skill-eval 树（及 eval-gen --save 曾落在
// ~/.pi/research/ 的 eval-*.md 清单）一次性迁入新根：能 rename 就 rename，rename 失败
// （跨卷/平台限制）退化为 copy（旧路径保留——copy 路径永不删除用户数据）。显式/env 解析
// 永不迁移——把 --dir 指向仓库 evals/ 时绝不能把用户数据搬进仓库。

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// EnvDirName overrides the eval data root without a CLI flag (CI / scripts).
//
// EnvDirName 免 CLI flag 覆盖 eval 数据根（CI / 脚本用）。
const EnvDirName = "FORGE_EVAL_DIR"

// EvalDir returns the default eval data root (no --dir flag). Kept as the single entry for
// read-side consumers (dashboard pulse, taskpipeline advisory) that have no flag context.
//
// EvalDir 返回默认 eval 数据根（不带 --dir）。保留给无 flag 上下文的读侧消费方
// （dashboard pulse、taskpipeline advisory）作唯一入口。
func EvalDir() (string, error) {
	return ResolveDir("")
}

// ResolveDir resolves the eval data root: explicit > EnvDirName > <GlobalHome>/evals.
// Migration of legacy data runs only on the default branch (see file header).
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
	if err := migrateLegacy(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// legacyPaths returns the pre-namespace locations: the skill-eval tree and its parent
// (which held eval-*.md checklists). ok=false when the home dir cannot be resolved —
// migration is best-effort and must never fail resolution.
//
// legacyPaths 返回命名空间化之前的位置：skill-eval 树与其父目录（曾放 eval-*.md 清单）。
// home 解析失败时 ok=false——迁移尽力而为，绝不让解析失败。
func legacyPaths() (tree, checklistDir string, ok bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", false
	}
	root := filepath.Join(home, ".pi", "research")
	return filepath.Join(root, "skill-eval"), root, true
}

// migrateLegacy moves the legacy tree + checklists into target, once. Both steps are
// individually idempotent: existing targets are never overwritten, absent sources are no-ops.
//
// migrateLegacy 把旧树 + 清单一次性迁入 target。两步各自幂等：已存在目标绝不覆盖，
// 不存在来源视为 no-op。
func migrateLegacy(target string) error {
	tree, checklistRoot, ok := legacyPaths()
	if !ok {
		return nil
	}
	if err := migrateLegacyTree(tree, target); err != nil {
		return err
	}
	return migrateLegacyChecklists(checklistRoot, filepath.Join(target, "checklists"))
}

// migrateLegacyTree: no legacy → no-op; target already exists → no-op (a later default
// resolution must not clobber whatever is there — rename onto a non-empty dir fails anyway,
// but stat-first keeps the intent explicit); otherwise rename, falling back to a copy that
// deliberately leaves the legacy tree in place.
//
// migrateLegacyTree：无旧树 → no-op；target 已存在 → no-op（后续默认解析不得覆盖——
// rename 到非空目录本就会失败，但先 stat 让意图显式）；否则 rename，失败退化为 copy，
// copy 路径刻意保留旧树不删。
func migrateLegacyTree(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	return copyTree(src, dst)
}

// migrateLegacyChecklists copies eval-*.md from the legacy research root into dstDir.
// Existing files are skipped (never overwrite); unreadable files are skipped silently —
// checklist loss is cosmetic, not worth failing resolution over.
//
// migrateLegacyChecklists 把旧 research 根下的 eval-*.md 复制进 dstDir。已存在文件跳过
// （绝不覆盖）；读不动的文件静默跳过——清单丢失只是面子问题，不值得让解析失败。
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

// copyTree recursively copies the src tree to dst (files only carry 0644, dirs 0755 —
// eval data is JSON/JSONL/MD, no executables). Used as the cross-device fallback of
// migrateLegacyTree.
//
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
