package taskpipeline

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// selfcheck_test.go —— discipline-first-gates P2/P4 的守卫：selfcheck 镜像纯计算、
// outcome 接入 selfcheck 条目、next-hint 时机训练、重复 advisory 折叠、BLOCKED
// 还债指引。

// TestSelfcheckPairingUntracked pins the exported pairing probe: untracked
// source files (no paired tests) are reported; a paired test file removes its
// source; whitelisted files never count. Same computation the verify gate runs
// (taskChangedFiles + coveragePairing) — no escape early-out (selfcheck wants
// facts even under an active escape).
//
// TestSelfcheckPairingUntracked 钉住导出的配对探针：untracked 源文件（无配对测试）
// 被报出；配对测试移除其源文件；白名单文件永不计数。与 verify 门禁同一计算
// （taskChangedFiles + coveragePairing）——无逃生早退（逃生下自检也要事实）。
func TestSelfcheckPairingUntracked(t *testing.T) {
	dir := initRepoWithMainGo(t)
	state := &TaskState{TaskRef: "feat/sc-pair", Branch: "feat/sc-pair"}

	for _, f := range []string{"x.go", "y.go"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("package p\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	missing, total := SelfcheckPairing(dir, state)
	if len(missing) != 2 || total != 2 {
		t.Fatalf("untracked x.go/y.go → missing=%v total=%d, want 2/2", missing, total)
	}

	// Go package 级兜底（hasMatchingTest 真实语义）：同目录任一 _test.go 覆盖整
	// 目录源码——镜像计算必须与门禁逐字一致，不得比门禁更严。
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte("package p\n"), 0644); err != nil {
		t.Fatal(err)
	}
	missing, total = SelfcheckPairing(dir, state)
	if len(missing) != 0 || total != 2 {
		t.Fatalf("after x_test.go → missing=%v total=%d, want []/2 (package-level fallback)", missing, total)
	}
}

// TestSelfcheckScopeDrift pins the exported scope probe: changes outside the
// declared PlanScope are reported; nil PlanScope (undeclared) is a no-op probe.
//
// TestSelfcheckScopeDrift 钉住导出的 scope 探针：超出 PlanScope 声明的改动被报出；
// PlanScope 未声明（空）时探针空转。
func TestSelfcheckScopeDrift(t *testing.T) {
	dir := initRepoWithMainGo(t)
	state := &TaskState{TaskRef: "feat/sc-scope", Branch: "feat/sc-scope"}

	if drift := SelfcheckScope(dir, state); drift != nil {
		t.Fatalf("empty PlanScope → nil drift, got %v", drift)
	}

	state.PlanScope = []string{"internal/keep/"}
	for _, f := range []string{"internal/keep/a.go", "internal/keep/a_test.go", "other/b.go"} {
		full := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("package p\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	drift := SelfcheckScope(dir, state)
	if len(drift) != 1 || drift[0] != "other/b.go" {
		t.Fatalf("drift = %v, want [other/b.go]", drift)
	}
}

// TestOutcomeSelfcheckMarksConfirmation pins the P2 outcome wiring: a
// selfcheck-pairing entry in the task (ANY result — the agent ran the mirror
// probe) upgrades a failing test-coverage gate to confirmation: the agent held
// the fact and did not act. Undone self-checks are discipline debt realized,
// not first-line absence.
//
// TestOutcomeSelfcheckMarksConfirmation 钉住 P2 的 outcome 接线：task 内存在
// selfcheck-pairing 条目（任意结果——agent 跑过镜像探针）即把失败的 test-coverage
// 门禁升为 confirmation：agent 已持有事实而未行动。做过自检未修是纪律债兑现，
// 不是第一防线缺位。
func TestOutcomeSelfcheckMarksConfirmation(t *testing.T) {
	root := t.TempDir()
	st := &TaskState{TaskRef: "feat/sc-conf"}
	// 场景 1：无任何先导信号 → discovery。
	if err := checkVerifyTestCoverage(root, st, []string{"internal/x/a.go", "internal/y/b.go"}); err != nil {
		t.Fatal(err)
	}
	if e := lastCoverageEntry(t, root, st.TaskRef); e.Outcome != checklog.OutcomeDiscovery {
		t.Fatalf("no prior signal → discovery, got %q", e.Outcome)
	}

	// 场景 2：selfcheck 条目在场且其 missing_list 与本轮 missing 有交集
	// （事实级——任意结果不够，守护监督清单 P2-1 的「刷量」通道由交集封死）。
	root2 := t.TempDir()
	st2 := &TaskState{TaskRef: "feat/sc-conf2"}
	if err := checklog.Record(root2, &checklog.Entry{
		Check: checklog.CheckSelfcheckPairing, TaskRef: st2.TaskRef,
		Passed: false, Checked: true,
		Meta: map[string]string{"missing_files": "2", "missing_list": "internal/x/a.go,internal/y/b.go"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := checkVerifyTestCoverage(root2, st2, []string{"internal/x/a.go", "internal/y/b.go"}); err != nil {
		t.Fatal(err)
	}
	if e := lastCoverageEntry(t, root2, st2.TaskRef); e.Outcome != checklog.OutcomeConfirmation {
		t.Fatalf("selfcheck entry with overlapping missing_list → confirmation, got %q", e.Outcome)
	}

	// 场景 3：selfcheck 在场但清单无交集（干净自检后漂移）→ discovery——
	// agent 从未被告知这批新文件，confirmation 会虚高 D3。
	root3 := t.TempDir()
	st3 := &TaskState{TaskRef: "feat/sc-conf3"}
	if err := checklog.Record(root3, &checklog.Entry{
		Check: checklog.CheckSelfcheckPairing, TaskRef: st3.TaskRef,
		Passed: true, Checked: true,
		Meta: map[string]string{"missing_files": "0", "missing_list": ""},
	}); err != nil {
		t.Fatal(err)
	}
	if err := checkVerifyTestCoverage(root3, st3, []string{"internal/x/a.go", "internal/y/b.go"}); err != nil {
		t.Fatal(err)
	}
	if e := lastCoverageEntry(t, root3, st3.TaskRef); e.Outcome != checklog.OutcomeDiscovery {
		t.Fatalf("clean selfcheck (no overlap) → discovery, got %q", e.Outcome)
	}
}

// TestNextHintSuggestsSelfcheck pins the P2 timing-training hint: when
// acceptance is pending and the task has no selfcheck-pairing entry yet, the
// next-hint reason appends the mirror-probe suggestion; once the agent has run
// it (or nothing is pending), the hint disappears — train the habit exactly
// until it exists.
//
// TestNextHintSuggestsSelfcheck 钉住 P2 时机训练提示：验收待跑且 task 尚无
// selfcheck-pairing 条目时，next-hint 的 Reason 追加镜像自检建议；agent 跑过
// （或无待跑验收）后提示消失——习惯训练恰好训练到它存在为止。
func TestNextHintSuggestsSelfcheck(t *testing.T) {
	root := t.TempDir()
	st := &TaskState{
		TaskRef:    "feat/sc-next",
		History:    []tasktypes.TaskGateResult{{Gate: GateImplement, Passed: true}},
		Acceptance: []AcceptanceCriterion{{Run: "go test ./..."}},
	}

	res := NextHint(root, st)
	if res.Next != "forge task verify-acceptance" {
		t.Fatalf("Next = %q, want verify-acceptance", res.Next)
	}
	if !strings.Contains(res.Reason, "selfcheck pairing") {
		t.Errorf("no selfcheck entry → reason must suggest `forge selfcheck pairing`, got: %q", res.Reason)
	}

	// selfcheck 跑过后提示消失。
	if err := checklog.Record(root, &checklog.Entry{
		Check: checklog.CheckSelfcheckPairing, TaskRef: st.TaskRef, Passed: true, Checked: true,
	}); err != nil {
		t.Fatal(err)
	}
	res = NextHint(root, st)
	if strings.Contains(res.Reason, "selfcheck pairing") {
		t.Errorf("selfcheck already run → hint must disappear, got: %q", res.Reason)
	}
}

// TestFoldRepeatAdvisory pins the P4 fold: the SECOND identical advisory for
// the same check within one task collapses to one "unchanged" stderr line —
// the checklog row is still recorded verbatim (audit trail never thins; only
// the interruption budget drops).
//
// TestFoldRepeatAdvisory 钉住 P4 折叠：同 task 内同 check 的第二条**完全相同**的
// advisory 在 stderr 折叠为一行 unchanged——checklog 条目照原文落盘（审计轨迹
// 不变薄，降的只是打断预算）。
func TestFoldRepeatAdvisory(t *testing.T) {
	root := t.TempDir()
	st := &TaskState{TaskRef: "feat/fold"}
	missing := []string{"internal/x/a.go", "internal/y/b.go"}

	// 第一次 verify：完整 advisory。
	out1 := captureVerifyStderr(t, root, st, missing)
	if !strings.Contains(out1, "internal/x/a.go") {
		t.Fatalf("first advisory must list the missing files, got: %q", out1)
	}
	// 第二次 verify（同 missing）：折叠为 unchanged 一行，不再罗列文件。
	out2 := captureVerifyStderr(t, root, st, missing)
	if !strings.Contains(out2, "unchanged since last verify") {
		t.Fatalf("second identical advisory must fold to unchanged, got: %q", out2)
	}
	if strings.Contains(out2, "internal/x/a.go") {
		t.Errorf("folded advisory must not re-list files, got: %q", out2)
	}
	// 审计轨迹不变薄：两条完整 checklog 条目都在。
	entries, err := checklog.LoadForTask(root, st.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.Check == CheckNameTestCoverage {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("checklog must keep both full rows, got %d", n)
	}
	// missing 变化（a.go 配上了）→ 不折叠，恢复完整输出。
	out3 := captureVerifyStderr(t, root, st, []string{"internal/y/b.go"})
	if !strings.Contains(out3, "internal/y/b.go") || strings.Contains(out3, "unchanged since last verify") {
		t.Fatalf("changed advisory must print in full, got: %q", out3)
	}
}

// captureVerifyStderr 跑一次 checkVerifyTestCoverage 并捕获 stderr。
func captureVerifyStderr(t *testing.T, root string, st *TaskState, missing []string) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	_ = checkVerifyTestCoverage(root, st, missing)
	_ = w.Close()
	os.Stderr = old
	return <-done
}

// TestGateBlockedDebtHint pins the P4 debt guidance: every gate BLOCKED message
// carries the skill-evolution repayment hint — an interception must feed the
// discipline layer, not just fix the instance.
//
// TestGateBlockedDebtHint 钉住 P4 还债指引：每条门禁 BLOCKED 消息携带
// skill-evolution 还债提示——拦截必须喂给纪律层，而不是只修实例。
func TestGateBlockedDebtHint(t *testing.T) {
	err := GateBlocked("task-verify 拒绝：缺测试 %d 个", 8)
	if err == nil {
		t.Fatal("GateBlocked must return an error")
	}
	if !strings.Contains(err.Error(), "skills decide") {
		t.Errorf("BLOCKED message must carry the skill-evolution repayment hint, got: %q", err.Error())
	}
}

// TestArtifactChainExpectation pins the P3 start-time probe: the expected-but-
// unregistered artifact nodes for the task's chain (same truth source as the
// implement gate's CheckArtifactChainGate — artifactchain.Load), with the
// next-node hint; fully registered or escaped → empty (silence matches intent).
//
// TestArtifactChainExpectation 钉住 P3 开工前探针：任务产物链里预期而未登记的
// 节点（与 implement 门禁 CheckArtifactChainGate 同一真相源——artifactchain.Load）
// 连同下一节点指引；全登记或逃生下为空（静默与意图一致）。
func TestArtifactChainExpectation(t *testing.T) {
	root := t.TempDir() // 无 schema.yaml → DefaultChain（proposal/spec/design/plan）

	st := &TaskState{TaskRef: "feat/expect"}
	missing, next := ArtifactChainExpectation(root, st)
	if len(missing) != 4 || missing[0] != "proposal" {
		t.Fatalf("fresh task on default chain → 4 missing starting at proposal, got %v (next=%q)", missing, next)
	}
	if next == "" {
		t.Fatal("next-node hint must be non-empty when nodes are missing")
	}

	// 全登记 → 空。
	st.SpecArtifacts = map[string]tasktypes.ArtifactRef{"proposal": {}, "spec": {}, "design": {}, "plan": {}}
	if missing, _ := ArtifactChainExpectation(root, st); len(missing) != 0 {
		t.Fatalf("fully registered → empty, got %v", missing)
	}

	// 逃生 → 空（逃生意图即静默，与 implement gate 一致）。
	st.SpecArtifacts = nil
	st.Overrides.ArtifactChain = "disable"
	if missing, next := ArtifactChainExpectation(root, st); len(missing) != 0 || next != "" {
		t.Fatalf("escaped task → empty probe, got %v/%q", missing, next)
	}
}

// TestBackstopEntrySharesVerifyShape pins the guard-audit P1-2 fix: the
// task-complete backstop — the layer that actually BLOCKs — records its
// test-coverage entry through the same shared constructor as verify (Meta +
// outcome stamp), so D1-D3 see both gate phases in one shape.
//
// TestBackstopEntrySharesVerifyShape 钉住守护审计 P1-2 修复：task-complete 兜底
// ——真正 BLOCK 的层——经与 verify 同一共享构造器落 test-coverage 条目
// （Meta + outcome 章），D1-D3 由此以同一形状看见两个 gate 相位。
func TestBackstopEntrySharesVerifyShape(t *testing.T) {
	dir := initRepoWithMainGo(t)
	state := &TaskState{TaskRef: "feat/backstop", Branch: "feat/backstop"}
	for _, f := range []string{"x.go", "y.go"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("package p\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := checkTestCoverageBackstop(dir, state); err != nil {
		t.Fatalf("2 unpaired files (<3 threshold) must stay advisory, got: %v", err)
	}
	e := lastCoverageEntry(t, dir, state.TaskRef)
	if e.Meta["missing_files"] != "2" {
		t.Errorf("backstop entry missing_files = %q, want 2 (same Meta shape as verify)", e.Meta["missing_files"])
	}
	if e.Outcome != checklog.OutcomeDiscovery {
		t.Errorf("no first-line signal → discovery on the backstop layer too, got %q", e.Outcome)
	}
}

// TestFoldRepeatAdvisory_ResultSetChangeRefolds pins review P2-1: with >3
// missing files the Detail shows only the count + first 3 names — the fold
// decision must compare the FULL set (via Meta), so swapping the 4th file
// (same count, same head-3) must NOT fold: the new file's name must reach
// stderr (the nameless-signal failure mode P1-B exists to kill).
//
// TestFoldRepeatAdvisory_ResultSetChangeRefolds 钉住复审 P2-1：>3 缺测文件时
// Detail 只含计数+前 3 名——折叠判定必须比对**全量集合**（走 Meta），第 4 个文件
// 换血（计数同、前 3 同）不得折叠：新文件名必须到达 stderr（P1-B 要消灭的正是
// 无名信号）。
func TestFoldRepeatAdvisory_ResultSetChangeRefolds(t *testing.T) {
	root := t.TempDir()
	st := &TaskState{TaskRef: "feat/fold-swap"}
	first := []string{"a.go", "b.go", "c.go", "d.go"}

	out1 := captureVerifyStderr(t, root, st, first)
	if !strings.Contains(out1, "a.go") {
		t.Fatalf("first advisory must list files, got: %q", out1)
	}
	// 换血：d 修掉、e 新增——计数 4 不变、前 3（a,b,c）不变，Detail 完全相同。
	out2 := captureVerifyStderr(t, root, st, []string{"a.go", "b.go", "c.go", "e.go"})
	if strings.Contains(out2, "unchanged since last verify") {
		t.Fatalf("swapped 4th file must NOT fold (full-set compare), got: %q", out2)
	}
	if !strings.Contains(out2, "e.go") {
		t.Errorf("the new file's name must reach stderr on the first disclosure, got: %q", out2)
	}
	// 真正集合不变（顺序不同）→ 折叠。
	out3 := captureVerifyStderr(t, root, st, []string{"c.go", "e.go", "a.go", "b.go"})
	if !strings.Contains(out3, "unchanged since last verify") {
		t.Errorf("same full set (reordered) must fold, got: %q", out3)
	}
}
