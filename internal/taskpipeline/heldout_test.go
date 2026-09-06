package taskpipeline

// heldout_test.go — held-out gap 门禁的表驱动测试：双套件判定（gap 阻断形态 /
// 两者同挂 warn / 全过 pass / 未登记跳过）+ 侧车往返 + 逃生舱。命令用跨平台真命令
//（echo / exit 1）——RunTestCommand 实跑，不 mock。

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

func TestHeldoutRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveHeldout(dir, "feat/x", []AcceptanceCriterion{
		{Run: "echo ok", Expected: "ok"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadHeldout(dir, "feat/x")
	if err != nil || len(got) != 1 || got[0].Run != "echo ok" {
		t.Fatalf("侧车往返失败: %+v %v", got, err)
	}
	// 未登记 → nil（区别于错误）
	none, err := LoadHeldout(dir, "feat/other")
	if err != nil || none != nil {
		t.Fatalf("未登记应 nil: %+v %v", none, err)
	}
}

func TestVerifyHeldout_GapStates(t *testing.T) {
	dir := t.TempDir()

	t.Run("gap_可见过保留集挂", func(t *testing.T) {
		s := &TaskState{TaskRef: "t/gap1", Acceptance: []AcceptanceCriterion{
			{Run: "echo v", Expected: "v", Passed: true}, // 可见已过（模拟 VerifyAcceptance 结果）
		}}
		SaveHeldout(dir, s.TaskRef, ParseAcceptance([]string{"exit 1"}))
		res := VerifyHeldout(dir, s)
		if !res.Checked || !res.VisiblePassed || res.HeldoutPassed {
			t.Fatalf("应 gap 形态: %+v", res)
		}
		rows, _ := checklog.LoadAll(dir)
		found := false
		for _, e := range rows {
			if e.Check == CheckNameHeldoutGap && e.EffectiveLevel() == checklog.LevelFail {
				found = true
			}
		}
		if !found {
			t.Fatal("gap 形态应落 fail 行")
		}
	})

	t.Run("both_fail_可见同挂只记warn", func(t *testing.T) {
		s := &TaskState{TaskRef: "t/gap2", Acceptance: []AcceptanceCriterion{
			{Run: "exit 1", Passed: false},
		}}
		SaveHeldout(dir, s.TaskRef, ParseAcceptance([]string{"exit 2"}))
		res := VerifyHeldout(dir, s)
		if !res.Checked || res.VisiblePassed || res.HeldoutPassed {
			t.Fatalf("应 both-fail: %+v", res)
		}
	})

	t.Run("pass_全过", func(t *testing.T) {
		s := &TaskState{TaskRef: "t/gap3", Acceptance: []AcceptanceCriterion{
			{Run: "echo v", Expected: "v", Passed: true},
		}}
		SaveHeldout(dir, s.TaskRef, ParseAcceptance([]string{"echo hidden :: hidden"}))
		res := VerifyHeldout(dir, s)
		if !res.HeldoutPassed {
			t.Fatalf("应全过: %+v", res)
		}
	})

	t.Run("skip_未登记", func(t *testing.T) {
		s := &TaskState{TaskRef: "t/gap4"}
		res := VerifyHeldout(dir, s)
		if res.Checked {
			t.Fatalf("未登记应跳过: %+v", res)
		}
	})
}

func TestCheckHeldoutFresh(t *testing.T) {
	dir := t.TempDir()
	s := &TaskState{TaskRef: "t/fresh", Acceptance: []AcceptanceCriterion{
		{Run: "echo v", Expected: "v", Passed: true},
	}}
	// 未登记 → 放行
	if ok, reasons := CheckHeldoutFresh(dir, s); !ok || len(reasons) != 0 {
		t.Fatalf("未登记应放行: %v %v", ok, reasons)
	}
	// 登记挂掉 → 拒绝
	SaveHeldout(dir, s.TaskRef, ParseAcceptance([]string{"exit 1"}))
	if ok, reasons := CheckHeldoutFresh(dir, s); ok || len(reasons) == 0 {
		t.Fatalf("挂掉应拒绝: %v %v", ok, reasons)
	}
	// 逃生舱 → 放行 + 留痕
	t.Setenv("FORGE_HELDOUT", "disable")
	if ok, _ := CheckHeldoutFresh(dir, s); !ok {
		t.Fatal("逃生舱应放行")
	}
	rows, _ := checklog.LoadAll(dir)
	found := false
	for _, e := range rows {
		if e.Check == checklog.CheckEscapeHatch {
			found = true
		}
	}
	if !found {
		t.Fatal("逃生应留 escape-hatch 痕")
	}
}
