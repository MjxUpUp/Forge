package clitask

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/pflag"
)

// runFinding 通过 cobra 全链路驱动 finding 命令（TestArtifactCmdCobraSurface
// 同款：SetArgs + Execute，防绕过解析面的测试盲区）。同进程多次 Execute 时
// cobra flag 值残留——先 VisitAll 重置回默认。
func runFinding(t *testing.T, args ...string) {
	t.Helper()
	taskFindingCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	// 经 Root 全链路（Root→finding）：子命令直调 Execute() 会冒泡到父级读空 args。
	// 同进程多次 Execute 时 cobra flag 值残留——先 VisitAll 重置回默认。
	full := append([]string{"finding"}, args...)
	Root.SetArgs(full)
	Root.SetOut(nil)
	Root.SetErr(nil)
	if err := Root.Execute(); err != nil {
		t.Fatalf("finding %v: %v", args, err)
	}
}

// setupLoopTask 建回环测试的活跃任务 + schema（max_rounds=2 收紧预算便于测试）。
func setupLoopTask(t *testing.T) (string, string) {
	t.Helper()
	return setupChainTask(t, "version: 1\nstages:\n  - name: proposal\nedges:\n  - from: review\n    to: implement\n    max_rounds: 2\n")
}

// TestFindingLoopEdgeLifecycle 钉住审查回环全生命周期（「回边语义」节 G2）：
// 登记 → resolve（指纹入记忆）→ 同指纹再登记 = 复活 → 立即耗尽 → complete
// pre-flight 拦截 → 人工 --reset-loop 清除（ResolvedPrints 保留）。
// 全部经 cobra 解析面驱动（TestArtifactCmdCobraSurface 同纪律）。
func TestFindingLoopEdgeLifecycle(t *testing.T) {
	dir, ref := setupLoopTask(t)

	// 1. 登记 finding。
	runFinding(t, "--content", "数据库连接泄漏")
	state, err := taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	var findingID string
	for _, f := range state.Findings {
		if f.Content == "数据库连接泄漏" {
			findingID = f.ID
		}
	}
	if findingID == "" {
		t.Fatalf("finding 未登记: %+v", state.Findings)
	}

	// 2. resolve：指纹记入 ResolvedPrints。
	runFinding(t, "--resolve", findingID)
	state, err = taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.ResolvedPrints) != 1 {
		t.Fatalf("resolve 应记一枚指纹: %+v", state.ResolvedPrints)
	}

	// 3. 复活：同指纹再登记 → 立即耗尽（轮次远未到预算 2 也拦——复活的严重度
	// 高于轮龄）。
	runFinding(t, "--content", "数据库连接泄漏")
	state, err = taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if state.LoopExhausted == nil || state.LoopExhausted.Reason != taskpipeline.LoopReasonRecurrence {
		t.Fatalf("复活应立即耗尽: %+v", state.LoopExhausted)
	}

	// 4. complete pre-flight 拦截。
	reasons := taskpipeline.CheckLoopExhausted(dir, state)
	if len(reasons) == 0 {
		t.Fatal("耗尽态应拦 complete")
	}
	if !strings.Contains(reasons[0], taskpipeline.LoopReasonRecurrence) {
		t.Fatalf("拦截原因应是复活: %v", reasons)
	}

	// 5. 人工重置：耗尽清除、ResolvedPrints 保留（复活记忆不丢）。
	runFinding(t, "--reset-loop", "--note", "人工裁决：已知问题降级")
	state, err = taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if state.LoopExhausted != nil {
		t.Fatal("重置应清耗尽态")
	}
	if len(state.ResolvedPrints) != 1 {
		t.Fatalf("重置应保留 ResolvedPrints: %+v", state.ResolvedPrints)
	}
}

// TestFindingLoopBudgetByReviewRounds 钉住轮次预算语义：open finding 跨过
// max_rounds 次复核 → MarkLoopExhaustedIfDue 耗尽（review pass 后调用）；
// 轮龄未到不耗尽。
func TestFindingLoopBudgetByReviewRounds(t *testing.T) {
	dir, ref := setupLoopTask(t)
	runFinding(t, "--content", "老问题")

	state, err := taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Findings) != 1 {
		t.Fatalf("前置 finding 缺失: %+v", state.Findings)
	}
	// 复核两轮后（ReviewRounds=2，finding.Round=1 → 轮龄 2 ≥ 预算 2）→ 耗尽。
	state.ReviewRounds = make([]taskpipeline.ReviewRound, 2)
	if marker := taskpipeline.MarkLoopExhaustedIfDue(dir, state); marker == nil {
		t.Fatal("轮龄达预算应耗尽")
	}
	// 轮龄未到（仅 1 轮）不耗尽。
	state.ReviewRounds = make([]taskpipeline.ReviewRound, 1)
	if marker := taskpipeline.MarkLoopExhaustedIfDue(dir, state); marker != nil {
		t.Fatalf("轮龄未到不应耗尽: %+v", marker)
	}
}
