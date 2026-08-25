package taskpipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/doclint"
)

// CheckNameDocGate / CheckNameDocLint are the checklog entries of the output→
// re-check loop: doc-lint records each L1 sweep the gate ran; doc-gate records
// the complete pre-flight verdict (L1 + L2 + critical findings).
//
// CheckNameDocGate / CheckNameDocLint 是输出→回检循环的 checklog 条目：
// doc-lint 记录门禁跑的每次 L1 扫描；doc-gate 记录完整 pre-flight 判定
// （L1 + L2 + critical findings）。
const (
	CheckNameDocGate checklog.CheckName = "doc-gate"
	CheckNameDocLint checklog.CheckName = "doc-lint"
)

// docGateDisableEnv lets a task exit the doc pre-flight at task-complete
// (symmetric to FORGE_ACCEPTANCE_GATE). Legitimate scenarios: pure-code tasks
// whose .md churn is incidental, or L2 review genuinely not executable. The CLI
// names this escape hatch in the BLOCKED text (no silent bypass); escape →
// checklog CheckEscapeHatch → evidence Strength cap Weak (has cost).
//
// docGateDisableEnv 让 task 可在 task-complete 处退出 doc pre-flight（与
// FORGE_ACCEPTANCE_GATE 对称）。合法场景：纯代码任务的 .md 变更属附带、或
// L2 评审确实不可执行。CLI 在 BLOCKED 文案里明示此逃生舱（不静默绕过）；
// 逃生 → checklog CheckEscapeHatch → evidence Strength cap Weak（有代价）。
const docGateDisableEnv = "FORGE_DOC_GATE"

// Doc gate convergence constants (docs/design/output-readability-gates.md):
// rubric threshold reuses the skill system's 75; 3 review rounds without
// passing escalates to human confirmation instead of infinite polishing.
//
// doc 门禁收敛常量（docs/design/output-readability-gates.md）：rubric 阈值复用
// skill 体系的 75 分；3 轮回检仍不过则升级人工确认，不无限打磨。
const (
	DocRubricThreshold = 75
	DocReviewMaxRounds = 3
)

// DocReviewSource findings are raised by the L2 doc review (forge task
// doc-review / rubric-docs.md). Critical ones must be resolved before the doc
// gate passes.
//
// DocReviewSource 的 findings 由 L2 文档回检提出（forge task doc-review /
// rubric-docs.md）。Critical 级未决则 doc gate 不放行。
const DocReviewSource = "doc-review"

// FindingSeverityCritical marks a doc-review finding that blocks the doc gate.
// Empty Severity (legacy findings) never blocks — additive field, old states
// keep their behavior.
//
// FindingSeverityCritical 标记阻断 doc gate 的文档回检发现。空 Severity
// （旧版 findings）永不阻断——增量字段，旧状态行为不变。
const FindingSeverityCritical = "critical"

// DocReview is the L2 evidence recorded by `forge task doc-review` after a
// rubric review (rubric-docs.md). The reviewer must not be the producer
// (anti-cheating rule 1); HeadCommit binds the review to the code snapshot so
// doc changes after the review invalidate it (freshness, same as acceptance).
//
// DocReview 是 `forge task doc-review` 在 rubric 评审（rubric-docs.md）后记录的
// L2 证据。回检者不能是产出者（防作弊纪律 1）；HeadCommit 把评审绑定到代码
// 快照——评审后改文档则失效（freshness，与 acceptance 同构）。
type DocReview struct {
	Passed      bool      `json:"passed"`
	RubricScore int       `json:"rubric_score"`
	Round       int       `json:"round"`
	Reviewer    string    `json:"reviewer,omitempty"`
	ReviewedAt  time.Time `json:"reviewed_at,omitempty"`
	HeadCommit  string    `json:"head_commit,omitempty"`
}

// changedMarkdownDocs lists the markdown deliverables of the task: .md files
// changed since the task's HeadCommit (committed + working tree) plus untracked
// .md, minus doclint-exempt paths and files that no longer exist. Empty when
// not a git repo or HeadCommit missing — the gate then short-circuits to pass
// (same degradation as acceptance's non-git path).
//
// changedMarkdownDocs 列出任务的 markdown 产物：自 task 的 HeadCommit 以来变更
// （已提交 + 工作区）与新增未跟踪的 .md，减去 doclint 豁免路径与已删除文件。
// 非 git 仓库或 HeadCommit 缺失时为空——门禁短路放行（与 acceptance 的非 git
// 退化一致）。
func changedMarkdownDocs(root string, state *TaskState) []string {
	if state == nil || state.HeadCommit == "" {
		return nil
	}
	out, err := exec.Command("git", "-C", root, "diff", "--name-only", state.HeadCommit).Output()
	if err != nil {
		// git missing / not a repo: degrade to no candidates (gate passes).
		//
		// git 缺失/非仓库：退化为无候选（门禁放行）。
		return nil
	}
	untracked, err := exec.Command("git", "-C", root, "ls-files", "--others", "--exclude-standard").Output()
	if err != nil {
		untracked = nil
	}

	seen := map[string]bool{}
	var docs []string
	add := func(name string) {
		name = filepath.ToSlash(strings.TrimSpace(name))
		if name == "" || seen[name] {
			return
		}
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			return
		}
		if doclint.PathExempt(name) {
			return
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			return // deleted mid-task
		}
		seen[name] = true
		docs = append(docs, name)
	}
	for _, line := range strings.Split(string(out), "\n") {
		add(line)
	}
	for _, line := range strings.Split(string(untracked), "\n") {
		add(line)
	}
	return docs
}

// CheckDocGate is task-complete's doc pre-flight — the process node of the
// output→re-check loop. Re-check previously had no node, no criteria and no
// cost ("检查一下" is an unfalsifiable imperative whose skip has zero
// consequence); this gives it all three:
//   - L1 (machine): every changed .md deliverable must pass doclint hard rules
//   - L2 (model): state.DocReview must be recorded, fresh (== HEAD) and
//     Passed with RubricScore ≥ 75; producer-self-review is rejected by the
//     rubric discipline, forge only verifies the evidence exists
//   - unresolved Critical doc-review findings block
//
// No doc deliverables → pass. Escape (per-task override / FORGE_DOC_GATE=
// disable) records a checklog audit entry then passes. Round cap: after
// DocReviewMaxRounds failed rounds the reason text escalates to human
// confirmation (escape hatch), never auto-passes.
//
// CheckDocGate 是 task-complete 的文档 pre-flight——输出→回检循环的流程节点。
// 回检此前无节点、无判据、无代价（「检查一下」是不可证伪的祈使句，跳过零后果）；
// 本检查补齐三者：
//   - L1（机器）：变更的 .md 产物全部通过 doclint 硬规则
//   - L2（模型）：state.DocReview 已记录、fresh（== HEAD）且 Passed 且
//     RubricScore ≥ 75；产出者自检被 rubric 纪律拒绝，forge 只验证证据存在
//   - 未决 Critical 文档回检 findings 阻断
//
// 无文档产物 → 放行。逃生（per-task override / FORGE_DOC_GATE=disable）落
// checklog 审计后放行。轮次上限：DocReviewMaxRounds 轮未过后 reason 文案升级
// 人工确认（逃生舱），绝不自动放行。
func CheckDocGate(root string, state *TaskState) (ok bool, reasons []string) {
	docs := changedMarkdownDocs(root, state)
	if len(docs) == 0 {
		return true, nil
	}
	if escapeDisabled(state, escapeDocGate, docGateDisableEnv) {
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckEscapeHatch,
			Passed:  true,
			Checked: true,
			Level:   checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  `escape-hatch: doc gate bypassed (per-task override or FORGE_DOC_GATE=disable); changed docs: ` + strings.Join(docs, ", "),
		})
		return true, nil
	}

	// L1 — deterministic sweep over every changed deliverable.
	//
	// L1——对每个变更产物做确定性扫描。
	var hardIssues []string
	for _, doc := range docs {
		issues, err := doclint.LintFile(filepath.Join(root, filepath.FromSlash(doc)))
		if err != nil {
			continue // unreadable already filtered by stat; defensive
		}
		for _, iss := range issues {
			if iss.Hard() {
				hardIssues = append(hardIssues, fmt.Sprintf("%s:%d [%s] %s", doc, iss.Line, iss.Rule, iss.Message))
			}
		}
	}
	recordAudit(root, &checklog.Entry{
		Check:   CheckNameDocLint,
		Passed:  len(hardIssues) == 0,
		Checked: true,
		TaskRef: state.TaskRef,
		Detail:  fmt.Sprintf("L1 sweep over %d changed docs: %d hard issues", len(docs), len(hardIssues)),
	})
	if len(hardIssues) > 0 {
		reasons = append(reasons, fmt.Sprintf("L1 lint 硬失败 %d 处（forge docs lint <paths> 可复现）: %s", len(hardIssues), strings.Join(hardIssues, "; ")))
	}

	// L2 — recorded rubric review evidence (freshness + score + passed).
	//
	// L2——已记录的 rubric 评审证据（freshness + 得分 + 通过）。
	head := GetHeadCommit(root)
	switch {
	case state.DocReview == nil || state.DocReview.ReviewedAt.IsZero():
		reasons = append(reasons, `L2 文档回检未记录——先按 code-review-gate/references/rubric-docs.md 评审（产出者不能自检），再 forge task doc-review --passed/--failed --score <N>`)
	case state.DocReview.HeadCommit != "" && head != "" && state.DocReview.HeadCommit != head:
		reasons = append(reasons, fmt.Sprintf(`L2 文档回检基于旧代码（快照 %s ≠ HEAD %s）——回检后改了产物，重新评审后 forge task doc-review`, shortCommit(state.DocReview.HeadCommit), shortCommit(head)))
	case !state.DocReview.Passed:
		if state.DocReview.Round >= DocReviewMaxRounds {
			reasons = append(reasons, fmt.Sprintf(`L2 文档回检已 %d 轮未过（轮次上限 %d）——升级人工确认：请用户裁定放行（确认后 forge task override --doc-gate disable，落 checklog 审计）或指出下一轮修复方向`, state.DocReview.Round, DocReviewMaxRounds))
		} else {
			reasons = append(reasons, fmt.Sprintf(`L2 文档回检未通过（第 %d 轮，得分 %d）——修复 rubric-docs.md 的 Critical/Important 发现后重新评审`, state.DocReview.Round, state.DocReview.RubricScore))
		}
	case state.DocReview.RubricScore < DocRubricThreshold:
		reasons = append(reasons, fmt.Sprintf(`L2 文档回检得分 %d 低于阈值 %d——按 rubric-docs.md 四维（结论前置/详略/证据/受众）改进后重新评审`, state.DocReview.RubricScore, DocRubricThreshold))
	}

	// Unresolved Critical findings raised by the doc review block the gate.
	//
	// 文档回检提出的未决 Critical findings 阻断门禁。
	for i := range state.Findings {
		f := &state.Findings[i]
		if f.Source == DocReviewSource && f.Severity == FindingSeverityCritical && f.Status == "open" {
			reasons = append(reasons, fmt.Sprintf("文档回检 Critical 未决（%s）：%s——修复后 forge task finding resolve %s", f.ID, f.Content, f.ID))
		}
	}

	recordAudit(root, &checklog.Entry{
		Check:   CheckNameDocGate,
		Passed:  len(reasons) == 0,
		Checked: true,
		TaskRef: state.TaskRef,
		Level:   checklog.LevelBlocked,
		Detail:  fmt.Sprintf("doc gate over %d changed docs: %d reasons", len(docs), len(reasons)),
	})
	return len(reasons) == 0, reasons
}

// shortCommit renders a commit hash for BLOCKED prose (12 chars).
//
// shortCommit 渲染 BLOCKED 文案用的 commit 短哈希（12 字符）。
func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}
