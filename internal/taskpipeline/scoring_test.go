package taskpipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// setupScoreableTask 建临时 git 仓库 + 全门禁通过的任务状态，评分远高于逃生舱
// 上限（process 100 / testing 100 / code-quality 100 / assertions 70 / scope 70 /
// efficiency 100 ≈ 92），让封顶可观测。返回 (root, state)，state 尚未评分。
func setupScoreableTask(t *testing.T, ref string) (string, *TaskState) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	state := &TaskState{
		TaskRef:    ref,
		Branch:     "master",
		HeadCommit: GetHeadCommit(dir),
		StartedAt:  time.Now(),
	}
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")
	state.RecordGateResult("task-complete", true, "")
	return dir, state
}

// TestScoreTask_EscapeOverrideCapsAt89 pins the escape-hatch cost.
//
// TestScoreTask_EscapeOverrideCapsAt89 钉住逃生舱代价：设了任一 per-task override
// （forge task override）的任务，即便原始评分 ~92/A，总分也封顶 89，grade 按封顶值
// 重算（89 < A 档下限 90 → B）。封顶信号取 state.Overrides 而非 checklog 逃生舱
// 条目——设了 override 但没走 bypass 分支的任务 checklog 无条目，代价必须相同。
// CappedReason 记录原因。
func TestScoreTask_EscapeOverrideCapsAt89(t *testing.T) {
	dir, state := setupScoreableTask(t, "cap-override")
	state.Overrides.TestCoverage = "disable"

	stderr := captureStderr(t, func() {
		if err := ScoreTask(dir, state); err != nil {
			t.Fatalf("ScoreTask: %v", err)
		}
	})
	_ = stderr // cap advisory not expected in this scenario (raw 86.5 < 89)
	if state.Score == nil {
		t.Fatal("ScoreTask 应写入 state.Score")
	}
	// 2026-08-29 语义更新：override 免门禁不免度量诚实性——testing 维度改为中性
	// 70（见 BuildEvaluateInput），本场景原始分 86.5 天然低于 89 上限，封顶分支不
	// 触发。契约收敛为「逃生任务恒不可能拿到 A」：要么被 89 封顶压回，要么被中性
	// 维度拖下；两种途径都不得给出 A 档。CappedReason 只在封顶实际触发时记录。
	if state.Score.Overall > escapeCapMaxScore {
		t.Errorf("用过逃生舱的任务总分不得超过 %v，got %.1f", escapeCapMaxScore, state.Score.Overall)
	}
	if state.Score.Grade == "A" {
		t.Errorf("逃生任务不得拿 A 档，got %s（%.1f）", state.Score.Grade, state.Score.Overall)
	}
	// 诚实维度钉：testing 维度必须言明被禁用，而不是报「无需测试」。
	dimFound := false
	for _, d := range state.Score.Dimensions {
		if string(d.Dimension) == "testing" {
			dimFound = true
			if !strings.Contains(d.Detail, "override") {
				t.Errorf("testing 维度应明示 override 禁用，got %q", d.Detail)
			}
			if d.Score != 70 {
				t.Errorf("override 禁用的 testing 维度应为中性 70，got %d", d.Score)
			}
		}
	}
	if !dimFound {
		t.Error("评分应包含 testing 维度")
	}
}

// TestScoreTask_EscapeDocGateCapsAt89 keeps the STRICT cap branch pinned after the 2026-08-29 honest-dimension change.
//
// TestScoreTask_EscapeDocGateCapsAt89 在 2026-08-29 诚实维度改动后继续钉死严格
// 封顶分支：doc-gate 逃生不动 testing 维度，本场景原始分仍 ~92，89 钳制必须精确
// 触发（==89、CappedReason、stderr ADVISORY）——不能因 test-coverage 逃生的诚实
// 降分而丢掉封顶分支的覆盖。
func TestScoreTask_EscapeDocGateCapsAt89(t *testing.T) {
	dir, state := setupScoreableTask(t, "cap-docgate-override")
	state.Overrides.DocGate = "disable"

	stderr := captureStderr(t, func() {
		if err := ScoreTask(dir, state); err != nil {
			t.Fatalf("ScoreTask: %v", err)
		}
	})
	if state.Score == nil {
		t.Fatal("ScoreTask 应写入 state.Score")
	}
	if state.Score.Overall != escapeCapMaxScore {
		t.Errorf("doc-gate 逃生任务总分应封顶 %v，got %.1f", escapeCapMaxScore, state.Score.Overall)
	}
	if state.Score.Grade != "B" {
		t.Errorf("封顶 89 的 Grade 应为 B，got %s", state.Score.Grade)
	}
	if state.Score.CappedReason == "" {
		t.Error("CappedReason 应记录封顶原因")
	}
	if !strings.Contains(stderr, "89") {
		t.Errorf("stderr 应含封顶提示，got %q", stderr)
	}
}

// TestScoreTask_NoOverrideNotCapped is the control: the same fully-gated task without overrides scores above the cap and records no CappedReason — the cap must not leak onto honest tasks.
//
// TestScoreTask_NoOverrideNotCapped 是对照：同样的全门禁任务不设 override 时评分
// 高于上限且无 CappedReason——封顶不得泄漏到诚实任务上。
func TestScoreTask_NoOverrideNotCapped(t *testing.T) {
	dir, state := setupScoreableTask(t, "cap-none")

	if err := ScoreTask(dir, state); err != nil {
		t.Fatalf("ScoreTask: %v", err)
	}
	if state.Score == nil {
		t.Fatal("ScoreTask 应写入 state.Score")
	}
	if state.Score.Overall <= escapeCapMaxScore {
		t.Errorf("无 override 任务不应被封顶（测试场景设计为 ~92 分），got %.1f——若评分逻辑变化导致天然低于上限，本测试需同步调整场景", state.Score.Overall)
	}
	if state.Score.CappedReason != "" {
		t.Errorf("无 override 任务不应有 CappedReason，got %q", state.Score.CappedReason)
	}
}

// TestScoreTask_EnvEscapeRecordedCapsAt89 pins the second cap signal (code-review 2026-08).
//
// TestScoreTask_EnvEscapeRecordedCapsAt89 钉住第二个封顶信号（code-review 2026-08）：
// env 形式逃生（FORGE_TEST_COVERAGE=disable …）经 escapeDisabled 绕过、不动
// state.Overrides，但 bypass 分支会记录 CheckEscapeHatch 条目。只看 Overrides
// 的信号会让这类任务照拿 A。
func TestScoreTask_EnvEscapeRecordedCapsAt89(t *testing.T) {
	dir, state := setupScoreableTask(t, "cap-env-escape")
	if err := checklog.Record(dir, &checklog.Entry{
		Check:   checklog.CheckEscapeHatch,
		Passed:  true,
		Checked: true,
		Level:   checklog.LevelWarn,
		TaskRef: state.TaskRef,
		Detail:  "escape-hatch: test-coverage gate bypassed (FORGE_TEST_COVERAGE=disable)",
	}); err != nil {
		t.Fatalf("record escape-hatch entry: %v", err)
	}

	if err := ScoreTask(dir, state); err != nil {
		t.Fatalf("ScoreTask: %v", err)
	}
	if state.Score == nil {
		t.Fatal("ScoreTask 应写入 state.Score")
	}
	if state.Score.Overall != escapeCapMaxScore {
		t.Errorf("checklog 有逃生舱条目的任务总分应封顶 %v，got %.1f", escapeCapMaxScore, state.Score.Overall)
	}
	if state.Score.Grade != "B" {
		t.Errorf("封顶 89 的 Grade 应为 B，got %s", state.Score.Grade)
	}
	if state.Score.CappedReason == "" {
		t.Error("CappedReason 应记录封顶原因")
	}
}

// TestBuildEvaluateInput_SkipsInfraEntries pins the gate/score coherence fix (code-review 2026-08).
//
// TestBuildEvaluateInput_SkipsInfraEntries 钉住 gate/score 一致性修复
// （code-review 2026-08）：INFRA: 前缀条目是 fail-open 基建故障——gate 不因它
// 判失败，scoring 也不能把它读成编译/断言失败。不跳过的话，task-implement 后
// 零编辑的任务会带着 INFRA 条目的 CompilePassed=false 进入评分，因 WSL/bash
// 故障被扣代码质量分。
func TestBuildEvaluateInput_SkipsInfraEntries(t *testing.T) {
	dir, state := setupScoreableTask(t, "infra-skip")
	state.SessionID = "sess-infra"
	// gate 历史：task-implement 通过 → 默认 compilePassed=true；checklog 里最新
	// 条目是 INFRA 基建故障（Passed=false）——不得覆盖默认值。
	if err := checklog.Record(dir, &checklog.Entry{
		Check:     checklog.CheckAutoCompile,
		Passed:    false,
		Checked:   true,
		Level:     checklog.LevelWarn,
		TaskRef:   state.TaskRef,
		SessionID: state.SessionID,
		Detail:    "INFRA: auto-compile.sh: bash: /c/.../forge-gate-123.sh: No such file or directory",
	}); err != nil {
		t.Fatalf("record INFRA entry: %v", err)
	}

	input, _, err := BuildEvaluateInput(dir, state)
	if err != nil {
		t.Fatalf("BuildEvaluateInput: %v", err)
	}
	if !input.CompilePassed {
		t.Error("INFRA 基建故障条目不得把 CompilePassed 拉成 false（gate fail-open 与 score 扣分矛盾）")
	}
	if !input.CompileChecked {
		t.Error("CompileChecked 应保持 gate 历史的 true")
	}
}

// TestScoreTask_EvidenceBundle_UntestedAndRisks pins P4: the evidence bundle
// must state what was NOT verified (changed source file without paired test →
// UntestedAreas) and the residual risk left behind (open findings →
// RemainingRisks as "severity: content"). Both are disclosure-only — the score
// itself must be unchanged by their presence (fixture without them scores the
// same as the escape tests' ~86.5 baseline).
//
// TestScoreTask_EvidenceBundle_UntestedAndRisks 钉死 P4：证据束必须声明「什么没
// 被验证」（改源码无配对测试 → UntestedAreas）与「剩余风险」（open 发现 →
// RemainingRisks，"severity: content"）。两者仅披露——字段存在与否不得改变分
// 数（对照逃生舱测试的 ~86.5 基线）。封顶：open 发现超过 scoringtypes.RemainingRisksCap 时
// 以汇总行占位，不静默丢弃。
func TestScoreTask_EvidenceBundle_UntestedAndRisks(t *testing.T) {
	dir, state := setupScoreableTask(t, "evidence-p4")
	// 任务期改动一个源文件，无配对测试（工作树未提交形态，taskChangedFiles 计入）。
	if err := os.WriteFile(filepath.Join(dir, "handler.go"), []byte("package main\n\nfunc H() int { return 1 }\n"), 0644); err != nil {
		t.Fatalf("write handler.go: %v", err)
	}
	// 两条 open 发现 + 一条 fixed（fixed 不算残余）。
	state.AddFinding(Finding{Content: "并发窗口未加锁", Severity: "critical", Status: "open"})
	state.AddFinding(Finding{Content: "日志可能含敏感字段", Severity: "minor", Status: "fixed"})

	if err := ScoreTask(dir, state); err != nil {
		t.Fatalf("ScoreTask: %v", err)
	}
	ev := state.Score.Evidence
	if ev == nil {
		t.Fatal("评分应带 EvidenceSummary")
	}
	foundHandler := false
	for _, f := range ev.UntestedAreas {
		if filepath.ToSlash(f) == "handler.go" {
			foundHandler = true
		}
		if strings.HasSuffix(f, "_test.go") {
			t.Errorf("UntestedAreas 不应含测试文件: %q", f)
		}
	}
	if !foundHandler {
		t.Errorf("UntestedAreas 应含 handler.go，got %v", ev.UntestedAreas)
	}
	if len(ev.RemainingRisks) != 1 || !strings.Contains(ev.RemainingRisks[0], "critical") || !strings.Contains(ev.RemainingRisks[0], "并发窗口未加锁") {
		t.Errorf("RemainingRisks 应恰含 open 的 critical 发现（fixed 不入），got %v", ev.RemainingRisks)
	}
}

// TestScoreTask_EvidenceBundle_RisksCapped pins the evidence-size budget:
// open findings beyond scoringtypes.RemainingRisksCap collapse into a summary line instead of
// echoing unboundedly (evidence bundle bloat is the same debt as code bloat).
//
// TestScoreTask_EvidenceBundle_RisksCapped 钉死证据体量预算：open 发现超过
// scoringtypes.RemainingRisksCap 时折叠为汇总行——证据束无界膨胀与代码膨胀同属债务。
func TestScoreTask_EvidenceBundle_RisksCapped(t *testing.T) {
	dir, state := setupScoreableTask(t, "evidence-cap")
	for i := 0; i < scoringtypes.RemainingRisksCap+3; i++ {
		state.AddFinding(Finding{Content: fmt.Sprintf("风险 %d", i), Severity: "minor", Status: "open"})
	}
	if err := ScoreTask(dir, state); err != nil {
		t.Fatalf("ScoreTask: %v", err)
	}
	risks := state.Score.Evidence.RemainingRisks
	if len(risks) != scoringtypes.RemainingRisksCap+1 {
		t.Fatalf("应恰为 %d 条（cap + 1 汇总行），got %d", scoringtypes.RemainingRisksCap+1, len(risks))
	}
	last := risks[len(risks)-1]
	if !strings.Contains(last, "未逐条列出") || !strings.Contains(last, "3") {
		t.Errorf("末行应是超限汇总（3 条未列），got %q", last)
	}
}

// TestScoreTask_EvidenceBundle_UntestedUnderEscape pins the M3 honesty fix
// (2026-09-12 review): with the test-coverage escape active, the gate path's
// CoverageMissing is nil-by-design — the disclosure must recompute with the
// escape-ignoring pairing instead of laundering the gap as "fully tested"
// (this repo has a precedent of exactly this whitewash regression). Regression
// to `return covered` must turn this test red.
//
// TestScoreTask_EvidenceBundle_UntestedUnderEscape 钉死 M3 诚实性修复（2026-09-12
// 审查）：test-coverage 逃生激活时，门禁路径的 CoverageMissing 按设计为 nil——
// 披露必须用无视逃生的配对口径现算，而不是把缺口洗成「全部有测」（本仓库有
// 同型洗白回归前科）。回归成 `return covered` 时本测试必须变红。
func TestScoreTask_EvidenceBundle_UntestedUnderEscape(t *testing.T) {
	dir, state := setupScoreableTask(t, "evidence-escape-honest")
	state.Overrides.TestCoverage = "disable"
	if err := os.WriteFile(filepath.Join(dir, "uncovered.go"), []byte("package main\n\nfunc U() int { return 2 }\n"), 0644); err != nil {
		t.Fatalf("write uncovered.go: %v", err)
	}
	if err := ScoreTask(dir, state); err != nil {
		t.Fatalf("ScoreTask: %v", err)
	}
	ev := state.Score.Evidence
	if ev == nil {
		t.Fatal("评分应带 EvidenceSummary")
	}
	found := false
	for _, f := range ev.UntestedAreas {
		if filepath.ToSlash(f) == "uncovered.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("逃生激活时 UntestedAreas 仍须如实列出未测源文件（不得洗白为空），got %v", ev.UntestedAreas)
	}
}
