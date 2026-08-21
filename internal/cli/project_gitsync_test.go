package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// project_gitsync_test.go — end-to-end two-"machine" sync over a git transport: two
// project checkouts sharing one .forge-project-id (= one project key, the dual-machine
// identity) and two isolated FORGE_DATA_HOMEs (= two machines' state), exchanging
// bundles through a bare git remote under nodes/<node_id>/<key>/.
//
// project_gitsync_test.go —— 经 git 传输的端到端双「机」同步：两个共享同一
// .forge-project-id 的项目检出（= 同一项目 key，双机身份）+ 两个隔离的
// FORGE_DATA_HOME（= 两台机器的状态），经 bare git remote 在
// nodes/<node_id>/<key>/ 布局下交换 bundle。

// gitSyncMachine builds one simulated machine: git checkout with the shared project
// ID and an isolated forge home.
//
// gitSyncMachine 构造一台模拟机器：带共享项目 ID 的 git 检出 + 隔离的 forge home。
func gitSyncMachine(t *testing.T, fpid string) (projRoot, home string) {
	t.Helper()
	projRoot = t.TempDir()
	home = t.TempDir()
	runGit(t, projRoot, `init`)
	runGit(t, projRoot, `-c`, `user.name=forge-test`, `-c`, `user.email=forge@test`, `commit`, `--allow-empty`, `-m`, `init`)
	if err := os.WriteFile(filepath.Join(projRoot, `.forge-project-id`), []byte(fpid+"\n"), 0644); err != nil {
		t.Fatalf("write fpid: %v", err)
	}
	// legacy .forge marker so findProjectRoot discovers the project (same shape as
	// the existing project-sync tests).
	//
	// legacy .forge 标记让 findProjectRoot 能发现项目（与既有 project-sync 测试同形）。
	if err := os.MkdirAll(filepath.Join(projRoot, `.forge`), 0755); err != nil {
		t.Fatalf("mkdir .forge: %v", err)
	}
	return projRoot, home
}

// writeTaskInto plants one task record (SaveTaskState takes the PROJECT ROOT and
// resolves the DataDir itself).
//
// writeTaskInto 落一条任务记录（SaveTaskState 收项目根，DataDir 由它自己解析）。
func writeTaskInto(t *testing.T, projRoot, home, ref, summary string) {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", home)
	if err := taskpipeline.SaveTaskState(projRoot, &taskpipeline.TaskState{
		TaskRef: ref, Branch: ref, Summary: summary, StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("save task: %v", err)
	}
}

// readTaskSummary loads a task's summary (LoadTaskState takes the PROJECT ROOT).
//
// readTaskSummary 读任务 summary（LoadTaskState 收项目根）。
func readTaskSummary(t *testing.T, projRoot, home, ref string) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", home)
	s, err := taskpipeline.LoadTaskState(projRoot, ref)
	if err != nil || s == nil {
		t.Fatalf("load task %s: %v", ref, err)
	}
	return s.Summary
}

// runProjectSync runs `forge project sync <args...>` as the given machine
// (cwd = projRoot, FORGE_DATA_HOME = home).
//
// runProjectSync 以指定机器身份运行 `forge project sync <args...>`。
func runProjectSyncForTest(t *testing.T, projRoot, home string, args ...string) {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", home)
	chdirAndRestore(t, projRoot)
	if err := projectSyncCmd.RunE(projectSyncCmd, args); err != nil {
		t.Fatalf("project sync %v: %v", args, err)
	}
}

// TestProjectSync_TwoMachineGitRoundtrip is THE Phase-1 transport property: work
// recorded on machine A becomes visible on machine B (and vice versa) purely through
// the git channel, and re-pull is free (ledger idempotency).
//
// TestProjectSync_TwoMachineGitRoundtrip 是 Phase 1 传输性质本体：机器 A 上记录
// 的工作经 git 通道在机器 B 上可见（反之亦然），且重复 pull 免费（账本幂等）。
func TestProjectSync_TwoMachineGitRoundtrip(t *testing.T) {
	fpid := `fpid_0123456789abcdef0123456789abcdef`
	projA, homeA := gitSyncMachine(t, fpid)
	projB, homeB := gitSyncMachine(t, fpid)
	remote := t.TempDir()
	runGit(t, remote, `init`, `--bare`)

	// Machine A records work and pushes.
	writeTaskInto(t, projA, homeA, `feat/from-a`, `machine A 的任务`)
	runProjectSyncForTest(t, projA, homeA, `init`, remote)
	runProjectSyncForTest(t, projA, homeA, `push`)

	// Machine B pulls → sees A's task.
	runProjectSyncForTest(t, projB, homeB, `init`, remote)
	runProjectSyncForTest(t, projB, homeB, `pull`)
	if got := readTaskSummary(t, projB, homeB, `feat/from-a`); got != `machine A 的任务` {
		t.Fatalf("B did not receive A's task: %q", got)
	}

	// Machine B records work and pushes; A pulls → sees B's task too.
	writeTaskInto(t, projB, homeB, `feat/from-b`, `machine B 的任务`)
	runProjectSyncForTest(t, projB, homeB, `push`)
	runProjectSyncForTest(t, projA, homeA, `pull`)
	if got := readTaskSummary(t, projA, homeA, `feat/from-b`); got != `machine B 的任务` {
		t.Fatalf("A did not receive B's task: %q", got)
	}

	// Re-pull is a ledger-skip (idempotent, free).
	runProjectSyncForTest(t, projA, homeA, `pull`)

	// The remote layout is nodes/<node_id>/<project-key>/bundle.tar.gz with two
	// distinct node prefixes. (The sync branch is not the remote's default HEAD, so
	// inspect the TREE directly instead of cloning.)
	//
	// 远端布局是 nodes/<node_id>/<project-key>/bundle.tar.gz，两个不同节点前缀。
	// （同步分支不是 remote 默认 HEAD，故直接查树而非 clone。）
	nodes := remoteNodes(t, remote)
	if len(nodes) != 2 {
		t.Fatalf("remote nodes/ = %v, want 2 distinct node prefixes", nodes)
	}
	for _, node := range nodes {
		matches := remoteFiles(t, remote, `nodes/`+node+`/`)
		// bundle.tar.gz + its signature sidecar (trust profile, node-identity §3).
		//
		// bundle.tar.gz + 其签名 sidecar（信任层，node-identity §3）。
		seen := map[string]bool{}
		for _, m := range matches {
			seen[filepath.Base(m)] = true
		}
		if len(matches) != 2 || !seen[`bundle.tar.gz`] || !seen[`bundle.tar.gz.sig`] {
			t.Fatalf("node %s files = %v, want bundle.tar.gz + bundle.tar.gz.sig", node, matches)
		}
	}
}

// remoteNodes lists the distinct node prefixes on the sync branch.
//
// remoteNodes 列出同步分支上的不同节点前缀。
func remoteNodes(t *testing.T, remote string) []string {
	t.Helper()
	out := remoteFiles(t, remote, `nodes/`)
	seen := map[string]bool{}
	var nodes []string
	for _, f := range out {
		node := strings.Split(strings.TrimPrefix(f, `nodes/`), `/`)[0]
		if !seen[node] {
			seen[node] = true
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// remoteFiles lists files under prefix on the sync branch of a bare remote.
//
// remoteFiles 列出 bare remote 同步分支上 prefix 下的文件。
func remoteFiles(t *testing.T, remote, prefix string) []string {
	t.Helper()
	cmd := exec.Command(`git`, `--git-dir`, remote, `ls-tree`, `-r`, `--name-only`, syncBranch, `--`, prefix)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ls-tree: %v\n%s", err, out)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == `` {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// TestProjectSync_InitUnreachableRemoteFails pins the fail-fast contract: an
// unreachable remote must error at init (not at first push) AND leave no binding.
//
// TestProjectSync_InitUnreachableRemoteFails 钉死 fail-fast 契约：不可达 remote
// 必须在 init 报错（而非首次 push）且不留下绑定。
func TestProjectSync_InitUnreachableRemoteFails(t *testing.T) {
	fpid := `fpid_aaaaaaaabbbbbbbbccccccccdddddddd`
	projA, homeA := gitSyncMachine(t, fpid)
	t.Setenv("FORGE_DATA_HOME", homeA)
	chdirAndRestore(t, projA)
	err := projectSyncCmd.RunE(projectSyncCmd, []string{`init`, filepath.Join(t.TempDir(), `nonexistent-remote`)})
	if err == nil {
		t.Fatal("init with unreachable remote must fail")
	}
	if _, serr := loadSyncStatus(forgedata.DataDirFor(projA)); serr == nil {
		t.Fatal("failed init left a sync-remote.json binding behind")
	}
}

// TestProjectSync_PullSkipsBadNodes: one corrupt bundle from a well-formed peer dir
// and one illegally-named dir must both be skipped with the pull itself succeeding.
//
// TestProjectSync_PullSkipsBadNodes：形态合法对端目录里的损坏 bundle 与非法目录名
// 都必须被跳过且 pull 本身成功。
func TestProjectSync_PullSkipsBadNodes(t *testing.T) {
	fpid := `fpid_eeeeeeeeffffffff0000000011111111`
	projA, homeA := gitSyncMachine(t, fpid)
	projB, homeB := gitSyncMachine(t, fpid)
	remote := t.TempDir()
	runGit(t, remote, `init`, `--bare`)

	writeTaskInto(t, projA, homeA, `feat/ok`, `good`)
	runProjectSyncForTest(t, projA, homeA, `init`, remote)
	runProjectSyncForTest(t, projA, homeA, `push`)

	// Plant a corrupt bundle under a VALID node name and a dir with an ILLEGAL name,
	// via a direct checkout of the sync branch.
	co := filepath.Join(t.TempDir(), `co`)
	runGit(t, t.TempDir(), `clone`, `-b`, syncBranch, remote, co)
	badNode := `fnode_99999999999999999999999999999999`
	projKey := ``
	for _, f := range remoteFiles(t, remote, `nodes/`) {
		parts := strings.Split(f, `/`)
		if len(parts) >= 3 && parts[0] == `nodes` {
			projKey = parts[2]
		}
	}
	if projKey == `` {
		t.Fatal("no project key on remote")
	}
	if err := os.MkdirAll(filepath.Join(co, `nodes`, badNode, projKey), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(co, `nodes`, badNode, projKey, `bundle.tar.gz`), []byte(`corrupt-not-gzip`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(co, `nodes`, `notanode`, projKey), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(co, `nodes`, `notanode`, projKey, `bundle.tar.gz`), []byte(`x`), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, co, `-c`, `user.name=t`, `-c`, `user.email=t@t`, `add`, `-A`)
	runGit(t, co, `-c`, `user.name=t`, `-c`, `user.email=t@t`, `commit`, `-m`, `plant bad nodes`)
	runGit(t, co, `push`, `origin`, `HEAD:`+syncBranch)

	// Pull must succeed and still import A's good bundle.
	runProjectSyncForTest(t, projB, homeB, `init`, remote)
	runProjectSyncForTest(t, projB, homeB, `pull`)
	if got := readTaskSummary(t, projB, homeB, `feat/ok`); got != `good` {
		t.Fatalf("good peer bundle not imported: %q", got)
	}
}

// TestProjectSync_StatusReportsNodes covers the observability surface: after a push,
// the persisted sync status records the remote, this machine's node prefix and the
// last push time.
//
// TestProjectSync_StatusReportsNodes 覆盖可观测面：push 后持久化的 sync 状态记录
// remote、本机节点前缀与最近推送时间。
func TestProjectSync_StatusReportsNodes(t *testing.T) {
	fpid := `fpid_fedcba9876543210fedcba9876543210`
	projA, homeA := gitSyncMachine(t, fpid)
	remote := t.TempDir()
	runGit(t, remote, `init`, `--bare`)

	writeTaskInto(t, projA, homeA, `feat/s`, `x`)
	runProjectSyncForTest(t, projA, homeA, `init`, remote)
	runProjectSyncForTest(t, projA, homeA, `push`)

	t.Setenv("FORGE_DATA_HOME", homeA)
	st, err := loadSyncStatus(forgedata.DataDirFor(projA))
	if err != nil {
		t.Fatalf("loadSyncStatus: %v", err)
	}
	if st.Remote != remote {
		t.Fatalf("status remote = %q, want %q", st.Remote, remote)
	}
	if st.NodeID == `` || st.LastPushAt == `` {
		t.Fatalf("status missing node/push stamp: %+v", st)
	}
}
