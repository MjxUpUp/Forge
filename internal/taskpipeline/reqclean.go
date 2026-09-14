package taskpipeline

// reqclean.go — 需求卫生检查（W0.1 复核报告 + 调研报告 docs/surveys/w0-dead-checks-review.md
// 指向的方向二最大产品空白第一子步）。
//
// reqclean.go — requirements hygiene check: deterministic scan of registered
// spec artifacts and acceptance criteria for quality issues.
//
// 设计约束（W0 宪法 + deterministic-first 原则）：
//   - advisory：永不阻断 v1（方向二第一步，先让用户看见问题再决定是否升级）
//   - 确定性：零 LLM 调用——歧义标记/验收缺失/模糊量词三类用模式匹配覆盖
//   - 复用 artifact chain 的 spec 登记面——不另造存储
//
// 已知边界：确定性模式只覆盖显式歧义标记与结构缺失；语义歧义（需求措辞含糊但
// 无显式标记）需要 LLM 判断，不在 v1 范围。零 findings ≠ 零歧义——只意味着
// 没有确定性可检的卫生问题。
//
// Known boundary: deterministic patterns cover explicit ambiguity markers and
// structural gaps only; semantic ambiguity (vague wording without explicit
// markers) requires LLM judgment, out of scope for v1. Zero findings ≠ zero
// ambiguity — it means no deterministically detectable hygiene issues.

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameReqHygiene 记录一次需求卫生扫描（advisory——forge 自己跑确定性扫描，
// agent 无法伪造结果侧）。
const CheckNameReqHygiene checklog.CheckName = "req-hygiene"

// reqHygieneDisableEnv 是需求卫生检查的逃生舱（沿 FORGE_DOC_GATE 模式）。
const reqHygieneDisableEnv = "FORGE_REQ_HYGIENE"

// reqHygieneAmbiguityMarkers 匹配显式歧义标记（TBD/待定/待补/可能…）。
// CJK 标记不用 \b——Go regexp 的 \b 仅识别 ASCII word boundary，
// 对 CJK 字符无效。ASCII 标记（TBD/TODO）保留 \b 防词内误匹配。
var reqHygieneAmbiguityMarkers = regexp.MustCompile(
	`(?i)(\bTBD\b|\bTODO\b|待定|待补|待确认|待讨论|可能|大概|也许|或许|视情况|后续确定|需考虑|酌情)`,
)

// reqHygieneVagueQuantifier 匹配无数字锚定的模糊量词。
var reqHygieneVagueQuantifier = regexp.MustCompile(`(?:一些|多个|若干|部分|某些|几个)`)

// reqHygieneFinding 是单条需求卫生 finding。
type reqHygieneFinding struct {
	Category string // ambiguity | vague-quantifier | missing-acceptance
	Detail   string
}

// CheckRequirementsHygiene 对已登记的 spec 产物与验收标准做确定性卫生扫描。
// 返回 findings 列表（空 = 无确定性可检问题）。advisory——永不阻断。
func CheckRequirementsHygiene(root string, state *TaskState) []reqHygieneFinding {
	var findings []reqHygieneFinding

	// 扫描已登记的 spec 产物文件
	for stage, ref := range state.SpecArtifacts {
		if stage != "spec" {
			continue
		}
		abs := artifactAbsPath(root, ref)
		body, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		findings = append(findings, scanSpecText(string(body))...)
	}

	// 有 spec 产物但无验收标准
	if hasSpecArtifacts(state) && len(state.Acceptance) == 0 {
		findings = append(findings, reqHygieneFinding{
			Category: "missing-acceptance",
			Detail:   "任务有 spec 产物但无验收标准（--accept 或 artifact --extract）",
		})
	}

	return findings
}

// scanSpecText 对单篇 spec 文本做歧义标记与模糊量词扫描。
func scanSpecText(text string) []reqHygieneFinding {
	var findings []reqHygieneFinding
	for i, line := range strings.Split(text, "\n") {
		for _, m := range reqHygieneAmbiguityMarkers.FindAllStringSubmatch(line, -1) {
			findings = append(findings, reqHygieneFinding{
				Category: "ambiguity",
				Detail:   fmt.Sprintf("歧义标记 %q（行 %d）", m[0], i+1),
			})
		}
		if reqHygieneVagueQuantifier.MatchString(line) {
			findings = append(findings, reqHygieneFinding{
				Category: "vague-quantifier",
				Detail:   fmt.Sprintf("模糊量词（行 %d）", i+1),
			})
		}
	}
	return findings
}

// hasSpecArtifacts 报告任务是否登记了 spec 类产物。
func hasSpecArtifacts(state *TaskState) bool {
	_, ok := state.SpecArtifacts["spec"]
	return ok
}

// RunReqHygiene 实跑需求卫生扫描并落 checklog（advisory——永不阻断）。
// 逃生：FORGE_REQ_HYGIENE=disable 落 escape-hatch 留痕。
func RunReqHygiene(root string, state *TaskState) {
	if escapeDisabled(state, escapeDocGate, reqHygieneDisableEnv) {
		recordAudit(root, &checklog.Entry{
			Check:   CheckNameReqHygiene,
			Passed:  true,
			Checked: true,
			Level:   checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  `escape-hatch: req hygiene bypassed (FORGE_REQ_HYGIENE=disable)`,
			Meta:    map[string]string{"escape.gate": "req-hygiene", "escape.reason": checklog.EscapeReasonEnv, "escape.owner": "env"},
		})
		return
	}

	findings := CheckRequirementsHygiene(root, state)
	e := &checklog.Entry{
		Check:   CheckNameReqHygiene,
		Passed:  true,
		Checked: true,
		Level:   checklog.LevelPass,
		TaskRef: state.TaskRef,
	}
	if len(findings) > 0 {
		e.Level = checklog.LevelAdvisory
		e.Detail = fmt.Sprintf("ADVISORY: 需求卫生 %d 项发现（歧义标记/模糊量词/验收缺失）——advisory 不阻断，建议清理", len(findings))
		for i, f := range findings {
			if i >= 5 {
				e.Detail += fmt.Sprintf(" …（其余 %d 项略）", len(findings)-5)
				break
			}
			e.Detail += fmt.Sprintf("\n  [%s] %s", f.Category, f.Detail)
		}
	}
	recordAudit(root, e)
}
