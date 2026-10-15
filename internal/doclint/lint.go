package doclint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Issue is one lint finding, line-addressed so the fix is actionable (the
// code-review-gate "every finding carries file:line" discipline applies to doc
// linting too.
//
// Issue 是一条 lint 发现，带行号使修复可操作（code-review-gate「每条发现带
// file:line」的纪律同样适用于文档 lint——整份文件「看起来冗长」的判词不可证伪）。
type Issue struct {
	Line     int      `json:"line"`
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// Hard reports whether the issue blocks (D1/D2/D5/D6 hard; D3/D4/D7/D8 advisory; D9-D11 follow their type's Advisory flag).
//
// Hard 报告该条是否阻断（D1/D2/D5/D6 硬；D3/D4/D7/D8 建议；D9-D11 随所属类型的
// Advisory 标记）。
func (i Issue) Hard() bool { return i.Severity == Hard }

// SkipMarker 让整个文件退出 doc lint（检查前 SkipScanLines 行）。
// 引用了禁令清单本身的元文档（本规则表自身的文档、设计留档）携带它——
// 把短语当数据引用不等于使用它。
const SkipMarker = "forge-doc-lint: skip"

// SkipScanLines 是扫描 SkipMarker 的头部行数。
const SkipScanLines = 10

// D8 结论前置窗口（typed docs）：必填标题看前 3 个 heading，结论枚举看前
// 10 行非围栏散文。D5/D6 判「有无」，D8 判「靠前」——D8 只在对应 D5/D6
// 通过时才出手，缺章节/缺枚举由硬规则兜底。
const (
	earlyHeadingLimit = 3
	earlyProseWindow  = 10
)

// exemptPathFrags 是永不 lint 的路径片段：依赖、构建产物、agent 会话状态、
// 归档、生成的 changelog、测试夹具（夹具为测 linter 刻意包含禁令短语）、
// append-only 决策日志（decisions.md 逐字引用诊断散文，由 skill-decisions
// guardrail 机器消费——是治理记录不是给人即时阅读的交付物）。
// skillintegrate/notes/ 同理：go:embed 的集成记录经 `forge skills integration`
// 供读，文件名即 skill 名——「session-retrospective.md」会按 BASE 名永久撞上
// retrospective 类型（D5-D8 对记录文本索要交付物结构）。
var exemptPathFrags = []string{
	"vendor/", "node_modules/", "dist/", ".git/", ".zcode/", "testdata/",
	"docs/skillhub-archive/", "changelog.md", "decisions.md",
	"skillintegrate/notes/",
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

// LintFile lints one markdown file.
//
// LintFile lint 单个 markdown 文件。豁免路径与 SkipMarker 文件返回空
// （未扫，不等于通过）。读取错误向上返回。
func LintFile(path string) ([]Issue, error) {
	return LintFileAs(path, path)
}

// LintFileAs lints the file at path but picks the DocType from typeName — for content stored under a generic name (task artifacts are all <stage>.md) whose real type is known from where it came from.
//
// LintFileAs lint path 处的文件，但按 typeName 选 DocType——内容存在通用名下（任务产物
// 一律 <stage>.md）、真实类型由来源文件名决定时用。豁免与 SkipMarker 仍按 path 判定。
func LintFileAs(path, typeName string) ([]Issue, error) {
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
	return LintText(typeName, text), nil
}

// LintText runs D1-D11 over text. filename (a path) is used only for DocType matching.
//
// LintText 对文本跑 D1-D11。filename（路径）只用于 DocType 匹配：先按 base 名，再按 PathContains 片段。
func LintText(filename, text string) []Issue {
	lines := strings.Split(text, "\n")
	var issues []Issue

	fenceOpen := false
	var fenceMarker string
	headings := []string{}
	diffFingerprintsOutsideFence := 0
	firstDiffLine := 0
	// nonFenced 只收集散文行：D4 证据标记与 D7 篇幅上限不得被围栏代码满足/
	// 撑大（围栏里的命令块是示例，不是「断言被实跑过」的证据）。
	var nonFenced strings.Builder
	// prose 是非围栏行及其原文件行号（D9-D11 逐段/逐行报告用）。
	var prose []proseLine
	// earlyProse 是前 earlyProseWindow 行非围栏散文（D8 结论位置窗口）。
	var earlyProse strings.Builder
	proseLines := 0
	// firstClaimLine 是散文中首个通过性断言的原文件行号（D4 触发；0=无）。
	firstClaimLine := 0
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
			} else if string(trimmed[0]) == fenceMarker && n >= fenceRunLen && strings.TrimSpace(trimmed[n:]) == "" {
				// CommonMark：只有纯标记行（无 info string）才能闭栏——```markdown
				// 示例内嵌的 ```go 不得提前关掉外栏，把示例散文漏进散文 lint。
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
		prose = append(prose, proseLine{no: lineNo, text: raw})
		proseLines++
		if proseLines <= earlyProseWindow {
			earlyProse.WriteString(raw)
			earlyProse.WriteByte('\n')
		}

		if firstClaimLine == 0 && passClaimRe.MatchString(raw) {
			firstClaimLine = lineNo
		}

		if strings.HasPrefix(trimmed, "#") {
			headings = append(headings, strings.ToLower(trimmed))
		}

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
	if dt := matchDocTypePath(filename); dt != nil && !isTemplateFile(filename) {
		nonFencedLines := strings.Count(nonFenced.String(), "\n")
		issues = append(issues, lintDocType(*dt, headings, text, nonFencedLines, earlyProse.String(), prose)...)
	}

	return issues
}

// fenceMarkerLen 返回行首 ```/~~~ run 的长度（非围栏标记行返回 0）。
// 语言后缀（```bash）不计入 run 长度。
//
// enumTokenPresent 判断结论枚举 token 是否出现在文档文本中。短 ASCII token
// （GO/NO-GO）必须词边界匹配：裸子串匹配会让日常单词（cargo、golang）满足 D6
// 硬规则而无需任何真实结论。非 ASCII token 保持子串语义（CJK 无词边界）。
func enumTokenPresent(lowerText, tok string) bool {
	low := strings.ToLower(tok)
	if low == "" {
		return false
	}
	ascii := true
	for _, r := range low {
		if r > 0x7E {
			ascii = false
			break
		}
	}
	if !ascii {
		return strings.Contains(lowerText, low)
	}
	re, err := regexp.Compile(`\b` + regexp.QuoteMeta(low) + `\b`)
	if err != nil {
		return strings.Contains(lowerText, low)
	}
	return re.MatchString(lowerText)
}

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

// isTemplateFile 判断文件名是否为 doc-generator 模板（结构定义，
// 豁免 D5-D7 实例规则）。
func isTemplateFile(filename string) bool {
	return strings.HasPrefix(strings.ToLower(filepath.Base(filename)), "template-")
}

// lintDocType 应用 D5（必填章节）、D6（结论枚举）、D7（篇幅上限）与 D8
// （结论位置——D5/D6 判有无，D8 判靠前；earlyProse 是前 earlyProseWindow
// 行非围栏散文），SurveyRules 类型再加 D9-D11（prose 是非围栏行及其行号）。
func lintDocType(dt DocType, headings []string, text string, lineCount int, earlyProse string, prose []proseLine) []Issue {
	issues := lintDocTypeRules(dt, headings, text, lineCount, earlyProse)
	if dt.SurveyRules {
		issues = append(issues, lintSurveyRules(dt, prose)...)
	}
	if dt.Advisory {
		for i := range issues {
			issues[i].Severity = Advisory
		}
	}
	return issues
}

// headingIndex returns the index of the first heading containing any of the
// "|"-separated alternatives in want (-1 = none).
//
// headingIndex 返回首个包含 want 中任一 "|" 分隔同义写法的标题下标（-1 = 无）。
func headingIndex(headings []string, want string) int {
	for idx, h := range headings {
		for _, alt := range strings.Split(strings.ToLower(want), "|") {
			if alt = strings.TrimSpace(alt); alt != "" && strings.Contains(h, alt) {
				return idx
			}
		}
	}
	return -1
}

// frontmatterTierRe captures `tier:` inside a leading YAML frontmatter block.
var frontmatterTierRe = regexp.MustCompile(`(?m)^tier:\s*([A-Za-z]+)\s*$`)

// frontmatterTier returns the `tier:` value of the leading frontmatter ("" = none).
//
// frontmatterTier 返回开头 frontmatter 的 `tier:` 值（"" = 无）。
func frontmatterTier(text string) string {
	if !strings.HasPrefix(text, "---\n") {
		return ""
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return ""
	}
	if m := frontmatterTierRe.FindStringSubmatch(text[4 : 4+end]); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

func lintDocTypeRules(dt DocType, headings []string, text string, lineCount int, earlyProse string) []Issue {
	var issues []Issue
	lowerText := strings.ToLower(text)

	for _, want := range dt.AnywhereHeadings {
		if headingIndex(headings, want) < 0 {
			issues = append(issues, Issue{
				Line: 1, Rule: "D5", Severity: Hard,
				Message: fmt.Sprintf("%s 缺必填章节「%s」", dt.ID, want),
			})
		}
	}

	for _, want := range dt.RequiredHeadings {
		foundAt := headingIndex(headings, want)
		if foundAt < 0 {
			issues = append(issues, Issue{
				Line: 1, Rule: "D5", Severity: Hard,
				Message: fmt.Sprintf("%s 缺必填章节「%s」", dt.ID, want),
			})
			continue
		}
		if foundAt >= earlyHeadingLimit {
			issues = append(issues, Issue{
				Line: 1, Rule: "D8", Severity: Advisory,
				Message: fmt.Sprintf("%s 必填章节「%s」排在第 %d 个标题——结论章节须在前 %d 个标题内（结论前置）", dt.ID, want, foundAt+1, earlyHeadingLimit),
			})
		}
	}

	if len(dt.ConclusionEnum) > 0 {
		found := false
		for _, tok := range dt.ConclusionEnum {
			if enumTokenPresent(lowerText, tok) {
				found = true
				break
			}
		}
		if !found {
			issues = append(issues, Issue{
				Line: 1, Rule: "D6", Severity: Hard,
				Message: fmt.Sprintf("%s 缺结论枚举（须含 %s 之一）", dt.ID, strings.Join(dt.ConclusionEnum, " / ")),
			})
		} else {
			// D6 已判「有」，D8 判「靠前」：枚举拖到文档后部 = 结论后置。
			lowerEarly := strings.ToLower(earlyProse)
			inEarly := false
			for _, tok := range dt.ConclusionEnum {
				if enumTokenPresent(lowerEarly, tok) {
					inEarly = true
					break
				}
			}
			if !inEarly {
				issues = append(issues, Issue{
					Line: 1, Rule: "D8", Severity: Advisory,
					Message: fmt.Sprintf("%s 结论枚举直到文档后部才出现——判定须在前 %d 行散文内（结论前置）", dt.ID, earlyProseWindow),
				})
			}
		}
	}

	maxLines := dt.MaxBodyLines
	label := dt.ID
	if tier := frontmatterTier(text); tier != "" {
		if n, ok := dt.MaxBodyLinesByTier[tier]; ok {
			maxLines = n
			label = dt.ID + "(" + tier + ")"
		}
	}
	if maxLines > 0 && lineCount > maxLines {
		issues = append(issues, Issue{
			Line: 1, Rule: "D7", Severity: Advisory,
			Message: fmt.Sprintf("%s 共 %d 行超篇幅上限 %d 行——明细下沉附录或拆分", label, lineCount, maxLines),
		})
	}
	return issues
}

// hasEvidenceMarker 判断文本是否携带任一 D4 豁免证据标记。
func hasEvidenceMarker(text string) bool {
	for _, re := range evidenceMarkerRes {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

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

// proseLine is one non-fenced line with its original file line number.
//
// proseLine 是一行非围栏文本及其原文件行号。
type proseLine struct {
	no   int
	text string
}

// lintSurveyRules applies D9 (no URL in the whole document; a paragraph or table row with a percentage/multiplier but no citation marker), D10 (no confidence marker anywhere) and D11 (internal research-pipeline labels, reported per line). Severity is Hard here; lintDocType downgrades it when the type is Advisory.
//
// lintSurveyRules 应用 D9（全文无 URL；含百分比/倍数的段落或表格行无引用标记）、D10
// （全文无信度标注）、D11（调研流水线内部标签，逐行报告）。此处给 Hard，类型为 Advisory
// 时由 lintDocType 统一降级。段落按空行、标题与表格行切分；脚注定义行本身含 URL。
func lintSurveyRules(dt DocType, prose []proseLine) []Issue {
	var issues []Issue
	var all strings.Builder
	for _, l := range prose {
		all.WriteString(l.text)
		all.WriteByte('\n')
	}
	body := all.String()
	if !surveyURLRe.MatchString(body) {
		issues = append(issues, Issue{Line: 1, Rule: "D9", Severity: Hard,
			Message: fmt.Sprintf("%s 全文无 URL 来源——调研结论须可追溯到原文（脚注定义行带 URL 与日期）", dt.ID)})
	}

	flush := func(start int, para string) {
		if para != "" && surveyNumericRe.MatchString(para) && !surveyCitationRe.MatchString(para) {
			issues = append(issues, Issue{Line: start, Rule: "D9", Severity: Hard,
				Message: "含百分比/倍数的段落没有引用标记——数字须挂 [^N^] 或 URL"})
		}
	}
	paraStart, para := 0, ""
	for _, l := range prose {
		trimmed := strings.TrimSpace(l.text)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			flush(paraStart, para)
			paraStart, para = 0, ""
		case strings.HasPrefix(trimmed, "|"):
			flush(paraStart, para)
			paraStart, para = 0, ""
			if surveyNumericRe.MatchString(trimmed) && !surveyCitationRe.MatchString(trimmed) {
				issues = append(issues, Issue{Line: l.no, Rule: "D9", Severity: Hard,
					Message: "含百分比/倍数的表格行没有引用标记——数字须挂 [^N^] 或 URL"})
			}
		default:
			if paraStart == 0 {
				paraStart = l.no
			}
			para += trimmed + "\n"
		}
	}
	flush(paraStart, para)

	if !surveyConfidenceRe.MatchString(body) {
		issues = append(issues, Issue{Line: 1, Rule: "D10", Severity: Hard,
			Message: fmt.Sprintf("%s 全文无信度标注——结论须标可信度（如「多源一致」「单一来源，未交叉验证」或 ✅⚠️❌）", dt.ID)})
	}
	for _, l := range prose {
		if m := surveyInternalLabelRe.FindString(stripInlineCode(l.text)); m != "" {
			issues = append(issues, Issue{Line: l.no, Rule: "D11", Severity: Hard,
				Message: fmt.Sprintf("调研流水线内部标签「%s」出现在交付物里——对外改用白话", m)})
		}
	}
	return issues
}
