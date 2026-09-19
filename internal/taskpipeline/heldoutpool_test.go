package taskpipeline

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// heldoutpool_test.go — L3 池行为钉（配对 heldoutpool.go）：沉淀/列池/抽取/
// 并入任务侧车的全链与拒绝面。

// TestHeldoutPool_RoundTrip: add two tagged sets → list filters by tag → draw
// picks name-ordered N → apply merges into the task sidecar (dedup by Run).
//
// TestHeldoutPool_RoundTrip：沉淀两套带标签集 → tag 过滤列池 → 名序抽 N →
// 并入任务侧车（按 Run 去重）。
func TestHeldoutPool_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	state := &TaskState{TaskRef: "feat/pool", Branch: "feat/x"}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	if err := AddHeldoutSet(dir, HeldoutSet{
		Name: "money-bounds", Tag: "money",
		Criteria: []AcceptanceCriterion{{Run: "go test -run TestMoney ./pay/... :: ok"}},
	}); err != nil {
		t.Fatalf(`沉淀 money 集: %v`, err)
	}
	if err := AddHeldoutSet(dir, HeldoutSet{
		Name: "refund-chain", Tag: "refund",
		Criteria: []AcceptanceCriterion{{Run: "go test -run TestRefund ./pay/... :: ok"}, {Run: "go version :: "}},
	}); err != nil {
		t.Fatalf(`沉淀 refund 集: %v`, err)
	}
	// 同名拒绝静默覆盖（换锚必须显式）。
	if err := AddHeldoutSet(dir, HeldoutSet{Name: "money-bounds", Criteria: []AcceptanceCriterion{{Run: "x"}}}); err == nil {
		t.Error(`同名池集应拒绝（拒绝静默换锚）`)
	}
	// tag 过滤列池。
	if got := ListHeldoutSets(dir, "money"); len(got) != 1 || got[0].Name != "money-bounds" {
		t.Errorf(`tag=money 应恰列 1 套，got %+v`, got)
	}
	if got := ListHeldoutSets(dir, ""); len(got) != 2 {
		t.Errorf(`无 tag 应列全套，got %d", len(got))`, len(got))
	}
	// 名序确定性抽取：money-bounds < refund-chain，取 1 套。
	sets, err := DrawHeldoutSets(dir, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 1 || sets[0].Name != "money-bounds" {
		t.Fatalf(`名序首套应为 money-bounds，got %+v`, sets)
	}
	// 并入任务侧车：去重 + Source 盖 manual（人出题按最弱层披露）。
	sets, _ = DrawHeldoutSets(dir, "", 0)
	total, err := ApplyHeldoutToTask(dir, state.TaskRef, sets)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Errorf(`并入后侧车应 3 条（2+1 去重），got %d`, total)
	}
	// 再并入一次：幂等（去重）。
	total2, _ := ApplyHeldoutToTask(dir, state.TaskRef, sets)
	if total2 != 3 {
		t.Errorf(`重复并入应幂等，got %d`, total2)
	}
	sc, _, lerr := loadHeldoutSidecar(dir, state.TaskRef)
	if lerr != nil || sc == nil {
		t.Fatalf(`侧车应存在: %v`, lerr)
	}
	for _, c := range sc.Criteria {
		if c.Source != AcceptanceSourceManual {
			t.Errorf(`池题应盖 manual 层，got %q`, c.Source)
		}
	}
	// 空池抽取报错并指路。
	empty := t.TempDir()
	if _, err := DrawHeldoutSets(empty, "", 1); err == nil {
		t.Error(`空池抽取应报错并指路 --add`)
	}
}

// TestApplyHeldoutToTask_PreservesRunCountAndAudits（阶段三复审建议项）：
// 并入保留侧车的既有使用计数与锚定时间（不清零——W4 记忆化暴露的对抗面），
// 换锚落 heldout-pool-apply 审计行（before/after 哈希有据可查）。
func TestApplyHeldoutToTask_PreservesRunCountAndAudits(t *testing.T) {
	dir := t.TempDir()
	const ref = "feat/pool-count"
	// 先建带计数与断言弱化测试的侧车（模拟已实跑 5 次的存量保留集）。
	if err := SaveHeldout(dir, ref, []AcceptanceCriterion{{Run: "go version :: "}}); err != nil {
		t.Fatal(err)
	}
	sc, _, err := loadHeldoutSidecar(dir, ref)
	if err != nil || sc == nil {
		t.Fatal(err)
	}
	sc.RunCount = 5
	beforeHash := sc.Hash
	if err := saveHeldoutSidecar(dir, ref, sc); err != nil {
		t.Fatal(err)
	}
	// 并入一套池题。
	_, err = ApplyHeldoutToTask(dir, ref, []HeldoutSet{{
		Name: "extra", Tag: "t",
		Criteria: []AcceptanceCriterion{{Run: "go test -run X ./p/... :: ok"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	after, _, err := loadHeldoutSidecar(dir, ref)
	if err != nil || after == nil {
		t.Fatal(err)
	}
	if after.RunCount != 5 {
		t.Errorf(`使用计数必须保留（W4 对抗面），got %d want 5`, after.RunCount)
	}
	if after.Hash == beforeHash {
		t.Errorf(`内容变化应翻转哈希（换锚事实）`)
	}
	found := false
	entries, _ := checklog.LoadForTask(dir, ref)
	for _, e := range entries {
		if e.Check == CheckNameHeldoutPoolApply {
			found = true
			if e.Meta["hash_before"] != beforeHash || e.Meta["hash_after"] != after.Hash {
				t.Errorf(`审计行应带前后哈希: %v`, e.Meta)
			}
		}
	}
	if !found {
		t.Error(`并入应落 heldout-pool-apply 审计行`)
	}
}
