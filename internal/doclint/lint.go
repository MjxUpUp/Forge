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
// build output, agent-session state, archives, generated changelog, test
// fixtures (fixtures deliberately contain banned phrases to exercise the
// linter), and append-only decision logs (decisions.md quotes diagnostic prose
// verbatim and is machine-consumed by the skill-decisions guardrail — it is a
// governance record, not a reader-facing deliverable).
//
// exemptPathFrags 是永不 lint 的路径片段：依赖、构建产物、agent 会话状态、
// 归档、生成的 changelog、测试夹具（夹具为测 linter 刻意包含禁令短语）、
// append-only 决策日志（decisions.md 逐字引用诊断散文，由 skill-decisions
// guardrail 机器消费——是治理记录不是给人即时阅读的交付物）。
var exemptPathFrags = []string{
	"vendor/", "node_modules/", "dist/", ".git/", ".zcode/", "testdata/",
	"docs/skillhub-archive/", "changelog.md", "decisions.md",
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
	// nonFenced collects prose lines only: D4 evidence markers and D7 line caps
	// must not be satisfied/inflated by fenced code (a fenced command block is
	// an example, not evidence the doc's claims were actually run).
	//
	// nonFenced 只收集散文行：D4 证据标记与 D7 篇幅上限不得被围栏代码满足/
	// 撑大（围栏里的命令块是示例，不是「断言被实跑过」的证据）。
	var nonFenced strings.Builder
	// firstClaimLine is the original line no. of the first prose pass-claim
	// (D4 trigger; 0 = none).
	//
	// firstClaimLine 是散文中首个通过性断言的原文件行号（D4 触发；0=无）。
	firstClaimLine := 0
	// fenceRunLen is the opening fence's marker length (3+ backticks/tildes).
	// CommonMark closes only with a run of >= that length — a 4-backtick outer
	// fence containing 3-backtick examples must not be closed early by the
	// inner marker (which would lint example prose as real prose).
	//
	// fenceRunLen 是开栏围栏的标记长度（≥3 个反引号/波浪号）。CommonMark 仅以
	// ≥该长度的 run 闭栏——4 反引号外栏内嵌 3 反引号示例时不得被内层提前
	// 闭栏（否则示例散文被当真实散文 lint）。
	fenceRunLen := 0

	for idx, raw := range lines {
		lineNo := idx + 1
		trimmed := strings.TrimSpace(raw)
		if n := fenceMarkerLen(trimmed); n > 0 {
			if !fenceOpen {
				fenceOpen = true
				fenceMarker = string(trimmed[0])
				fenceRunLen = n
			} else if string(trimmed[0]) == fenceMarker && n >= fenceRunLen {
				fenceOpen = false
				fenceMarker = ""
				fenceRunLen = 0
			}
			continue
		}
		if fenceOpen {
			continue
		}
		nonFenced.WriteString(raw)
		nonFenced.WriteByte('\n')

		if firstClaimLine == 0 && passClaimRe.MatchString(raw) {
			firstClaimLine = lineNo
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
	// document's PROSE. BOTH the claim trigger and the evidence scan are
	// prose-only: a claim that exists only inside a fence is an example (a
	// question like 测试通过了？ or a criteria description), not the document
	// asserting its own verification — flagging it points the reader at a fence
	// line (review round 2 finding N1/N2). firstClaimLine keeps the ORIGINAL
	// file line (nonFenced's own numbering diverges once fences are dropped).
	//
	// D4：通过性断言须与文档散文中至少一个证据标记共存。触发与证据扫描都只在
	// 散文：仅存在于围栏内的「断言」是示例（如「测试通过了？」设问、判据描述），
	// 不是文档自证实跑——命中它会指向围栏行（复审二轮 N1/N2）。firstClaimLine
	// 保留原文件行号（剔除围栏后 nonFenced 自身行号会偏离）。
	if firstClaimLine > 0 && !hasEvidenceMarker(nonFenced.String()) {
		issues = append(issues, Issue{
			Line: firstClaimLine, Rule: "D4", Severity: Advisory,
			Message: "通过性断言无证据标记——正文（非代码块）须含命令/输出引用（反引号）、file:line、URL 或百分比之一",
		})
	}

	// D5-D7: type-scoped rules. Template files (doc-generator's references/
	// template-*.md) define structure rather than being filled instances —
	// their骨架 lives inside code fences — so instance rules skip them;
	// universal D1-D4 still apply (a template must not use the phrases it bans).
	// D7 counts non-fenced lines only: the cap targets prose bloat, a report
	// embedding long fenced output is a structure choice, not verbosity.
	//
	// D5-D7：类型作用域规则。模板文件（doc-generator 的 references/
	// template-*.md）是结构定义不是填写实例——骨架在代码围栏内——
	// 实例规则跳过；通用 D1-D4 仍生效（模板不得裸用自己禁掉的短语）。
	// D7 只数非围栏行：上限管的是散文膨胀，嵌长输出围栏的报告是结构选择
	// 不是冗长。
	if dt := matchDocType(filename); dt != nil && !isTemplateFile(filename) {
		nonFencedLines := strings.Count(nonFenced.String(), "\n")
		issues = append(issues, lintDocType(*dt, headings, text, nonFencedLines)...)
	}

	return issues
}

// fenceMarkerLen returns the length of a leading ```/~~~ run (0 if the line is
// not a fence marker). Info-string suffixes (```bash) do not extend the run.
//
// fenceMarkerLen 返回行首 ```/~~~ run 的长度（非围栏标记行返回 0）。
// 语言后缀（```bash）不计入 run 长度。
func fenceMarkerLen(trimmed string) int {
	if len(trimmed) < 3 {
		return 0
	}
	c := trimmed[0]
	if c != '`' && c != '~' {
		return 0
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == c {
		n++
	}
	if n < 3 {
		return 0
	}
	return n
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
// flagged. An odd backtick count means the line ends inside an unterminated
// span — the trailing segment is dropped (the conservative direction: less
// text to match, i.e. prefer a miss over flagging a quoted example).
//
// stripInlineCode 移除 `...` 片段，使「作为数据引用」的短语不被命中。
// 反引号数量为奇数说明行尾落在未闭合 span 内——丢弃尾部片段（保守方向：
// 匹配面更小，即宁可漏报也不误伤引用示例）。
func stripInlineCode(line string) string {
	parts := strings.Split(line, "`")
	var sb strings.Builder
	for i, p := range parts {
		if i%2 == 0 {
			sb.WriteString(p)
		}
	}
	return sb.String()
}
