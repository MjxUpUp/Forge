package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
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

	// Pull reports the bad node as a pull-level error (policy-visible) while still
	// importing the good peer's bundle (fault isolation).
	//
	// pull 把坏节点作为 pull 级错误报告（策略可见），同时仍导入好对端的 bundle
	// （容错隔离）。
	runProjectSyncForTest(t, projB, homeB, `init`, remote)
	t.Setenv("FORGE_DATA_HOME", homeB)
	chdirAndRestore(t, projB)
	pullErr := projectSyncCmd.RunE(projectSyncCmd, []string{`pull`})
	if pullErr == nil || !strings.Contains(pullErr.Error(), `pull 部分失败`) {
		t.Fatalf("pull must report the failed node: %v", pullErr)
	}
	if got := readTaskSummary(t, projB, homeB, `feat/ok`); got != `good` {
		t.Fatalf("good peer bundle not imported: %q", got)
	}
}

// TestProjectSync_PushRetryAfterRemoteMoves pins THE convergence property the push
// retry exists for: when the remote advances between this machine's fetch and its
// push (the two-machine race), the losing push must converge — a peer pulling
// afterwards sees the loser's NEW work, not the stale bundle already on the remote.
// The race is injected deterministically: a pre-push hook in A's cache repo pushes
// a third-party commit during A's FIRST push, making it non-fast-forward so the
// retry path runs.
//
// Regression shape (fixed in fix/dsh-review-followup): the retry probed
// os.Stat(bundle) to decide whether to re-pack — but the retry's own
// `checkout -B origin/forge-sync` had already reverted the worktree copy to the
// OLD bundle from the remote tree, so Stat succeeded, no re-pack happened, the
// re-commit saw a clean tree, and the second push was an empty success: the new
// bundle was silently dropped while "✅ 已推送" printed.
//
// TestProjectSync_PushRetryAfterRemoteMoves 钉死 push 重试存在的收敛性质：remote
// 在本机 fetch 与 push 之间前进（双机竞态）时，失败的 push 必须收敛——之后 pull
// 的对端要看到败者的新工作，而不是远端上已有的旧 bundle。竞态用确定性注入：A
// 缓存仓里的 pre-push hook 在 A 首次 push 期间推入第三方提交，使该 push 非快进、
// 走进重试路径。
func TestProjectSync_PushRetryAfterRemoteMoves(t *testing.T) {
	if runtime.GOOS == `windows` {
		t.Skip(`shell pre-push hook not portable to windows`)
	}
	fpid := `fpid_7777777788888888aaaaaaaabbbbbbbb`
	projA, homeA := gitSyncMachine(t, fpid)
	projB, homeB := gitSyncMachine(t, fpid)
	remote := t.TempDir()
	runGit(t, remote, `init`, `--bare`)

	// A pushes once — the remote tree now carries A's prefix with the v1 bundle.
	writeTaskInto(t, projA, homeA, `feat/v1`, `v1 work`)
	runProjectSyncForTest(t, projA, homeA, `init`, remote)
	runProjectSyncForTest(t, projA, homeA, `push`)

	// Prepare a third-party commit in a scratch clone (parented on the current
	// tip). The pre-push hook will push it during A's first push — that is the
	// injected "the other machine won the race".
	co := filepath.Join(t.TempDir(), `co`)
	runGit(t, t.TempDir(), `clone`, `-b`, syncBranch, remote, co)
	if err := os.WriteFile(filepath.Join(co, `race-marker`), []byte(`race`), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, co, `-c`, `user.name=t`, `-c`, `user.email=t@t`, `add`, `-A`)
	runGit(t, co, `-c`, `user.name=t`, `-c`, `user.email=t@t`, `commit`, `-m`, `remote moves`)

	// Install the hook in A's cache repo (the same repo gitOut pushes from; push
	// does not disable hooks). One-shot via flag file so the retry's second push
	// passes through untouched.
	flag := filepath.Join(t.TempDir(), `race.done`)
	t.Setenv(`FORGE_RACE_CO`, co)
	t.Setenv(`FORGE_RACE_FLAG`, flag)
	hooks := filepath.Join(homeA, `sync-cache`, fmt.Sprintf(`%x`, forgedata.PathKey(remote)), `.git`, `hooks`)
	if err := os.MkdirAll(hooks, 0755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"[ -f \"$FORGE_RACE_FLAG\" ] && exit 0\n" +
		": > \"$FORGE_RACE_FLAG\"\n" +
		"git -C \"$FORGE_RACE_CO\" push origin HEAD:" + syncBranch + "\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(hooks, `pre-push`), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	// A records NEW work and pushes: fetch (old tip) → pack v2 → commit → first
	// push fires the hook → remote moves → push rejected → retry path runs.
	writeTaskInto(t, projA, homeA, `feat/v2`, `v2 work`)
	runProjectSyncForTest(t, projA, homeA, `push`)

	// The property: a peer pulling afterwards MUST see the v2 work.
	runProjectSyncForTest(t, projB, homeB, `init`, remote)
	runProjectSyncForTest(t, projB, homeB, `pull`)
	if got := readTaskSummary(t, projB, homeB, `feat/v2`); got != `v2 work` {
		t.Fatalf("race loser's push did not converge: feat/v2 missing on peer after pull (got %q)", got)
	}
}

// TestProjectSync_PullWarnsOnKeyMismatch: a path-identity machine pulling an
// ID-identity peer's prefix must be TOLD the two keys do not line up — without the
// hint the misalignment is unobservable (pull succeeds with 0 bundles, exit 0) and
// the recovery (forge project adopt) is never suggested.
//
// TestProjectSync_PullWarnsOnKeyMismatch：路径身份机器 pull 到 ID 身份对端的前缀
// 时必须被告知两机 key 不对齐——没有这条提示错位完全不可观测（pull 以 0 bundle
// 成功、exit 0），恢复手段（forge project adopt）永远不会被提出。
func TestProjectSync_PullWarnsOnKeyMismatch(t *testing.T) {
	projA, homeA := gitSyncMachine(t, `fpid_a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6`)
	// B: path identity — no .forge-project-id, its key derives from its own path.
	projB, homeB := t.TempDir(), t.TempDir()
	runGit(t, projB, `init`)
	runGit(t, projB, `-c`, `user.name=forge-test`, `-c`, `user.email=forge@test`, `commit`, `--allow-empty`, `-m`, `init`)
	if err := os.MkdirAll(filepath.Join(projB, `.forge`), 0755); err != nil {
		t.Fatal(err)
	}
	remote := t.TempDir()
	runGit(t, remote, `init`, `--bare`)

	writeTaskInto(t, projA, homeA, `feat/k`, `x`)
	runProjectSyncForTest(t, projA, homeA, `init`, remote)
	runProjectSyncForTest(t, projA, homeA, `push`)

	var buf bytes.Buffer
	projectSyncCmd.SetOut(&buf)
	t.Cleanup(func() { projectSyncCmd.SetOut(nil) })
	t.Setenv("FORGE_DATA_HOME", homeB)
	chdirAndRestore(t, projB)
	if err := projectSyncCmd.RunE(projectSyncCmd, []string{`init`, remote}); err != nil {
		t.Fatalf("init: %v", err)
	}
	buf.Reset()
	if err := projectSyncCmd.RunE(projectSyncCmd, []string{`pull`}); err != nil {
		t.Fatalf("pull: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `不一致`) || !strings.Contains(out, `forge project adopt`) {
		t.Fatalf("key misalignment must be named with the adopt recovery hint, got: %s", out)
	}
}

// TestProjectSync_StatusQuotesIllegalNodeDirs: `nodes seen` must not echo crafted
// directory names raw (ANSI escapes / newlines straight to the terminal) — the same
// attacker-influenceable input the pull path shape-checks.
//
// TestProjectSync_StatusQuotesIllegalNodeDirs：`nodes seen` 不得原文回显精心构造的
// 目录名（ANSI 转义/换行直达终端）——与 pull 路径形态检查的同一攻击者可影响输入。
func TestProjectSync_StatusQuotesIllegalNodeDirs(t *testing.T) {
	projA, homeA := gitSyncMachine(t, `fpid_b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6`)
	remote := t.TempDir()
	runGit(t, remote, `init`, `--bare`)
	writeTaskInto(t, projA, homeA, `feat/s2`, `x`)
	runProjectSyncForTest(t, projA, homeA, `init`, remote)
	runProjectSyncForTest(t, projA, homeA, `push`)

	// Control characters are illegal in windows filenames — pick an illegal-per-
	// ValidNodeID name each filesystem accepts, and assert the raw-escape property
	// only where the escape variant exists.
	//
	// 控制字符在 windows 文件名里非法——选各文件系统都接受、但 ValidNodeID 拒收
	// 的名字；裸转义断言只在存在转义变体的平台上做。
	evil := `bad` + "\x1b" + `[31mnode`
	if runtime.GOOS == "windows" {
		evil = `not a node!`
	}
	cacheNodes := filepath.Join(homeA, `sync-cache`, fmt.Sprintf(`%x`, forgedata.PathKey(remote)), `nodes`)
	if err := os.MkdirAll(filepath.Join(cacheNodes, evil), 0755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	projectSyncCmd.SetOut(&buf)
	t.Cleanup(func() { projectSyncCmd.SetOut(nil) })
	t.Setenv("FORGE_DATA_HOME", homeA)
	chdirAndRestore(t, projA)
	if err := projectSyncCmd.RunE(projectSyncCmd, []string{`status`}); err != nil {
		t.Fatalf("status: %v", err)
	}
	out := buf.String()
	if runtime.GOOS != "windows" && strings.Contains(out, "\x1b[31m") {
		t.Fatalf("raw ANSI escape must not reach the terminal output, got: %q", out)
	}
	if !strings.Contains(out, `（非法节点名）`) {
		t.Fatalf("illegal dir must be shown quoted and marked, got: %s", out)
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

// TestProjectSync_OutcomeRecorded: every sync init/push/pull outcome (success AND
// failure) lands in checklog as a project-sync entry with the op in Meta — the
// failure-visible record the panel needs (sync-remote.json stamps successes only, so
// a failed push used to leave the old timestamp standing and tell no one).
//
// TestProjectSync_OutcomeRecorded：每次 sync init/push/pull 的成败都落 checklog 的
// project-sync 条目、操作名在 Meta——面板需要的「失败可见」记录（sync-remote.json
// 只给成功打戳，失败的 push 此前留着旧时间戳、谁也不告诉）。
func TestProjectSync_OutcomeRecorded(t *testing.T) {
	fpid := `fpid_aabbccddeeff00112233445566778899`
	projA, homeA := gitSyncMachine(t, fpid)
	remote := t.TempDir()
	runGit(t, remote, `init`, `--bare`)

	writeTaskInto(t, projA, homeA, `feat/sync-obs`, `观测同步`)
	runProjectSyncForTest(t, projA, homeA, `init`, remote)
	runProjectSyncForTest(t, projA, homeA, `push`)

	// 失败操作也落章：向不可达 remote init（探测失败在绑定前返回错误）。
	badErr := func() error {
		t.Setenv("FORGE_DATA_HOME", homeA)
		chdirAndRestore(t, projA)
		return projectSyncCmd.RunE(projectSyncCmd, []string{`init`, filepath.Join(t.TempDir(), `no-such-remote`)})
	}()
	if badErr == nil {
		t.Fatal("不可达 remote 的 init 必须失败")
	}

	t.Setenv("FORGE_DATA_HOME", homeA)
	entries, err := checklog.LoadAllAll(projA)
	if err != nil {
		t.Fatal(err)
	}
	var ops []checklog.Entry
	for _, e := range entries {
		if e.Check == checklog.CheckProjectSync {
			ops = append(ops, e)
		}
	}
	if len(ops) != 3 {
		t.Fatalf("project-sync 条目数 = %d, want 3（init+push+失败init）: %+v", len(ops), ops)
	}
	// 时间序：init → push → 失败 init。
	if ops[0].Meta[checklog.MetaKeySyncOp] != `init` || !ops[0].Passed || ops[0].Level != checklog.LevelPass {
		t.Errorf("init 落章异常: %+v", ops[0])
	}
	if ops[1].Meta[checklog.MetaKeySyncOp] != `push` || !ops[1].Passed || !strings.Contains(ops[1].Detail, `文件`) {
		t.Errorf("push 落章异常（成功应带文件数 note）: %+v", ops[1])
	}
	if ops[2].Meta[checklog.MetaKeySyncOp] != `init` || ops[2].Passed || ops[2].Level != checklog.LevelFail {
		t.Errorf("失败 init 落章异常（失败必须 LevelFail 可见）: %+v", ops[2])
	}
	if !strings.Contains(ops[2].Detail, `sync init 失败`) {
		t.Errorf("失败 Detail 应带操作名与原因: %q", ops[2].Detail)
	}
	// status 是只读操作——不得落章（每次轮询都落章会刷屏）。
	runProjectSyncForTest(t, projA, homeA, `status`)
	entries, err = checklog.LoadAllAll(projA)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.Check == checklog.CheckProjectSync {
			n++
		}
	}
	if n != 3 {
		t.Errorf("status 后 project-sync 条目数 = %d, want 3（status 不落章）", n)
	}
}
