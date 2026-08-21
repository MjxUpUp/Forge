package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/nodeid"
	"github.com/MjxUpUp/Forge/internal/projectsync"
	"github.com/spf13/cobra"
)

// project_sync.go — `forge project sync init|push|pull|status`: continuous two-way
// sync over a git transport (docs/design sync plan Phase 1). The channel is a plain
// git branch (default name forge-sync) on any remote the user already has credentials
// for — zero new protocol, zero new trust surface. Layout:
//
//	nodes/<node_id>/<project_key>/bundle.tar.gz
//
// Each node writes ONLY its own prefix (git write access is the permission layer)
// and reads everyone else's; pull imports every other node's bundle through the
// standard `project import` path, whose ledger makes re-pulls free and whose lineage
// rules make trust decisions. The bundle file is overwritten per push — history lives
// in git, not in filenames.
//
// project_sync.go —— `forge project sync init|push|pull|status`：经 git 传输的
// 持续双向同步（同步计划 Phase 1）。通道是用户已有凭据的任意 remote 上的一条普通
// git 分支（默认名 forge-sync）——零新协议、零新信任面。布局见上。每个节点只写
// 自己的前缀（git 写权限即权限层）、读所有他人；pull 经标准 `project import`
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

// syncStatus is the machine-local sync binding + last-op stamps (DataDir/sync-remote.json).
//
// syncStatus 是机器本地的同步绑定 + 最近操作戳（DataDir/sync-remote.json）。
type syncStatus struct {
	Remote     string `json:"remote"`
	NodeID     string `json:"node_id"`
	LastPushAt string `json:"last_push_at,omitempty"`
	LastPullAt string `json:"last_pull_at,omitempty"`
}

// syncBranch is the fixed branch name on the sync remote (immune to each machine's
// init.defaultBranch drift).
//
// syncBranch 是同步 remote 上的固定分支名（免疫各机 init.defaultBranch 漂移）。
const syncBranch = `forge-sync`

func syncStatusPath(dataDir string) string { return filepath.Join(dataDir, `sync-remote.json`) }

// loadSyncStatus reads the machine-local sync binding.
//
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

// saveSyncStatus persists the binding atomically.
//
// saveSyncStatus 原子持久化绑定。
func saveSyncStatus(dataDir string, st *syncStatus) error {
	raw, err := json.MarshalIndent(st, ``, `  `)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dataDir, `sync-remote-*.tmp`)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, syncStatusPath(dataDir))
}

// gitOut runs git in dir and returns trimmed stdout.
//
// gitOut 在 dir 跑 git 并返回裁剪后的 stdout。
func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command(`git`, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ``, fmt.Errorf(`git %v: %w\n%s`, args, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// ensureSyncCache clones-or-updates the local cache repo for the remote and leaves it
// on the sync branch at the remote tip (or a fresh empty branch when the remote does
// not have it yet).
//
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
	// Fetch the sync branch into a local ref; absence on the remote is fine (first push).
	//
	// 拉同步分支到本地 ref；远端没有不视为错误（首次 push）。
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

// syncCommitAll commits any staged/unstaged changes; returns false when clean.
//
// syncCommitAll 提交全部变更；干净时返回 false。
func syncCommitAll(dir, msg string) (bool, error) {
	out, err := gitOut(dir, `status`, `--porcelain`)
	if err != nil {
		return false, err
	}
	if out == `` {
		return false, nil
	}
	if _, err := gitOut(dir, `add`, `-A`); err != nil {
		return false, err
	}
	if _, err := gitOut(dir, `-c`, `user.name=forge-sync`, `-c`, `user.email=forge-sync@local`, `commit`, `-m`, msg); err != nil {
		return false, err
	}
	return true, nil
}

func runProjectSync(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf(`需要子命令：init <remote> | push | pull | status`)
	}
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	dataDir := forgedata.DataDirFor(root)
	out := cmd.OutOrStdout()

	switch args[0] {
	case `init`:
		if len(args) != 2 {
			return fmt.Errorf(`用法：forge project sync init <remote>`)
		}
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return err
		}
		id, err := nodeid.LoadOrCreate()
		if err != nil {
			return err
		}
		st := &syncStatus{Remote: args[1], NodeID: id.NodeID}
		if err := saveSyncStatus(dataDir, st); err != nil {
			return err
		}
		// Fail fast on a bad remote instead of at first push.
		if _, err := ensureSyncCache(st.Remote); err != nil {
			return fmt.Errorf(`remote 不可达: %w`, err)
		}
		fmt.Fprintf(out, `✅ 已绑定同步 remote：%s（node=%s，分支 %s）\n`, st.Remote, st.NodeID, syncBranch)
		return nil

	case `push`:
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
		// Export straight into the cache repo (via temp + rename inside the same dir).
		//
		// 直接导出进缓存仓库（同目录 temp + rename）。
		dest := filepath.Join(dir, `nodes`, id.NodeID, key)
		if err := os.MkdirAll(dest, 0755); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(dest, `.bundle-*.tmp`)
		if err != nil {
			return err
		}
		tmpName := tmp.Name()
		defer func() { _ = os.Remove(tmpName) }()
		origin, err := exportOrigin(root)
		if err != nil {
			_ = tmp.Close()
			return err
		}
		manifest, perr := projectsync.Pack(projectsync.PackInput{
			DataDir:      dataDir,
			Origin:       origin,
			ForgeVersion: cleanVersion(cmd.Root().Version),
			Now:          time.Now(),
		}, tmp)
		if cerr := tmp.Close(); cerr != nil {
			return cerr
		}
		if perr != nil {
			return fmt.Errorf(`打包失败: %w`, perr)
		}
		if err := os.Rename(tmpName, filepath.Join(dest, `bundle.tar.gz`)); err != nil {
			return err
		}
		committed, err := syncCommitAll(dir, fmt.Sprintf(`sync: %s %s (%d files)`, id.NodeID, key, len(manifest.Files)))
		if err != nil {
			return err
		}
		if !committed {
			fmt.Fprintln(out, `bundle 无变化——跳过 commit/push`)
		} else if _, err := gitOut(dir, `push`, `origin`, `HEAD:`+syncBranch); err != nil {
			return fmt.Errorf(`git push: %w`, err)
		}
		st.NodeID = id.NodeID
		st.LastPushAt = time.Now().UTC().Format(time.RFC3339)
		if err := saveSyncStatus(dataDir, st); err != nil {
			return err
		}
		fmt.Fprintf(out, `✅ 已推送 bundle %s（%d 文件）→ nodes/%s/%s/\n`, manifest.BundleID, len(manifest.Files), id.NodeID, key)
		return nil

	case `pull`:
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
		entries, _ := os.ReadDir(nodesDir) // 无 nodes/ = 远端还没有任何推送
		imported := 0
		for _, e := range entries {
			if !e.IsDir() || e.Name() == id.NodeID {
				continue // 自己的前缀不导入
			}
			bundle := filepath.Join(nodesDir, e.Name(), key, `bundle.tar.gz`)
			if _, err := os.Stat(bundle); err != nil {
				continue
			}
			fmt.Fprintf(out, `导入节点 %s 的 bundle…\n`, e.Name())
			if err := runProjectImport(projectImportCmd, []string{bundle}); err != nil {
				return fmt.Errorf(`导入节点 %s 失败: %w`, e.Name(), err)
			}
			imported++
		}
		st.LastPullAt = time.Now().UTC().Format(time.RFC3339)
		if err := saveSyncStatus(dataDir, st); err != nil {
			return err
		}
		fmt.Fprintf(out, `✅ pull 完成：处理 %d 个他机 bundle（账本幂等，已导入的自动跳过）\n`, imported)
		return nil

	case `status`:
		st, err := loadSyncStatus(dataDir)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, `remote:     %s\nnode:       %s\nlast push:  %s\nlast pull:  %s\n`,
			st.Remote, st.NodeID, orDashSync(st.LastPushAt), orDashSync(st.LastPullAt))
		// Peers visible in the cache (best effort — cache may not exist yet).
		//
		// 缓存中可见的对端（尽力而为——缓存可能尚未建立）。
		home, herr := forgedata.GlobalHome()
		if herr == nil {
			cacheGlob := filepath.Join(home, `sync-cache`, fmt.Sprintf(`%x`, forgedata.PathKey(st.Remote)), `nodes`)
			if entries, rerr := os.ReadDir(cacheGlob); rerr == nil {
				names := []string{}
				for _, e := range entries {
					if e.IsDir() {
						names = append(names, e.Name())
					}
				}
				sort.Strings(names)
				fmt.Fprintf(out, `nodes seen: %v\n`, names)
			}
		}
		return nil
	}
	return fmt.Errorf(`未知子命令 %q（init|push|pull|status）`, args[0])
}

func orDashSync(s string) string {
	if s == `` {
		return `—`
	}
	return s
}
