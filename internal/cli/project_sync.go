package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/nodeid"
	"github.com/MjxUpUp/Forge/internal/projectsync"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

// project_sync.go —— `forge project sync init|push|pull|status`：经 git 传输的
// 持续双向同步（同步计划 Phase 1）。通道是用户已有凭据的任意 remote 上的一条普通
// git 分支（默认名 forge-sync）——零新协议、零新信任面。布局：
//
//	nodes/<node_id>/<project_key>/bundle.tar.gz
//
// 每个节点只写自己的前缀（git 写权限即权限层）、读所有他人；pull 经标准 `project import`
// 路径导入每个他机 bundle——账本让重复 pull 免费，lineage 规则裁决信任。
// bundle 文件每次 push 覆盖——历史留在 git 里，不在文件名里。

func init() {
	projectCmd.AddCommand(projectSyncCmd)
}

var projectSyncCmd = &cobra.Command{
	Use:   `sync <init|push|pull|status> [remote]`,
	Short: `经 git 通道持续双向同步项目数据（nodes/<node_id>/ 前缀布局）`,
	Long: `forge project sync —— 多机器持续同步（Phase 1 传输层）。

  init <remote>   绑定同步 remote（任意 git URL/路径；用 forge-sync 分支）
  push            导出本机 bundle 推到 nodes/<node_id>/<key>/ 并 git push
  pull            git 拉取后导入所有他机 bundle（账本幂等，重复 pull 免费）
  status          显示绑定 remote、本机节点、最近 push/pull 时间

机器本地配置存 DataDir/sync-remote.json（allowlist 默认拒绝——永不随 bundle
旅行）；同步缓存仓库在 ~/.forge/sync-cache/。`,
	RunE: runProjectSync,
}

// syncStatus 是机器本地的同步绑定 + 最近操作戳（DataDir/sync-remote.json）。
type syncStatus struct {
	Remote     string `json:"remote"`
	NodeID     string `json:"node_id"`
	LastPushAt string `json:"last_push_at,omitempty"`
	LastPullAt string `json:"last_pull_at,omitempty"`
}

// syncBranch 是同步 remote 上的固定分支名（免疫各机 init.defaultBranch 漂移）。
const syncBranch = `forge-sync`

func syncStatusPath(dataDir string) string { return filepath.Join(dataDir, `sync-remote.json`) }

// loadSyncStatus 读机器本地同步绑定。
func loadSyncStatus(dataDir string) (*syncStatus, error) {
	raw, err := os.ReadFile(syncStatusPath(dataDir))
	if err != nil {
		return nil, fmt.Errorf(`同步未初始化（先运行 forge project sync init <remote>）: %w`, err)
	}
	var st syncStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf(`sync-remote.json 损坏: %w`, err)
	}
	return &st, nil
}

// saveSyncStatus 原子持久化绑定。
func saveSyncStatus(dataDir string, st *syncStatus) error {
	raw, err := json.MarshalIndent(st, ``, `  `)
	if err != nil {
		return err
	}
	// util.AtomicWrite（temp + fsync + rename 重试）——此前手写无 fsync 版本，
	// 收敛到共享实现。
	return util.AtomicWrite(syncStatusPath(dataDir), raw, 0600)
}

// gitOut 在 dir 跑 git 并返回裁剪后的 stdout。
func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command(`git`, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ``, fmt.Errorf(`git %v: %w`+"\n"+`%s`, args, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// ensureSyncCache 克隆或更新 remote 的本地缓存仓库，并停在同步分支的远端尖端
// （远端尚无该分支时是全新的空分支）。
func ensureSyncCache(remote string) (string, error) {
	home, err := forgedata.GlobalHome()
	if err != nil {
		return ``, err
	}
	dir := filepath.Join(home, `sync-cache`, fmt.Sprintf(`%x`, forgedata.PathKey(remote)))
	if _, err := os.Stat(filepath.Join(dir, `.git`)); err != nil {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return ``, err
		}
		if _, err := gitOut(dir, `init`); err != nil {
			return ``, fmt.Errorf(`git init: %w`, err)
		}
		if _, err := gitOut(dir, `remote`, `add`, `origin`, remote); err != nil {
			return ``, fmt.Errorf(`git remote add: %w`, err)
		}
	}
	// 区分「远端无分支」（正常——首次 push）与「remote 不可达/认证失败」（必须响亮
	// 失败）：裸 git fetch 对两者都返回 exit 128，先用 ls-remote 探测才能保住
	// init 的 fail-fast 承诺。
	if _, err := gitOut(dir, `ls-remote`, `--heads`, `origin`, syncBranch); err != nil {
		return ``, fmt.Errorf(`remote 不可达或认证失败（ls-remote 探测）: %w`, err)
	}
	if _, err := gitOut(dir, `fetch`, `origin`, `+refs/heads/`+syncBranch+`:refs/remotes/origin/`+syncBranch); err != nil {
		if _, err2 := gitOut(dir, `checkout`, `-B`, syncBranch); err2 != nil {
			return ``, fmt.Errorf(`checkout fresh sync branch: %w`, err2)
		}
		return dir, nil
	}
	if _, err := gitOut(dir, `checkout`, `-B`, syncBranch, `origin/`+syncBranch); err != nil {
		return ``, fmt.Errorf(`checkout sync branch: %w`, err)
	}
	return dir, nil
}

// syncCommitAll 只暂存给定前缀（此前崩溃的 push 可能留下 .bundle-*.tmp 残骸——
// 整树 add 会把它提交并推上远端）；提交时关签名与 hooks（用户全局
// commit.gpgsign 或 hooksPath 不得卡死同步通道）。干净时返回 false。
func syncCommitAll(dir, prefix, msg string) (bool, error) {
	out, err := gitOut(dir, `status`, `--porcelain`, `--`, prefix)
	if err != nil {
		return false, err
	}
	if out == `` {
		return false, nil
	}
	if _, err := gitOut(dir, `add`, `--`, prefix); err != nil {
		return false, err
	}
	if _, err := gitOut(dir, `-c`, `user.name=forge-sync`, `-c`, `user.email=forge-sync@local`,
		`-c`, `commit.gpgsign=false`, `-c`, `core.hooksPath=/dev/null`,
		`commit`, `-m`, msg); err != nil {
		return false, err
	}
	return true, nil
}

// rePackBundle 把项目 bundle 打包进 dest/bundle.tar.gz（temp + rename），并先清扫
// 此前崩溃残留的 .bundle-*.tmp。
func rePackBundle(cmd *cobra.Command, root, dataDir, dest string) (*projectsync.Manifest, error) {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return nil, err
	}
	if debris, _ := filepath.Glob(filepath.Join(dest, `.bundle-*.tmp`)); len(debris) > 0 {
		for _, d := range debris {
			_ = os.Remove(d)
		}
	}
	tmp, err := os.CreateTemp(dest, `.bundle-*.tmp`)
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	origin, err := exportOrigin(root)
	if err != nil {
		_ = tmp.Close()
		return nil, err
	}
	manifest, perr := projectsync.Pack(projectsync.PackInput{
		DataDir:      dataDir,
		Origin:       origin,
		ForgeVersion: cleanVersion(cmd.Root().Version),
		Now:          time.Now(),
	}, tmp)
	if cerr := tmp.Close(); cerr != nil {
		return nil, cerr
	}
	if perr != nil {
		return nil, fmt.Errorf(`打包失败: %w`, perr)
	}
	if err := os.Rename(tmpName, filepath.Join(dest, `bundle.tar.gz`)); err != nil {
		return nil, err
	}
	return manifest, nil
}

func runProjectSync(cmd *cobra.Command, args []string) (err error) {
	if len(args) < 1 {
		return fmt.Errorf(`需要子命令：init <remote> | push | pull | status`)
	}
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	dataDir := forgedata.DataDirFor(root)
	out := cmd.OutOrStdout()

	// 操作结果落章（project-sync，observation 类——见 CheckProjectSync）：
	// sync-remote.json 只给成功的 push/pull 打戳，失败操作留着旧时间戳、终端之外
	// 不可见。在具名返回值上 defer 一个记录器：各 case 的 `return <expr>` 在
	// deferred 函数运行前完成赋值。status 是只读操作——每次轮询都落章会刷屏，
	// 故它永不设置 syncOp。
	var syncOp, syncNote string
	defer func() {
		if syncOp != `` {
			recordSyncOutcome(root, syncOp, syncNote, err)
		}
	}()

	switch args[0] {
	case `init`:
		if len(args) != 2 {
			return fmt.Errorf(`用法：forge project sync init <remote>`)
		}
		syncOp = `init`    // 参数校验通过才落章——CLI 笔误（漏 remote）不上面板
		syncNote = args[1] // remote——成败都带进落章（失败时回答「绑的哪个 remote」）
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return err
		}
		id, err := nodeid.LoadOrCreate()
		if err != nil {
			return err
		}
		// 先校验 remote 再持久化绑定——探测失败不得留下半成品 sync-remote.json。
		if _, err := ensureSyncCache(args[1]); err != nil {
			return fmt.Errorf(`remote 不可达: %w`, err)
		}
		st := &syncStatus{Remote: args[1], NodeID: id.NodeID}
		if err := saveSyncStatus(dataDir, st); err != nil {
			return err
		}
		fmt.Fprintf(out, `✅ 已绑定同步 remote：%s（node=%s，分支 %s）`+"\n", st.Remote, st.NodeID, syncBranch)
		return nil

	case `push`:
		syncOp = `push`
		st, err := loadSyncStatus(dataDir)
		if err != nil {
			return err
		}
		id, err := nodeid.LoadOrCreate()
		if err != nil {
			return err
		}
		key, err := forgedata.Key(root)
		if err != nil {
			return err
		}
		dir, err := ensureSyncCache(st.Remote)
		if err != nil {
			return err
		}
		// 直接导出进缓存仓库（同目录 temp + rename）。
		dest := filepath.Join(dir, `nodes`, id.NodeID, key)
		manifest, err := rePackBundle(cmd, root, dataDir, dest)
		if err != nil {
			return err
		}
		// .sig sidecar 随 bundle 一起进节点前缀（对端 pull 时对照各自 trust store
		// 验真）。团队档：签名失败是硬错误（未签名的推送反正会被每个对端拒收）。
		if _, _, serr := writeBundleSigRespectingPolicy(filepath.Join(dest, `bundle.tar.gz`)); serr != nil {
			return serr
		}
		committed, err := syncCommitAll(dir, filepath.Join(`nodes`, id.NodeID, key),
			fmt.Sprintf(`sync: %s %s (%d files)`, id.NodeID, key, len(manifest.Files)))
		if err != nil {
			return err
		}
		if !committed {
			fmt.Fprintln(out, `bundle 无变化——跳过 commit/push`)
			syncNote = `bundle 无变化`
		} else {
			// push 带一次重拉重试：双机同时 push 时败者非快进；重同步缓存并重新提交
			// 即可收敛且不丢任何一侧（各节点只写自己前缀）。
			if _, err := gitOut(dir, `push`, `origin`, `HEAD:`+syncBranch); err != nil {
				if _, rerr := ensureSyncCache(st.Remote); rerr != nil {
					return fmt.Errorf(`git push: %w（重拉缓存也失败: %v）`, err, rerr)
				}
				// 无条件从活 DataDir 重打包——绝不 Stat 探测 worktree 文件。上面的重拉
				// 已 `checkout -B` 到远端 tip，会把 worktree 里我们前缀下的 bundle 回退
				// 成远端树上的旧字节：文件存在但是旧的，Stat 探测会跳过重打包，重试就
				// 什么都没推（TestProjectSync_PushRetryAfterRemoteMoves 钉住的收敛洞）。
				if _, err := rePackBundle(cmd, root, dataDir, dest); err != nil {
					return err
				}
				// 新 bundle 字节 = 新摘要——sidecar 必须重签。
				if _, _, serr := writeBundleSigRespectingPolicy(filepath.Join(dest, `bundle.tar.gz`)); serr != nil {
					return serr
				}
				recommitted, err := syncCommitAll(dir, filepath.Join(`nodes`, id.NodeID, key),
					fmt.Sprintf(`sync: %s %s (%d files, retry)`, id.NodeID, key, len(manifest.Files)))
				if err != nil {
					return err
				}
				if !recommitted {
					// Bytes identical to the remote tree — nothing of ours was lost.
					fmt.Fprintln(out, `重试后 bundle 无变化——远端已含同内容`)
				} else if _, err := gitOut(dir, `push`, `origin`, `HEAD:`+syncBranch); err != nil {
					return fmt.Errorf(`git push（含一次重试）: %w`, err)
				}
			}
		}
		st.NodeID = id.NodeID
		st.LastPushAt = time.Now().UTC().Format(time.RFC3339)
		if err := saveSyncStatus(dataDir, st); err != nil {
			return err
		}
		if syncNote == `` {
			syncNote = fmt.Sprintf(`%d 文件 → nodes/%s/%s/`, len(manifest.Files), id.NodeID, key)
		}
		fmt.Fprintf(out, `✅ 已推送 bundle %s（%d 文件）→ nodes/%s/%s/`+"\n", manifest.BundleID, len(manifest.Files), id.NodeID, key)
		return nil

	case `pull`:
		syncOp = `pull`
		st, err := loadSyncStatus(dataDir)
		if err != nil {
			return err
		}
		id, err := nodeid.LoadOrCreate()
		if err != nil {
			return err
		}
		key, err := forgedata.Key(root)
		if err != nil {
			return err
		}
		dir, err := ensureSyncCache(st.Remote)
		if err != nil {
			return err
		}
		nodesDir := filepath.Join(dir, `nodes`)
		entries, err := os.ReadDir(nodesDir)
		if err != nil && !os.IsNotExist(err) {
			// 把权限/IO 失败折叠成"零 bundle"会给一次没发生的同步盖上 PASS
			//（LastPullAt + checklog）——H-1 零计数伪装健康模式。
			return fmt.Errorf(`读取同步缓存 nodes/ 失败: %w`, err)
		}
		// 无 nodes/ = 远端还没有任何推送（IsNotExist → entries 为 nil）
		imported := 0
		var failed []string
		for _, e := range entries {
			if !e.IsDir() || e.Name() == id.NodeID {
				continue // 自己的前缀不导入
			}
			// 远端目录名是攻击者可影响输入——当作节点前先过形态检查（伪造归因 /
			// 精心构造目录名的终端转义注入）。
			if !nodeid.ValidNodeID(e.Name()) {
				fmt.Fprintf(out, `⚠ 跳过非法节点目录名 %q`+"\n", e.Name())
				continue
			}
			bundle := filepath.Join(nodesDir, e.Name(), key, `bundle.tar.gz`)
			if _, err := os.Stat(bundle); err != nil {
				if !os.IsNotExist(err) {
					// 瞬时 stat 失败（Windows 杀软/sharing-violation、目录不可读）
					// 绝不能落进 key 错位分支——那条路指引用户跑 `forge project
					// adopt`（不可逆的身份迁移）。改为响亮跳过该节点。
					fmt.Fprintf(out, `⚠ 节点 %s 的 bundle 暂不可读（%v），本次跳过`+"\n", e.Name(), err)
					failed = append(failed, e.Name())
					continue
				}
				// A peer prefix that exists but not under OUR key is not silence-worthy:
				// the peer pushes under its project key while ours differs (e.g. both
				// machines still path-identity with different checkout paths) — the two
				// nodes then see each other's prefixes forever while every pull reports
				// “处理 0 个” with exit 0. Name the misalignment instead.
				//
				// 对端前缀存在但不在本机 key 下——这不该静默：对端按它的项目 key 推送
				// 而本机 key 不同（如双机仍是路径身份、检出路径不同）——两台机器从此
				// 互相看得见前缀，而每次 pull 都「处理 0 个」exit 0。点名错位。
				warnPeerKeyMismatch(out, nodesDir, e.Name(), key)
				continue
			}
			fmt.Fprintf(out, `导入节点 %s 的 bundle…`+"\n", e.Name())
			// 逐节点容错隔离（一个坏 bundle 不得 brick 整个 pull）——但失败被收集
			// 并在结尾作为 pull 级错误报告：策略性拒收（团队档未签/签名无效）若只
			// 打一行警告就 exit 0，等于静默的同步失败。
			if err := runProjectImport(projectImportCmd, []string{bundle}); err != nil {
				fmt.Fprintf(out, `⚠ 节点 %s 导入失败: %v`+"\n", e.Name(), err)
				failed = append(failed, e.Name())
				continue
			}
			imported++
		}
		st.LastPullAt = time.Now().UTC().Format(time.RFC3339)
		if err := saveSyncStatus(dataDir, st); err != nil {
			return err
		}
		syncNote = fmt.Sprintf(`导入 %d 个他机 bundle，失败 %d`, imported, len(failed))
		if len(failed) > 0 {
			return fmt.Errorf(`pull 部分失败：%d 个节点导入被拒/失败 %v（已导入 %d 个；修复后对端重推或本机调整信任配置后再 pull）`, len(failed), failed, imported)
		}
		fmt.Fprintf(out, `✅ pull 完成：处理 %d 个他机 bundle（账本幂等，已导入的自动跳过）`+"\n", imported)
		return nil

	case `status`:
		st, err := loadSyncStatus(dataDir)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, `remote:     %s`+"\n"+`node:       %s`+"\n"+`last push:  %s`+"\n"+`last pull:  %s`+"\n",
			st.Remote, st.NodeID, orDashSync(st.LastPushAt), orDashSync(st.LastPullAt))
		// 缓存中可见的对端（尽力而为——缓存可能尚未建立）。
		home, herr := forgedata.GlobalHome()
		if herr == nil {
			cacheGlob := filepath.Join(home, `sync-cache`, fmt.Sprintf(`%x`, forgedata.PathKey(st.Remote)), `nodes`)
			if entries, rerr := os.ReadDir(cacheGlob); rerr == nil {
				names := []string{}
				for _, e := range entries {
					if !e.IsDir() {
						continue
					}
					// 与 pull 路径同一攻击者可影响输入：精心构造的目录名（ANSI 转义/
					// 换行）不得原文直达终端——过形态检查，非法名引显。
					if nodeid.ValidNodeID(e.Name()) {
						names = append(names, e.Name())
					} else {
						names = append(names, fmt.Sprintf(`%q（非法节点名）`, e.Name()))
					}
				}
				sort.Strings(names)
				fmt.Fprintf(out, `nodes seen: %v`+"\n", names)
			}
		}
		return nil
	}
	return fmt.Errorf(`未知子命令 %q（init|push|pull|status）`, args[0])
}

// warnPeerKeyMismatch 解释「对端前缀下有 bundle、但没有一个在本机 key 下」的双机
// key 错位。没有这条提示，错位完全不可观测：pull 零 bundle 成功、status 两机互相
// 可见，恢复手段（forge project adopt）永远不会被提出。
func warnPeerKeyMismatch(out io.Writer, nodesDir, peer, key string) {
	peerKeys, err := os.ReadDir(filepath.Join(nodesDir, peer))
	if err != nil {
		return
	}
	var keys []string
	for _, e := range peerKeys {
		if !e.IsDir() || e.Name() == key {
			continue // 本机 key 前缀存在但 bundle 缺失是另一种状态，不是 key 错位
		}
		// 远端目录名是攻击者可影响输入——引显，绝不原文输出（与上方节点名检查
		// 同一威胁模型）。
		keys = append(keys, fmt.Sprintf(`%q`, e.Name()))
	}
	if len(keys) == 0 {
		return // 没有非本机 key 的子目录：无对齐信息可给
	}
	fmt.Fprintf(out, `⚠ 节点 %s 推的是 key=%v 的 bundle，与本机 key=%s 不一致（互相看得见但永远同步不上）——两机各跑一次 forge project adopt 对齐身份`+"\n", peer, keys, key)
}

func orDashSync(s string) string {
	if s == `` {
		return `—`
	}
	return s
}

// recordSyncOutcome 尽力落章一次同步操作结果（observation 类——见
// CheckProjectSync）。面板此前唯一的 sync 信号（sync-remote.json）只给成功操作
// 打戳——失败的 push 留着旧时间戳，失败在终端之外完全不可见；本函数是让失败
// 可见的记录。Level：成功 pass，失败 fail。尽力而为：落章失败绝不得打断同步
// 操作本身。
func recordSyncOutcome(root, op, note string, opErr error) {
	e := &checklog.Entry{
		Check:   checklog.CheckProjectSync,
		Checked: true,
		Meta:    map[string]string{checklog.MetaKeySyncOp: op},
	}
	if opErr != nil {
		e.Passed, e.Level = false, checklog.LevelFail
		if note != `` {
			e.Detail = fmt.Sprintf(`sync %s 失败（%s）: %v`, op, note, opErr)
		} else {
			e.Detail = fmt.Sprintf(`sync %s 失败: %v`, op, opErr)
		}
		// git 报错文本可含多行完整输出（gitOut 嵌入 %s）——Detail 是人类可读摘要，
		// 截断到 300 rune 防超长行进 jsonl/feed（渲染侧另有 esc）。
		if r := []rune(e.Detail); len(r) > 300 {
			e.Detail = string(r[:300]) + `…`
		}
	} else {
		e.Passed, e.Level = true, checklog.LevelPass
		e.Detail = strings.TrimSpace(`sync ` + op + ` 成功 ` + note)
	}
	_ = checklog.Record(root, e)
}
