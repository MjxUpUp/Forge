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

// TestResetLoopExhaustion 钉住人工重置：exhausted 与轮次清零、审计行落地、
// ResolvedPrints 保留（复活检测记忆不随重置丢失）。
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
	// 断言以盘上真相为准（ResetLoopExhaustion 的 merge 在锁内副本上生效，
	// 外层快照不代表盘上状态——与 complete pre-flight 的重载语义一致）。
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
}
