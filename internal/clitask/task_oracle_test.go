package clitask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/artifactchain"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/pflag"
)

// task_oracle_test.go — oracle-pipeline 阶段一的 CLI 面行为钉：accept 补登
// （manual 层 + 去重 + 完成后拒绝）、conventions 兜底接线（登记+实跑全链）、
// task report 组装与渲染、artifact --init-schema 一键接线。

// resetArtifactFlags 经 cobra 全链路多次 Execute 时重置 flag 残留（与
// TestArtifactCmdCobraSurface 同款——同进程多次 Execute 会泄上一次值）。
func resetArtifactFlags() {
	taskArtifactCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
}

// TestTaskAcceptCmd_RegistersManualTier: `forge task accept` registers criteria
// stamped manual-tier; re-registering the same command dedups to zero with the
// original tier preserved.
//
// TestTaskAcceptCmd_RegistersManualTier：forge task accept 登记的标准盖 manual
// 层；重复登记同命令去重为零新增且原层级保留。
func TestTaskAcceptCmd_RegistersManualTier(t *testing.T) {
	dir, taskRef := setupChainTask(t, "")

	run := func(args ...string) error {
		resetTaskCmdFlags(taskAcceptCmd)
		full := append([]string{"accept"}, args...)
		Root.SetArgs(full)
		Root.SetOut(nil)
		Root.SetErr(nil)
		return Root.Execute()
	}
	out := captureStdout(t, func() {
		if err := run(`echo manual-ok :: manual-ok`); err != nil {
			t.Fatalf(`accept 应成功: %v`, err)
		}
	})
	if !strings.Contains(out, "manual") {
		t.Errorf(`输出应明示 manual 层（如实披露）: %q`, out)
	}
	st, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if len(st.Acceptance) != 1 {
		t.Fatalf(`应登记 1 条，got %d`, len(st.Acceptance))
	}
	if st.Acceptance[0].Source != taskpipeline.AcceptanceSourceManual {
		t.Errorf(`补登应盖 manual 层，got %q`, st.Acceptance[0].Source)
	}
	if st.Acceptance[0].Run != "echo manual-ok" || st.Acceptance[0].Expected != "manual-ok" {
		t.Errorf(`解析应保留 run/expected: %+v`, st.Acceptance[0])
	}

	// 重复登记同命令 → 去重（0 新增），层级不变。
	out2 := captureStdout(t, func() {
		if err := run(`echo manual-ok :: manual-ok`); err != nil {
			t.Fatalf(`重复 accept 不应报错（去重）: %v`, err)
		}
	})
	if !strings.Contains(out2, "0 条新增") {
		t.Errorf(`重复登记应提示 0 新增: %q`, out2)
	}
	st2, _ := taskpipeline.LoadTaskState(dir, taskRef)
	if len(st2.Acceptance) != 1 || st2.Acceptance[0].Source != taskpipeline.AcceptanceSourceManual {
		t.Errorf(`去重后应仍 1 条且 manual 层: %+v`, st2.Acceptance)
	}
}

// TestTaskAcceptCmd_RejectsCompleted: a completed task's exam is final — late
// registration is refused (the exam must be settled before delivery).
//
// TestTaskAcceptCmd_RejectsCompleted：已完成任务的考卷定稿——拒绝事后补登。
func TestTaskAcceptCmd_RejectsCompleted(t *testing.T) {
	_, taskRef := setupChainTask(t, "")
	st, err := taskpipeline.LoadTaskState(filepath.Join(mustWd(t)), taskRef)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	now := time.Now()
	st.CompletedAt = &now
	if err := taskpipeline.SaveTaskState(mustWd(t), st); err != nil {
		t.Fatalf(`SaveTaskState: %v`, err)
	}
	resetTaskCmdFlags(taskAcceptCmd)
	// 已完成任务对活跃任务检测不可见（ActiveTaskState 返回 nil）——补登拒绝分支
	// 经 --ref 显式加载触达。
	Root.SetArgs([]string{"accept", `echo late :: late`, "--ref", taskRef})
	Root.SetOut(nil)
	Root.SetErr(nil)
	err = Root.Execute()
	if err == nil || !strings.Contains(err.Error(), "已完成") {
		t.Fatalf(`已完成任务补登应被拒: %v`, err)
	}
}

// TestTaskComplete_BlockedWhenZeroAcceptance (review P2-6): CLI-level pin of
// the registration gate's BLOCKED copy — the exit guidance (accept/extract/
// conventions) and the escape hatch must both be named, and the refusal keeps
// the task active (verify-acceptance can still register+run).
//
// TestTaskComplete_BlockedWhenZeroAcceptance（审查 P2-6）：CLI 级钉登记门的
// BLOCKED 文案——出口指引（accept/extract/conventions）与逃生舱都要点名，且
// 拒绝保持任务 active（verify-acceptance 仍可登记+实跑）。
func TestTaskComplete_BlockedWhenZeroAcceptance(t *testing.T) {
	dir, taskRef := setupChainTask(t, "")
	st, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range taskpipeline.DefaultGates() {
		st.RecordGateResult(g.ID, true, "")
	}
	if err := taskpipeline.SaveTaskState(dir, st); err != nil {
		t.Fatal(err)
	}
	var runErr error
	_ = captureStdout(t, func() { runErr = runTaskCompleteAt(dir, st) })
	if runErr == nil {
		t.Fatal(`零验收非 generic 任务 complete 应被登记门拦截`)
	}
	for _, want := range []string{"acceptance registration", "forge task accept", "FORGE_ACCEPTANCE_GATE"} {
		if !strings.Contains(runErr.Error(), want) {
			t.Errorf("BLOCKED 文案缺 %q：\n%v", want, runErr)
		}
	}
	reloaded, _ := taskpipeline.LoadTaskState(dir, taskRef)
	if reloaded.CompletedAt != nil {
		t.Error(`登记门拒绝不得标记完成——任务保持 active 以便补登后重试`)
	}
}

// resetTaskCmdFlags 重置单个子命令的 flag 残留（cobra 同进程多次 Execute）。
func resetTaskCmdFlags(cmd interface {
	Flags() *pflag.FlagSet
}) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
}

// mustWd 返回当前工作目录（setupChainTask 已 t.Chdir 到临时项目）。
func mustWd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

// writeConventionsProfile 写最小可用 conventions 档案（version=1）到 DataDir。
func writeConventionsProfile(t *testing.T, dir, lint, testCmd, build string) {
	t.Helper()
	profDir := filepath.Join(forgedata.DataDirFor(dir), "conventions")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"version":1,"stack":"go","lint":"` + lint + `","test":"` + testCmd + `","build":"` + build + `"}`
	if err := os.WriteFile(filepath.Join(profDir, "profile.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRegisterConventionsDefaults_Wiring: with a profile present, the wiring
// registers exactly the non-empty commands stamped conventions and syncs the
// in-memory copy (so the caller's verify run covers them).
//
// TestRegisterConventionsDefaults_Wiring：有档案时接线恰好登记非空命令（盖
// conventions 层）并同步调用方内存副本（后续实跑覆盖它们）。
func TestRegisterConventionsDefaults_Wiring(t *testing.T) {
	dir, taskRef := setupChainTask(t, "")
	// go version：可移植的快命令（RunTestCommand 是裸 exec 无 shell，echo 在
	// Windows 不可执行）。
	writeConventionsProfile(t, dir, "", "go version", "")

	st, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	added, zeroReason := registerConventionsDefaults(dir, st)
	if len(added) != 1 {
		t.Fatalf(`档案只有 test 命令 → 应登记 1 条，got %d（zeroReason=%q）: %v`, len(added), zeroReason, added)
	}
	if added[0].Source != taskpipeline.AcceptanceSourceConventions {
		t.Errorf(`应盖 conventions 层，got %q`, added[0].Source)
	}
	if len(st.Acceptance) != 1 {
		t.Errorf(`内存副本应同步（后续 VerifyAcceptance 实跑它）: %d 条`, len(st.Acceptance))
	}
	disk, _ := taskpipeline.LoadTaskState(dir, taskRef)
	if len(disk.Acceptance) != 1 || disk.Acceptance[0].Source != taskpipeline.AcceptanceSourceConventions {
		t.Errorf(`盘上状态应有 1 条 conventions 层标准: %+v`, disk.Acceptance)
	}
}

// TestVerifyAcceptance_FallbackRuns: zero-acceptance task + conventions profile
// → verify-acceptance auto-registers the fallback suite AND actually runs it
// (passed criteria, deterministic row recorded). End-to-end for the L1 bottom
// rung.
//
// TestVerifyAcceptance_FallbackRuns：零标准任务 + conventions 档案 →
// verify-acceptance 自动登记兜底套件并真实实跑（标准通过、deterministic 行落
// 盘）。L1 最底一级的端到端。
func TestVerifyAcceptance_FallbackRuns(t *testing.T) {
	dir, taskRef := setupChainTask(t, "")
	writeConventionsProfile(t, dir, "", "go version", "")

	var err error
	captureStdout(t, func() {
		err = runTaskVerifyAcceptanceAt(dir, "", false)
	})
	if err != nil {
		t.Fatalf(`verify-acceptance（兜底）应全过: %v`, err)
	}
	st, lerr := taskpipeline.LoadTaskState(dir, taskRef)
	if lerr != nil {
		t.Fatalf(`LoadTaskState: %v`, lerr)
	}
	if len(st.Acceptance) != 1 || !st.Acceptance[0].Passed {
		t.Fatalf(`兜底标准应已实跑且通过: %+v`, st.Acceptance)
	}
	if st.Acceptance[0].Source != taskpipeline.AcceptanceSourceConventions {
		t.Errorf(`兜底标准应盖 conventions 层，got %q`, st.Acceptance[0].Source)
	}
	entries, _ := checklog.LoadForTask(dir, taskRef)
	found := false
	for _, e := range entries {
		if e.Check == taskpipeline.CheckNameAcceptance && e.Passed {
			found = true
		}
	}
	if !found {
		t.Error(`实跑应落 acceptance deterministic 行`)
	}
}

// TestArtifactInitSchema_WritesAndRefusesOverwrite: init-schema writes the
// default chain with spec promoted to human; a second call without --force is
// refused (explicit ownership), --force overwrites.
//
// TestArtifactInitSchema_WritesAndRefusesOverwrite：init-schema 写默认链且
// spec 升 human 档；无 --force 的二次调用被拒（显式承担覆盖），--force 覆盖。
func TestArtifactInitSchema_WritesAndRefusesOverwrite(t *testing.T) {
	dir, _ := setupChainTask(t, "")
	out := captureStdout(t, func() {
		if err := runArtifactInitSchema(false); err != nil {
			t.Fatalf(`init-schema: %v`, err)
		}
	})
	if !strings.Contains(out, "human") {
		t.Errorf(`输出应说明 spec=human 档: %q`, out)
	}
	chain, warns := artifactchain.Load(dir)
	for _, w := range warns {
		t.Errorf(`Load 回读不应告警: %s`, w)
	}
	specStage, ok := chain.StageByName("spec")
	if !ok || specStage.Mode != artifactchain.ModeHuman {
		t.Fatalf(`spec 应为 human 档: %+v`, specStage)
	}
	if s, _ := chain.StageByName("design"); s.Mode != artifactchain.ModeAdvisory {
		t.Errorf(`design 应维持 advisory: %+v`, s)
	}
	// 无 --force 覆盖拒绝。
	if err := runArtifactInitSchema(false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf(`已存在 schema 应拒绝并指引 --force: %v`, err)
	}
	if err := runArtifactInitSchema(true); err != nil {
		t.Fatalf(`--force 应覆盖: %v`, err)
	}
}

// TestBuildAndRenderTaskReport: the report assembles every disclosure section
// from TaskState + checklog and renders it as the acceptor's single page.
//
// TestBuildAndRenderTaskReport：验收单从 TaskState + checklog 组装全部披露段
// 并渲染成验收方单页。
func TestBuildAndRenderTaskReport(t *testing.T) {
	dir, taskRef := setupChainTask(t, "")
	st, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	st.Acceptance = []taskpipeline.AcceptanceCriterion{
		{Run: "go build ./...", Source: taskpipeline.AcceptanceSourceStart, Passed: true, AcceptedHeadCommit: "abc"},
		{Run: "go test ./... :: ok", Source: taskpipeline.AcceptanceSourceManual, Passed: false, Output: "FAIL: boom\nsecond line"},
	}
	st.ReviewPassed = true
	st.ReviewRounds = []taskpipeline.ReviewRound{{ReviewedAt: time.Now()}, {ReviewedAt: time.Now()}}
	st.Checklist = append(st.Checklist, taskpipeline.ChecklistItem{ID: 1, Desc: "a", Done: true}, taskpipeline.ChecklistItem{ID: 2, Desc: "b"})
	st.Findings = append(st.Findings,
		taskpipeline.Finding{Content: "退款金额计算越界", Severity: "critical", Status: "open"},
		taskpipeline.Finding{Content: "已修的问题", Severity: "minor", Status: "fixed"},
	)
	st.SpecArtifacts = map[string]taskpipeline.ArtifactRef{"spec": {Path: "specs/x/spec.md", Hash: "aa"}}
	st.ArtifactApprovals = map[string]taskpipeline.ArtifactApproval{"spec": {By: "alice", At: time.Now(), Hash: "aa"}}
	st.Score = &scoringtypes.ScoreResult{Overall: 88, Grade: "B", Evidence: &scoringtypes.EvidenceSummary{
		UntestedAreas:  []string{"internal/pay/refund.go"},
		RemainingRisks: []string{"critical: 退款金额计算越界"},
	}}
	if err := taskpipeline.SaveTaskState(dir, st); err != nil {
		t.Fatalf(`SaveTaskState: %v`, err)
	}
	// checklog：一条 acceptance deterministic 行 + 一条 acceptance-gate 逃生行。
	if err := checklog.Record(dir, &checklog.Entry{
		Check: taskpipeline.CheckNameAcceptance, Passed: false, Checked: true, TaskRef: taskRef,
		Source: checklog.EvidenceDeterministic,
	}); err != nil {
		t.Fatal(err)
	}
	esc := checklog.EscapeHatchEntry("acceptance-gate", checklog.EscapeReasonOverride, taskRef, "escape-hatch: test")
	esc.TaskRef = taskRef // EscapeHatchEntry 不自动填 TaskRef（生产调用方同样显式补设）
	if err := checklog.Record(dir, esc); err != nil {
		t.Fatal(err)
	}
	if err := taskpipeline.SaveHeldout(dir, taskRef, []taskpipeline.AcceptanceCriterion{{Run: "go version"}}); err != nil {
		t.Fatal(err)
	}

	reloaded, _ := taskpipeline.LoadTaskState(dir, taskRef)
	rep, err := buildTaskReport(dir, reloaded)
	if err != nil {
		t.Fatalf(`buildTaskReport: %v`, err)
	}
	if !rep.HasManualTier || rep.TierCounts[taskpipeline.AcceptanceSourceManual] != 1 {
		t.Errorf(`manual 层探测/计数错误: %+v`, rep.TierCounts)
	}
	if !rep.HeldoutRegistered {
		t.Error(`held-out 侧车应被探测到`)
	}
	if rep.SpecApprovalBy != "alice" {
		t.Errorf(`spec 审批人应读出: %q`, rep.SpecApprovalBy)
	}
	if len(rep.RemainingRisks) != 1 || !strings.Contains(rep.RemainingRisks[0], "critical") {
		t.Errorf(`残留风险应只含 open finding: %v`, rep.RemainingRisks)
	}
	if rep.Escapes["acceptance-gate"] != 1 {
		t.Errorf(`逃生舱库存应记 acceptance-gate×1: %v`, rep.Escapes)
	}
	if rep.EvidenceStrength == "" {
		t.Error(`证据强度应非空（checklog 已有行）`)
	}

	text := renderTaskReport(rep)
	for _, want := range []string{
		"交付验收单: feat/chain-cli",
		"考卷（验收标准 1/2 通过）",
		"(start) go build ./...",
		"(manual) go test ./... :: ok",
		"FAIL: boom",
		"含事后补登标准（manual 层）",
		"held-out 保留集 已登记",
		"spec 已由 alice 审批",
		"未验证面：1 个改动源文件",
		"internal/pay/refund.go",
		"残留风险：1 条 open",
		"critical: 退款金额计算越界",
		"逃生舱：1 次（acceptance-gate×1）",
		"审查：已过（2 轮）",
		"对账单：1/2 勾",
		"forge task verify-acceptance --ref feat/chain-cli",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("验收单应含 %s\n—— 全文:\n%s", want, text)
		}
	}
}

// TestRenderTaskReport_EmptyBranches: pure render branches — no score → 未评分
// + 未验证面未知；零残留 → 无未决 finding；无 spec → 无 spec 产物；零逃生 → 无。
//
// TestRenderTaskReport_EmptyBranches：渲染纯函数的空分支——无评分 → 未评分 +
// 未验证面未知；零残留 → 无未决 finding；无 spec → 无 spec 产物；零逃生 → 无。
func TestRenderTaskReport_EmptyBranches(t *testing.T) {
	rep := &taskReportJSON{TaskRef: "feat/empty", TierCounts: map[string]int{}}
	text := renderTaskReport(rep)
	for _, want := range []string{
		"未评分",
		"未验证面：未知（任务未评分",
		"残留风险：无未决 finding",
		"held-out 保留集 未登记 · 无 spec 产物",
		"逃生舱：无",
		"审查：未审 · 对账单：0/0 勾",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("空报告应含 %s\n—— 全文:\n%s", want, text)
		}
	}
}
