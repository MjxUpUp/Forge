package taskpipeline

// executor_check_verify_scans.go — ExecuteTaskGate 拆分（refactor/executor-pipeline 第一步）：
// task-verify 的机械扫描段（cheat-scan / doc-gate 提前量 / unused-scan / conventions-lint）。
// 代码体自 executor.go 的 ExecuteTaskGate 原样提取，行为等价——仅变量引用改为参数名
// （findingsDirty 由 scanCheatFindings 产出、经参数传入 scanUnusedFindings，两段之后的
// 持久化保持在 unused-scan 段末尾原位执行，顺序不变）。
//
// executor_check_verify_scans.go — ExecuteTaskGate decomposition
// (refactor/executor-pipeline step 1): task-verify's mechanical-scan section (cheat-scan /
// the doc-gate early nudge / unused-scan / conventions-lint). Bodies were extracted
// verbatim from ExecuteTaskGate in executor.go — behavior-equivalent; only variable
// references became parameter names (findingsDirty is produced by scanCheatFindings,
// passed into scanUnusedFindings, and the once-only persist still runs in its original
// position at the end of the unused-scan section — order unchanged).

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// scanCheatFindings runs the cheat-scan (advisory): mechanically detect the AI-cheat
// patterns enumerated in ScanCheatPatterns (cheatscan.go — the single source of truth for
// the count and list; currently 7: type-suppression / error-swallow / dead-branch /
// comment-only-fix / comment-as-debt / phantom-import / path-assumption). The first 4 were
// previously judged by an LLM sub-agent at code-review time — the LLM re-samples the same
// diff each round and catches different subsets, which is the source of the 'every review
// round raises new issues' perception; this scan pulls them into deterministic detection.
// comment-as-debt catches 'the comment flags a problem but does not solve it'
// (the anti-pattern at level 0 of the laziness ladder, the root of code rot) — the nudge
// below spells out the handling path (convert to a forge task or fix it inline) for the
// agent. Scans task-added lines (+ lines); hits go to checklog:cheat-scan. Purely
// advisory (heuristics can false-positive — comment-only especially) and never blocking;
// the trace is left for review verification.
// CheckCheatScan is excluded from BuildEvidenceChain — it is an observation, not
// 'verification evidence'; counting it would inflate Strength. The LLM-reviewer accordingly
// retreats to semantic judgments only (design / architecture / whether mocks are illusory).
//
// It returns findingsDirty: whether new fingerprints entered the per-task reported set —
// the caller threads it into scanUnusedFindings, which persists the state once after both
// scan sections.
//
// scanCheatFindings 跑 cheat-scan（advisory）：机械检测 ScanCheatPatterns 枚举的 AI 作弊模式
// （数量与清单以 cheatscan.go 为唯一真相源，当前 7 类：type-suppression / error-swallow /
// dead-branch / comment-only-fix / comment-as-debt / phantom-import / path-assumption）。
// 前 4 类此前全靠 LLM 子 agent 在 code-review 时判断——LLM 每轮对同一 diff 重新采样
// 抓不同子集，是「每轮 review 冒新问题」的体感来源；本扫描把它们抽到
// deterministic。comment-as-debt 抓「注释标识问题但不解决」（懒惰阶梯
// 反第 0 级，屎山根源）——下方 nudge 把处置路径（转 forge task 或当场修）明确
// 告诉 agent。扫任务新增行（+ 行），命中记 checklog:cheat-scan。纯 advisory
// （启发式有假阳性可能——comment-only 尤甚）绝不阻塞，留痕供 review 核查。
// CheckCheatScan 在 BuildEvidenceChain 中被排除——它是观测非「验证证据」，计入
// 会虚高 Strength。LLM-reviewer 据此退到只做语义判断（设计/架构/mock 是否幻觉）。
// 返回 findingsDirty：是否有新指纹入集合——由调用方传入 scanUnusedFindings，
// 两段之后统一持久化一次。
func scanCheatFindings(root string, state *TaskState) (findingsDirty bool) {
	cheats := ScanCheatPatterns(root, state)
	// findingsDirty 汇总 cheat/unused 两段是否有新指纹入集合，两段之后统一持久化一次。
	//
	// findingsDirty accumulates whether either scan section added new fingerprints;
	// the state is persisted once after both sections.
	findingsDirty = false
	// Same-finding suppression (2026-08 noise audit) — run BEFORE the checklog record so
	// the audit entry carries the fresh/suppressed breakdown (dedupSuffix): the
	// entry stays full-truth (Passed/Detail reflect the actual scan) while repeat FAILs on
	// an unchanged diff become distinguishable from genuinely new hits. The agent-facing
	// advisory below still only renders fresh findings.
	//
	// 同 finding 抑制（2026-08 噪音审计）——先于 checklog 记录执行，使审计条目带上
	// 新发现/被抑制拆分（dedupSuffix）：条目保持全量真相（Passed/Detail 反映当次
	// 真实扫描），同时让重扫同一 diff 的重复 FAIL 与真正的新命中可区分。下方
	// agent 面向的 advisory 仍只渲染新 finding。
	var freshCheats []CheatFinding
	if len(cheats) > 0 {
		cheatKeys := make([]string, len(cheats))
		for i, c := range cheats {
			cheatKeys[i] = cheatFindingKey(c)
		}
		fresh, dirty := filterUnreported(state, cheatKeys)
		findingsDirty = findingsDirty || dirty
		freshSet := keySet(fresh)
		for _, c := range cheats {
			if freshSet[cheatFindingKey(c)] {
				freshCheats = append(freshCheats, c)
			}
		}
	}
	recordAudit(root, &checklog.Entry{
		Check:   checklog.CheckCheatScan,
		Passed:  len(cheats) == 0,
		Checked: true,
		TaskRef: state.TaskRef,
		Detail:  cheatScanDetail(cheats) + dedupSuffix(len(cheats), len(freshCheats)),
	})
	if len(cheats) > 0 {
		// 同 finding 抑制（2026-08 噪音审计）：指纹（规则|文件：行）已报告过的 finding 不
		// 再逐条重发——修复后 verify 重试重扫同一 diff，否则会逐字重发（Translate(method)
		// 8 次）。checklog 条目保持全量（审计真相），抑制的只是 agent 面向的重复提醒。
		//
		// Same-finding suppression (2026-08 noise audit): findings whose fingerprint
		// (rule|file:line) was already reported are not re-emitted line by line — a
		// post-fix verify retry re-scans the same diff and would otherwise re-emit
		// them verbatim (Translate(method) 8 times). The checklog entry stays full
		// (audit truth); only the agent-facing repeat nudge is suppressed.
		if len(freshCheats) == 0 {
			fmt.Fprintf(os.Stderr, "%scheat-scan 命中 %d 处疑似 AI 作弊模式%s（advisory 不阻塞）\n", GateAdvisory("[task-verify] "), len(cheats), allReportedNote())
		} else {
			fmt.Fprintf(os.Stderr, "%scheat-scan 命中 %d 处疑似 AI 作弊模式%s（advisory 不阻塞；机械检测供 review 核查）\n", GateAdvisory("[task-verify] "), len(freshCheats), suppressNote(len(cheats)-len(freshCheats)))
			for _, c := range freshCheats {
				loc := c.File
				if c.Line > 0 {
					loc = c.File + ":" + strconv.Itoa(c.Line)
				}
				fmt.Fprintf(os.Stderr, "  ⚠ [%s|%s] %s — %s\n", c.Severity, c.Pattern, loc, c.Snippet)
			}
			// comment-as-debt specific nudge (plan B): commenting a problem ≠ solving it (the
			// anti-pattern at level 0 of the laziness ladder). Spell out the handling path for the
			// agent — convert to a forge task for tracking (intent preserved, gated) or fix it
			// inline. Without this, the agent easily dismisses low-severity comment-as-debt as
			// noise, 'marking it counts as doing it'. Raw string avoids Windows input double-quote
			// corrosion.
			//
			// comment-as-debt 专属 nudge（B 方案）：注释标识问题 ≠ 解决（懒惰阶梯反第 0
			// 级）。把处置路径明确告诉 agent——转 forge task 跟踪（保留意图、被门禁
			// 追踪）或当场修。不加则 agent 易把低 severity 的 comment-as-debt 当噪音
			// 忽略，「标注了就当做了」。raw string 规避 Windows 输入双引号腐蚀。
			debtCount := 0
			for _, c := range freshCheats {
				if c.Pattern == CheatCommentDebt {
					debtCount++
				}
			}
			if debtCount > 0 {
				fmt.Fprintf(os.Stderr, "%s"+`%d 处 comment-as-debt——注释标识问题 ≠ 解决（懒惰阶梯反第 0 级）。处置：当场修掉；或转 forge task start --ref <ref> --title <描述> 跟踪（本任务完结后开）。加载 implementation-discipline skill 按懒惰阶梯归位。advisory 不阻塞。`+"\n", GateAdvisory("[task-verify] "), debtCount)
			}
		}
	}
	return findingsDirty
}

// adviseVerifyDocGate is the doc-gate advisory (output→re-check loop,
// docs/design/output-readability-gates.md): when the task changed markdown deliverables
// but no fresh L2 review is recorded, remind at task-verify — BEFORE complete hard-blocks
// on it. Enforcement stays at CheckDocGate (task-complete pre-flight); this is the early
// nudge so the agent fixes L1 and runs the rubric review while still in verify, not after
// hitting the BLOCKED. Pure advisory.
//
// adviseVerifyDocGate 是 doc-gate advisory（输出→回检循环，docs/design/output-readability-gates.md）：
// 任务变更了 markdown 产物但无 fresh 的 L2 回检记录时，在 task-verify 提醒——抢在
// complete 硬拦截之前。执法仍在 CheckDocGate（task-complete pre-flight）；这是提前量
// nudge，让 agent 还在 verify 阶段就修 L1、跑 rubric 评审，而不是撞上 BLOCKED 才回头。
// 纯 advisory。
func adviseVerifyDocGate(root string, state *TaskState) {
	if docs := changedMarkdownDocs(root, state); len(docs) > 0 {
		head := GetHeadCommit(root)
		stale := state.DocReview == nil || state.DocReview.ReviewedAt.IsZero() ||
			(state.DocReview.HeadCommit != "" && head != "" && state.DocReview.HeadCommit != head)
		if stale {
			fmt.Fprintf(os.Stderr, "%s"+`文档产物 %d 个（%s）尚无 fresh 的 L2 回检——complete 前须：forge docs lint 修 L1 → 按 doc-review skill 评审（产出者不能自检）→ forge task doc-review 记录。advisory 不阻塞，complete 的 doc gate 会拦。`+"\n", GateAdvisory("[task-verify] "), len(docs), strings.Join(docs, ", "))
		}
	}
}

// scanUnusedFindings runs the unused-scan (advisory): layer-1 wiring detection —
// newly-added exported symbols (Go func/type/method, TS export, Rust pub) that no
// production line in this task references. Catches "implemented but never wired"
// (Forge's own BUG-1: inferDesignPhases had zero production callers — dead code,
// surfaced only by review). Unit tests verify the implementation, not the wiring — a
// broken wire leaves tests green and the feature dead; this scan exposes it
// mechanically. Advisory (library/reflection/external-consumer exports legitimately
// have no in-repo caller); never blocks. CheckUnusedScan is excluded from
// BuildEvidenceChain — an observation, not verification evidence (same as cheat-scan).
// Layer-2 (referenced but semantically unwired — registered but never instantiated) is not
// mechanically decidable → stays with the LLM reviewer / code-review-gate.
//
// findingsDirty (accumulated from the cheat-scan section) is threaded in; when either
// scan section added new fingerprints, the per-task reported set is persisted once here —
// in the same position (right after the unused-scan section) as before the extraction.
//
// scanUnusedFindings 跑 unused-scan（advisory）：层 1 接线检测——本次新增的导出符号（Go
// func/type/method、TS export、Rust pub）在本任务生产代码里零引用。抓"实现了但没接线"
// （Forge 自己的 BUG-1：inferDesignPhases 零生产调用方——dead code，靠 review 才浮出）。
// 单测验实现不验接线——接线一断测试照绿、功能已死；本扫描机械暴露。advisory（库/反射/
// 外部消费的导出合法地无仓内调用方）；绝不阻塞。CheckUnusedScan 在 BuildEvidenceChain
// 中排除——观测非验证证据（同 cheat-scan）。层 2（引用了但语义没接通——注册了但从未
// 实例化）机械不可判 → 仍归 LLM reviewer / code-review-gate。
// findingsDirty（cheat-scan 段累积）经参数传入；两段扫描若有新指纹入集合，在此（原位：
// unused-scan 段末尾）持久化一次。
func scanUnusedFindings(root string, state *TaskState, findingsDirty bool) {
	unused := ScanUnusedSymbols(root, state)
	// 同 finding 抑制先于 checklog 记录执行（与 cheat-scan 段同一模式）：审计条目带上
	// 新发现/被抑制拆分（dedupSuffix），重扫同一 diff 的重复 FAIL 与真新命中可区分。
	//
	// Same-finding suppression runs BEFORE the checklog record (same pattern as the
	// cheat-scan section): the audit entry carries the fresh/suppressed
	// breakdown (dedupSuffix), so repeat FAILs on an unchanged diff are
	// distinguishable from genuinely new hits.
	var freshUnused []UnusedFinding
	if len(unused) > 0 {
		unusedKeys := make([]string, len(unused))
		for i, u := range unused {
			unusedKeys[i] = unusedFindingKey(u)
		}
		fresh, dirty := filterUnreported(state, unusedKeys)
		findingsDirty = findingsDirty || dirty
		freshSet := keySet(fresh)
		for _, u := range unused {
			if freshSet[unusedFindingKey(u)] {
				freshUnused = append(freshUnused, u)
			}
		}
	}
	recordAudit(root, &checklog.Entry{
		Check:   checklog.CheckUnusedScan,
		Passed:  len(unused) == 0,
		Checked: true,
		TaskRef: state.TaskRef,
		Detail:  unusedScanDetail(unused) + dedupSuffix(len(unused), len(freshUnused)),
	})
	if len(unused) > 0 {
		// 同 finding 抑制（同 cheat-scan 段注释）：指纹 规则|文件：行|符号。
		//
		// Same-finding suppression (see the cheat-scan section's comment):
		// fingerprint = rule|file:line|symbol.
		if len(freshUnused) == 0 {
			fmt.Fprintf(os.Stderr, "%s"+`unused-scan 发现 %d 个疑似未接线的导出符号%s（advisory 不阻塞）`+"\n", GateAdvisory("[task-verify] "), len(unused), allReportedNote())
		} else {
			fmt.Fprintf(os.Stderr, "%s"+`unused-scan 发现 %d 个疑似未接线的导出符号%s（advisory 不阻塞；层1机械检测：确认已挂入调用链/registry/路由，或属外部消费者则忽略）`+"\n", GateAdvisory("[task-verify] "), len(freshUnused), suppressNote(len(unused)-len(freshUnused)))
			for _, u := range freshUnused {
				loc := u.File
				if u.Line > 0 {
					loc = u.File + ":" + strconv.Itoa(u.Line)
				}
				fmt.Fprintf(os.Stderr, "  ⚠ [%s] %s — %s\n", u.Kind, loc, u.Symbol)
			}
		}
	}
	// 两段扫描若有新指纹入集合，持久化一次（best-effort；失败最坏下次重报一遍，
	// 优于阻塞 gate）。与 DesignPhases 持久化同款模式。
	//
	// Persist once if either scan added new fingerprints (best-effort; a failure at
	// worst re-reports once next run — better than blocking the gate). Same pattern
	// as the DesignPhases persistence.
	if findingsDirty {
		if err := MergeOrPersistTaskState(root, state, func(s *TaskState) error {
			s.ReportedFindings = state.ReportedFindings
			return nil
		}); err != nil {
			fmt.Fprintln(os.Stderr, "[task-verify] reported-findings persist failed:", err)
		}
	}
}

// adviseConventionsLint is the conventions-lint advisory: the project's conventions
// profile declares a lint command (conventions-profile layer 3 — mechanical style belongs
// to the toolchain, the research consensus "rules are advisory, hooks are deterministic");
// this task has tool telemetry, yet no Bash command in the task scope matched the lint
// signature → one nudge to run it before claiming done. Purely advisory (the lint may
// have run in CI/editor — toollog only sees the host's Bash); CheckConventionsLint also
// stays silent (no checklog row) for unadopted projects and telemetry-less hosts.
// CheckConventionsLint is excluded from BuildEvidenceChain — an observation, not
// verification evidence. Escape: FORGE_CONVENTIONS_LINT=disable.
//
// adviseConventionsLint 是 conventions-lint（advisory）：项目规范档案声明了 lint 命令
// （conventions-profile 层 3——机械风格交给工具链，业界共识「规则建议、hooks 确定性」）；
// 本任务有工具遥测、但任务范围内无 Bash 命令命中 lint 签名 → 声称完成前提醒跑一次。
// 纯 advisory（lint 可能经 CI/编辑器跑过——toollog 只见宿主 Bash）；未采纳项目与无遥测
// 宿主上 CheckConventionsLint 保持静默（不落 checklog）。CheckConventionsLint 在
// BuildEvidenceChain 中被排除——观测非验证证据。逃生：FORGE_CONVENTIONS_LINT=disable。
func adviseConventionsLint(root string, state *TaskState) {
	lintOutcome := CheckConventionsLint(root, state)
	recordConventionsLintAudit(root, state, lintOutcome)
	if lintOutcome.Applicable && !lintOutcome.Ran {
		fmt.Fprintf(os.Stderr, "%sconventions-lint——本任务 Bash 历史未见档案声明的 lint 命令（%s）：声称完成前跑一次，机械风格交给工具链（advisory 不阻塞；关闭：FORGE_CONVENTIONS_LINT=disable）\n",
			GateAdvisory("[task-verify] "), lintOutcome.LintCmd)
	}
}
