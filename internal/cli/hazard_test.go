package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hazard"
	"github.com/spf13/cobra"
)

// TestRunHazardConfirm_RejectsInvalidFingerprint pins the format validation of the confirm --fingerprint path taking effect at the cli layer: invalid fingerprints (truncated/non-hex) must return an error, not print a fake success message.
//
// TestRunHazardConfirm_RejectsInvalidFingerprint 钉住 confirm --fingerprint 路径的格式
// 校验从 cli 层生效：非法指纹（残缺/非 hex）必须返回 error，而非打印"✅ 已确认"虚假成功
// （2026-07 AgentWorld 事故根因：agent 抄错指纹，confirm 不校验直接写入错文件名报成功，
// hook 用真指纹查不到、继续拦）。
//
// 校验逻辑在 internal/hazard.ConfirmByFingerprint（见
// TestConfirmByFingerprint_RejectsInvalidFormat），本测试确认 cli 层 runHazardConfirm
// 透传该 error，不会走到 fmt.Printf 的"✅ 已确认"。直接调 runHazardConfirm 绕开 hazard-guard
// 对 "confirm --fingerprint" 的既有误伤（confirm 含 rm、--fingerprint 含 -f...r 被误判 rm -rf）。
//
// 传 &cobra.Command{} 而非 nil：runHazardConfirm 当前不用 cmd，但传真实对象防未来有人加
// cmd.Flags()/cmd.OutOrStdout() 时测试 panic。
func TestRunHazardConfirm_RejectsInvalidFingerprint(t *testing.T) {
	// "abc" 既非 64 字符也非合法 hex——校验必拒，且不落盘。
	hazardConfirmFingerprint = "abc"
	t.Cleanup(func() { hazardConfirmFingerprint = "" })

	err := runHazardConfirm(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("runHazardConfirm must reject invalid --fingerprint instead of printing success")
	}
	// 断言 error 确实来自指纹校验，而非 findProjectRoot 失败——否则在"项目根找不到"
	// 时测试也会通过（假阳性），与测试名承诺不符。
	if !strings.Contains(err.Error(), "invalid fingerprint") {
		t.Fatalf("error must come from fingerprint validation, got: %v", err)
	}
}

// TestShortFingerprint pins the length guard on confirmation fingerprints read back from disk (untrusted input): values shorter than 12 chars must not panic on the display slice.
//
// TestShortFingerprint 钉住从磁盘读回的确认指纹（不可信输入）的长度守卫：
// 短于 12 字符的值不得在展示切片上 panic。
func TestShortFingerprint(t *testing.T) {
	cases := map[string]string{
		"":               "",
		"abc":            "abc",
		"0123456789ab":   "0123456789ab",
		"0123456789abcd": "0123456789ab",
	}
	for in, want := range cases {
		if got := shortFingerprint(in); got != want {
			t.Errorf("shortFingerprint(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestHazardCmd_LongGuidanceCopy pins the 2026-08 help-text revision (the same copy fix as the hazard-guard block message): the Long help must carry the pre-authorization path (user already instructed/confirmed this turn → confirm --last directly, no second ask), must reference the host tool's confirmation mechanism generically (the AskUserQuestion enumeration missed kimi/copilot/zcode), and must not carry the FORGE_ALLOW_HAZARD migration note (changelog, not action guidance).
//
// TestHazardCmd_LongGuidanceCopy 钉死 2026-08 帮助文案修订（与 hazard-guard
// block 文案同批修复）：Long 必须含授权路径（用户本回合已明确指令/确认过 →
// 直接 confirm --last 无需二次确认）、工具指代须泛化（AskUserQuestion 枚举漏了
// kimi/copilot/zcode）、不得再带 FORGE_ALLOW_HAZARD 迁移说明（changelog 不是
// 行动指引）。
func TestHazardCmd_LongGuidanceCopy(t *testing.T) {
	for _, anchor := range []string{"无需二次确认", "confirm --last", "你所在工具的提问确认机制"} {
		if !strings.Contains(hazardCmd.Long, anchor) {
			t.Errorf("hazard Long help missing %q:\n%s", anchor, hazardCmd.Long)
		}
	}
	for _, gone := range []string{"FORGE_ALLOW_HAZARD", "AskUserQuestion"} {
		if strings.Contains(hazardCmd.Long, gone) {
			t.Errorf("hazard Long help must not contain %q anymore:\n%s", gone, hazardCmd.Long)
		}
	}
}

// TestHazardLogRejectsResetEvents（delivery-hardening P0-1 回归钉）：confirm /
// halt-release 是清账重置点——经 hazard log 调试入口写入被拒（否则 agent 一条
// `forge hazard log confirm x` 零交互清空悬账，墙-1b 被证伪）。
func TestHazardLogRejectsResetEvents(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, `init`)
	runGit(t, dir, `config`, `user.email`, `t@t.com`)
	runGit(t, dir, `config`, `user.name`, `T`)
	// forge 项目锚（projectroot.Find 需要项目形态——.forge 目录同 setupChainTask）。
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	for _, bad := range []string{"confirm", "halt-release", "reset", "whatever"} {
		if err := runHazardLog(nil, []string{bad, "some command"}); err == nil {
			t.Errorf("事件类型 %q 应被白名单拒绝", bad)
		} else if !strings.Contains(err.Error(), "真人终端") {
			t.Errorf("拒绝文案应指向真人终端出口: %v", err)
		}
	}
	// hook 实际使用的三类事件放行（写入临时项目的事件流）。
	for _, okType := range []string{"block", "release", "data"} {
		if err := runHazardLog(nil, []string{okType, "some command"}); err != nil {
			t.Errorf("事件类型 %q 应放行: %v", okType, err)
		}
	}
}

// TestHaltReleaseCore pins haltReleaseCore's ledger-clearing behavior across the
// three states (halted / pending-not-halted / events-destroyed) plus the clean no-op —
// the 2026-09-22 fix: the delivery gate's error message pointed users at
// `forge hazard halt release --yes` while the not-halted path was a no-op
// ("未处于 safe-halt，无需解锁"), making the exit unreachable; the
// events-destroyed state was a full deadlock (confirm --last has no events to read,
// release no-ops, the gate blocks forever).
//
// TestHaltReleaseCore 钉 haltReleaseCore 三态清账行为 + 干净账 no-op——2026-09-22
// 修复的回归锚：清账门报错曾把用户指到 halt release，而未停机路径是 no-op（出口
// 不可达）；事件流被毁态则死锁（confirm --last 无事件可读、release no-op、清账门
// 永拦）。终端判别在 RunE 层（测试环境 stdin 非 char device），此处直测核心函数。
func TestHaltReleaseCore(t *testing.T) {
	newProject := func(t *testing.T) *forgedata.Project {
		t.Helper()
		t.Setenv("FORGE_DATA_HOME", t.TempDir())
		p, err := forgedata.ProjectFor(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("未停机悬账核销", func(t *testing.T) {
		p := newProject(t)
		if err := hazard.AppendEvent(p, hazard.Event{Type: hazard.EventBlock, Command: "rm -rf /tmp/x"}); err != nil {
			t.Fatal(err)
		}
		st := hazard.CheckHalt(p)
		if st.Halted || st.Blocks != 1 {
			t.Fatalf("前置：1 个未确认拦截应为未停机悬账, got halted=%v blocks=%d", st.Halted, st.Blocks)
		}
		if err := haltReleaseCore(p, st); err != nil {
			t.Fatal(err)
		}
		if after := hazard.CheckHalt(p); after.Blocks != 0 {
			t.Fatalf("核销后计数应归零, got %d——未停机悬账出口仍不可达则清账门死锁", after.Blocks)
		}
	})

	t.Run("干净账no-op不写事件", func(t *testing.T) {
		p := newProject(t)
		if err := haltReleaseCore(p, hazard.CheckHalt(p)); err != nil {
			t.Fatal(err)
		}
		if !hazard.EventsDestroyed(p) {
			// no-op 不得创建事件流（HazardsEventsPath 不存在 = 未写过任何事件）。
			if _, err := os.Stat(p.HazardsEventsPath()); err == nil {
				t.Fatal("干净账 no-op 不应写 release 事件——会伪造清账审计行")
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		} else {
			t.Fatal("前置：全新项目不应判为事件被毁")
		}
	})

	t.Run("事件被毁核销解死锁", func(t *testing.T) {
		p := newProject(t)
		// 造"事件曾落盘后被清除"：先落一个 block（建目录+文件），再删 events.jsonl。
		if err := hazard.AppendEvent(p, hazard.Event{Type: hazard.EventBlock, Command: "rm -rf /tmp/x"}); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(p.HazardsEventsPath()); err != nil {
			t.Fatal(err)
		}
		if !hazard.EventsDestroyed(p) {
			t.Fatal("前置：目录在而文件无应判事件被毁")
		}
		// CheckHalt 读不到事件 → Blocks=0 未停机——旧实现此处 no-op 即死锁。
		if err := haltReleaseCore(p, hazard.CheckHalt(p)); err != nil {
			t.Fatal(err)
		}
		if hazard.EventsDestroyed(p) {
			t.Fatal("核销应重建事件流（release 事件落盘），仍判被毁则死锁未解")
		}
	})

	t.Run("停机解锁原行为保持", func(t *testing.T) {
		p := newProject(t)
		for i := 0; i < hazard.HaltThreshold; i++ {
			if err := hazard.AppendEvent(p, hazard.Event{Type: hazard.EventBlock, Command: "rm -rf /tmp/x"}); err != nil {
				t.Fatal(err)
			}
		}
		st := hazard.CheckHalt(p)
		if !st.Halted {
			t.Fatal("前置：连续拦截达阈值应停机")
		}
		if err := haltReleaseCore(p, st); err != nil {
			t.Fatal(err)
		}
		if after := hazard.CheckHalt(p); after.Halted || after.Blocks != 0 {
			t.Fatalf("解锁后应停机解除且计数归零, got halted=%v blocks=%d", after.Halted, after.Blocks)
		}
	})
}
