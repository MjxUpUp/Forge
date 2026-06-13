package experience

import (
	"fmt"

	"github.com/Harness/forge/internal/scoringtypes"
)

// proposalTemplate seeds the content of an auto-generated experience proposal
// for a given low-scoring dimension.
type proposalTemplate struct {
	Category    string
	Title       string
	Description string
	Patterns    []string
	Severity    string
}

// dimensionTemplates maps each scoring dimension to the experience rule it
// suggests when that dimension scores low (<70). These are starting points the
// AI agent refines via `forge experience accept/reject` — their real purpose is
// to guarantee every mandatory review has at least one proposal, so the review
// can be resolved. Without them, a mandatory review deadlocked: AcceptProposal
// (the only path to ReviewResolved) had no proposal object to act on.
var dimensionTemplates = map[scoringtypes.Dimension]proposalTemplate{
	scoringtypes.DimensionProcess: {
		Category:    "gotchas",
		Title:       "门禁按序推进，失败先修代码",
		Description: "task-implement → task-verify → task-complete 顺序推进；门禁失败说明代码有 bug，先定位根因再推进，不跳过不弱化。",
		Severity:    "warning",
	},
	scoringtypes.DimensionTesting: {
		Category:    "patterns",
		Title:       "新代码配测试，测试失败即代码 bug",
		Description: "新增/修改的功能代码必须有对应测试；测试失败时问“代码哪里写错了”而非“怎么让测试过”，禁止弱化断言。",
		Severity:    "warning",
	},
	scoringtypes.DimensionCodeQuality: {
		Category:    "gotchas",
		Title:       "每次修改后编译必须通过",
		Description: "auto-compile hook 自动检查；编译失败先修编译错误再继续后续工作。",
		Severity:    "error",
	},
	scoringtypes.DimensionAssertions: {
		Category:    "gotchas",
		Title:       "不弱化断言（t.Fatal/assert!）",
		Description: "禁止 t.Fatal→t.Log、严格检查→接受任意、t.Skip；提交前审查 diff 显式检查断言是否被降级。",
		Severity:    "error",
	},
	scoringtypes.DimensionScope: {
		Category:    "patterns",
		Title:       "改动粒度适当，聚焦单一职责",
		Description: "非平凡变更（>10 行）走 Forge 任务；一个任务一个主题，避免单次改动跨多个无关模块。",
		Severity:    "info",
	},
	scoringtypes.DimensionEfficiency: {
		Category:    "patterns",
		Title:       "高效推进，减少无谓往返",
		Description: "先调研再动手（查 API、确认类型、读现有代码），避免凭猜测返工；独立操作并行执行。",
		Severity:    "info",
	},
	scoringtypes.DimensionToolSelection: {
		Category:    "patterns",
		Title:       "探查代码用专用工具而非 bash 管道",
		Description: "探查文件内容/结构/grep 关键词时优先用 Grep/Glob/Read，不用 bash 的 grep/ls/tail/cat；bash 仅用于执行命令（git/go/gh/forge）。",
		Patterns:    []string{`(?i)\bBash\b.*\b(grep|ls|cat|tail|head)\b`},
		Severity:    "warning",
	},
	scoringtypes.DimensionSkillHit: {
		Category:    "patterns",
		Title:       "用 forge skill 编排质量流程",
		Description: "项目级管道用 /forge-pipeline，完整协议用 /forge-quality；skill-hit 衡量是否按既定流程工作而非凭直觉。",
		Severity:    "info",
	},
}

// GenerateProposalsForReview creates one experience proposal per low-scoring
// dimension, linked to the given review (taskRef). This is the step that was
// missing: it ensures a mandatory review always has proposals, so
// `forge experience accept <id>` can resolve it.
//
// Idempotent: dimensions already holding a proposed proposal for this review
// are skipped, so re-running (e.g. re-scoring a task) does not duplicate.
// Returns the number of proposals created.
func GenerateProposalsForReview(root, taskRef string, lows []LowDimension) (int, error) {
	existing, err := ListProposals(root, PropProposed)
	if err != nil {
		return 0, fmt.Errorf("list existing proposals for dedup: %w", err)
	}
	haveTitle := make(map[string]bool)
	for _, p := range existing {
		if p.SourceReview == taskRef {
			haveTitle[p.Title] = true
		}
	}

	created := 0
	for _, d := range lows {
		tmpl, ok := dimensionTemplates[d.Dimension]
		if !ok {
			continue // no template for this dimension — skip silently
		}
		if haveTitle[tmpl.Title] {
			continue // already proposed for this review
		}
		proposal := &ExperienceProposal{
			SourceReview: taskRef,
			Category:     tmpl.Category,
			Title:        tmpl.Title,
			Description:  tmpl.Description,
			Patterns:     tmpl.Patterns,
			Severity:     tmpl.Severity,
			Status:       PropProposed,
		}
		if err := SaveProposal(root, proposal); err != nil {
			return created, fmt.Errorf("save proposal for %s: %w", d.Dimension, err)
		}
		haveTitle[tmpl.Title] = true
		created++
	}
	return created, nil
}

// GenerateForExistingReview loads an existing review and backfills proposals
// for it. Used by `forge experience generate <task-ref>` to repair reviews
// created before the auto-generation fix landed.
func GenerateForExistingReview(root, taskRef string) (int, error) {
	review, err := LoadReview(root, taskRef)
	if err != nil {
		return 0, err
	}
	n, err := GenerateProposalsForReview(root, taskRef, review.LowDimensions)
	if err != nil {
		return n, err
	}
	// A persisted review may have empty LowDimensions (e.g. a B-grade task
	// upgraded to mandatory due to missing hooks). `forge experience generate`
	// must still leave it resolvable, so backfill a fallback if nothing was
	// generated and the review is mandatory.
	if n == 0 && review.Mandatory {
		return GenerateFallbackProposal(root, taskRef)
	}
	return n, nil
}

// GenerateFallbackProposal creates a single generic proposal for a review that
// has no low-scoring dimensions but still must be resolvable. This closes the
// B-grade deadlock path: a task scored in [70,80) with every dimension ≥70 has
// LowDimensions=[], yet it is upgraded to a MANDATORY review when critical
// hooks didn't run. With zero low dims GenerateProposalsForReview returns 0 and
// the mandatory review would deadlock — no proposal to accept. This backfills
// one so `forge experience accept <id>` can resolve it.
//
// Idempotent: no-op (returns 0) if the review already has any proposed proposal.
func GenerateFallbackProposal(root, taskRef string) (int, error) {
	existing, err := ListProposals(root, PropProposed)
	if err != nil {
		return 0, fmt.Errorf("list existing proposals: %w", err)
	}
	for _, p := range existing {
		if p.SourceReview == taskRef {
			return 0, nil // already has a proposal — nothing to backfill
		}
	}
	proposal := &ExperienceProposal{
		SourceReview: taskRef,
		Category:     "gotchas",
		Title:        "质量复盘：关键 hook 未执行，补充测试与门禁验证",
		Description:  "本任务因关键质量 hook 未执行被升级为 mandatory review，但没有低分维度。审阅变更确认编译/断言/测试均到位后 accept 解除 review；若已补执行可 reject。",
		Severity:     "warning",
		Status:       PropProposed,
	}
	if err := SaveProposal(root, proposal); err != nil {
		return 0, fmt.Errorf("save fallback proposal: %w", err)
	}
	return 1, nil
}
