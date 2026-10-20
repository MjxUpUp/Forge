package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/artifactchain"
	"github.com/MjxUpUp/Forge/internal/checklog"
)

// writeLoopSchema 写回边 schema 到测试项目（FORGE_DATA_HOME 隔离后 SchemaPath
// 落临时目录——与 artifactchain_gate_test 同款纪律）。
func writeLoopSchema(t *testing.T, dir, schema string) {
	t.Helper()
	p := artifactchain.SchemaPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLoopFingerprintNorm 钉住指纹规范化：首尾/连续空白折叠不算差异（CJK 文本
// 的排版空格是噪声），大小写折叠同理；实质不同文不同指纹。
func TestLoopFingerprintNorm(t *testing.T) {
	a := LoopFingerprint("  数据库连接泄漏  ")
	b := LoopFingerprint("数据库连接泄漏")
	c := LoopFingerprint("另一种问题")
	d := LoopFingerprint("DB connection leak")
	e := LoopFingerprint("db CONNECTION leak")
	if a != b {
		t.Fatalf("空白折叠后应同指纹: %q vs %q", a, b)
	}
	if a == c {
		t.Fatal("不同内容不应同指纹")
	}
	if d != e {
		t.Fatalf("大小写折叠后应同指纹: %q vs %q", d, e)
	}
	if len(a) != 16 {
		t.Fatalf("指纹应为 16 hex: %q", a)
	}
}

// TestLoopBudgetExhaustionByAge 钉住轮龄预算语义：open finding 轮龄 ≥ 预算 →
// 耗尽标记持久化；轮龄未到 → 不耗尽；resolved finding 不计龄。
func TestLoopBudgetExhaustionByAge(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeLoopSchema(t, dir, "version: 1\nstages:\n  - name: proposal\nedges:\n  - from: review\n    to: implement\n    max_rounds: 2\n")

	state := &TaskState{TaskRef: "loop-budget", Branch: "feat/loop"}
	state.ReviewRounds = []ReviewRound{{}, {}, {}} // 已过 3 轮
	state.Findings = []Finding{
		{ID: "f1", Content: "旧问题", Status: "open", Round: 1},    // 轮龄 3 ≥ 2 → 耗尽
		{ID: "f2", Content: "刚提的新问题", Status: "open", Round: 3}, // 轮龄 1
		{ID: "f3", Content: "已修", Status: "fixed", Round: 1},    // resolved 不计龄
	}
	marker := MarkLoopExhaustedIfDue(dir, state)
	if marker == nil || marker.Reason != LoopReasonRounds {
		t.Fatalf("轮龄超预算应耗尽: %+v", marker)
	}
	if !strings.Contains(marker.Detail, "f1") {
		t.Fatalf("耗尽应指向超龄 finding f1: %+v", marker)
	}
	// 持久化验证。
	reloaded, err := LoadTaskState(dir, "loop-budget")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.LoopExhausted == nil {
		t.Fatal("耗尽标记应持久化")
	}
}

// TestLoopBudgetNotExhaustedWhenYoung 钉住零误伤面：全部 open findings 轮龄未到
// 预算 → 不耗尽（新登记 finding 轮龄 1，永不立即因轮龄耗尽）。
func TestLoopBudgetNotExhaustedWhenYoung(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeLoopSchema(t, dir, "version: 1\nstages:\n  - name: proposal\nedges:\n  - from: review\n    to: implement\n    max_rounds: 3\n")

	state := &TaskState{TaskRef: "loop-young", Branch: "feat/loop"}
	state.ReviewRounds = []ReviewRound{{}, {}} // 2 轮
	state.Findings = []Finding{{ID: "f1", Content: "新", Status: "open", Round: 2}}
	if marker := MarkLoopExhaustedIfDue(dir, state); marker != nil {
		t.Fatalf("轮龄 1 < 3 不应耗尽: %+v", marker)
	}
}

// TestFindingRevivalImmediateExhaustion 钉住复发语义：指纹命中 ResolvedPrints 的
// finding 登记 → 立即耗尽（rounds 未到预算也拦——复活的严重度高于轮龄）。
func TestFindingRevivalImmediateExhaustion(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeLoopSchema(t, dir, "version: 1\nstages:\n  - name: proposal\nedges:\n  - from: review\n    to: implement\n    max_rounds: 9\n")

	state := &TaskState{TaskRef: "loop-revival", Branch: "feat/loop"}
	state.Findings = []Finding{{ID: "f1", Content: "数据库连接泄漏", Status: "open", Round: 1}}
	state.ResolvedPrints = []string{LoopFingerprint("数据库连接泄漏")}

	CheckFindingRevival(dir, state, "数据库连接泄漏")
	if state.LoopExhausted == nil || state.LoopExhausted.Reason != LoopReasonRecurrence {
		t.Fatalf("复活应立即耗尽: %+v", state.LoopExhausted)
	}
}

// TestFindingRevivalSyncsCallerState 钉住盘上已有 state 时的内存同步（B0）：
// MergeOrPersistTaskState 改的是锁内重载副本，调用方 state 也必须带上耗尽标记，
// 且盘上同样持久化。
func TestFindingRevivalSyncsCallerState(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)

	state := &TaskState{TaskRef: "loop-revival-disk", Branch: "feat/loop"}
	state.ResolvedPrints = []string{LoopFingerprint("数据库连接泄漏")}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	if !CheckFindingRevival(dir, state, "数据库连接泄漏") {
		t.Fatal("指纹命中应检出复活")
	}
	if state.LoopExhausted == nil || state.LoopExhausted.Reason != LoopReasonRecurrence {
		t.Fatalf("调用方内存 state 应同步耗尽标记: %+v", state.LoopExhausted)
	}
	reloaded, err := LoadTaskState(dir, "loop-revival-disk")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.LoopExhausted == nil || reloaded.LoopExhausted.Reason != LoopReasonRecurrence {
		t.Fatalf("耗尽标记应持久化: %+v", reloaded.LoopExhausted)
	}
}

// TestCheckLoopExhaustedEscape 钉住逃生舱：FORGE_ARTIFACT_CHAIN=disable 下耗尽
// 不拦且落 escape-hatch 审计行（逃生有痕）。
func TestCheckLoopExhaustedEscape(t *testing.T) {
	t.Setenv("FORGE_ARTIFACT_CHAIN", "disable")
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	state := &TaskState{TaskRef: "loop-escape", Branch: "feat/loop"}
	state.LoopExhausted = &LoopExhaustion{Reason: LoopReasonRounds, Detail: "d"}
	if reasons := CheckLoopExhausted(dir, state); len(reasons) != 0 {
		t.Fatalf("逃生舱下不应拦: %v", reasons)
	}
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Check == checklog.CheckEscapeHatch && strings.Contains(e.Detail, "loop") {
			found = true
		}
	}
	if !found {
		t.Fatal("逃生必须落 escape-hatch 审计行")
	}
}

// TestResetLoopExhaustion 钉住人工重置：耗尽态清除、记下新预算起算轮、审计行
// 落地、ResolvedPrints 保留（复活检测记忆不随重置丢失）。
func TestResetLoopExhaustion(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeLoopSchema(t, dir, "version: 1\nstages:\n  - name: proposal\nedges:\n  - from: review\n    to: implement\n")

	state := &TaskState{TaskRef: "loop-reset", Branch: "feat/loop"}
	state.ReviewRounds = []ReviewRound{{}, {}, {}} // 3 轮
	state.Findings = []Finding{{ID: "f1", Content: "未修问题", Status: "open", Round: 1}}
	state.ResolvedPrints = []string{LoopFingerprint("历史已解决项")}
	if marker := MarkLoopExhaustedIfDue(dir, state); marker == nil {
		t.Fatal("前置：轮龄超预算应先耗尽")
	}
	if err := ResetLoopExhaustion(dir, state, "人工裁决：降级为已知问题，继续"); err != nil {
		t.Fatal(err)
	}
	// 断言以盘上真相为准（与 complete pre-flight 的重载语义一致）；调用方内存
	// 同步另由 TestLoopExhaustionNoLeak 钉住。
	reloaded, err := LoadTaskState(dir, "loop-reset")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.LoopExhausted != nil || reloaded.RepairRounds != 0 {
		t.Fatalf("重置应持久化（exhausted 清零）: %+v", reloaded)
	}
	if len(reloaded.ResolvedPrints) == 0 {
		t.Fatal("ResolvedPrints 应保留（复活检测记忆不随重置丢失）")
	}
	if reloaded.LoopResetRound != 4 || reloaded.LoopResetAt == nil {
		t.Fatalf("重置应记下新预算起算轮（len(ReviewRounds)+1=4）与时间: round=%d at=%v",
			reloaded.LoopResetRound, reloaded.LoopResetAt)
	}
}

// preflightAfterReload 模拟 complete 的 pre-flight：每次都从盘上重载 state 再检查
// （与 loadTaskOrActive → CheckLoopExhausted 同序）。
func preflightAfterReload(t *testing.T, dir, ref string) []string {
	t.Helper()
	s, err := LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	return CheckLoopExhausted(dir, s)
}

// addReviewRound 追加一轮 review（只动轮次结构，模拟一次 review pass）。
func addReviewRound(t *testing.T, dir, ref string) {
	t.Helper()
	s, err := LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	s.ReviewRounds = append(s.ReviewRounds, ReviewRound{})
	if err := SaveTaskState(dir, s); err != nil {
		t.Fatal(err)
	}
}

// TestLoopExhaustionNoLeak 钉住回环耗尽的两处漏放（loop-contract B0 / REQ-0）：
//  1. 首次检出：重算命中的那一次 pre-flight 必须当场拦，而不是只写盘、下次才拦；
//  2. 人工重置：--reset-loop 后发放一份完整新预算——连续 pre-flight 都放行，
//     再经过 max_rounds 轮 review 才重新耗尽（旧实现只清标记、不清轮龄，重置后
//     第一次放行、第二次立刻再拦）。
func TestLoopExhaustionNoLeak(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeLoopSchema(t, dir, "version: 1\nstages:\n  - name: proposal\nedges:\n  - from: review\n    to: implement\n    max_rounds: 2\n")

	const ref = "loop-noleak"
	state := &TaskState{TaskRef: ref, Branch: "feat/loop"}
	state.ReviewRounds = []ReviewRound{{}, {}, {}}                                    // 3 轮
	state.Findings = []Finding{{ID: "f1", Content: "超龄问题", Status: "open", Round: 1}} // 轮龄 3 ≥ 2
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}

	// 1. 首次检出即拦，连续两次重载都拦；内存 state 同步带上标记。
	for i := 1; i <= 2; i++ {
		s, err := LoadTaskState(dir, ref)
		if err != nil {
			t.Fatal(err)
		}
		if reasons := CheckLoopExhausted(dir, s); len(reasons) == 0 {
			t.Fatalf("第 %d 次 pre-flight：超龄 open finding 应拦（首次检出不得放行）", i)
		}
		if s.LoopExhausted == nil {
			t.Fatalf("第 %d 次 pre-flight：内存 state 应同步耗尽标记", i)
		}
	}

	// 2. 人工重置 → 连续两次重载都放行。
	s, err := LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := ResetLoopExhaustion(dir, s, "人工裁决：换方向继续"); err != nil {
		t.Fatal(err)
	}
	if s.LoopExhausted != nil || s.LoopResetRound != 4 {
		t.Fatalf("重置应同步调用方内存 state: exhausted=%v round=%d", s.LoopExhausted, s.LoopResetRound)
	}
	for i := 1; i <= 2; i++ {
		if reasons := preflightAfterReload(t, dir, ref); len(reasons) != 0 {
			t.Fatalf("重置后第 %d 次 pre-flight 应放行（新预算未用完）: %v", i, reasons)
		}
	}

	// 3. 新预算按轮消耗：再 1 轮仍放行，满 max_rounds=2 轮重新耗尽。
	addReviewRound(t, dir, ref)
	if reasons := preflightAfterReload(t, dir, ref); len(reasons) != 0 {
		t.Fatalf("重置后仅 1 轮（< 预算 2）应放行: %v", reasons)
	}
	addReviewRound(t, dir, ref)
	if reasons := preflightAfterReload(t, dir, ref); len(reasons) == 0 {
		t.Fatal("重置后满 max_rounds 轮仍 open → 应重新耗尽")
	}
}

// TestLoopExhaustionMaxRoundsOne 钉住 max_rounds=1 的边界（合法配置）：新预算
// 起算后轮龄最小为 1，已 ≥ 预算——重置后应立即再耗尽，这是设计行为而非漏放。
func TestLoopExhaustionMaxRoundsOne(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeLoopSchema(t, dir, "version: 1\nstages:\n  - name: proposal\nedges:\n  - from: review\n    to: implement\n    max_rounds: 1\n")

	const ref = "loop-max1"
	state := &TaskState{TaskRef: ref, Branch: "feat/loop"}
	state.ReviewRounds = []ReviewRound{{}}
	state.Findings = []Finding{{ID: "f1", Content: "问题", Status: "open", Round: 1}}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	if reasons := preflightAfterReload(t, dir, ref); len(reasons) == 0 {
		t.Fatal("max_rounds=1、轮龄 1：首次 pre-flight 应拦")
	}
	s, err := LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := ResetLoopExhaustion(dir, s, "人工裁决"); err != nil {
		t.Fatal(err)
	}
	if reasons := preflightAfterReload(t, dir, ref); len(reasons) == 0 {
		t.Fatal("max_rounds=1：重置后轮龄仍为 1 ≥ 预算，应立即再耗尽")
	}
}

// TestLoopExhaustionStaleSnapshotKeepsConcurrentReset 钉住锁内重判（B0 评审
// Minor #5）：pre-flight 持有的快照读取之后恰有一次 --reset-loop 落盘——本次仍按
// 快照拦（偏严），但旧快照的判定不得把刚重置的盘上状态再标回耗尽。
func TestLoopExhaustionStaleSnapshotKeepsConcurrentReset(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeLoopSchema(t, dir, "version: 1\nstages:\n  - name: proposal\nedges:\n  - from: review\n    to: implement\n    max_rounds: 2\n")

	const ref = "loop-stale"
	state := &TaskState{TaskRef: ref, Branch: "feat/loop"}
	state.ReviewRounds = []ReviewRound{{}, {}, {}}
	state.Findings = []Finding{{ID: "f1", Content: "超龄问题", Status: "open", Round: 1}}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}

	stale, err := LoadTaskState(dir, ref) // pre-flight 的快照（重置前）
	if err != nil {
		t.Fatal(err)
	}
	other, err := LoadTaskState(dir, ref) // 并发的人工重置
	if err != nil {
		t.Fatal(err)
	}
	if err := ResetLoopExhaustion(dir, other, "人工裁决"); err != nil {
		t.Fatal(err)
	}

	if reasons := CheckLoopExhausted(dir, stale); len(reasons) == 0 {
		t.Fatal("旧快照超龄：本次 pre-flight 仍应按快照拦")
	}
	reloaded, err := LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.LoopExhausted != nil {
		t.Fatalf("旧快照不得把并发重置后的盘上状态再标回耗尽: %+v", reloaded.LoopExhausted)
	}
}
