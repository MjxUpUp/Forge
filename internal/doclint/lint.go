package doclint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Issue is one lint finding, line-addressed so the fix is actionable (the
// code-review-gate "every finding carries file:line" discipline applies to
// doc linting too — a whole-file "looks verbose" verdict is unfalsifiable).
//
// Issue 是一条 lint 发现，带行号使修复可操作（code-review-gate「每条发现带
// file:line」的纪律同样适用于文档 lint——整份文件「看起来冗长」的判词不可证伪）。
type Issue struct {
	Line     int      `json:"line"`
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// Hard reports whether the issue blocks (D1/D2/D5/D6 hard; D3/D4/D7 advisory).
//
// Hard 报告该条是否阻断（D1/D2/D5/D6 硬；D3/D4/D7 建议）。
func (i Issue) Hard() bool { return i.Severity == Hard }

// SkipMarker opts a whole file out of doc lint (first SkipScanLines lines).
// Meta-documents that quote the banned list (this rule table's own docs, the
// design record) carry it; quoting a phrase as data is not using it.
//
// SkipMarker 让整个文件退出 doc lint（检查前 SkipScanLines 行）。
// 引用了禁令清单本身的元文档（本规则表自身的文档、设计留档）携带它——
// 把短语当数据引用不等于使用它。
const SkipMarker = "forge-doc-lint: skip"

// SkipScanLines is how many leading lines are scanned for SkipMarker.
//
// SkipScanLines 是扫描 SkipMarker 的头部行数。
const SkipScanLines = 10

// exemptPathFrags are path fragments that are never linted: dependencies,
// build output, agent-session state, archives, generated changelog, and test
// fixtures (fixtures deliberately contain banned phrases to exercise the linter).
//
// exemptPathFrags 是永不 lint 的路径片段：依赖、构建产物、agent 会话状态、
// 归档、生成的 changelog、测试夹具（夹具为测 linter 刻意包含禁令短语）。
var exemptPathFrags = []string{
	"vendor/", "node_modules/", "dist/", ".git/", ".zcode/", "testdata/",
	"docs/skillhub-archive/", "CHANGELOG.md",
}

// PathExempt reports whether a slash-normalized relative path is exempt.
//
// PathExempt 判断斜杠规范化的相对路径是否豁免。
func PathExempt(path string) bool {
	norm := strings.ReplaceAll(path, "\\", "/")
	lower := strings.ToLower(norm)
	for _, frag := range exemptPathFrags {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}

// LintFile lints one markdown file. Exempt paths and SkipMarker files return
// no issues (not "pass" — unscanned). Read errors are returned.
//
// LintFile lint 单个 markdown 文件。豁免路径与 SkipMarker 文件返回空
// （未扫，不等于通过）。读取错误向上返回。
func LintFile(path string) ([]Issue, error) {
	if PathExempt(path) {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	head := strings.SplitN(text, "\n", SkipScanLines+1)
	for _, line := range head {
		if strings.Contains(line, SkipMarker) {
			return nil, nil
		}
	}
	return LintText(path, text), nil
}

// LintText runs D1-D7 over text. filename is used only for DocType matching.
//
// LintText 对文本跑 D1-D7。filename 只用于 DocType 匹配。
func LintText(filename, text string) []Issue {
	lines := strings.Split(text, "\n")
	var issues []Issue

	fenceOpen := false
	var fenceMarker string
	headings := []string{}
	diffFingerprintsOutsideFence := 0
	firstDiffLine := 0

	for idx, raw := range lines {
		lineNo := idx + 1
		trimmed := strings.TrimSpace(raw)

		// Fence tracking: a ```/~~~ toggle only at line start (indented fences
		// inside lists are still fences; list content is not).
		//
		// 围栏跟踪：行首的 ```/~~~ 才切换围栏状态（列表内缩进围栏也是围栏，
		// 列表内容不是）。
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			if !fenceOpen {
				fenceOpen = true
				fenceMarker = marker
			} else if marker == fenceMarker {
				fenceOpen = false
				fenceMarker = ""
			}
			continue
		}
		if fenceOpen {
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			headings = append(headings, strings.ToLower(trimmed))
		}

		// D3: raw unified-diff fingerprint outside fences = diff restatement.
		//
		// D3：围栏外的原始 unified diff 指纹 = 复述 diff。
		for _, re := range diffFingerprints {
			if re.MatchString(raw) {
				diffFingerprintsOutsideFence++
				if firstDiffLine == 0 {
					firstDiffLine = lineNo
				}
				break
			}
		}

		// D1/D2 run against inline-code-stripped lines: quoting a phrase inside
		// backticks (rule tables, examples) is data, not usage.
		//
		// D1/D2 对剥除行内代码后的行匹配：反引号内引用短语（规则表、示例）
		// 是数据不是使用。
		stripped := stripInlineCode(raw)
		for _, p := range BannedPhrases {
			if p.Pattern.MatchString(stripped) {
				issues = append(issues, Issue{
					Line: lineNo, Rule: "D1", Severity: Hard,
					Message: fmt.Sprintf("禁令短语命中「%s」：%s", p.Pattern.String(), p.Reason),
				})
			}
		}
		for _, p := range EvidenceFreeConclusions {
			if p.Pattern.MatchString(stripped) {
				issues = append(issues, Issue{
					Line: lineNo, Rule: "D2", Severity: Hard,
					Message: fmt.Sprintf("无证据整体结论命中「%s」：%s", p.Pattern.String(), p.Reason),
				})
			}
		}
	}

	if diffFingerprintsOutsideFence >= diffFingerprintThreshold {
		issues = append(issues, Issue{
			Line: firstDiffLine, Rule: "D3", Severity: Advisory,
			Message: fmt.Sprintf("围栏外发现 %d 行 unified diff 指纹——复述 diff（读者看 diff 本体，正文写动机与验证）", diffFingerprintsOutsideFence),
		})
	}

	// D4: pass-claims must co-occur with at least one evidence marker in the
	// whole document (file-level judgment — the claim itself may be anywhere).
	//
	// D4：通过性断言须与全文至少一个证据标记共存（文件级判定——断言可在任意位置）。
	if m := passClaimRe.FindStringIndex(text); m != nil {
		if !hasEvidenceMarker(text) {
			claimLine := 1 + strings.Count(text[:m[0]], "\n")
			issues = append(issues, Issue{
				Line: claimLine, Rule: "D4", Severity: Advisory,
				Message: "通过性断言无证据标记——正文须含命令/输出引用（反引号）、file:line、URL 或百分比之一",
			})
		}
	}

	// D5-D7: type-scoped rules. Template files (doc-generator's references/
	// template-*.md) define structure rather than being filled instances —
	// their骨架 lives inside code fences — so instance rules skip them;
	// universal D1-D4 still apply (a template must not use the phrases it bans).
	//
	// D5-D7：类型作用域规则。模板文件（doc-generator 的 references/
	// template-*.md）是结构定义不是填写实例——骨架在代码围栏内——
	// 实例规则跳过；通用 D1-D4 仍生效（模板不得裸用自己禁掉的短语）。
	if dt := matchDocType(filename); dt != nil && !isTemplateFile(filename) {
		issues = append(issues, lintDocType(*dt, headings, text, len(lines))...)
	}

	return issues
}

// isTemplateFile reports whether the base filename is a doc-generator template
// (structure definition, exempt from D5-D7 instance rules).
//
// isTemplateFile 判断文件名是否为 doc-generator 模板（结构定义，
// 豁免 D5-D7 实例规则）。
func isTemplateFile(filename string) bool {
	return strings.HasPrefix(strings.ToLower(filepath.Base(filename)), "template-")
}

// lintDocType applies D5 (required headings), D6 (conclusion enum) and
// D7 (body-line cap).
//
// lintDocType 应用 D5（必填章节）、D6（结论枚举）与 D7（篇幅上限）。
func lintDocType(dt DocType, headings []string, text string, lineCount int) []Issue {
	var issues []Issue
	lowerText := strings.ToLower(text)

	for _, want := range dt.RequiredHeadings {
		found := false
		for _, h := range headings {
			if strings.Contains(h, strings.ToLower(want)) {
				found = true
				break
			}
		}
		if !found {
			issues = append(issues, Issue{
				Line: 1, Rule: "D5", Severity: Hard,
				Message: fmt.Sprintf("%s 缺必填章节「%s」", dt.ID, want),
			})
		}
	}

	if len(dt.ConclusionEnum) > 0 {
		found := false
		for _, tok := range dt.ConclusionEnum {
			if strings.Contains(lowerText, strings.ToLower(tok)) {
				found = true
				break
			}
		}
		if !found {
			issues = append(issues, Issue{
				Line: 1, Rule: "D6", Severity: Hard,
				Message: fmt.Sprintf("%s 缺结论枚举（须含 %s 之一）", dt.ID, strings.Join(dt.ConclusionEnum, " / ")),
			})
		}
	}

	if dt.MaxBodyLines > 0 && lineCount > dt.MaxBodyLines {
		issues = append(issues, Issue{
			Line: 1, Rule: "D7", Severity: Advisory,
			Message: fmt.Sprintf("%s 共 %d 行超篇幅上限 %d 行——明细下沉附录或拆分", dt.ID, lineCount, dt.MaxBodyLines),
		})
	}
	return issues
}

// hasEvidenceMarker reports whether text carries any D4-acquitting marker.
//
// hasEvidenceMarker 判断文本是否携带任一 D4 豁免证据标记。
func hasEvidenceMarker(text string) bool {
	for _, re := range evidenceMarkerRes {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// stripInlineCode removes `...` spans so quoted-as-data phrases are not
// flagged. Odd/unbalanced backticks fall back to the raw line (conservative:
// better one false negative than flagging a quoted example).
//
// stripInlineCode 移除 `...` 片段，使「作为数据引用」的短语不被命中。
// 反引号不配对时回退原始行（保守——宁可漏报也不误伤引用示例）。
func stripInlineCode(line string) string {
	parts := strings.Split(line, "`")
	if len(parts)%2 == 0 {
		return line
	}
	var sb strings.Builder
	for i, p := range parts {
		if i%2 == 0 {
			sb.WriteString(p)
		}
	}
	return sb.String()
}
