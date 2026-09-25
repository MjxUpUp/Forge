package taskpipeline

// acceptance_register.go — oracle-pipeline L1（考卷层级制 + 零验收硬前置 +
// conventions 兜底）。设计：正确性的传递链把「考卷」的来源分级（见 tasktypes
// AcceptanceSource* 梯子），并在 complete 边界强制「无考卷不得交付」——2026-09-07
// 实证任务以 ratio 0.08 完成只因零登记（executor acceptance advisory 自陈的缺口），
// deterministic 核心不能停留在「选装」。

import (
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/conventions"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// 考卷层级常量重导出（model_alias.go 同款路径——cli 消费方不直接 import tasktypes；
// 定义与层级语义的单一真相源在 tasktypes.AcceptanceCriterion 旁）。
const (
	AcceptanceSourceSpecExtract = tasktypes.AcceptanceSourceSpecExtract
	AcceptanceSourceStart       = tasktypes.AcceptanceSourceStart
	AcceptanceSourceConventions = tasktypes.AcceptanceSourceConventions
	AcceptanceSourceManual      = tasktypes.AcceptanceSourceManual
)

// CheckAcceptanceQuality is task-complete's exam-quality pre-flight
// (delivery-hardening 墙-2a): the registration gate closes ZERO exams; this one
// closes the CHEAPEST weak ones — an exam must contain at least one TEST-class
// command (selfreport's testCmdPrefixes list), otherwise `go build` alone
// constitutes a "passing" exam. Downgrades to a no-op when the repo has no test
// capability at all (CheckTestCapability.HasTests==0 — toy/fixture repos
// legitimately carry no tests; hard-blocking there would be a false wall).
// Escape shares the acceptance-gate hatch.
//
// CheckAcceptanceQuality 是 task-complete 的考卷质量前置（delivery-hardening
// 墙-2a）：登记门关"零考卷"，本检查关"最廉价的弱考卷"——考卷须含至少一条
// 测试类命令（selfreport 的 testCmdPrefixes 清单），否则单条 go build 就构成
// "通过的考卷"。仓库完全无测试能力时跳过（CheckTestCapability.HasTests==0
// ——玩具/fixture 仓合法地没有测试，硬拦是假墙）。逃生共用 acceptance-gate 舱。
func CheckAcceptanceQuality(root string, state *TaskState) (ok bool, reasons []string) {
	if state == nil || state.IsGeneric() || len(state.Acceptance) == 0 {
		return true, nil // 零考卷由登记门管；此处只管质量
	}
	if escapeDisabled(state, escapeAcceptanceGate, acceptanceGateDisableEnv) {
		return true, nil // 逃生行已由登记门/freshness 门落过，不重复记
	}
	for _, c := range state.Acceptance {
		if isTestClassCommand(c.Run) {
			return true, nil
		}
	}
	cap := CheckTestCapability(root)
	if !cap.HasTests {
		return true, nil // 无测试能力的仓库：质量要求无的放矢（test-capability advisory 层已提醒）
	}
	return false, []string{
		fmt.Sprintf("考卷 %d 条标准中无任何测试类命令（go test/pytest/cargo test 等）——单条构建命令不构成合格考卷（考卷质量下限）。补登记：forge task accept \"go test ./... :: ok\"；确属纯构建/文档任务逃生（落审计）: forge task override --acceptance-gate disable 或 FORGE_ACCEPTANCE_GATE=disable", len(state.Acceptance)),
	}
}

// isTestClassCommand 报告命令是否测试类。清单单一真相源是 selfreport 的
// testCmdPrefixes，但排除 `go vet`（审查 P2-3：vet 是静态分析零断言执行，
// 与"单条 go build 不构成考卷"的立法动机同构）；匹配经 commandSegments
// 归一（`cd pkg && go test` / `FOO=1 go test` 也命中——与 selfreport 同一
// 分段器，防"声称口径共享、实际只共享清单"的漂移）。
func isTestClassCommand(run string) bool {
	for _, seg := range commandSegments(run) {
		for _, p := range testCmdPrefixes {
			if p == "go vet" {
				continue
			}
			if strings.HasPrefix(seg, p) {
				return true
			}
		}
	}
	return false
}

// StampAcceptanceSource stamps empty-Source criteria with the given tier, in
// place. Never overwrites an existing Source: relabeling a post-hoc exam
// (manual) as a pre-code one (start/spec-extract) would launder the weakest
// tier into the strongest — the stamp is write-once at the registration
// point by design.
//
// StampAcceptanceSource 就地把 Source 为空的验收标准盖成给定层级。绝不覆写
// 已有 Source：把事后补登（manual）改标成先于代码（start/spec-extract）等于把
// 最弱层洗成最强层——盖章按设计只在登记点发生一次。
func StampAcceptanceSource(cs []AcceptanceCriterion, source string) {
	for i := range cs {
		if cs[i].Source == "" {
			cs[i].Source = source
		}
	}
}

// CheckAcceptanceRegistered is task-complete's registration pre-flight
// (oracle-pipeline L1): a non-generic delivery task must carry at least one
// registered acceptance criterion before it can complete. Zero criteria =
// the whole deterministic acceptance core silently no-ops (verify-acceptance
// returns nil, CheckAcceptanceFresh passes on the empty set) — the 2026-09-07
// incident shipped a ratio-0.08 completion through exactly that gap. Escape
// reuses the acceptance-gate hatch (per-task override / FORGE_ACCEPTANCE_GATE
// =disable) with the usual audited row; generic tasks are exempt (no code
// gates apply to them).
//
// CheckAcceptanceRegistered 是 task-complete 的登记前置（oracle-pipeline L1）：
// 非 generic 交付任务 complete 前必须至少登记一条验收标准。零标准 = 整个
// deterministic 验收核心静默空转（verify-acceptance 直接返回、CheckAcceptanceFresh
// 对空集放行）——2026-09-07 事故正是从这个缺口以 ratio 0.08 完成交付。逃生复用
// acceptance-gate 舱（per-task override / FORGE_ACCEPTANCE_GATE=disable）并照常落
// 审计行；generic 任务豁免（本就不走代码门禁）。
func CheckAcceptanceRegistered(root string, state *TaskState) (ok bool, reasons []string) {
	if state == nil || state.IsGeneric() {
		return true, nil
	}
	if len(state.Acceptance) > 0 {
		return true, nil
	}
	if escapeDisabled(state, escapeAcceptanceGate, acceptanceGateDisableEnv) {
		row := checklog.EscapeHatchEntry("acceptance-gate", checklog.EscapeReasonOverride, state.TaskRef,
			`escape-hatch: acceptance registration gate bypassed — task completed with ZERO registered acceptance criteria (per-task override or FORGE_ACCEPTANCE_GATE=disable)`)
		row.TaskRef = state.TaskRef
		recordAudit(root, row)
		return true, nil
	}
	return false, []string{
		`非 generic 交付任务零验收标准——考卷缺位：verify-acceptance 与 complete 的验收链对空集全部平凡通过，deterministic 核心等于没开`,
	}
}

// ConventionsDefaultAcceptance builds the fallback exam from the project's
// conventions profile (build/test/lint commands), stamped conventions-tier.
// This is the bottom rung of the oracle ladder: when nothing was registered,
// verify-acceptance falls back to the project's OWN declared commands rather
// than the implementer's pick — the data already lives in the profile
// (forge conventions init); this wires it into the acceptance pipeline.
// Returns (nil, nil) when no profile exists (caller keeps the old
// zero-criteria behavior; the registration gate then blocks complete) and
// (nil, err) when the profile is present but unreadable/corrupt — the caller
// must NOT tell the user to rebuild an innocent profile for a read failure.
//
// ConventionsDefaultAcceptance 从项目 conventions 档案（build/test/lint 命令）
// 构建兜底考卷，盖 conventions 层。这是考卷梯子的最底一级：零登记时
// verify-acceptance 回落到项目自己声明的命令，而不是实现者自选——数据本就在
// 档案里（forge conventions init 扫的），此处只是接进验收管线。无档案返回
// (nil, nil)（调用方保持旧的零标准行为；登记门届时在 complete 拦截）；档案在
// 但读不出/损坏返回 (nil, err)——调用方不得把读失败说成「无档案可建」。
func ConventionsDefaultAcceptance(root string) ([]AcceptanceCriterion, error) {
	profile, err := conventions.LoadProfile(forgedata.DataDirFor(root))
	if err != nil {
		return nil, fmt.Errorf("conventions 档案读取失败: %w", err)
	}
	if profile == nil {
		return nil, nil
	}
	var cs []AcceptanceCriterion
	for _, cmd := range []string{profile.BuildCmd, profile.TestCmd, profile.LintCmd} {
		if cmd == "" {
			continue
		}
		cs = append(cs, AcceptanceCriterion{Run: cmd, Source: AcceptanceSourceConventions})
	}
	return cs, nil
}

// FormatAcceptanceTier renders one criterion's tier for human-facing surfaces
// (report / gate messages): empty Source renders as the legacy start-tier it
// behaves as, so legacy states never display a blank tier.
//
// FormatAcceptanceTier 渲染单条标准的层级供人读表面（验收单/门禁文案）：空
// Source 按其实际行为渲染成 start 层——存量 state 永远不显示空白层级。
func FormatAcceptanceTier(source string) string {
	if source == "" {
		return AcceptanceSourceStart
	}
	return source
}

// AcceptanceTierCounts summarizes a task's acceptance by tier — the report's
// "考卷来源" line: how many criteria came from each rung of the ladder.
//
// AcceptanceTierCounts 按层级汇总任务的验收标准——验收单「考卷来源」行的数据：
// 梯子的每一级各出了几道题。
func AcceptanceTierCounts(cs []AcceptanceCriterion) map[string]int {
	counts := map[string]int{}
	for _, c := range cs {
		counts[FormatAcceptanceTier(c.Source)]++
	}
	return counts
}

// HasManualTierAcceptance reports whether any criterion was authored post-hoc
// (manual tier) — the delivery report's disclosure trigger: the implementer
// wrote part of the exam after seeing the code, and the acceptor deserves
// that fact in one glance instead of per-criterion tier labels.
//
// HasManualTierAcceptance 报告是否存在事后补登（manual 层）的标准——验收单的
// 披露触发器：实现者看过代码之后才出了部分考卷，验收方值得一眼看到这个事实，
// 而不是逐条读层级标签。
func HasManualTierAcceptance(cs []AcceptanceCriterion) bool {
	for _, c := range cs {
		if c.Source == AcceptanceSourceManual {
			return true
		}
	}
	return false
}

// RegisterAcceptance merges new criteria into the task's acceptance under the
// per-task lock, stamping the given tier on the ADDED entries (StampAcceptance
// Source fills only empty Sources — a pre-set Source on an addition passes
// through unchanged; current callers all pass empty-Source criteria). The
// closure re-checks CompletedAt under the lock: the exam is finalized at
// delivery, so late registration is refused even inside the race window
// between an outside check and the merge. Returns the added subset (post-merge,
// Source stamped) so the caller can print exactly what entered the exam.
// Dedup by Run keeps re-registration of an existing command a no-op (its
// original tier survives).
//
// RegisterAcceptance 在 per-task 锁内把新标准合并进任务验收集，给实际新增的
// 条目盖给定层级（StampAcceptanceSource 只盖空 Source——addition 自带非空
// Source 的原样穿透；当前调用方都传空 Source 条目）。闭包在锁内复查
// CompletedAt：考卷在交付时定稿，外部检查与合并之间的竞态窗口内的补登同样
// 拒绝。返回新增子集（已盖 Source）供调用方精确打印进了什么题。按
// (Run, Expected, Assertions) 三元组去重——重复登记同一检查是 no-op（其原层级保留）。
func RegisterAcceptance(root, taskRef string, addition []AcceptanceCriterion, source string) ([]AcceptanceCriterion, error) {
	var added []AcceptanceCriterion
	err := MutateTaskState(root, taskRef, func(s *TaskState) error {
		if s.CompletedAt != nil {
			return fmt.Errorf("任务已完成——考卷在交付前定稿，锁内拒绝补登（发现问题的正确出口是开新修复任务并带上失败的验收命令）")
		}
		base := len(s.Acceptance)
		StampAcceptanceSource(addition, source)
		s.Acceptance = MergeAcceptance(s.Acceptance, addition)
		added = s.Acceptance[base:]
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("登记验收标准失败: %w", err)
	}
	return added, nil
}
