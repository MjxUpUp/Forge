package taskpipeline

// heldout_test.go — held-out gap 门禁的表驱动测试：双套件判定（gap 阻断形态 /
// 两者同挂 warn / 全过 pass / 未登记跳过）+ 侧车往返 + 逃生舱。命令用跨平台真命令
//（echo / exit 1）——RunTestCommand 实跑，不 mock。

import (
	"os"
	"path/filepath"
	"strings"
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

// TestHeldout_RunCountRotation 钉死 W4 轮换触发：使用计数逐次递增；到达阈值时
// PASS 判定降级为 warn（保留集可能已被记忆——记忆化是 SpecBench 实证的攻击面）；
// checklog Meta 带计数与内容哈希（前后对比暴露静默换锚）。重出集（SaveHeldout）
// 计数归零——「新锚」语义。
func TestHeldout_RunCountRotation(t *testing.T) {
	dir := t.TempDir()
	ref := "feat/rotate"
	if err := SaveHeldout(dir, ref, []AcceptanceCriterion{{Run: "echo ok", Expected: "ok"}}); err != nil {
		t.Fatal(err)
	}
	state := &TaskState{TaskRef: ref, Acceptance: []AcceptanceCriterion{{Run: "echo ok", Expected: "ok"}}}

	var lastWarn, lastDetail string
	for i := 1; i <= heldoutRotateThreshold; i++ {
		res := VerifyHeldout(dir, state)
		if !res.HeldoutPassed {
			t.Fatalf("第 %d 次：全过套件不应挂", i)
		}
		entries, err := checklog.LoadAll(dir)
		if err != nil {
			t.Fatal(err)
		}
		var last checklog.Entry
		for _, e := range entries {
			if e.Check == CheckNameHeldoutGap {
				last = e
			}
		}
		if i < heldoutRotateThreshold && last.Level == checklog.LevelWarn {
			t.Fatalf("第 %d 次未到阈值不应出轮换 warn（%v）", i, last.Detail)
		}
		lastWarn = string(last.Level)
		lastDetail = last.Detail
	}
	if lastWarn != string(checklog.LevelWarn) {
		t.Errorf("第 %d 次（阈值）应为 warn（轮换建议），got %q：%s", heldoutRotateThreshold, lastWarn, lastDetail)
	}
	if !strings.Contains(lastDetail, "轮换") {
		t.Errorf("阈值 warn 应含轮换建议，got %q", lastDetail)
	}

	// Meta 携带计数与哈希（防静默换锚的机器载荷）。
	side, exists, err := loadHeldoutSidecar(dir, ref)
	if err != nil || !exists || side.RunCount != heldoutRotateThreshold || side.Hash == "" {
		t.Fatalf("侧车计数/哈希异常: count=%d hash=%q exists=%v err=%v", side.RunCount, side.Hash, exists, err)
	}
	before := side.Hash

	// 重出集 = 新锚：计数归零，同内容哈希不变（内容没换 → 哈希同）。
	if err := SaveHeldout(dir, ref, side.Criteria); err != nil {
		t.Fatal(err)
	}
	fresh, _, _ := loadHeldoutSidecar(dir, ref)
	if fresh.RunCount != 0 {
		t.Errorf("重出集应归零计数，got %d", fresh.RunCount)
	}
	if fresh.Hash != before {
		t.Errorf("同内容哈希应稳定（换锚检测依赖它），before=%s after=%s", before, fresh.Hash)
	}
}

// TestHeldout_LegacyArraySidecar：v1 裸数组侧车兼容——旧任务升级后首次实跑
// 计数从 0 起步，不因形态迁移报损坏。
func TestHeldout_LegacyArraySidecar(t *testing.T) {
	dir := t.TempDir()
	ref := "feat/legacy"
	if err := os.MkdirAll(filepath.Join(dataHome(dir), "heldout"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `[{"run":"echo legacy","expected":"legacy"}]`
	path := filepath.Join(dataHome(dir), "heldout", "feat__legacy.json")
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	state := &TaskState{TaskRef: ref}
	res := VerifyHeldout(dir, state)
	if !res.Checked || !res.HeldoutPassed {
		t.Fatalf("旧数组侧车应正常实跑，got %+v", res)
	}
	side, _, _ := loadHeldoutSidecar(dir, ref)
	if side.RunCount != 1 || side.Hash == "" {
		t.Errorf("旧形态首跑后计数应为 1 且有哈希，got %d/%q", side.RunCount, side.Hash)
	}
}
