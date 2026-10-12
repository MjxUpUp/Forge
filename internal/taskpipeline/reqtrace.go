package taskpipeline

// reqtrace.go —— 需求 ID 追溯（design-stage-hardening W3，提案 D1）：验收条目声明
// 自己验证的需求 ID，spec/design 产物声明需求，两边对账出「已验证 / 未验证 / 孤儿」
// 覆盖表。把「哪条需求没被验证」从 agent 自述变成机械可答。
//
// reqtrace.go — requirement-ID traceability (design-stage-hardening W3, D1):
// acceptance criteria declare which requirement IDs they verify; spec/design
// artifacts declare requirements; reconciling both yields a verified /
// unverified / orphan coverage table.
//
// 两种声明写法并存（D1）：
//  1. 前缀：`accept[REQ-3]: <cmd> :: <expected>`；accept 围栏内 `[REQ-3] <cmd> :: <expected>`
//  2. 行尾：`... #REQ-3 #REQ-4`——只认行末连续的 `#(REQ|NFR|INV)-<数字>` 标记，
//     其余 `#` 原样保留（shell 注释、期望文本中的 `#` 不被误吞）
//
// 两种写法同时出现时取并集去重。

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/artifactchain"
	"github.com/MjxUpUp/Forge/internal/checklog"
)

// reqIDPattern is the single spelling source of a requirement ID.
//
// reqIDPattern 是需求 ID 的唯一拼写源。
const reqIDPattern = `(?:REQ|NFR|INV)-\d+`

var (
	// reqIDRe finds requirement IDs anywhere in a line (declaration scan).
	reqIDRe = regexp.MustCompile(`\b` + reqIDPattern + `\b`)
	// trailingTraceRe matches the run of trailing `#REQ-n` tokens at line end.
	trailingTraceRe = regexp.MustCompile(`(?:\s+#` + reqIDPattern + `)+\s*$`)
	// prefixTraceRe matches the `[REQ-1, NFR-2]` bracket (accept-line prefix or fence prefix).
	prefixTraceRe = regexp.MustCompile(`^\[\s*(` + reqIDPattern + `(?:\s*,\s*` + reqIDPattern + `)*)\s*\]`)
)

// splitTrailingTraces strips trailing `#REQ-n` tokens from s, returning the rest
// and the IDs in order of appearance.
//
// splitTrailingTraces 剥离 s 行末的 `#REQ-n` 标记，返回剩余部分与按出现序的 ID。
func splitTrailingTraces(s string) (string, []string) {
	loc := trailingTraceRe.FindStringIndex(s)
	if loc == nil {
		return s, nil
	}
	return strings.TrimRight(s[:loc[0]], " \t"), reqIDRe.FindAllString(s[loc[0]:], -1)
}

// splitPrefixTraces strips a leading `[REQ-n, …]` bracket from s.
//
// splitPrefixTraces 剥离 s 开头的 `[REQ-n, …]` 方括号。`[ -f x ]` 这类 shell
// test 不匹配（括号内必须全是需求 ID）。
func splitPrefixTraces(s string) (string, []string) {
	m := prefixTraceRe.FindStringSubmatchIndex(s)
	if m == nil {
		return s, nil
	}
	return strings.TrimSpace(s[m[1]:]), reqIDRe.FindAllString(s[m[2]:m[3]], -1)
}

// mergeTraces unions trace ID lists, preserving first-seen order.
//
// mergeTraces 合并多组追溯 ID（并集去重，保留首次出现顺序）。
func mergeTraces(lists ...[]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range lists {
		for _, id := range l {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// DeclaredRequirementIDs returns the requirement IDs an artifact text declares:
// every ID mentioned in prose outside acceptance declarations and code fences
// (accept lines/fences reference requirements; other fences are examples).
// Sorted, unique.
//
// DeclaredRequirementIDs 返回产物文本声明的需求 ID：正文中、验收声明与代码围栏
// 之外提到的全部 ID（accept 行与 accept 围栏是引用需求，其他围栏是示例）。排序去重。
func DeclaredRequirementIDs(text string) []string {
	seen := map[string]bool{}
	openMarker := ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if isFenceMarker(line) && (openMarker == "" || strings.HasPrefix(line, openMarker)) {
			if openMarker == "" {
				openMarker = line[:3]
			} else {
				openMarker = ""
			}
			continue
		}
		if openMarker != "" {
			continue
		}
		if _, ok := acceptListCommand(line); ok {
			continue
		}
		for _, id := range reqIDRe.FindAllString(line, -1) {
			seen[id] = true
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// RequirementCoverage is the reconciliation of declared requirements against
// acceptance traces.
//
// RequirementCoverage 是声明需求与验收追溯的对账结果。
type RequirementCoverage struct {
	Declared   []string          `json:"declared"`
	Verified   []string          `json:"verified"`             // 至少一条追溯验收，且全部 Passed
	Unverified map[string]string `json:"unverified,omitempty"` // ID → 原因（无验收追溯 / 追溯验收未通过）
	Orphans    []string          `json:"orphans,omitempty"`    // 验收追溯了但产物未声明的 ID
}

// HasTraceData reports whether the task carries any requirement IDs at all —
// tasks that never adopted IDs get no coverage noise.
//
// HasTraceData 报告任务是否用到了需求 ID——从未采用 ID 的任务不产生覆盖噪音。
func (c RequirementCoverage) HasTraceData() bool {
	return len(c.Declared) > 0 || len(c.Orphans) > 0
}

// UnverifiedIDs returns the unverified IDs in sorted order.
func (c RequirementCoverage) UnverifiedIDs() []string {
	out := make([]string, 0, len(c.Unverified))
	for id := range c.Unverified {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// ComputeRequirementCoverage reconciles declared IDs against the criteria's traces.
//
// ComputeRequirementCoverage 用验收条目的 Traces 对账声明的需求 ID。纯函数。
func ComputeRequirementCoverage(declared []string, criteria []AcceptanceCriterion) RequirementCoverage {
	cov := RequirementCoverage{Declared: append([]string(nil), declared...), Unverified: map[string]string{}}
	declSet := map[string]bool{}
	for _, id := range declared {
		declSet[id] = true
	}
	traced := map[string][]AcceptanceCriterion{}
	orphanSet := map[string]bool{}
	for _, c := range criteria {
		for _, id := range c.Traces {
			traced[id] = append(traced[id], c)
			if !declSet[id] {
				orphanSet[id] = true
			}
		}
	}
	for _, id := range declared {
		cs := traced[id]
		if len(cs) == 0 {
			cov.Unverified[id] = "无验收追溯"
			continue
		}
		allPassed := true
		for _, c := range cs {
			if !c.Passed {
				allPassed = false
				break
			}
		}
		if allPassed {
			cov.Verified = append(cov.Verified, id)
		} else {
			cov.Unverified[id] = "追溯验收未通过或未实跑"
		}
	}
	for id := range orphanSet {
		cov.Orphans = append(cov.Orphans, id)
	}
	sort.Strings(cov.Orphans)
	return cov
}

// TaskRequirementCoverage computes coverage from the task's registered spec /
// design / plan artifacts (unreadable artifacts are skipped).
//
// TaskRequirementCoverage 从任务已登记的 spec/design/plan 产物计算覆盖（不可读
// 产物跳过）。
func TaskRequirementCoverage(root string, state *TaskState) RequirementCoverage {
	var declared []string
	for _, ref := range state.SpecArtifacts {
		body, err := os.ReadFile(artifactAbsPath(root, ref))
		if err != nil {
			continue
		}
		declared = append(declared, DeclaredRequirementIDs(string(body))...)
	}
	declared = mergeTraces(declared)
	sort.Strings(declared)
	return ComputeRequirementCoverage(declared, state.Acceptance)
}

// FormatRequirementCoverage renders the three-column coverage table for humans.
//
// FormatRequirementCoverage 渲染给人看的三栏覆盖表。
func FormatRequirementCoverage(c RequirementCoverage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "需求覆盖：已验证 %d / 声明 %d", len(c.Verified), len(c.Declared))
	if len(c.Verified) > 0 {
		fmt.Fprintf(&b, "\n  已验证: %s", strings.Join(c.Verified, ", "))
	}
	for _, id := range c.UnverifiedIDs() {
		fmt.Fprintf(&b, "\n  未验证: %s（%s）", id, c.Unverified[id])
	}
	if len(c.Orphans) > 0 {
		fmt.Fprintf(&b, "\n  孤儿（验收追溯了但产物未声明）: %s", strings.Join(c.Orphans, ", "))
	}
	return b.String()
}

// CheckNameReqCoverage is the checklog name of the requirement-coverage check.
//
// CheckNameReqCoverage 是需求覆盖检查的 checklog 名。
const CheckNameReqCoverage checklog.CheckName = "req-coverage"

// reqCoverageDisableEnv is the escape hatch of the requirement-coverage check.
//
// reqCoverageDisableEnv 是需求覆盖检查的逃生舱。
const reqCoverageDisableEnv = "FORGE_REQ_COVERAGE"

// escapeReqCoverage is the which key of the requirement-coverage escape (v1 env only).
//
// escapeReqCoverage 是需求覆盖逃生的 which 键（v1 仅 env）。
const escapeReqCoverage = "req-coverage"

// reqCoverageBlocking reports whether unverified requirements should block
// complete: only when the chain puts spec or design at rubric tier or stricter
// (proposal W3.3 — advisory by default).
//
// reqCoverageBlocking 报告未验证需求是否应阻断 complete：仅当产物链把 spec 或
// design 设为 rubric 及更严档位（提案 W3.3——默认 advisory）。
func reqCoverageBlocking(chain *artifactchain.Chain) bool {
	for _, name := range []string{"spec", "design"} {
		if s, ok := chain.StageByName(name); ok && s.Mode != artifactchain.ModeAdvisory {
			return true
		}
	}
	return false
}

// CheckRequirementCoverageAtComplete runs the complete-time coverage check:
// advisory by default, blocking when reqCoverageBlocking holds. Tasks without
// requirement IDs are silent (no entry).
//
// CheckRequirementCoverageAtComplete 跑 complete 时的需求覆盖检查：默认 advisory，
// reqCoverageBlocking 成立时阻断。没用需求 ID 的任务静默（不落条目）。
func CheckRequirementCoverageAtComplete(root string, state *TaskState) error {
	cov := TaskRequirementCoverage(root, state)
	if !cov.HasTraceData() {
		return nil
	}
	if escapeDisabled(state, escapeReqCoverage, reqCoverageDisableEnv) {
		recordAudit(root, &checklog.Entry{
			Check: checklog.CheckEscapeHatch, Passed: true, Checked: true, Level: checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  "escape-hatch: req coverage bypassed (FORGE_REQ_COVERAGE=disable)",
			Meta:    map[string]string{"escape.gate": "req-coverage", "escape.reason": checklog.EscapeReasonEnv, "escape.owner": "env"},
		})
		return nil
	}
	unverified := cov.UnverifiedIDs()
	entry := &checklog.Entry{
		Check: CheckNameReqCoverage, Passed: len(unverified) == 0, Checked: true, TaskRef: state.TaskRef,
		Level: checklog.LevelPass,
		Meta: map[string]string{
			"declared":   fmt.Sprint(len(cov.Declared)),
			"verified":   fmt.Sprint(len(cov.Verified)),
			"unverified": strings.Join(unverified, ","),
			"orphans":    strings.Join(cov.Orphans, ","),
		},
	}
	if len(unverified) == 0 {
		entry.Detail = fmt.Sprintf("全部 %d 条声明需求已验证", len(cov.Declared))
		recordAudit(root, entry)
		return nil
	}
	chain, _ := artifactchain.Load(root)
	if reqCoverageBlocking(chain) {
		entry.Level = checklog.LevelBlocked
		entry.Detail = "unverified requirements: " + strings.Join(unverified, ",")
		recordAudit(root, entry)
		return GateBlocked("task-complete 拒绝：%d 条声明需求未验证（%s）——给它们补带 accept[REQ-n] 的验收并实跑 verify-acceptance；产物链 spec/design 处于 rubric 及以上档位时本检查阻断，默认链只 advisory（首发即 blocked 于显式升档的链，见 docs/design/compat-commitments.md §二；逃生：%s=disable）",
			len(unverified), strings.Join(unverified, ", "), reqCoverageDisableEnv)
	}
	entry.Passed = true
	entry.Level = checklog.LevelAdvisory
	entry.Detail = "unverified requirements: " + strings.Join(unverified, ",")
	recordAudit(root, entry)
	fmt.Fprintf(os.Stderr, "%s\n", GateAdvisory("[task-complete] %s", FormatRequirementCoverage(cov)))
	return nil
}
