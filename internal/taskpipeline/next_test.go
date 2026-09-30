package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// TestNextDecision_GateChain pins the single-command derivation (design B / vNext P1-2): the
// gate order must match the real chain exactly — implement → verify-acceptance (pending
// criteria) → gate task-verify → review pass → gate task-complete → task complete — and the
// no-active-task branch keys on attribution first.
//
// TestNextDecision_GateChain 钉死单命令推导（设计 B）：门禁顺序与真实链严格一致——
// implement → verify-acceptance（未实跑）→ gate task-verify → review pass → gate
// task-complete → task complete；无活跃任务分支以归属问题优先。
func TestNextDecision_GateChain(t *testing.T) {
	withGates := func(reviewed bool, acceptance string, gates ...string) *TaskState {
		st := &TaskState{TaskRef: "feat/x"}
		for _, g := range gates {
			st.History = append(st.History, TaskGateResult{Gate: g, Passed: true})
		}
		st.ReviewPassed = reviewed
		st.Acceptance = []AcceptanceCriterion{{Run: "go test ./...", AcceptedHeadCommit: acceptance}}
		return st
	}
	cases := []struct {
		name string
		st   *TaskState
		want string
	}{
		{"no gates", withGates(false, ""), "forge task gate task-implement"},
		{"implement done, acceptance pending", withGates(false, "", GateImplement), "forge task verify-acceptance"},
		{"acceptance ran, verify pending", withGates(false, "abc123", GateImplement), "forge task gate task-verify"},
		{"verify done, review pending", withGates(false, "abc123", GateImplement, GateVerify), "forge review pass"},
		{"review done, complete gate pending", withGates(true, "abc123", GateImplement, GateVerify), "forge task gate task-complete"},
		{"all gates + review", withGates(true, "abc123", GateImplement, GateVerify, GateComplete), "forge task complete"},
	}
	for _, c := range cases {
		got := NextDecision("feat/x", false, 0, c.st)
		if got.Next != c.want {
			t.Errorf("%s: Next = %q, want %q", c.name, got.Next, c.want)
		}
		if got.Reason == "" || got.State == nil {
			t.Errorf("%s: Reason/State must be populated, got %+v", c.name, got)
		}
		if strings.Contains(got.Next, "&&") {
			t.Errorf("Next must be a single command, got %q", got.Next)
		}
	}

	// 已完结任务（status --ref 展示路径喂进 CompletedAt!=nil）：视同无活跃任务——
	// 不建议「再跑 complete」。
	done := withGates(true, "abc123", GateImplement, GateVerify, GateComplete)
	now := time.Now()
	done.CompletedAt = &now
	if got := NextDecision("feat/x", false, 0, done); got.Next == "forge task complete" {
		t.Fatal("completed task must not be told to run complete again")
	}

	// 无活跃任务：脏树 → 建任务收编；干净 → status。
	if got := NextDecision("main", true, 0, nil); !strings.Contains(got.Next, "forge task start") {
		t.Errorf("dirty tree without task: Next = %q, want task start", got.Next)
	}
	if got := NextDecision("main", false, 0, nil); got.Next != "forge status" {
		t.Errorf("clean tree without task: Next = %q, want forge status", got.Next)
	}
}

// TestNextDecision_BehindRemote pins the behind-upstream branch (acceptance followup
// 2026-09-30): a clean tree with no active task on a branch whose upstream is ahead must
// suggest a sync before new work — starting from a stale base is exactly what the
// 1.73.2 stale-snapshot rebuild made expensive. Dirty trees keep the attribution
// discipline (collect changes into a task first); an active task's gate chain keeps
// priority; behind=0 must not change the status fallback.
//
// TestNextDecision_BehindRemote 钉住落后远端分支（验收跟进批 2026-09-30）：干净树、无
// 活跃任务且 upstream 领先时，先建议同步再开工——在旧基线上叠改正是 1.73.2 旧快照重建
// 事故放大成本的形态。脏树维持归属纪律（先收编变更）；活跃任务门禁链保持优先；
// behind=0 不得改变 status 回落。
func TestNextDecision_BehindRemote(t *testing.T) {
	got := NextDecision("main", false, 3, nil)
	if got.Next != "git pull --ff-only" {
		t.Fatalf("clean tree behind remote: Next = %q, want git pull --ff-only", got.Next)
	}
	if !strings.Contains(got.Reason, "3") {
		t.Errorf("Reason must carry the behind count, got %q", got.Reason)
	}
	if got.State["behind_remote"] != 3 {
		t.Errorf("State[behind_remote] = %v, want 3", got.State["behind_remote"])
	}

	// 脏树优先收编（归属纪律不因落后而跳过）。
	if got := NextDecision("main", true, 3, nil); !strings.Contains(got.Next, "forge task start") {
		t.Errorf("dirty tree behind remote: Next = %q, want task start first", got.Next)
	}
	// 活跃任务门禁链优先于同步建议。
	st := &TaskState{TaskRef: "feat/x", History: []TaskGateResult{{Gate: GateImplement, Passed: true}}}
	if got := NextDecision("feat/x", false, 2, st); got.Next != "forge task verify-acceptance" {
		t.Errorf("active task behind remote: Next = %q, want gate chain", got.Next)
	}
	// 未落后维持原状。
	if got := NextDecision("main", false, 0, nil); got.Next != "forge status" {
		t.Errorf("not behind: Next = %q, want forge status", got.Next)
	}
}

// TestNextHint_RecordsChecklogRow pins the B1 measurement surface: NextHint derives the hint
// from live git state and records one next-hint advisory row carrying the suggested command.
//
// TestNextHint_RecordsChecklogRow 钉住 B1 度量面：NextHint 从实况 git 推导并落一条 next-hint
// advisory 行，携带建议命令（harness-audit B1 据此算采纳率）。
func TestNextHint_RecordsChecklogRow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	run("commit", "--allow-empty", "-m", "init")
	// 未归属变更（未跟踪文件）→ 脏树 → next = task start 收编。
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := NextHint(dir, nil)
	if res.Next != "forge task start --ref <ref> --branch --title <title>" {
		t.Fatalf("empty repo dirty-tree hint = %q, want task start", res.Next)
	}
	all, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	var rows []checklog.Entry
	for _, e := range all {
		if e.Check == checklog.CheckNextHint {
			rows = append(rows, e)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("expected one next-hint row, got %d", len(rows))
	}
	if rows[0].Meta[checklog.MetaKeySuggested] != res.Next {
		t.Fatalf("suggested = %q, want %q", rows[0].Meta[checklog.MetaKeySuggested], res.Next)
	}
}

// TestGitBehindRemote probes the rev-list upstream wiring against a real git repo: no
// upstream ref → 0 (fail-open), in-sync → 0, upstream ahead by 2 → 2 (commit-tree builds
// upstream-only commits without moving HEAD).
//
// TestGitBehindRemote 对真实 git 仓库钉住 upstream 探测：无 upstream ref → 0
// （fail-open）、同步 → 0、upstream 领先 2 → 2（commit-tree 构造仅存在于 upstream 的
// 提交，不动 HEAD）。
func TestGitBehindRemote(t *testing.T) {
	dir := t.TempDir()
	out := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	out("init")
	out("config", "user.email", "t@example.com")
	out("config", "user.name", "t")
	out("commit", "--allow-empty", "-m", "base")
	out("branch", "-M", "main") // git init 默认分支随版本漂移——显式钉到 main 再配 upstream。
	out("config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	out("config", "branch.main.remote", "origin")
	out("config", "branch.main.merge", "refs/heads/main")
	if got := GitBehindRemote(dir); got != 0 {
		t.Fatalf("no upstream ref: got %d, want 0 (fail-open)", got)
	}
	out("update-ref", "refs/remotes/origin/main", "HEAD")
	if got := GitBehindRemote(dir); got != 0 {
		t.Fatalf("in-sync upstream: got %d, want 0", got)
	}
	tree := out("rev-parse", "HEAD^{tree}")
	head := out("rev-parse", "HEAD")
	c1 := out("commit-tree", tree, "-m", "up1", "-p", head)
	c2 := out("commit-tree", tree, "-p", c1)
	out("update-ref", "refs/remotes/origin/main", c2)
	if got := GitBehindRemote(dir); got != 2 {
		t.Fatalf("upstream ahead by 2: got %d, want 2", got)
	}
}
