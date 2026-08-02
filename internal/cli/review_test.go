package cli

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// TestRenderReviewPassBlindSpot (scheme 3 blind_spot trigger): `forge review pass` is a
// stamp-decisive action; when evidence is weak (Weak/Unverified) it must emit an ADVISORY
// reminding that this review stamps over blind-spot evidence and the reviewer must have
// critic-level verification; when evidence is trustworthy (Strong) or there is nothing to
// calibrate (NoData) it stays silent to avoid noise.
//
// TestRenderReviewPassBlindSpot（方案3 blind_spot 触发）：forge review pass 是 stamp 决定性
// 动作，证据弱时（Weak/Unverified）须发 ADVISORY 提醒本次 review 盖在盲区证据上、reviewer 须已
// critic 级核验；证据可信（Strong）或无可校准（NoData）时静默不噪声。
func TestRenderReviewPassBlindSpot(t *testing.T) {
	// Unverified (zero deterministic) → rubber-stamp high-risk ADVISORY.
	//
	// Unverified（零 deterministic）→ rubber-stamp 高风险 ADVISORY
	adv := renderReviewPassBlindSpot(checklog.EvidenceChain{Deterministic: 0, AgentClaim: 3})
	if !strings.HasPrefix(adv, "ADVISORY:") || !strings.Contains(adv, "零 deterministic") {
		t.Errorf("Unverified 应发 rubber-stamp ADVISORY，得 %q", adv)
	}

	// Weak (deterministic in minority) → cross-check ADVISORY.
	//
	// Weak（deterministic 占少数）→ 加核 ADVISORY
	adv = renderReviewPassBlindSpot(checklog.EvidenceChain{Deterministic: 1, AgentClaim: 4})
	if !strings.HasPrefix(adv, "ADVISORY:") || !strings.Contains(adv, "占比低") {
		t.Errorf("Weak 应发加核 ADVISORY，得 %q", adv)
	}

	// Strong (deterministic in majority) → silent (evidence trustworthy, no noise).
	//
	// Strong（deterministic 占多数）→ 静默（证据可信，不噪声）
	adv = renderReviewPassBlindSpot(checklog.EvidenceChain{Deterministic: 4, AgentClaim: 1})
	if adv != "" {
		t.Errorf("Strong 应静默，得 %q", adv)
	}

	// NoData (no evidence at all) → silent (nothing to calibrate).
	//
	// NoData（无任何证据）→ 静默（无可校准）
	adv = renderReviewPassBlindSpot(checklog.EvidenceChain{})
	if adv != "" {
		t.Errorf("NoData 应静默，得 %q", adv)
	}

	// Scheme5↔Scheme3 interaction: Strong but used the escape hatch → Strength caps to Weak →
	// triggers ADVISORY (escape has a cost). Note: ratio is actually not low (0.8), so the
	// wording reports escape-hatch not low-ratio — pointing at the real cause: skipping the gate.
	//
	// 方案5↔方案3 联动：Strong 但用了逃生舱 → Strength cap 到 Weak → 触发 ADVISORY（逃生有代价）。
	// 注意：ratio 实际不低（0.8），故措辞不报「占比低」而报「逃生舱」——点出真正原因是跳过 gate。
	adv = renderReviewPassBlindSpot(checklog.EvidenceChain{Deterministic: 4, AgentClaim: 1, UsedEscapeHatch: true})
	if !strings.HasPrefix(adv, "ADVISORY:") || !strings.Contains(adv, "逃生舱") || strings.Contains(adv, "占比低") {
		t.Errorf("UsedEscapeHatch 致 Strong→Weak 应触发逃生舱 ADVISORY（非占比低），得 %q", adv)
	}

	// Anti-false-claim regression: ratio is already low (0.25) and the escape hatch was used →
	// Weak. Here claiming not-weak is a false claim (0.25 is genuinely low), so the wording
	// must fall back to low-ratio — it cannot uniformly report escape-hatch just because
	// UsedEscapeHatch=true. Failure scenario: 1 det + 3 agent-claim + escape, ratio=0.25; the
	// old impl would output a not-weak message, misleading.
	//
	// 防假声明回归：ratio 本就低（0.25）且用了逃生舱 → Weak。此时「本不弱」是假声明
	//（0.25 确实低），必须回落「占比低」措辞——不能因 UsedEscapeHatch=true 就一刀切报逃生舱。
	// 失败场景：1 det + 3 agent-claim + escape，ratio=0.25；旧实现会输出「ratio=0.25 本不弱」误导。
	adv = renderReviewPassBlindSpot(checklog.EvidenceChain{Deterministic: 1, AgentClaim: 3, UsedEscapeHatch: true})
	if !strings.HasPrefix(adv, "ADVISORY:") || !strings.Contains(adv, "占比低") || strings.Contains(adv, "本不弱") {
		t.Errorf("ratio<0.5+escape 应回落占比低措辞（本不弱是假声明），得 %q", adv)
	}
}

// TestRunReviewPassAt_ExplicitRef pins the `forge review pass --ref` path: an explicit
// ref loads THAT task directly (bypassing active-task detection — the common case is
// marking review for a task started in another session) and persists ReviewPassed.
// A nonexistent ref must error out and NOT fall through to the branch-stamp branch —
// silently stamping the branch when the agent meant a specific task would mark the
// wrong thing.
//
// TestRunReviewPassAt_ExplicitRef 钉住 `forge review pass --ref` 路径：显式 ref
// 直接加载该任务（绕过活跃任务检测——常见场景是给另一个 session 起的任务补标
// review）并持久化 ReviewPassed。不存在的 ref 必须报错，且不得回落分支 stamp
// 分支——agent 指明任务时静默标分支等于标错对象。
func TestRunReviewPassAt_ExplicitRef(t *testing.T) {
	dir := t.TempDir()
	const ref = `feat/explicit-ref`
	state := &taskpipeline.TaskState{TaskRef: ref, Branch: `feat/explicit-ref`}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}

	if err := runReviewPassAt(dir, ref); err != nil {
		t.Fatalf("runReviewPassAt(--ref): %v", err)
	}
	reloaded, err := taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.ReviewPassed {
		t.Error("显式 ref 的任务应被标记 ReviewPassed=true")
	}

	// Nonexistent ref → hard error, no stamp fallback.
	//
	// 不存在的 ref → 硬报错，不回落 stamp
	if err := runReviewPassAt(dir, `feat/nonexistent`); err == nil {
		t.Fatal("ref 不存在应报错返回（不回落分支 stamp 分支）")
	}
}

// TestReviewRefFlagsRegistered pins the --ref flag registration on all three review
// subcommands (pass is the primary; gate/status share the same active-task detection
// limitation and get the flag for consistency).
//
// TestReviewRefFlagsRegistered 钉住三个 review 子命令的 --ref flag 注册（pass 是
// 主目标；gate/status 有同样的活跃任务检测局限，为一致性一并加）。
func TestReviewRefFlagsRegistered(t *testing.T) {
	for name, cmd := range map[string]*cobra.Command{
		"pass":   reviewPassCmd,
		"gate":   reviewGateCmd,
		"status": reviewStatusCmd,
	} {
		if cmd.Flags().Lookup("ref") == nil {
			t.Errorf("review %s 应注册 --ref flag", name)
		}
	}
}

// TestRenderReviewStatus_ExplicitRef pins the status side of the --ref contract:
// explicit ref renders that task's status without any active-task context.
//
// TestRenderReviewStatus_ExplicitRef 钉住 status 侧的 --ref 契约：显式 ref 在无
// 活跃任务上下文时也能渲染该任务的状态。
func TestRenderReviewStatus_ExplicitRef(t *testing.T) {
	dir := t.TempDir()
	const ref = `feat/status-ref`
	state := &taskpipeline.TaskState{TaskRef: ref, Branch: `feat/status-ref`}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := renderReviewStatus(dir, ref); err != nil {
			t.Fatalf("renderReviewStatus(--ref): %v", err)
		}
	})
	if !strings.Contains(out, ref) {
		t.Errorf("status 输出应包含任务 ref %q，got: %q", ref, out)
	}

	if err := renderReviewStatus(dir, `feat/nonexistent`); err == nil {
		t.Fatal("status 的 ref 不存在应报错返回")
	}
}
