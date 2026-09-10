package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
		got := NextDecision("feat/x", false, c.st)
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

	// 无活跃任务：脏树 → 建任务收编；干净 → status。
	if got := NextDecision("main", true, nil); !strings.Contains(got.Next, "forge task start") {
		t.Errorf("dirty tree without task: Next = %q, want task start", got.Next)
	}
	if got := NextDecision("main", false, nil); got.Next != "forge status" {
		t.Errorf("clean tree without task: Next = %q, want forge status", got.Next)
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
