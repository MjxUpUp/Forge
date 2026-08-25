package cli

import (
	"os"
	"path/filepath"
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

	// 2026-08 evidence-scaled cap: heavy evidence (ratio>=0.85 && det>=20) + escape →
	// marginal escape does NOT downgrade → Strength stays Strong → silent. The advisory
	// derives from checklog.EscapeDowngradedStrength (single source), so this pins the
	// derivation at the advisory surface — the escape ADVISORY must not fire on
	// well-evidenced tasks (that was the flat-tax noise).
	//
	// 2026-08 证据缩放 cap：重证据（ratio>=0.85 且 det>=20）+逃生 → 边际逃生不降档 →
	// Strength 保持 Strong → 静默。ADVISORY 从 checklog.EscapeDowngradedStrength 派生
	//（单一真相源），此处钉住派生在 ADVISORY 面的行为——逃生 ADVISORY 不得在证据充分
	// 的任务上触发（那正是平价税噪声）。
	adv = renderReviewPassBlindSpot(checklog.EvidenceChain{Deterministic: 100, AgentClaim: 2, UsedEscapeHatch: true})
	if adv != "" {
		t.Errorf("重证据边际逃生应 Strong 静默（无 ADVISORY），得 %q", adv)
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

// TestRunReviewPassAt_ReworkRoundRequiresRecheck pins the re-review requirement surfaced
// at re-stamp time (2026-08 protocol gap fix): when `forge review pass` stamps a round whose
// code snapshot CHANGED since the previous stamped round (head OR workdir-change hash — the
// baseline delta that comes from fixing previous review findings), the output must carry an
// explicit ADVISORY that this stamp is only legitimate if a fresh read-only re-review agent
// already verified the fixes — the fixer cannot self-certify. Two silence contracts: round 1
// (no previous round) and a same-state repeat stamp (transient-failure retry / baseline
// rebuild — no code change, no re-review owed). Rationale: the snapshot loop
// (review-fix-recheck, executor.go task-complete HARD block) enforces only the SHAPE of the
// loop (re-stamp after code changes); without this cue, a fixer can stamp without
// re-reviewing and the protocol shows zero difference from an honest round (2026-08 real
// case: round-1 fixes were stamped without re-review and passed task-complete; the gap was
// only caught by a human asking "did you re-review?"). Trigger is the snapshot delta, not
// the bare round count — bare counts misfire on no-change re-stamps.
//
// TestRunReviewPassAt_ReworkRoundRequiresRecheck 钉住重新盖章时的复审要求提示
// （2026-08 协议缺口修复）：`forge review pass` 盖的轮次若代码快照自上一枚盖章轮以来
// 「变了」（head 或工作区变更 hash——源于修复上一轮 review 发现的基线增量），输出必须
// 带明确 ADVISORY——本枚章只在「已重新派只读复审 agent 验证过修复」时合法，修复者
// 不能自证。两份静默契约：第 1 轮（无上一轮可比）与同状态重复盖章（瞬态失败重试/
// 重建基线——无代码变更即不欠复审）。动机：快照闭环（review-fix-recheck，
// executor.go task-complete 硬阻断）只强制循环的「形状」（改码后重新盖章）；没有本
// 提示，修复者可以不复审直接盖章，协议输出与诚实轮零差别（2026-08 真实案例：第一轮
// 修复未经复审直接盖章过了 task-complete，缺口靠人工追问「复审了吗」才暴露）。触发
// 条件是快照增量而非裸轮次计数——裸计数会在无变更重盖上误响。
func TestRunReviewPassAt_ReworkRoundRequiresRecheck(t *testing.T) {
	dir := t.TempDir()
	// Real git fixture: the trigger is the snapshot delta (head/hash), which needs a
	// moving HEAD — a bare temp dir has empty head/hash forever and can never fire.
	//
	// 真实 git 夹具：触发条件是快照增量（head/hash），需要会动的 HEAD——裸临时目录
	// 的 head/hash 恒空，永远不触发。
	runGit(t, dir, `init`)
	runGit(t, dir, `config`, `user.email`, `test@test.com`)
	runGit(t, dir, `config`, `user.name`, `Test`)
	os.WriteFile(filepath.Join(dir, `main.go`), []byte("package main\n\nfunc main() {}\n"), 0644)
	runGit(t, dir, `add`, `.`)
	runGit(t, dir, `commit`, `-m`, `initial`)

	const ref = `feat/recheck-req`
	state := &taskpipeline.TaskState{TaskRef: ref, Branch: `feat/recheck-req`}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}

	// Round 1: first stamp — silent (nothing fixed yet, no re-review owed).
	//
	// 第 1 轮：首次盖章——静默（尚无修复，不欠复审）。
	first := captureStdout(t, func() {
		if err := runReviewPassAt(dir, ref, ""); err != nil {
			t.Fatalf("runReviewPassAt 第 1 次: %v", err)
		}
	})
	if strings.Contains(first, "复审") {
		t.Errorf(`第 1 轮盖章不应出现复审提示（尚无修复发生），got: %q`, first)
	}

	// Round 2 with NO code change (same-state repeat stamp: transient retry / baseline
	// rebuild) — still silent: no snapshot delta, no re-review owed.
	//
	// 第 2 轮且无代码变更（同状态重复盖章：瞬态重试/重建基线）——依旧静默：
	// 无快照增量，不欠复审。
	repeat := captureStdout(t, func() {
		if err := runReviewPassAt(dir, ref, ""); err != nil {
			t.Fatalf("runReviewPassAt 无变更重盖: %v", err)
		}
	})
	if strings.Contains(repeat, "复审") {
		t.Errorf(`同状态重复盖章不欠复审、应静默（触发条件是快照增量非轮次计数），got: %q`, repeat)
	}

	// Fix + commit → snapshot delta (HEAD moved). Round 3 stamp must carry the
	// re-review requirement.
	//
	// 修复 + 提交 → 快照增量（HEAD 移动）。第 3 轮盖章必须带复审要求。
	os.WriteFile(filepath.Join(dir, `main.go`), []byte("package main\n\nfunc main() { println(1) }\n"), 0644)
	runGit(t, dir, `add`, `.`)
	runGit(t, dir, `commit`, `-m`, `fix: apply review findings`)
	third := captureStdout(t, func() {
		if err := runReviewPassAt(dir, ref, ""); err != nil {
			t.Fatalf("runReviewPassAt 修复后重盖: %v", err)
		}
	})
	if !strings.Contains(third, "复审") || !strings.Contains(third, "自证") {
		t.Errorf(`快照变更后的重新盖章应提示复审要求（复审+自证），got: %q`, third)
	}
	if !strings.Contains(third, "ADVISORY") {
		t.Errorf(`复审要求应以 ADVISORY 前缀可见（对齐 renderReviewPassBlindSpot 风格），got: %q`, third)
	}

	// Workdir-only delta (pins the OTHER OR-branch): modify source WITHOUT committing —
	// HEAD unchanged, but the workdir change-hash differs from round 3's. Round 4 must
	// still fire (uncommitted fixes are fixes too). Without this segment, deleting the
	// prev.ChangeHash != hash OR-term would leave this test green.
	//
	// 仅工作区增量（钉住 OR 的另一半）：不 commit 只改源码——HEAD 不变，但工作区
	// 变更 hash 与第 3 轮不同。第 4 轮仍必须提示（未提交的修复也是修复）。没有这段，
	// 误删 prev.ChangeHash != hash 这个 OR 项时本测试仍会绿。
	os.WriteFile(filepath.Join(dir, `main.go`), []byte("package main\n\nfunc main() { println(2) }\n"), 0644)
	fourth := captureStdout(t, func() {
		if err := runReviewPassAt(dir, ref, ""); err != nil {
			t.Fatalf("runReviewPassAt 工作区增量重盖: %v", err)
		}
	})
	if !strings.Contains(fourth, "复审") {
		t.Errorf(`工作区增量（HEAD 未动、hash 变）的重新盖章同样欠复审、必须提示，got: %q`, fourth)
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

	if err := runReviewPassAt(dir, ref, ""); err != nil {
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
	if err := runReviewPassAt(dir, `feat/nonexistent`, ""); err == nil {
		t.Fatal("ref 不存在应报错返回（不回落分支 stamp 分支）")
	}
}

// TestRunReviewPassAt_RecordsRounds pins the rework-metric raw material: each `forge review
// pass` appends one ReviewRound to the task state AND records one checklog review-pass entry
// (detail carries the round number) — so the review-rework loop is reconstructible after the
// fact, not just the latest snapshot.
//
// TestRunReviewPassAt_RecordsRounds 钉住返工度量原料：每次 `forge review pass` 既向
// task state 追加一条 ReviewRound，也落一条 checklog review-pass 条目（detail 带轮次
// 号）——审查-返工循环事后可完整重建，而非只剩最后一次快照。
func TestRunReviewPassAt_RecordsRounds(t *testing.T) {
	dir := t.TempDir()
	const ref = `feat/rework-rounds`
	state := &taskpipeline.TaskState{TaskRef: ref, Branch: `feat/rework-rounds`}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err := runReviewPassAt(dir, ref, ""); err != nil {
			t.Fatalf("runReviewPassAt 第 %d 次: %v", i+1, err)
		}
	}

	reloaded, err := taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.ReviewRounds) != 2 {
		t.Fatalf(`两次 pass 应落 2 条 ReviewRound, got %d`, len(reloaded.ReviewRounds))
	}

	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	rounds := 0
	for _, e := range entries {
		if e.Check == checklog.CheckReviewPass && e.TaskRef == ref {
			rounds++
			if !strings.Contains(e.Detail, "review round") {
				t.Errorf(`review-pass detail 应带轮次号: %q`, e.Detail)
			}
		}
	}
	if rounds != 2 {
		t.Errorf(`checklog 应有 2 条 review-pass 条目, got %d`, rounds)
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

// TestRunReviewPassAt_Note pins `forge review pass --note` (usage-log gap: agents
// wanted to leave the review conclusion text at pass time and hit "unknown flag").
// The note must land on BOTH audit surfaces: the appended ReviewRound (task state)
// and the checklog review-pass entry detail. Empty note keeps the legacy shapes.
//
// TestRunReviewPassAt_Note 钉住 `forge review pass --note`（usage 日志缺口：agent 想
// 在 pass 时留审查结论文本却撞 unknown flag）。note 须落到两个审计面：追加的
// ReviewRound（task state）与 checklog review-pass 条目 detail。空 note 保持旧形状。
func TestRunReviewPassAt_Note(t *testing.T) {
	dir := t.TempDir()
	const ref = `feat/review-note`
	state := &taskpipeline.TaskState{TaskRef: ref, Branch: `feat/review-note`}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}

	if err := runReviewPassAt(dir, ref, "审查结论：双轨无发现，快照一致"); err != nil {
		t.Fatalf("runReviewPassAt --note: %v", err)
	}

	reloaded, err := taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.ReviewRounds) != 1 {
		t.Fatalf(`一次 pass 应落 1 条 ReviewRound, got %d`, len(reloaded.ReviewRounds))
	}
	if reloaded.ReviewRounds[0].Note != "审查结论：双轨无发现，快照一致" {
		t.Errorf(`ReviewRound.Note 未持久化, got %q`, reloaded.ReviewRounds[0].Note)
	}

	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Check == checklog.CheckReviewPass && e.TaskRef == ref {
			found = true
			if !strings.Contains(e.Detail, "note: 审查结论：双轨无发现") {
				t.Errorf(`checklog review-pass detail 应带 note: %q`, e.Detail)
			}
		}
	}
	if !found {
		t.Fatal(`checklog 缺 review-pass 条目`)
	}

	// Empty note: ReviewRound.Note stays empty and the detail keeps the legacy shape
	// (no "note:" suffix) — no noise for the overwhelmingly common flagless pass.
	//
	// 空 note：ReviewRound.Note 保持空、detail 保持旧形状（无 "note:" 后缀）——
	// 绝大多数不带 flag 的 pass 不留噪声。
	if err := runReviewPassAt(dir, ref, ""); err != nil {
		t.Fatalf("runReviewPassAt 空 note: %v", err)
	}
	reloaded, err = taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ReviewRounds[1].Note != "" {
		t.Errorf(`空 note 的 ReviewRound.Note 应为空, got %q`, reloaded.ReviewRounds[1].Note)
	}
	entries, _ = checklog.LoadAll(dir)
	var last *checklog.Entry
	for i := range entries {
		if entries[i].Check == checklog.CheckReviewPass && entries[i].TaskRef == ref {
			last = &entries[i]
		}
	}
	if last == nil || strings.Contains(last.Detail, "note:") {
		t.Errorf(`空 note 的 detail 不应带 note 后缀, got %+v`, last)
	}
}

// TestReviewPassNoteFlagRegistered pins the --note flag registration on review pass.
//
// TestReviewPassNoteFlagRegistered 钉住 review pass 的 --note flag 注册。
func TestReviewPassNoteFlagRegistered(t *testing.T) {
	if reviewPassCmd.Flags().Lookup("note") == nil {
		t.Error("review pass 应注册 --note flag")
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
