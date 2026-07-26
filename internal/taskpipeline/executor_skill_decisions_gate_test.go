package taskpipeline

// executor_skill_decisions_gate_test.go — integration test for the skill-decisions guardrail
// at the task-verify gate layer (component B). Mirrors the review snapshot / test-coverage
// hard-gate test pattern: real git repo + ExecuteTaskGate full chain. Nails three key points:
// editing SKILL.md without recording a decision -> BLOCKED; recording a decision -> pass;
// escape-hatch -> pass + CheckEscapeHatch recorded (Weak ceiling cost).
//
// Base snapshot point (critical): base = headShort is taken AFTER the base-skill commit —
// by then foo skill's SKILL.md v1 + decisions.md (1 entry) are in place, so the base-version
// decisions.md has 1 entry as the counting anchor. If taken at the init commit (foo skill not
// yet created), gitShowPath(base, decisions.md) reads empty -> old=0 -> cur(1)>0=true ->
// false recorded pass, BLOCKED test goes green spuriously.
//
// executor_skill_decisions_gate_test.go — skill-decisions guardrail 在 task-verify 门禁层的
// 集成测试（B 组件）。镜像 review snapshot / test-coverage 硬门禁测试模式：真实 git 仓库 +
// ExecuteTaskGate 全链路。钉死三个关键点：改 SKILL.md 未记决策→BLOCKED；记决策→pass；
// escape-hatch→pass + 落 CheckEscapeHatch（Weak ceiling 代价）。
//
// base 取值点（关键）：base = headShort 在"base skill"commit **之后**取——此时 foo skill 的
// SKILL.md v1 + decisions.md(1 条) 已就位，base 版本 decisions.md 才有 1 条作计数锚点。
// 若在 init commit 取（foo skill 还没建），gitShowPath(base, decisions.md) 读到空→old=0→
// cur(1)>0=true→误判「已记」放行，BLOCKED 测试假绿。

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// TestTaskVerify_SkillDecisionsGuardrail_BlockOnUnrecorded edits SKILL.md (a behavior change)
// but adds no new decision in decisions.md this task -> task-verify BLOCKED. This is the core
// blocking point of component B — advisory-zero-triggered (dogfood rule), must be blocking to
// make skill-decisions enforced.
//
// TestTaskVerify_SkillDecisionsGuardrail_BlockOnUnrecorded 改 SKILL.md（行为变更）但本 task 未在
// decisions.md 新增决策 → task-verify BLOCKED。这是 B 组件的核心阻断点——advisory 0 触发
// （dogfood 铁律），必须 blocking 才让 skill-decisions 强制生效。
func TestTaskVerify_SkillDecisionsGuardrail_BlockOnUnrecorded(t *testing.T) {
	dir := initTaskGitRepo(t)
	// Base already has foo skill: SKILL.md v1 + decisions.md 1 entry (base..HEAD counting anchor).
	//
	// base 已有 foo skill：SKILL.md v1 + decisions.md 1 条（base..HEAD 计数锚点）。
	writeSrc(t, dir, "skills/foo/SKILL.md", "v1\n")
	writeSrc(t, dir, "skills/foo/decisions.md", "## [d-base1] accept\nbase 决策\n")
	commitAll(t, dir, "base skill")
	base := headShort(t, dir) // task start HEAD = foo skill 已就位（base 版 decisions.md 有 1 条）

	// Edit SKILL.md (behavior change) + commit -> HEAD moves, taskChangedFiles(base..HEAD)
	// includes SKILL.md. decisions.md keeps base's 1 entry (no new decision recorded).
	//
	// 改 SKILL.md（行为变更）+ commit → HEAD 移动，taskChangedFiles(base..HEAD) 含 SKILL.md。
	// decisions.md 保持 base 的 1 条（未记新决策）。
	writeSrc(t, dir, "skills/foo/SKILL.md", "v2 changed\n")
	commitAll(t, dir, "edit SKILL.md")

	state := fullyGatedState("sd-block")
	state.HeadCommit = base // task start 时 HEAD=base
	// Isolate the work-activity gate (0 tool uses would BLOCK first, ahead of skill-decisions)
	// — this test focuses on the skill-decisions guardrail; work-activity is out of scope, skip
	// via its escape hatch.
	//
	// 隔离 work-activity 门禁（0 tool uses 会先 BLOCKED，挡在 skill-decisions 之前）——本测试
	// 专注 skill-decisions guardrail，work-activity 不在测试范围，用其逃生舱跳过。
	state.Overrides.WorkActivity = "disable"

	_, err := ExecuteTaskGate(dir, "task-verify", state)
	if err == nil {
		t.Fatal(`改 SKILL.md 未记决策应 BLOCKED，实际放行——guardrail 失效（advisory 0 触发，必须 blocking）`)
	}
	if !strings.Contains(err.Error(), "SKILL.md") || !strings.Contains(err.Error(), "decisions.md") {
		t.Fatalf(`拒绝原因应含 SKILL.md 和 decisions.md，got: %v`, err)
	}
	// P1: BLOCKED must hit disk — checklog has CheckNameSkillDecisions Passed=false (so
	// score/dashboard/audit can surface that skill-decisions blocked here, aligning with
	// test-coverage BLOCKED recording checklog first).
	//
	// P1：BLOCKED 必落盘——checklog 有 CheckNameSkillDecisions Passed=false（让 score/dashboard/
	// audit 照出「skill-decisions 阻断过」，对齐 test-coverage BLOCKED 先记 checklog）。
	entries, _ := checklog.LoadForTask(dir, "sd-block")
	var blocked bool
	for _, e := range entries {
		if e.Check == CheckNameSkillDecisions && !e.Passed {
			blocked = true
			break
		}
	}
	if !blocked {
		t.Fatalf(`BLOCKED 应落 CheckNameSkillDecisions Passed=false 留痕，实际无——audit 照不出阻断`)
	}
}

// TestTaskVerify_SkillDecisionsGuardrail_PassWhenRecorded edits SKILL.md and the working-copy
// decisions.md adds a new entry (uncommitted) -> task-verify passes. Mirrors the real flow:
// an agent recording a decision may not commit immediately; skillDecisionsRecorded reads the
// working-copy file (including uncommitted) to avoid false negatives.
//
// TestTaskVerify_SkillDecisionsGuardrail_PassWhenRecorded 改 SKILL.md 且工作区 decisions.md
// 新增决策条目（未 commit）→ task-verify 过。镜像真实流：agent 记决策后未必立即 commit，
// skillDecisionsRecorded 读工作区文件（含未提交）才不漏判。
func TestTaskVerify_SkillDecisionsGuardrail_PassWhenRecorded(t *testing.T) {
	dir := initTaskGitRepo(t)
	writeSrc(t, dir, "skills/foo/SKILL.md", "v1\n")
	writeSrc(t, dir, "skills/foo/decisions.md", "## [d-base1] accept\nbase\n")
	commitAll(t, dir, "base skill")
	base := headShort(t, dir)

	writeSrc(t, dir, "skills/foo/SKILL.md", "v2 changed\n")
	commitAll(t, dir, "edit SKILL.md")
	// Working-copy new decision (uncommitted) -> currentDecisionsContent reads the working
	// copy and picks up the 2nd entry > base's 1 entry.
	//
	// 工作区新增决策（未 commit）→ currentDecisionsContent 读工作区抓到第 2 条 > base 1 条。
	writeSrc(t, dir, "skills/foo/decisions.md", "## [d-base1] accept\n## [d-new1] accept\n新决策\n")

	state := fullyGatedState("sd-pass")
	state.HeadCommit = base
	state.Overrides.WorkActivity = "disable" // 隔离 work-activity 门禁，专注测 skill-decisions

	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf(`已记决策应过（工作区未提交条目也算），got: %v`, err)
	}
}

// TestTaskVerify_SkillDecisionsGuardrail_EscapeHatchBypasses edits SKILL.md without recording
// a decision + per-task override --skill-decisions disable -> not blocked + CheckEscapeHatch
// recorded (Weak ceiling cost). Nails the escape hatch: trivial edits (typo/format) have an
// exit, but using it pays an evidence-downgrade cost (prevents the hard-gate + global escape
// hatch = fake hard gate blowback).
//
// TestTaskVerify_SkillDecisionsGuardrail_EscapeHatchBypasses 改 SKILL.md 未记决策 + per-task
// override --skill-decisions disable → 不阻断 + 落 CheckEscapeHatch（Weak ceiling 代价）。
// 钉死逃生舱：trivial 改动（typo/格式）有出路，但用了要付 evidence 降级代价（防「硬门禁+
// 全局逃生舱=假硬门禁」反噬）。
func TestTaskVerify_SkillDecisionsGuardrail_EscapeHatchBypasses(t *testing.T) {
	dir := initTaskGitRepo(t)
	writeSrc(t, dir, "skills/foo/SKILL.md", "v1\n")
	writeSrc(t, dir, "skills/foo/decisions.md", "## [d-base1] accept\nbase\n")
	commitAll(t, dir, "base skill")
	base := headShort(t, dir)
	writeSrc(t, dir, "skills/foo/SKILL.md", "v2 changed\n")
	commitAll(t, dir, "edit SKILL.md")

	state := fullyGatedState("sd-escape")
	state.HeadCommit = base
	state.Overrides.SkillDecisions = "disable" // 镜像 forge task override --skill-decisions disable
	state.Overrides.WorkActivity = "disable"   // 隔离 work-activity 门禁（也走 escape-hatch，与本测试并存）

	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf(`escape-hatch 应放行，got: %v`, err)
	}
	// Escape hatch must hit disk — assert checklog has a CheckEscapeHatch entry for
	// skill-decisions (work-activity also escapes but its detail omits the skill-decisions
	// token; this precisely anchors the skill-decisions one). Lets score/dashboard surface
	// that the hatch was used rather than truly recording, and offsets the evidence Strength
	// cap=Weak cost.
	//
	// 逃生舱必须落盘——断言 checklog 有 skill-decisions 的 CheckEscapeHatch 条目（work-activity
	// 也走 escape 但其 detail 不含"skill-decisions"，此处精确锚定 skill-decisions 那条）。
	// 让 score/dashboard 照出「靠逃生舱而非真记决策」，并对冲 evidence Strength cap Weak 的代价。
	entries, _ := checklog.LoadForTask(dir, "sd-escape")
	var found bool
	for _, e := range entries {
		if e.Check == checklog.CheckEscapeHatch && strings.Contains(e.Detail, "skill-decisions") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf(`skill-decisions escape-hatch 应落 CheckEscapeHatch 留痕（detail 含 skill-decisions），实际无——score/dashboard 照不出"靠逃生舱绕过 guardrail"`)
	}
}

// TestTaskVerify_SkillDecisionsGuardrail_FailOpenDetailHonest base commit unreachable
// (amend/rebase rewrites history, git object vanishes) + working copy edits SKILL.md
// (uncommitted) -> taskChangedFiles source 2 (diff HEAD) still captures SKILL.md -> blocking
// set non-empty -> skillDecisionsRecorded fail-open -> not blocked (aligns with the review
// snapshot philosophy) + pass-path Detail honestly marks fail-open skipped check, not
// claiming decision recorded (fail-open didn't truly verify recorded; audit must distinguish
// truly recorded vs slipped through via fail-open — structured-log accuracy).
//
// TestTaskVerify_SkillDecisionsGuardrail_FailOpenDetailHonest base commit 不可达（amend/rebase
// 改写历史致 git 对象消失）+ 工作区改 SKILL.md（未 commit）→ taskChangedFiles 源2（diff HEAD）
// 仍捕获 SKILL.md → blocking 非空 → skillDecisionsRecorded fail-open → 不阻断（对齐 review
// snapshot 哲学）+ 通过路径 Detail **诚实**标「fail-open 跳过校验」，不宣称「已记决策」
// （fail-open 没真验证 recorded，audit 须能区分「真记」vs「fail-open 溜过」——结构化日志准确性）。
func TestTaskVerify_SkillDecisionsGuardrail_FailOpenDetailHonest(t *testing.T) {
	dir := initTaskGitRepo(t)
	writeSrc(t, dir, "skills/foo/SKILL.md", "v1\n")
	writeSrc(t, dir, "skills/foo/decisions.md", "## [d-base1] accept\nbase\n")
	commitAll(t, dir, "base skill")

	// Working-copy edit to SKILL.md (uncommitted) -> taskChangedFiles source 2 (git diff HEAD)
	// captures skills/foo/SKILL.md.
	//
	// 工作区改 SKILL.md（未 commit）→ taskChangedFiles 源2（git diff HEAD）捕获 skills/foo/SKILL.md。
	writeSrc(t, dir, "skills/foo/SKILL.md", "v2 changed\n")

	state := fullyGatedState("sd-failopen")
	state.HeadCommit = "deadbeefnotarealcommit123" // 不可达 base（amend/rebase 改写致对象消失）
	state.Overrides.WorkActivity = "disable"       // 隔离 work-activity 门禁，专注测 skill-decisions

	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf(`base 不可达应 fail-open 放行（amend/rebase 正常流），got: %v`, err)
	}
	// Pass-path Detail is honest: marks fail-open, not claiming decision recorded (did not
	// truly verify recorded).
	//
	// 通过路径 Detail 诚实：标"fail-open"，不宣称「已记决策」（未真验证 recorded）。
	entries, _ := checklog.LoadForTask(dir, "sd-failopen")
	var detail string
	for _, e := range entries {
		if e.Check == CheckNameSkillDecisions && e.Passed {
			detail = e.Detail
			break
		}
	}
	if detail == "" {
		t.Fatalf(`fail-open 通过路径应落 CheckNameSkillDecisions Passed=true 条目，实际无`)
	}
	if !strings.Contains(detail, "fail-open") {
		t.Fatalf(`fail-open 通过路径 Detail 应标 "fail-open"，got: %q`, detail)
	}
	if strings.Contains(detail, "已记决策") || strings.Contains(detail, "已在本 task 记决策") {
		t.Fatalf(`fail-open 不应宣称"已记决策"（未真验证 recorded），got: %q`, detail)
	}
}
