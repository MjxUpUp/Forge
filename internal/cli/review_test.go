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

// TestRunReviewPassAt_ReworkRoundRequiresRecheck pins the self-refresh guard
// (2026-08-25 gate-loopholes, replacing the 2026-08 post-stamp advisory): when
// `forge review pass` re-stamps a baseline whose SOURCE CONTENT changed since
// the last stamp — the fix-then-recommit loop shape — a bare pass is REFUSED;
// the agent must either record the re-review conclusion (--note, the honest
// flow) or explicitly self-certify (--acknowledge-changes, recorded as a WARN
// self-refresh audit). Non-source deltas (commit-message amend with identical
// content) need no acknowledgment, and a same-state repeat stamp (transient
// retry) stays silent. Rationale: the snapshot loop (task-complete HARD block)
// enforces only the loop's SHAPE; the old advisory let a fixer self-refresh the
// baseline with zero re-review and zero audit trail (real usage-log case: the
// agent answered the HARD block by simply re-running `forge review pass`).
//
// TestRunReviewPassAt_ReworkRoundRequiresRecheck 钉住自助刷新守卫
// （2026-08-25 gate-loopholes，取代 2026-08 的盖章后 advisory）：当
// `forge review pass` 重盖的基线自上次盖章以来【源码内容】已变——修复再提交的
// 循环形状——裸 pass 一律【拒绝】；agent 须记录复审结论（--note，诚实流）或显式
// 自我承担（--acknowledge-changes，记 WARN self-refresh 审计）。非源码增量
// （amend commit message、内容不变）无需确认；同状态重复盖章（瞬态重试）保持
// 静默。动机：快照闭环（task-complete 硬阻断）只强制循环的「形状」；旧
// advisory 让修复者零复审零审计地自助刷新基线（真实 usage 日志案例：agent 被
// HARD 拦后直接自己重跑 `forge review pass` 放行）。
func TestRunReviewPassAt_ReworkRoundRequiresRecheck(t *testing.T) {
	dir := t.TempDir()
	// Real git fixture: the guard trigger is the content-fingerprint delta,
	// which needs a moving HEAD — a bare temp dir has empty head/hash forever
	// and can never fire.
	//
	// 真实 git 夹具：守卫触发条件是内容指纹增量，需要会动的 HEAD——裸临时目录
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

	// Round 1: first stamp — silent (no previous baseline, no acknowledgment owed).
	//
	// 第 1 轮：首次盖章——静默（无既有基线，不欠确认）。
	first := captureStdout(t, func() {
		if err := runReviewPassAt(dir, ref, "", false); err != nil {
			t.Fatalf("runReviewPassAt 第 1 次: %v", err)
		}
	})
	if strings.Contains(first, "复审") {
		t.Errorf(`第 1 轮盖章不应出现复审提示（尚无修复发生），got: %q`, first)
	}

	// Round 2 with NO code change (same-state repeat stamp: transient retry /
	// baseline rebuild) — still silent: no content delta, nothing to confirm.
	//
	// 第 2 轮且无代码变更（同状态重复盖章：瞬态重试/重建基线）——依旧静默：
	// 无内容增量，无需确认。
	repeat := captureStdout(t, func() {
		if err := runReviewPassAt(dir, ref, "", false); err != nil {
			t.Fatalf("runReviewPassAt 无变更重盖: %v", err)
		}
	})
	if strings.Contains(repeat, "复审") {
		t.Errorf(`同状态重复盖章无需确认、应静默（触发条件是内容增量非轮次计数），got: %q`, repeat)
	}

	// Fix + commit → content delta. A BARE re-stamp must now be REFUSED (the
	// loophole closed), and the refusal must point at the re-review protocol.
	//
	// 修复 + 提交 → 内容增量。裸盖章现在必须被【拒绝】（漏洞关闭），且拒绝
	// 文案须指明复审协议。
	os.WriteFile(filepath.Join(dir, `main.go`), []byte("package main\n\nfunc main() { println(1) }\n"), 0644)
	runGit(t, dir, `add`, `.`)
	runGit(t, dir, `commit`, `-m`, `fix: apply review findings`)
	if err := runReviewPassAt(dir, ref, "", false); err == nil {
		t.Fatal("快照变更后的裸盖章必须被拒绝（self-refresh 漏洞）")
	} else if !strings.Contains(err.Error(), "复审") || !strings.Contains(err.Error(), "acknowledge-changes") {
		t.Errorf("拒绝文案须指明复审协议与确认途径，got: %v", err)
	}
	// The refusal must not have stamped: still exactly 2 rounds.
	//
	// 拒绝不得落章：仍恰为 2 轮。
	if st, err := taskpipeline.LoadTaskState(dir, ref); err != nil || len(st.ReviewRounds) != 2 {
		t.Fatalf("被拒绝的盖章不得追加 ReviewRound, err=%v rounds=%v", err, st)
	}

	// Honest flow: re-review done, conclusion recorded via --note → stamp
	// succeeds; the entry is an ordinary pass (note IS the acknowledgment), no
	// self-refresh marker.
	//
	// 诚实流：复审已做、结论经 --note 记录 → 盖章成功；条目为普通 pass
	// （note 即确认），无 self-refresh 标记。
	if err := runReviewPassAt(dir, ref, "复审结论：修复正确，无新发现", false); err != nil {
		t.Fatalf("--note 记复审结论的盖章应放行: %v", err)
	}

	// Workdir-only delta (uncommitted fix) → bare pass refused again;
	// --acknowledge-changes stamps AND records a WARN self-refresh audit.
	//
	// 仅工作区增量（未提交修复）→ 裸盖章再次被拒；--acknowledge-changes
	// 放行并记 WARN self-refresh 审计。
	os.WriteFile(filepath.Join(dir, `main.go`), []byte("package main\n\nfunc main() { println(2) }\n"), 0644)
	if err := runReviewPassAt(dir, ref, "", false); err == nil {
		t.Fatal("工作区增量的裸盖章同样必须被拒绝")
	}
	if err := runReviewPassAt(dir, ref, "", true); err != nil {
		t.Fatalf("--acknowledge-changes 显式确认应放行: %v", err)
	}
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	var last *checklog.Entry
	for i := range entries {
		if entries[i].Check == checklog.CheckReviewPass && entries[i].TaskRef == ref {
			last = &entries[i]
		}
	}
	if last == nil || last.Level != checklog.LevelWarn || !strings.Contains(last.Detail, "self-refresh") {
		t.Errorf("--acknowledge-changes 的盖章须记 WARN self-refresh 审计, got %+v", last)
	}

	// Non-source delta: amend ONLY the commit message (content identical) →
	// bare pass stays allowed and silent — the guard distinguishes source
	// content changes from non-source ones (no false positive on amends).
	//
	// 非源码增量：只 amend commit message（内容不变）→ 裸盖章照常放行且静默
	// ——守卫区分源码内容变更与非源码变更（amend 不误伤）。
	runGit(t, dir, `add`, `.`)
	runGit(t, dir, `commit`, `-m`, `fix: workdir fix`)
	runGit(t, dir, `commit`, `--amend`, `-m`, `fix: workdir fix (message only)`)
	silent := captureStdout(t, func() {
		if err := runReviewPassAt(dir, ref, "", false); err != nil {
			t.Fatalf("仅 commit message 变更（内容不变）不应要求确认: %v", err)
		}
	})
	if strings.Contains(silent, "复审") || strings.Contains(silent, "self-refresh") {
		t.Errorf("非源码变更（amend message）应静默放行，got: %q", silent)
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

	if err := runReviewPassAt(dir, ref, "", false); err != nil {
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
	if err := runReviewPassAt(dir, `feat/nonexistent`, "", false); err == nil {
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
		if err := runReviewPassAt(dir, ref, "", false); err != nil {
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

	if err := runReviewPassAt(dir, ref, "审查结论：双轨无发现，快照一致", false); err != nil {
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
	if err := runReviewPassAt(dir, ref, "", false); err != nil {
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

// TestRunReviewPassAt_BaselineUnreachableAudited pins the fail-open audit trail
// (review minor #2): when the previous review baseline commit is unreachable
// (history rewritten by amend/rebase), a bare `forge review pass` re-stamps
// fail-open — but that fail-open must NOT be silent: the checklog review-pass
// entry upgrades to WARN with a baseline-unreachable marker, aligning with the
// executor's fail-open which also persists a checklog entry.
//
// TestRunReviewPassAt_BaselineUnreachableAudited 钉住 fail-open 的审计留痕
// （review minor #2）：上次审查基线 commit 不可达（amend/rebase 改写历史）时，
// 裸 `forge review pass` 按 fail-open 重盖章——但 fail-open 不得静默：
// checklog review-pass 条目升级为 WARN 并打 baseline-unreachable 标记，与
// executor fail-open 同样落 checklog 的做法对齐。
func TestRunReviewPassAt_BaselineUnreachableAudited(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, `init`)
	runGit(t, dir, `config`, `user.email`, `test@test.com`)
	runGit(t, dir, `config`, `user.name`, `Test`)
	os.WriteFile(filepath.Join(dir, `main.go`), []byte("package main\n\nfunc main() {}\n"), 0644)
	runGit(t, dir, `add`, `.`)
	runGit(t, dir, `commit`, `-m`, `initial`)

	const ref = `feat/unreachable-base`
	state := &taskpipeline.TaskState{TaskRef: ref, Branch: ref}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}

	// First stamp establishes a real baseline.
	//
	// 首次盖章建立真实基线。
	if err := runReviewPassAt(dir, ref, "", false); err != nil {
		t.Fatalf("首次盖章: %v", err)
	}

	// Rewrite the state's baseline to a commit that never existed — simulating
	// a baseline whose object was rewritten away (cheaper and more
	// deterministic than reflog-expire + gc to make a real commit unreachable).
	//
	// 把 state 里的基线改写成一个从不存在的 commit——模拟对象被改写掉的基线
	// （比 reflog expire + gc 让真实 commit 不可达更省事、更确定）。
	st, err := taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	st.ReviewedHeadCommit = `deadbeefdeadbeefdeadbeefdeadbeefdeadbeef`
	if err := taskpipeline.SaveTaskState(dir, st); err != nil {
		t.Fatal(err)
	}

	// Bare re-stamp: fail-open (no refusal), but a WARN baseline-unreachable
	// audit entry must be recorded.
	//
	// 裸重盖章：fail-open（不拒绝），但必须记 WARN 级 baseline-unreachable
	// 审计条目。
	if err := runReviewPassAt(dir, ref, "", false); err != nil {
		t.Fatalf("基线不可达应 fail-open 放行: %v", err)
	}
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	var last *checklog.Entry
	for i := range entries {
		if entries[i].Check == checklog.CheckReviewPass && entries[i].TaskRef == ref {
			last = &entries[i]
		}
	}
	if last == nil {
		t.Fatal("缺 review-pass 条目")
	}
	if last.Level != checklog.LevelWarn || !strings.Contains(last.Detail, "baseline-unreachable") {
		t.Errorf("fail-open 重盖章须记 WARN 级 baseline-unreachable 审计, got level=%s detail=%q", last.Level, last.Detail)
	}
}
