package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// convergedState 返回一条验收已通过、两道门禁已过的 state（done 的候选）。
func convergedState() *TaskState {
	st := &TaskState{TaskRef: "v", Branch: "feat/v"}
	st.Acceptance = []AcceptanceCriterion{{Run: "go test ./...", Passed: true, AcceptedHeadCommit: "abc"}}
	st.History = []tasktypes.TaskGateResult{{Gate: GateImplement, Passed: true, CompletedAt: time.Now()}}
	return st
}

func hasCode(v LoopVerdict, code string) bool {
	for _, r := range v.Reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}

// TestEvaluateLoopVerdict 用表驱动钉住每个 Code 的判词、Resolve、清除条件，以及
// escalate > done > continue 的优先级（loop 契约 B1-2 / REQ-2）。
func TestEvaluateLoopVerdict(t *testing.T) {
	base := func() LoopInputs {
		return LoopInputs{State: convergedState(), Cfg: DefaultLoopConfig(), AcceptanceFresh: true, Branch: "feat/v"}
	}
	withReset := func(st *TaskState, gate string, results ...bool) *TaskState {
		for _, p := range results {
			st.History = append(st.History, tasktypes.TaskGateResult{Gate: gate, Passed: p, CompletedAt: time.Now()})
		}
		return st
	}
	cases := []struct {
		name    string
		in      func() LoopInputs
		verdict string
		code    string // 期望出现的 Code（空 = 不应有任何原因）
	}{
		{"全收敛且新鲜 → done", base, LoopVerdictDone, ""},
		{"验收复发", func() LoopInputs {
			in := base()
			in.Recurrences = []AcceptanceRecurrence{{CriterionIdx: 0, Run: "x", Count: 2, Fingerprint: "f"}}
			return in
		}, LoopVerdictEscalate, LoopCodeAcceptanceRecurrence},
		{"回环耗尽", func() LoopInputs { in := base(); in.LoopExhausted = true; return in }, LoopVerdictEscalate, LoopCodeLoopExhausted},
		{"hazard 待确认", func() LoopInputs { in := base(); in.HazardPendingOwn = 1; return in }, LoopVerdictEscalate, LoopCodeHazardPending},
		{"input-required", func() LoopInputs {
			in := base()
			in.State.Assignment = &tasktypes.Assignment{Status: tasktypes.AssignInputRequired, LastQuestion: "用哪个库？"}
			return in
		}, LoopVerdictEscalate, LoopCodeInputRequired},
		{"失败次数预算", func() LoopInputs {
			in := base()
			in.Cfg.MaxFailedVerifies = 3
			in.FailedVerifies = 3
			return in
		}, LoopVerdictEscalate, LoopCodeBudgetFailedVerifies},
		{"token 预算", func() LoopInputs { in := base(); in.TokensUsed = in.Cfg.TokenBudget; return in }, LoopVerdictEscalate, LoopCodeBudgetTokens},
		{"改动文件预算", func() LoopInputs {
			in := base()
			in.Cfg.MaxChangedFiles = 3
			in.ChangedFiles = 4
			return in
		}, LoopVerdictEscalate, LoopCodeBudgetChangedFiles},
		{"无进展：连续 K 次通过条数未增", func() LoopInputs {
			in := base()
			in.Cfg.MaxVerifiesWithoutProgress = 3
			in.PassCounts = []int{2, 2, 2, 2}
			return in
		}, LoopVerdictEscalate, LoopCodeNoProgress},
		{"有进展：最近一次增加 → 不判无进展", func() LoopInputs {
			in := base()
			in.Cfg.MaxVerifiesWithoutProgress = 3
			in.PassCounts = []int{2, 2, 2, 3}
			return in
		}, LoopVerdictDone, ""},
		{"同一门禁连续两次 BLOCKED", func() LoopInputs {
			in := base()
			in.State = withReset(in.State, GateVerify, false, false)
			return in
		}, LoopVerdictEscalate, LoopCodeGateRepeatBlocked},
		{"门禁 BLOCKED 后通过 → 清除", func() LoopInputs {
			in := base()
			in.State = withReset(in.State, GateVerify, false, false, true)
			return in
		}, LoopVerdictDone, ""},
		{"重置之前的 BLOCKED 不计", func() LoopInputs {
			in := base()
			in.State = withReset(in.State, GateVerify, false, false)
			reset := time.Now().Add(time.Second)
			in.State.LoopResetAt = &reset
			return in
		}, LoopVerdictDone, ""},
		{"零条验收 → continue", func() LoopInputs { in := base(); in.State.Acceptance = nil; return in }, LoopVerdictContinue, ""},
		{"有一条 Passed=false → continue（非 git 下 fresh 也放行时仍不 done）", func() LoopInputs {
			in := base()
			in.State.Acceptance = append(in.State.Acceptance, AcceptanceCriterion{Run: "sh fail.sh"})
			return in
		}, LoopVerdictContinue, ""},
		{"通过后又改了源码（不新鲜）→ continue", func() LoopInputs { in := base(); in.AcceptanceFresh = false; return in }, LoopVerdictContinue, ""},
		{"escalate 优先于 done", func() LoopInputs { in := base(); in.HazardPendingOwn = 2; return in }, LoopVerdictEscalate, LoopCodeHazardPending},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := EvaluateLoopVerdict(c.in())
			if v.Verdict != c.verdict {
				t.Fatalf("verdict=%s want %s (%+v)", v.Verdict, c.verdict, v.Reasons)
			}
			if c.code == "" {
				if len(v.Reasons) != 0 {
					t.Fatalf("不应有原因: %+v", v.Reasons)
				}
				return
			}
			if !hasCode(v, c.code) {
				t.Fatalf("应含 %s: %+v", c.code, v.Reasons)
			}
			for _, r := range v.Reasons {
				if r.Resolve == "" || r.Detail == "" {
					t.Fatalf("每条原因须带 Detail 与 Resolve: %+v", r)
				}
			}
		})
	}
	// Next 与 NextDecision 同源：done 态下一步是 verify 门（implement 已过、验收已跑）。
	v := EvaluateLoopVerdict(base())
	want := NextDecision("feat/v", false, 0, convergedState()).Next
	if v.Next != want || !strings.Contains(v.Next, "task-verify") {
		t.Fatalf("Next=%q want %q", v.Next, want)
	}
	// 每个 Code 都有解除方式（封闭枚举守卫）。
	for _, code := range []string{LoopCodeAcceptanceRecurrence, LoopCodeLoopExhausted, LoopCodeHazardPending, LoopCodeInputRequired,
		LoopCodeBudgetFailedVerifies, LoopCodeBudgetTokens, LoopCodeBudgetChangedFiles, LoopCodeNoProgress, LoopCodeGateRepeatBlocked} {
		if loopResolve[code] == "" {
			t.Fatalf("Code %s 缺 Resolve", code)
		}
	}
}

// TestLoopBudgetVerdict 钉住 protocol.yml loop 预算段经 LoadLoopConfig 进判词
// （loop 契约 B1-3 / REQ-3）：配置 max_changed_files: 3、改动 4 个文件 → escalate
// 且含 budget-changed-files；不配置 → 无任何 budget-* Code；recurrence_threshold
// 负数关闭复发判定。
func TestLoopBudgetVerdict(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeProto := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, ".forge", "protocol.yml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 4 个改动文件（未提交 → TaskChangedFiles 计入）。
	for _, n := range []string{"a.go", "b.go", "c.go", "d.go"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st := &TaskState{TaskRef: "budget", Branch: "feat/testcov", HeadCommit: gitHead(t, dir)}
	if err := SaveTaskState(dir, st); err != nil {
		t.Fatal(err)
	}

	writeProto("version: 1\nloop:\n  max_changed_files: 3\n")
	cfg := LoadLoopConfig(dir)
	if cfg.MaxChangedFiles != 3 || cfg.RecurrenceThreshold != DefaultRecurrenceThreshold || cfg.TokenBudget <= 0 {
		t.Fatalf("配置解析错: %+v", cfg)
	}
	v := ComputeLoopVerdict(dir, st)
	if v.Verdict != LoopVerdictEscalate || !hasCode(v, LoopCodeBudgetChangedFiles) {
		t.Fatalf("改动 4 > 预算 3 应 escalate(budget-changed-files): %+v", v)
	}

	writeProto("version: 1\n")
	v = ComputeLoopVerdict(dir, st)
	for _, r := range v.Reasons {
		if strings.HasPrefix(r.Code, "budget-") {
			t.Fatalf("未配置预算不应出现 budget-* Code: %+v", v.Reasons)
		}
	}

	// 纯解析边界。
	r := ResolveLoopConfig(&protocol.LoopConfig{RecurrenceThreshold: -1, TokenBudget: 10})
	if r.RecurrenceThreshold != 0 || r.TokenBudget != 10 {
		t.Fatalf("负阈值应关闭复发、显式 token 预算生效: %+v", r)
	}
	if ResolveLoopConfig(nil) != DefaultLoopConfig() {
		t.Fatal("nil 应等于默认")
	}
}

// TestComputeLoopVerdictNoSideEffects 钉住判词只读：计算前后 checklog 与 state 文件
// 字节不变（status/resume 高频调用，判词不得写盘、不得落审计行）。
func TestComputeLoopVerdictNoSideEffects(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	st := &TaskState{TaskRef: "ro", Branch: "feat/testcov", HeadCommit: gitHead(t, dir)}
	st.Findings = []Finding{{ID: "f1", Content: "超龄", Status: "open", Round: 1}}
	st.ReviewRounds = make([]ReviewRound, 5)
	if err := SaveTaskState(dir, st); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var b strings.Builder
		_ = filepath.Walk(os.Getenv("FORGE_DATA_HOME"), func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				data, _ := os.ReadFile(p)
				b.WriteString(p + ":" + string(data) + "\n")
			}
			return nil
		})
		return b.String()
	}
	before := snapshot()
	v := ComputeLoopVerdict(dir, st)
	if !hasCode(v, LoopCodeLoopExhausted) {
		t.Fatalf("超龄 open finding 应判回环耗尽: %+v", v)
	}
	if after := snapshot(); after != before {
		t.Fatal("判词计算不得写盘（state / checklog 变了）")
	}
	if st.LoopExhausted != nil {
		t.Fatal("判词不得改写调用方 state")
	}
}

// TestFormatLoopVerdictLine 钉住单行形态。
func TestFormatLoopVerdictLine(t *testing.T) {
	v := LoopVerdict{Verdict: LoopVerdictEscalate, Reasons: []LoopReason{{Code: "a"}, {Code: "b"}}, Next: "forge task gate task-verify"}
	if got := FormatLoopVerdictLine(v); got != "loop: escalate（a, b） → next: forge task gate task-verify" {
		t.Fatalf("got %q", got)
	}
	if got := FormatLoopVerdictLine(LoopVerdict{Verdict: LoopVerdictContinue}); got != "loop: continue" {
		t.Fatalf("got %q", got)
	}
}
