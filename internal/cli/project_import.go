package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/datamerge"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/projectsync"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// project_import.go — `forge project import`: land a bundle on this machine.
//
// Trust model (lineage-conditional, decided with the user): same derived key =
// same-identity lineage (the developer's other machine) ⇒ results preserved
// (scores/completion/gate history) by default, sessions ghosted; key mismatch ⇒
// full StripForeignGateSignals by default (foreign gate signals never satisfy local
// gates), --trust-foreign for the explicit informed exception; --untrusted forces
// the strip even for same-key bundles.
//
// Routing: same key → direct merge under per-task locks; key mismatch → same merge
// after lineage determination, plus a registry sync. When the bundle comes from an
// ID-identity project and this machine is still path-identity, the default REFUSES
// with guidance (pull + adopt, or --adopt-id to take the bundle's ID wholesale —
// which also migrates this machine's existing data to the ID key first).
//
// Idempotency: the machine-local ledger (imports.jsonl) skips an already-imported
// bundle_id (--force redoes); the merge itself is idempotent regardless (exact-line
// dedup + union semantics) — a crash mid-import leaves no ledger line and re-runs
// converge.
//
// project_import.go —— `forge project import`：把 bundle 落地到本机。
//
// 信任模型（lineage 条件判定，已与用户定案）：派生 key 相同 = 同身份 lineage
// （同一开发者的另一台机器）⇒ 默认保留结果字段（评分/完成/门禁历史）、幽灵化
// session；key 不匹配 ⇒ 默认完整 StripForeignGateSignals（外来门禁信号绝不满足
// 本机门禁），--trust-foreign 是显式知情的例外；--untrusted 对同 key bundle 也
// 强制剥离。
//
// 路由：同 key → 逐任务锁下直接合并；key 不匹配 → lineage 判定后同样合并，另加
// 注册表同步。bundle 来自 ID 身份项目而本机仍是路径身份时默认拒绝并给指引
// （pull + adopt，或 --adopt-id 直接采纳 bundle 的 ID——采纳前先把本机既有数据
// 迁到 ID key）。
//
// 幂等：机器本地账本（imports.jsonl）跳过已导入的 bundle_id（--force 重做）；
// 合并本身无论账本都幂等（精确行去重 + 并集语义）——导入中途崩溃不留账本行，
// 重跑收敛。

func init() {
	projectImportCmd.Flags().Bool(`dry-run`, false, `校验并列出将执行的动作，不落盘`)
	projectImportCmd.Flags().Bool(`untrusted`, false, `按不可信处理：即使同 key 也完整剥离外来门禁/评分/完成信号`)
	projectImportCmd.Flags().Bool(`trust-foreign`, false, `key 不匹配时仍按受信合并（外来 bundle 的显式放行）`)
	projectImportCmd.Flags().Bool(`force`, false, `已导入过的 bundle 重新导入（默认跳过）`)
	projectImportCmd.Flags().Bool(`adopt-id`, false, `本机无 ID 而 bundle 来自 ID 身份项目时，直接采纳其项目 ID（含本机数据迁移）`)
}

var projectImportCmd = &cobra.Command{
	Use:   `import <bundle.tar.gz> [--dry-run] [--untrusted] [--trust-foreign] [--force] [--adopt-id]`,
	Short: `校验并合并项目 bundle 到本机（lineage 信任 + 幂等账本）`,
	Long: `forge project import —— 把 forge project export 产出的 bundle 落地到本机。

流程：校验（逐文件 sha256 + 版本守卫 + 路径安全）→ 账本查重 → 身份路由 →
信任变换（session 幽灵化恒做；key 不匹配默认剥离外来门禁信号）→ 合并
（任务逐个加锁按并集/单调语义合并；jsonl 按时间戳有序合并 + 精确行去重）→
记账本。

bundle 来自 ID 身份项目而本机仍是路径身份：默认拒绝——先 git pull 拿到
.forge-project-id 后 forge project adopt，或加 --adopt-id 直接采纳。`,
	RunE: runProjectImport,
}

func runProjectImport(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf(`需要一个 bundle 文件参数（forge project export 产物）`)
	}
	dryRun, _ := cmd.Flags().GetBool(`dry-run`)
	untrusted, _ := cmd.Flags().GetBool(`untrusted`)
	trustForeign, _ := cmd.Flags().GetBool(`trust-foreign`)
	force, _ := cmd.Flags().GetBool(`force`)
	adoptID, _ := cmd.Flags().GetBool(`adopt-id`)
	out := cmd.OutOrStdout()

	if untrusted && trustForeign {
		return fmt.Errorf(`--untrusted 与 --trust-foreign 互斥（强制剥离 vs 显式放行，二选一）`)
	}

	root, err := findProjectRoot()
	if err != nil {
		return fmt.Errorf(`%w（导入前先在本机 forge init）`, err)
	}

	// 流式读：--include quarantine 的 bundle 可达数百 MB，不整体驻内存（导出侧
	// Pack 本就是流式，导入侧对齐）。
	f, err := os.Open(args[0])
	if err != nil {
		return fmt.Errorf(`读取 bundle 失败: %w`, err)
	}
	defer f.Close()

	// Pass 1 — whole-file digest ONLY. Both the ledger dedup and the trust verdict
	// key on bytes, so both run BEFORE the attacker-controlled tar.gz is ever
	// parsed: a team-profile hard-reject must not have already gone through Unpack's
	// tar path (path traversal / oversized headers / symlink races live there), and
	// a re-imported bundle must not pay the tar parse at all.
	//
	// 第一遍只算整文件摘要。账本查重与信任判定都以字节为键，都在解析攻击者可控
	// 的 tar.gz 之前执行：团队档硬拒不该已经过 Unpack 的 tar 路径（路径逃逸/超大
	// 头/symlink 竞争都在那），重复导入的 bundle 更不该付 tar 解析成本。
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf(`读取 bundle 失败: %w`, err)
	}
	bundleSHA := fmt.Sprintf(`%x`, h.Sum(nil))
	if _, err := f.Seek(0, 0); err != nil {
		return fmt.Errorf(`重读 bundle 失败: %w`, err)
	}

	localKey, err := forgedata.Key(root)
	if err != nil {
		return err
	}
	localDataDir := forgedata.RootDir(localKey)

	// 账本 digest 查重（幂等第一道，先于解包）：同字节 bundle 直接免费跳过。
	if !force {
		if imported, lerr := projectsync.HasImportedSHA(localDataDir, bundleSHA); lerr == nil && imported {
			fmt.Fprintln(out, `该 bundle 已导入过（账本 digest 命中）——跳过；强制重导入用 --force`)
			return nil
		}
	}

	// Trust verdict (node-identity.md §3): signed bundles verify against the trust
	// store; invalid signatures and team-mode unsigned bundles are hard-rejected
	// here — BEFORE unpacking (see pass-1 comment).
	//
	// 信任判定（node-identity.md §3）：签名 bundle 对照 trust store 验真；签名无效
	// 与团队档未签名在此硬拒——先于解包（见第一遍注释）。
	if verr := verifyBundleForImport(root, args[0], bundleSHA, out, dryRun); verr != nil {
		return verr
	}

	// staging 必须在 FORGE_DATA_HOME 之外（系统 temp）——绝不让半成品被 DataDir
	// 扫描器（dashboard/feed/doctor）发现。
	staging, terr := os.MkdirTemp(``, `forge-project-import-*`)
	if terr != nil {
		return terr
	}
	defer os.RemoveAll(staging)

	// Pass 2 — unpack through a TeeReader into a SECOND hash: the verdict above ran
	// on the pass-1 digest; this re-hash proves the bytes that reach Unpack are the
	// SAME bytes (a local swap between the two passes would otherwise feed unverified
	// content into the merge while the ledger records the old digest).
	//
	// 第二遍——经 TeeReader 进第二个 hash 解包：上面的判定基于第一遍摘要；此重
	// 哈希证明到达 Unpack 的字节与验签字节相同（否则两遍之间本地换文件会把未验
	// 签内容送进合并、而账本记的仍是旧摘要）。
	h2 := sha256.New()
	manifest, uerr := projectsync.Unpack(io.TeeReader(f, h2), staging)
	if uerr != nil {
		return fmt.Errorf(`bundle 校验失败: %w`, uerr)
	}
	if rebind := fmt.Sprintf(`%x`, h2.Sum(nil)); rebind != bundleSHA {
		return fmt.Errorf(`bundle 在验签后被修改（两遍读取摘要不一致）——拒绝导入`)
	}
	fmt.Fprintf(out, `bundle：%s`+"\n"+`  来源：%s@%s %s（key=%s mode=%s）`+"\n"+`  文件：%d 个，导出于 %s`+"\n",
		manifest.BundleID, manifest.Origin.User, manifest.Origin.Hostname, manifest.Origin.Root,
		manifest.Origin.Key, manifest.Origin.KeyMode, len(manifest.Files), manifest.ExportedAt.Format(`2006-01-02 15:04`))

	// 导入侧 allowlist 强制：manifest 本身不可信（无签名），Unpack 只保证
	// manifest↔tar 一致——清单里伪造的 imports.jsonl / 锚文件 / hooks / 敏感 store
	// 在此剥除，绝不进活 DataDir。
	stripped, serr := projectsync.StripNonAllowlisted(filepath.Join(staging, `data`))
	if serr != nil {
		return fmt.Errorf(`staging 剥离失败: %w`, serr)
	}
	if len(stripped) > 0 {
		fmt.Fprintf(out, `⚠ 已剥除 %d 个 allowlist 外条目（manifest 不可信，导入侧默认拒绝）：%s`+"\n",
			len(stripped), strings.Join(stripped, `, `))
	}

	// 账本 bundle_id 查重（幂等第二道，digest 未命中时兜底——bundle_id 语义上的
	// 重复而字节有差异的罕见情形）。
	if !force {
		if imported, lerr := projectsync.HasImportedBundle(localDataDir, manifest.BundleID); lerr == nil && imported {
			fmt.Fprintln(out, `该 bundle 已导入过（账本命中）——跳过；强制重导入用 --force`)
			return nil
		}
	}

	// ID 引导：bundle 是 ID 身份而本机是路径身份 → 默认拒绝给指引；--adopt-id
	// 直接采纳（先迁本机数据再翻身份，复用 adopt 的落地序列）。
	if manifest.Origin.Key != localKey && manifest.Origin.KeyMode == `id` {
		gitDir, gerr := forgedata.ResolvedGitDir(root)
		if gerr != nil {
			return fmt.Errorf(`解析 git 根失败: %w`, gerr)
		}
		localHasID := false
		if _, ierr := forgedata.ReadProjectID(filepath.Dir(gitDir)); ierr == nil {
			localHasID = true
		}
		if !localHasID {
			if !adoptID && !trustForeign {
				return fmt.Errorf(`bundle 来自 ID 身份项目（key=%s），本机仍是路径身份（key=%s）`+"\n"+`先对齐身份再导入：`+"\n"+`  1) git pull 拿到 .forge-project-id 后运行 forge project adopt`+"\n"+`  2) 或加 --adopt-id 直接采纳 bundle 的项目 ID（本机既有数据自动迁移）`+"\n"+`  3) 或加 --trust-foreign 按跨身份合并（默认剥离外来门禁信号）`, manifest.Origin.Key, localKey)
			}
			if adoptID {
				if manifest.Origin.ProjectID == `` {
					return fmt.Errorf(`bundle manifest 缺 project_id，无法 --adopt-id`)
				}
				oldKey, oerr := forgedata.KeyFromPath(root)
				if oerr != nil {
					return oerr
				}
				newKey := forgedata.IDKey(manifest.Origin.ProjectID)
				fmt.Fprintln(out, `采纳 bundle 的项目 ID（--adopt-id）`)
				if _, aerr := applyAdoption(filepath.Dir(gitDir), manifest.Origin.ProjectID, oldKey, newKey, dryRun, out); aerr != nil {
					return aerr
				}
				if dryRun {
					fmt.Fprintln(out, `（dry-run：身份未翻转，以下按跨 key 计划展示）`)
				} else {
					// 身份已翻转：本机 key 即 bundle key，进入同 key 路径。
					localKey = newKey
					localDataDir = forgedata.RootDir(newKey)
				}
			}
		}
	}

	// Lineage 信任判定：同 key（采纳后重算过的 localKey 也算）= 同身份 lineage。
	trusted := manifest.Origin.Key == localKey && !untrusted
	if trustForeign {
		trusted = true
	}
	trustNote := `受信（同身份 lineage，保留结果字段）`
	if untrusted && manifest.Origin.Key == localKey {
		trustNote = `不可信（--untrusted：同 key 仍剥离外来信号）`
	} else if !trusted {
		trustNote = `不可信（key 不匹配：剥离外来门禁/评分/完成信号；--trust-foreign 放行）`
	}
	fmt.Fprintf(out, `信任：%s`+"\n", trustNote)

	// 任务合并（命令层，逐任务锁防丢更新）+ 其余文件合并（datamerge）。
	stagingData := filepath.Join(staging, `data`)
	taskActions, terr2 := mergeStagingTasks(root, stagingData, trusted, dryRun)
	if terr2 != nil {
		return terr2
	}
	for _, a := range taskActions {
		fmt.Fprintln(out, a)
	}
	actions, derr := datamerge.Dirs(stagingData, localDataDir, datamerge.Options{
		DryRun:           dryRun,
		DedupExactLines:  true,
		TaskPolicy:       datamerge.TaskSkip, // 任务已由上方锁合并处理
		TrustResults:     trusted,
		MergeConclusions: true, // act 结论参与时间戳合并+去重（rekey 不参与——legacy 零变化）
		NoFromBackup:     true, // staging 一次性；回滚保障 = bundle 原件
	})
	if derr != nil {
		return fmt.Errorf(`合并失败: %w`, derr)
	}
	for _, a := range actions {
		fmt.Fprintln(out, a)
	}
	// 跨 key 合并刻意【不】同步注册表：bundle 自声明的 Origin.Key 是不可信输入，
	// registry.Rekey 会移除/改写本机以该 key 登记的任意条目——伪造 manifest 即可
	// 篡改机器本地注册表。bundle key 在本机本就不该有条目（同 key 路径才是常态），
	// 残留漂移由 forge registry audit 检出。
	//
	// Cross-key merge deliberately does NOT sync the registry: the bundle's
	// self-declared Origin.Key is untrusted input, and registry.Rekey would
	// remove/re-key any local entry carrying it — a forged manifest could rewrite
	// machine-local registry state. A bundle key should have no local entry in
	// the first place (same-key is the canonical path); residual drift is caught
	// by forge registry audit.

	if dryRun {
		fmt.Fprintln(out, `（dry-run：以上动作未落盘，账本未记）`)
		return nil
	}

	// 账本最后记：中途崩溃 → 无记录 → 重跑安全收敛。
	rec := projectsync.ImportRecord{
		BundleID:   manifest.BundleID,
		SHA256:     bundleSHA,
		ImportedAt: time.Now(),
		FromKey:    manifest.Origin.Key,
		ToKey:      localKey,
		Counts:     fmt.Sprintf(`%d 个任务动作, %d 个清单文件（剥除 %d）`, len(taskActions), len(manifest.Files), len(stripped)),
	}
	if lerr := projectsync.AppendImportRecord(localDataDir, rec); lerr != nil {
		fmt.Fprintf(out, `warn: 账本记录失败（不影响数据）：%v`+"\n", lerr)
	}
	fmt.Fprintf(out, `✅ 导入完成（%s）`+"\n", rec.Counts)
	if manifest.Origin.Key != localKey {
		fmt.Fprintf(out, `提示：两台机器各跑一次 forge project adopt 后，后续同步免 key 重映射`+"\n")
	}
	return nil
}

// mergeStagingTasks merges staging/data/tasks/*.json into the local DataDir under a
// per-task lock (load-inside-lock, mirror of task import's lost-update guard).
// In-memory transforms: GhostForeignSessions ALWAYS; StripForeignGateSignals when
// !trusted. A staging task that fails to parse as TaskState is skipped with a
// warning (bundle sha guarantees transport integrity, not schema validity).
//
// mergeStagingTasks 在 per-task 锁下把 staging/data/tasks/*.json 合并进本机
// DataDir（锁内重载，镜像 task import 的防丢更新守卫）。内存变换：
// GhostForeignSessions 恒做；!trusted 时加 StripForeignGateSignals。不可解析为
// TaskState 的 staging 任务跳过并警告（bundle sha 保证传输完整性，不保证 schema
// 合法性）。
func mergeStagingTasks(root, stagingData string, trusted, dryRun bool) ([]string, error) {
	tasksDir := filepath.Join(stagingData, `tasks`)
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var actions []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, `.json`) {
			continue
		}
		incoming, lerr := loadStagingTask(filepath.Join(tasksDir, name))
		if lerr != nil {
			actions = append(actions, fmt.Sprintf(`skip   tasks/%s（非合法 TaskState: %v）`, name, lerr))
			continue
		}
		// 以传入任务内部的 task_ref 为锁/加载目标：文件名经 SanitizeRef 折叠
		// （feat/x → feat-x.json），按文件名反推 ref 会与 TaskRef 不一致，被
		// LoadTaskState 的串号校验误判成「本地无此任务」而覆盖本地状态。
		//
		// The incoming task's internal task_ref is the lock/load key: the file
		// name is SanitizeRef-folded (feat/x → feat-x.json), so deriving the ref
		// from the file name mismatches TaskRef, trips LoadTaskState's
		// cross-ref guard, and would read as "no local task" — clobbering local
		// state.
		ref := incoming.TaskRef

		taskpipeline.GhostForeignSessions(incoming)
		// 受信路径也标记外来验收：Run 是外来可执行字符串——verify-acceptance 的
		// 执行闸（AcceptanceForeign && !--trust-foreign 拒绝）只由 Strip 武装，
		// 若受信路径跳过它，同 key 导入会把 2026-08-15 审查定性的「任意命令执行」
		// 向量从 sync 路径重新引入。--trust-foreign 是既有逃生口，成本为零。
		//
		// The trusted path ALSO marks acceptance foreign: Run is a foreign
		// executable string — verify-acceptance's execution gate (refuse when
		// AcceptanceForeign && !--trust-foreign) is armed only by Strip; skipping
		// it on the sync path would reintroduce the arbitrary-command-execution
		// vector through the same-key route. --trust-foreign is the existing
		// escape hatch at zero cost.
		if len(incoming.Acceptance) > 0 {
			incoming.AcceptanceForeign = true
		}
		if !trusted {
			taskpipeline.StripForeignGateSignals(incoming)
		}

		if dryRun {
			actions = append(actions, fmt.Sprintf(`plan   tasks/%s（%s）`, name, trustTag(trusted)))
			continue
		}

		unlock, lkErr := taskpipeline.LockTask(root, ref)
		if lkErr != nil {
			actions = append(actions, fmt.Sprintf(`skip   tasks/%s（加锁失败: %v）`, name, lkErr))
			continue
		}
		local, loadErr := taskpipeline.LoadTaskState(root, ref)
		// 新增判定用文件存在性而非 LoadTaskState 的错误：LoadTaskState 在文件存在
		// 但内部 TaskRef 与请求 ref 不一致时也报错（SanitizeRef 折叠碰撞：feat/x 与
		// feat:x 共享 feat-x.json）——那种「碰撞串号」错误若被当成「本机无此任务」，
		// 下面的 SaveTaskState 会把本地碰撞任务整文件覆盖（静默丢数据）。
		//
		// Freshness is decided by FILE EXISTENCE, not by LoadTaskState's error:
		// LoadTaskState also errors when the file exists but its internal TaskRef
		// mismatches the requested ref (SanitizeRef folding collisions: feat/x and
		// feat:x share feat-x.json) — treating that as "no local task" would let
		// SaveTaskState clobber the colliding local task wholesale (silent data
		// loss).
		localPath := filepath.Join(forgedata.DataDirFor(root), `tasks`, taskcontext.SanitizeRef(ref)+`.json`)
		_, statErr := os.Stat(localPath)
		switch {
		case statErr != nil && loadErr != nil:
			// 文件确实不存在：整任务落地（已幽灵化/按需剥离/验收已标记）。
			if serr := taskpipeline.SaveTaskState(root, incoming); serr != nil {
				unlock()
				return actions, fmt.Errorf(`写入任务 %s 失败: %w`, ref, serr)
			}
			actions = append(actions, fmt.Sprintf(`move   tasks/%s（新增，%s）`, name, trustTag(trusted)))
		case loadErr != nil:
			// 文件存在但加载失败（ref 折叠碰撞 / 本地文件损坏）——绝不覆盖，跳过警告。
			actions = append(actions, fmt.Sprintf(`skip   tasks/%s（本地任务文件存在但不可加载（ref 碰撞或损坏），拒绝覆盖: %v）`, name, loadErr))
		default:
			if trusted {
				taskpipeline.MergeTaskStateSync(local, incoming)
			} else {
				taskpipeline.MergeTaskState(local, incoming)
			}
			if serr := taskpipeline.SaveTaskState(root, local); serr != nil {
				unlock()
				return actions, fmt.Errorf(`合并写入任务 %s 失败: %w`, ref, serr)
			}
			actions = append(actions, fmt.Sprintf(`merge-task tasks/%s（%s）`, name, trustTag(trusted)))
		}
		unlock()
	}
	return actions, nil
}

func trustTag(trusted bool) string {
	if trusted {
		return `单调同步合并`
	}
	return `并集合并（外来信号已剥离）`
}

// loadStagingTask parses a staging task file into a TaskState.
//
// loadStagingTask 把 staging 任务文件解析为 TaskState。
func loadStagingTask(path string) (*taskpipeline.TaskState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s taskpipeline.TaskState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.TaskRef == `` {
		return nil, fmt.Errorf(`task_ref 为空`)
	}
	return &s, nil
}

// loadStagingTask 之上的信任变换在 mergeStagingTasks 的内存中完成；本文件不再有
// 其他辅助。注意：跨 key 合并刻意【不】同步注册表——bundle 自声明的 Origin.Key
// 不可信，registry.Rekey 会按其改写本机注册表（伪造 manifest 即可篡改），残留漂移
// 由 forge registry audit 检出（见上方 mergeStagingTasks 内说明）。
