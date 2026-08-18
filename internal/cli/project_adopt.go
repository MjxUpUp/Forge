package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/datamerge"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

// project_adopt.go — `forge project adopt`: adopt a repo-born project ID
// (.forge-project-id) and migrate this machine's data from the path-derived key to
// the ID-derived key.
//
// ORDERING (correctness-critical, plan pressure-test #1): migrate data FIRST, write
// the ID file SECOND, sync the registry LAST. Writing the ID file first would flip
// every concurrent hook's DataDirFor to the new (empty) key dir for the whole
// migration window. The residual window (ID written ↔ registry synced) is covered by
// registry's path-fallback matching.
//
// project_adopt.go —— `forge project adopt`：采纳 repo-born 项目 ID
// （.forge-project-id），并把本机数据从路径 key 迁到 ID key。
//
// 顺序（正确性关键，方案压力测试 #1）：先迁数据、再写 ID 文件、最后同步注册表。
// 先写 ID 会让迁移窗口内所有并发 hook 的 DataDirFor 翻到新（空）key 目录。
// 残余窗口（ID 已写 ↔ 注册表未同步）由注册表路径回退匹配兜底。

func init() {
	projectAdoptCmd.Flags().Bool(`dry-run`, false, `列出将执行的动作，不落盘`)
	projectAdoptCmd.Flags().Bool(`regenerate`, false, `已有 ID 时强制换新 ID（复制粘贴共享 ID 的逃生口）`)
}

var projectAdoptCmd = &cobra.Command{
	Use:   `adopt [--dry-run] [--regenerate]`,
	Short: `采纳 repo-born 项目 ID，跨机器身份对齐（数据自动迁移）`,
	Long: `forge project adopt —— 生成/采纳 .forge-project-id 并迁移本机数据到 ID key。

身份文件在主 worktree 根（.forge-project-id，内容 fpid_<32hex>），建议 commit 进
git：另一台机器 pull 后跑一次 adopt 即对齐——两台机器对同一 clone 推导同一 key，
此后 export/import 不再需要 key 重映射。

动作顺序：先迁移数据（旧 path key → 新 ID key，复用 rekey 合并语义），再写 ID
文件（身份翻转点），最后同步注册表。活会话存在时先警告。

--regenerate：已共享/污染的 ID 换新（其他机器需重跑 adopt 或 import 处理 key
不匹配）。--dry-run 只列动作。`,
	RunE: runProjectAdopt,
}

func runProjectAdopt(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool(`dry-run`)
	regenerate, _ := cmd.Flags().GetBool(`regenerate`)
	out := cmd.OutOrStdout()
	cwd, _ := os.Getwd()

	// 主 worktree 根 = 解析后 common .git 目录的父目录（ID 文件落点）。
	gitDir, err := forgedata.ResolvedGitDir(cwd)
	if err != nil {
		return fmt.Errorf(`本命令需要 git 项目（v1 不支持非 git 项目 adopt）: %w`, err)
	}
	mainRoot := filepath.Dir(gitDir)

	existingID, idErr := forgedata.ReadProjectID(mainRoot)
	hasValidID := idErr == nil

	// 幂等退出：已有合法 ID 且不强制换新。
	if hasValidID && !regenerate {
		key, _ := forgedata.Key(cwd)
		fmt.Fprintf(out, `已启用项目 ID：%s\nkey=%s（key_mode=id）\n数据目录：%s\n`, existingID, key, forgedata.RootDir(key))
		fmt.Fprintln(out, `无需动作（换新 ID 用 --regenerate）`)
		return nil
	}

	newID, err := generateProjectID()
	if err != nil {
		return err
	}
	newKey := forgedata.IDKey(newID)

	// 迁移源 key：无 ID（首采）= 路径体系 key；--regenerate = 旧 ID 的 key。
	var oldKey string
	if hasValidID {
		oldKey = forgedata.IDKey(existingID)
	} else {
		oldKey, err = forgedata.KeyFromPath(cwd)
		if err != nil {
			return err
		}
	}

	fmt.Fprintf(out, `项目：%s\n新 ID：%s\n%s → %s\n`, mainRoot, newID, oldKey, newKey)

	migrated, aerr := applyAdoption(mainRoot, newID, oldKey, newKey, dryRun, out)
	if aerr != nil {
		return aerr
	}

	if dryRun {
		fmt.Fprintln(out, `（dry-run：以上动作未落盘，ID 文件未写入）`)
		return nil
	}

	// 身份翻转与注册表同步在 applyAdoption 内完成（与 import --adopt-id 共享同一
	// 落地序列：迁数据 → 写 ID → 同步注册表）。

	commitHint := `git add .forge-project-id && git commit`
	if ignored, _ := gitIgnored(mainRoot, forgedata.ProjectIDFileName); ignored {
		commitHint = `git add -f .forge-project-id && git commit（该文件当前被 .gitignore 命中，需 -f）`
	}
	fmt.Fprintf(out, `✅ adopt 完成%s；另一台机器 git pull 后运行 forge project adopt 即对齐\n下一步：%s\n`, migratedNote(migrated), commitHint)
	if regenerate && hasValidID {
		fmt.Fprintln(out, `⚠ 已强制换新 ID：其他机器下一次 export/import 会遇到 key 不匹配——在其上重跑 forge project adopt --regenerate（拿新 ID 需先 pull）或按 import 提示处理`)
	}
	return nil
}

func migratedNote(m bool) string {
	if m {
		return `（数据已迁移到 ID key）`
	}
	return ``
}

// applyAdoption is the shared landing sequence of `project adopt` and `project
// import --adopt-id`: live-session precheck → data migration (oldKey→newKey, legacy
// to-wins) → write the ID file (the identity flip point) → registry sync. Returns
// whether data was migrated. The order is correctness-critical — see the file header.
//
// applyAdoption 是 `project adopt` 与 `project import --adopt-id` 共享的落地序列：
// 活会话预检 → 数据迁移（oldKey→newKey，legacy to-wins）→ 写 ID 文件（身份翻转
// 点）→ 注册表同步。返回是否迁移了数据。顺序是正确性关键——见文件头注。
func applyAdoption(mainRoot, newID, oldKey, newKey string, dryRun bool, out io.Writer) (bool, error) {
	// 活会话预检：新鲜锚文件说明有 agent 正在写 DataDir——合并是并集语义丢更新
	// 窗口极小，但用户应知情。
	if warn := liveSessionWarning(forgedata.RootDir(oldKey)); warn != `` {
		fmt.Fprintf(out, `⚠ %s\n`, warn)
	}

	oldDir := forgedata.RootDir(oldKey)
	newDir := forgedata.RootDir(newKey)
	migrated := false
	if dirHasContent(oldDir) && oldKey != newKey {
		fmt.Fprintf(out, `迁移数据：%s → %s\n`, oldDir, newDir)
		// adopt 阶段没有「对端快照」，用 legacy to-wins（新目录通常为空 ⇒ 实际是
		// 全量搬入）；jsonl 仍走时间戳有序合并。
		actions, aerr := datamerge.Dirs(oldDir, newDir, datamerge.Options{DryRun: dryRun})
		if aerr != nil {
			return false, fmt.Errorf(`数据迁移失败: %w`, aerr)
		}
		for _, a := range actions {
			fmt.Fprintln(out, a)
		}
		migrated = true
	} else {
		fmt.Fprintln(out, `旧 key 数据目录为空或不存在，无需迁移`)
	}
	if dryRun {
		return migrated, nil
	}

	// 身份翻转点：写 ID 文件（主 worktree 根）。
	idPath := filepath.Join(mainRoot, forgedata.ProjectIDFileName)
	if err := util.AtomicWrite(idPath, []byte(newID+"\n"), 0644); err != nil {
		return migrated, fmt.Errorf(`写入 %s 失败: %w`, idPath, err)
	}

	// 注册表同步（失败仅警告：路径回退匹配兜底，forge doctor 可检出残留）。
	if oldKey != newKey {
		if removed, rerr := registry.Rekey(oldKey, newKey); rerr != nil {
			fmt.Fprintf(out, `warn: 注册表同步失败（数据已迁移）：%v\n`, rerr)
		} else if removed > 0 {
			fmt.Fprintf(out, `注册表：同步 %d 条条目 → 新 key\n`, removed)
		}
	}
	return migrated, nil
}

// generateProjectID produces a new fpid_<32hex>.
//
// generateProjectID 生成新的 fpid_<32hex>。
func generateProjectID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ``, fmt.Errorf(`生成项目 ID 失败: %w`, err)
	}
	return `fpid_` + hex.EncodeToString(b[:]), nil
}

// dirHasContent reports whether dir contains at least one regular file AT ANY
// DEPTH. Depth matters: a DataDir's payload usually lives in subdirectories
// (tasks/, act/, sessions/) with only jsonl logs at the top level — a fresh clone's
// path-key dir can hold JUST tasks/ and nothing else, which a top-level-only probe
// would call empty, silently skipping the adoption migration (caught by
// TestProjectImport_IDBundleRefusesThenAdopts). Walk stops at the first hit.
//
// dirHasContent 报告 dir 在任意深度上是否含至少一个普通文件。深度很关键：
// DataDir 的载荷通常在子目录（tasks/、act/、sessions/），顶层只有 jsonl——
// 全新 clone 的路径 key 目录可能只有 tasks/ 别无他物，只看顶层会误判为空、
// 静默跳过 adopt 迁移（由 TestProjectImport_IDBundleRefusesThenAdopts 抓到）。
// Walk 命中第一个即停。
func dirHasContent(dir string) bool {
	if dir == `` {
		return false
	}
	found := false
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 单个条目读失败不影响其余判断（best-effort 探测）
		}
		// 备份壳不算活数据：只剩 .rekey-backup-* 的旧目录不做无意义迁移
		// （与 registry.Audit 的 dataDirHasPayload 同语义）。
		//
		// Backup shells are not live data: an old dir holding only
		// .rekey-backup-* must not trigger a pointless migration (same
		// semantics as registry.Audit's dataDirHasPayload).
		if d.IsDir() && d.Name() != filepath.Base(dir) && strings.HasPrefix(d.Name(), `.rekey-backup-`) {
			return filepath.SkipDir
		}
		if !found && d.Type().IsRegular() {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// liveSessionWarning returns a warning when fresh (<10min) session anchors exist in
// dataDir — an agent is likely mid-session and writing.
//
// liveSessionWarning 在 dataDir 存在新鲜（<10min）会话锚时返回警告——很可能有
// agent 正在会话中写入。
func liveSessionWarning(dataDir string) string {
	if dataDir == `` {
		return ``
	}
	matches, _ := filepath.Glob(filepath.Join(dataDir, `active-task-ref*`))
	matches = append(matches, filepath.Join(dataDir, `session.json`))
	cutoff := time.Now().Add(-10 * time.Minute)
	for _, m := range matches {
		info, err := os.Stat(m)
		if err == nil && info.ModTime().After(cutoff) {
			return fmt.Sprintf(`检测到新鲜会话锚（%s，<10min）——可能有 agent 正在写入；建议静默期再 adopt（合并不丢数据，但窗口内的新增记录需再同步）`, filepath.Base(m))
		}
	}
	return ``
}

// gitIgnored reports whether path is ignored by git in repo (check-ignore).
//
// gitIgnored 报告 path 在 repo 内是否被 git ignore（check-ignore）。
func gitIgnored(repo, path string) (bool, error) {
	cmd := exec.Command(`git`, `-C`, repo, `check-ignore`, `-q`, path)
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return false, nil // 未被忽略
		}
		return false, err
	}
	return true, nil
}
