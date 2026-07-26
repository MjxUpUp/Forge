package taskpipeline

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// cheatscan.go — deterministic AI-cheat pattern scanner.
//
// Motivation (root cause in the forge-review-deterministic-shift memory): among the 11
// AI-cheat categories in code-review-gate, the mechanically detectable ones (type-suppression,
// error-swallow / dead-branch / comment-only-fix) were previously all judged by LLM sub-agents.
// LLMs re-sample the same diff every round, catching different subsets → the source of the
// perception that every review surfaces new problems.
//
// This scanner extracts those mechanical patterns into a deterministic check at task-verify time:
// scan task-scoped added lines (+ lines), and on hit record a checklog:cheat-scan entry
// (deterministic, advisory non-blocking). The LLM reviewer accordingly retreats to only
// semantic judgments (design/architecture/mock hallucinations).
//
// Only the added side: cheats almost always live on + lines (new type-suppression directives,
// new empty catches, new always-false branches). assertion-strip is not in this scanner —
// assertion-check.sh already covers it deterministically (Step 1 aggregated it into review-time
// conclusions as a separate task).
//
// cheatscan.go — deterministic AI-cheat 模式扫描器。
//
// 动机（根因见 forge-review-deterministic-shift memory）：code-review-gate 的
// 11 类 AI 作弊模式里，机械可检的那几类（type-suppression / error-swallow /
// dead-branch / comment-only-fix）此前全靠 LLM 子 agent 判断。LLM 每轮对同一 diff
// 重新采样、抓不同子集 → "每轮 review 都冒新问题"的体感来源。
//
// 本扫描器把这些机械模式抽到 task-verify 时的 deterministic 检测：扫任务范围的
// 新增行（+ 行），命中记一条 checklog:cheat-scan（deterministic，advisory 不阻塞）。
// LLM-reviewer 据此退到只做语义判断（设计/架构/mock 是否幻觉）。
//
// 只看新增侧：作弊几乎都在 + 行（新增类型抑制指令、新增空 catch、新增永假分支）。
// assertion-strip 不在本扫描器——assertion-check.sh 已 deterministic 覆盖（Step 1
// 把它聚合成 review 时结论是独立任务）。

// CheatPattern identifies a category of AI-cheat pattern detected by the scanner
// (the mechanically detectable subset).
//
// CheatPattern 标识扫描器检测的一类 AI 作弊模式（机械可检的子集）。
type CheatPattern string

const (
	// CheatTypeSuppression: new type/warning suppression directives — TS ts-ignore/ts-nocheck/ts-expect-error,
	// eslint disable, Rust allow attributes, Python mypy type-ignore, Java SuppressWarnings annotations.
	// Hiding warnings rather than fixing them. (Comments deliberately do not concatenate the directive sigil;
	// see the typeSuppressionRe note.)
	//
	// CheatTypeSuppression：新增类型/告警抑制指令——TS 的 ts-ignore/ts-nocheck/ts-expect-error、
	// eslint 的 disable、Rust 的 allow 属性、Python mypy 的 type-ignore、Java 的
	// SuppressWarnings 注解。把警告藏起来而非解决。（注释里不连写指令 sigil，见 typeSuppressionRe 注。）
	CheatTypeSuppression CheatPattern = "type-suppression"
	// CheatErrorSwallow: new empty catch / except...: pass — silently swallows errors, problems never surface.
	//
	// CheatErrorSwallow：新增空 catch / except...: pass——静默吞掉错误，问题永不暴露。
	CheatErrorSwallow CheatPattern = "error-swallow"
	// CheatDeadBranch: new always-false branches (if(false)/if(0)/if(1===2)) — looks like edges handled, never executes.
	//
	// CheatDeadBranch：新增永假分支（if(false)/if(0)/if(1===2)）——看起来处理了边界，实际永不执行。
	CheatDeadBranch CheatPattern = "dead-branch"
	// CheatCommentOnly: a source file whose added lines are all comments/blank with zero logic change —
	// suspected claim-a-fix-but-only-added-comments. Heuristic (severity=low): pure-doc tasks may false-fire; advisory prompts review.
	//
	// CheatCommentOnly：某源码文件的新增行全是注释/空行、零逻辑变更——疑似"声称修复但只加了注释"。
	// 启发式（severity=low）：纯文档任务可能误报，advisory 提示核查而非定罪。
	CheatCommentOnly CheatPattern = "comment-only-fix"
	// CheatCommentDebt: debt markers inside new comment lines — AI lazily flags something is wrong/todo
	// without resolving it in this change. The reverse-laziness-ladder level 0 (comments replacing action
	// → code-rot root): looks responsible (annotated) but zero action, no one follows up. severity=low:
	// legitimate follow-up markers also hit; advisory prompts review (convert to a forge task or fix on
	// the spot) rather than convict. detectCommentDebt scans only comment lines; marker words are
	// concatenated via debtMarkerWords so the scanner does not false-flag its own patterns as debt.
	//
	// CheatCommentDebt：新增注释行里的"债务标记"——AI 偷懒用注释标识"这里有问题/待办"
	// 但本变更不解决。是懒惰阶梯反第 0 级（注释替代行动 → 屎山根源）：看起来负责任（标注了），
	// 实际零行动，后续无人跟进。severity=low：合理的后续跟踪标记也会命中，advisory 提示核查
	// （转 forge task 跟踪 或 当场修）而非定罪。detectCommentDebt 只扫注释行；标记词用
	// debtMarkerWords 拼接，避免扫描器扫自身源码时把模式定义/注释里的词误判为债务。
	CheatCommentDebt CheatPattern = "comment-as-debt"
)

// CheatFinding is a single mechanically detected suspected cheat. Advisory — detection may
// false-fire, leaves a trail for review, never blocks.
//
// CheatFinding 是一次机械检测到的疑似作弊。advisory——检测有假阳性可能，留痕供
// review 核查，绝不阻塞。
type CheatFinding struct {
	Pattern  CheatPattern `json:"pattern"`
	File     string       `json:"file"`
	Line     int          `json:"line,omitempty"`
	Snippet  string       `json:"snippet"`
	// Severity: high (mechanically high confidence) / low (heuristic).
	Severity string       `json:"severity"` // "high"（机械高置信）/ "low"（启发式）
}

// addedLine is a single added line from the task-scoped diff (just the + line content, owning
// file, and new-file line number).
//
// addedLine 是任务范围 diff 的一条新增行（仅 + 行内容 + 归属文件 + 新文件行号）。
type addedLine struct {
	file   string
	lineNo int
	text   string
}

// ScanCheatPatterns scans task-scoped added lines and mechanically detects 4 AI-cheat patterns.
// Purely deterministic (computed by the gate, agent cannot forge). Returns findings (empty = clean).
// Failure-tolerant: on git/file-read errors it skips that source (returning what was collected),
// never panics — the reliability of advisory detection comes from what-it-catches-is-accurate,
// not from must-scan-everything.
//
// ScanCheatPatterns 扫描任务范围内的新增行，机械检测 4 类 AI 作弊模式。
// 纯 deterministic（gate 实算，agent 无法伪造）。返回 findings（空=干净）。
// 失败容忍：git/读文件出错时跳过该源（返回已收集的），绝不 panic——advisory 检测
// 的可靠性来自"扫到了就准"，不来自"必须扫全"。
//
// Three precision filters (avoid crying wolf when self-checking):
//  1. Exclude test files (isTestFile): tests often contain pattern strings as input (e.g. if (false)),
//     and the most common test-side cheat (assertion weakening) is already covered by assertion-check;
//     type/dead/error cheats almost only live in production source — excluding tests sharpens
//     precision with little loss (rare and low-risk).
//  2. dead-branch/error-swallow skip comment lines: patterns described in comments (// if false {)
//     are not real cheats.
//  3. type-suppression uses inStringLiteral to exclude literal mentions (see detectTypeSuppression):
//     directive names written out in regex/strings (e.g. inside regexp.MustCompile) are naming/
//     description, not real suppression; Python/Rust directives additionally require their #
//     prefix to avoid false-fire on plain-text type: ignore.
//
// Residual false-positives (doc comments referencing directive syntax, Python single-quote
// strings) are on the conservative side by design — advisory non-blocking, and deterministic-stable
// (no per-round LLM re-sampling), so the perception of new problems every round does not recur.
// That is why comments in this file deliberately avoid concatenating the full directive sigil,
// lest the scanner false-fire on its own source.
//
// 精度三过滤器（避免"自己检自己"时狼来了）：
//  1. 排除测试文件（isTestFile）：测试常含模式字符串作输入（"if (false)"），
//     且测试侧最常见作弊（断言弱化）由 assertion-check 已覆盖；type/dead/error
//     作弊几乎只在生产源码——排除测试文件大幅提精度，少漏（罕见且低危）。
//  2. dead-branch/error-swallow 跳过注释行：注释里描述模式（"// if false {"）不是
//     真作弊。
//  3. type-suppression 用 inStringLiteral 排除字面量提及（见 detectTypeSuppression）：
//     正则/字符串里写出的指令名（如 regexp.MustCompile 里）是命名/描述，不是真抑制；
//     Python/Rust 指令额外要求其 # 前缀，避免普通文本里的 "type: ignore" 误报。
//
// 残留假阳性（文档注释引用指令语法、Python 单引号串内）是已知保守侧——advisory
// 不阻塞，且 deterministic 稳定（不像 LLM 每轮重采样），不重现"每轮冒新问题"。
// 故本文件注释刻意不连写指令的完整 sigil，免得扫描器扫自己的源码时误报。
func ScanCheatPatterns(root string, state *TaskState) []CheatFinding {
	added := collectAddedLines(root, state)
	if len(added) == 0 {
		return nil
	}
	var prod []addedLine
	for _, a := range added {
		if !isTestFile(a.file) {
			prod = append(prod, a)
		}
	}
	if len(prod) == 0 {
		return nil
	}
	// Non-comment lines — used by dead-branch/error-swallow.
	var code []addedLine // 非注释行——dead-branch/error-swallow 用
	for _, a := range prod {
		if !isCommentOrBlank(a.text) {
			code = append(code, a)
		}
	}
	var findings []CheatFinding
	findings = append(findings, detectTypeSuppression(prod)...)
	findings = append(findings, detectErrorSwallow(code)...)
	findings = append(findings, detectDeadBranch(code)...)
	findings = append(findings, detectCommentOnly(prod)...)
	findings = append(findings, detectCommentDebt(prod)...)
	return findings
}

// --- Detectors ---
//
// --- 检测器 ---

// typeSuppressionRe: type/warning suppression directives, matched anywhere. Paired with
// inStringLiteral to exclude literal mentions — directive names written out in regex definitions
// (inside regexp.MustCompile) or strings are naming/description, not real suppression. Real
// suppressions are either leading annotations (Java SuppressWarnings, Rust allow attributes) or
// inside comments (TS ts-ignore family, eslint disable, Python mypy type-ignore). Python/Rust
// require their # prefix to avoid false-fire on plain-text type: ignore.
//
// Note: this comment and the CheatTypeSuppression comment deliberately avoid concatenating
// the full directive sigil (e.g. writing @ together with ts-ignore) — the scanner scans its own
// source, and a full directive text in a comment would be false-flagged as real suppression.
// Descriptive writing (directive names without sigil/prefix) does not trigger the regex, so it is safe.
//
// typeSuppressionRe：类型/告警抑制指令，任意位置匹配。配 inStringLiteral 排除字面量
// 提及——正则定义（regexp.MustCompile 里）或字符串里写出的指令名是命名/描述，不是真抑制。
// 真抑制要么是引领的注解（Java SuppressWarnings、Rust allow 属性），要么在注释里
// （TS ts-ignore 族、eslint disable、Python mypy type-ignore）。Python/Rust 要求其
// # 前缀，避免普通文本里的 "type: ignore" 误报。
//
// 注：本注释与 CheatTypeSuppression 注释刻意不连写指令的完整 sigil（如把 @ 与 ts-ignore
// 连写）——本扫描器会扫自己的源码，注释里的完整指令文本会被当成真抑制误报。描述性写法
// （指令名不带 sigil/前缀）不触发正则，故安全。
var typeSuppressionRe = []*regexp.Regexp{
	// TS whole-file suppression.
	regexp.MustCompile(`@ts-nocheck`),        // TS 整文件抑制
	// TS single-line suppression.
	regexp.MustCompile(`@ts-ignore`),         // TS 单行抑制
	// TS expected error (still a suppression).
	regexp.MustCompile(`@ts-expect-error`),   // TS 期望错误（仍是抑制）
	// eslint line/block disable.
	regexp.MustCompile(`eslint-disable`),     // eslint 行/块禁用
	// Python mypy (requires # prefix).
	regexp.MustCompile(`#\s*type:\s*ignore`), // Python mypy（要求 # 前缀）
	// Rust attribute (requires #[ prefix).
	regexp.MustCompile(`#\[allow`),           // Rust 属性（要求 #[ 前缀）
	// Java annotation.
	regexp.MustCompile(`@SuppressWarnings`),  // Java 注解
}

// detectTypeSuppression: new lines containing type/warning suppression directives (anywhere),
// excluding mentions inside string literals. Only one record per line (multiple suppressions on
// the same line collapse).
//
// detectTypeSuppression：新增行含类型/告警抑制指令（任意位置），但排除字符串字面量内
// 的提及。一行只记一次（同行的多个抑制归一）。
func detectTypeSuppression(added []addedLine) []CheatFinding {
	var out []CheatFinding
	for _, a := range added {
		for _, re := range typeSuppressionRe {
			loc := re.FindStringIndex(a.text)
			if loc == nil {
				continue
			}
			if inStringLiteral(a.text, loc[0]) {
				// Literal mention (inside regex definition/string) — not a real suppression.
				continue // 字面量提及（正则定义/字符串内）非真抑制
			}
			out = append(out, CheatFinding{
				Pattern:  CheatTypeSuppression,
				File:     a.file,
				Line:     a.lineNo,
				Snippet:  clip(a.text),
				Severity: "high",
			})
			break
		}
	}
	return out
}

// inStringLiteral reports whether position pos in line falls inside a string literal: counts
// unescaped " and ` before pos (odd = inside a string). Single quotes do not count — in Go/C/Rust
// they are character literals (no directive text); Python single-quote strings are rare and an
// acceptable false-positive. Mechanical approximation: true string awareness needs per-language
// tokenization, too heavy for advisory detection; this approximation already covers the most
// common literal mentions (Go raw strings, double-quoted strings).
//
// inStringLiteral 报 line 的 pos 位置是否落在字符串字面量内：数 pos 之前未转义的 " 和 `
// 的奇偶（奇=在串内）。单引号不计——Go/C/Rust 里是字符字面量（不含指令文本），
// Python 单引号串是少见且可接受的假阳性。机械近似：真正的字符串感知需分语言 tokenize，
// 对 advisory 检测过重；本近似已覆盖最常见的字面量提及（Go raw string、双引号串）。
func inStringLiteral(line string, pos int) bool {
	count := 0
	escaped := false
	for i := 0; i < pos && i < len(line); i++ {
		c := line[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' || c == '`' {
			count++
		}
	}
	return count%2 == 1
}

var errorSwallowRe = []*regexp.Regexp{
	// Single-line empty catch: catch {} / catch (e) {} / catch (e: Err) {} — cross-language (JS/TS/Java/C#).
	//
	// 空 catch 单行：catch {} / catch (e) {} / catch (e: Err) {} —— 跨语言（JS/TS/Java/C#）。
	regexp.MustCompile(`\bcatch\s*(\([^)]*\))?\s*\{\s*\}`),
	// Python except ... : pass (same-line pass = real swallow).
	//
	// Python except ... : pass（同行的 pass = 真吞）。
	regexp.MustCompile(`\bexcept\b[^\n]*:\s*pass`),
}

// detectErrorSwallow: new empty catch / except:pass. Conservative single-line high-confidence signal —
// multi-line empty catches (body on later lines) are hard to judge line-by-line and go to the LLM; avoids bare except:.
//
// detectErrorSwallow：新增空 catch / except:pass。保守取单行高置信信号——多行空
// catch（body 在后续行）难逐行判，留给 LLM；避免 bare except: 的误报。
func detectErrorSwallow(added []addedLine) []CheatFinding {
	var out []CheatFinding
	for _, a := range added {
		for _, re := range errorSwallowRe {
			if re.MatchString(a.text) {
				out = append(out, CheatFinding{
					Pattern:  CheatErrorSwallow,
					File:     a.file,
					Line:     a.lineNo,
					Snippet:  clip(a.text),
					Severity: "high",
				})
				break
			}
		}
	}
	return out
}

var deadBranchRe = []*regexp.Regexp{
	// if (false) / if(false) / if (0) / if(0) / if (1===2) / if (1==2)
	regexp.MustCompile(`\bif\s*\(\s*(?:false|0|1\s*={2,3}\s*2|1\s*={1,2}\s*2)\s*\)`),
	// Go/Rust/JS lexing: if false {  /  Python: if False: / if 0:
	//
	// Go/Rust/JS 词法：if false {  /  Python: if False: / if 0:
	regexp.MustCompile(`\bif\s+(?:false|False|0)\b`),
	// while (false) / while (0)
	regexp.MustCompile(`\bwhile\s*\(\s*(?:false|0)\s*\)`),
}

// detectDeadBranch: new always-false branches. if(0) requires 0 to be followed immediately by )
// (so if(0===x) is not wrongly hit).
//
// detectDeadBranch：新增永假分支。if(0) 要求 0 后紧跟 )（不误伤 if(0===x)）。
func detectDeadBranch(added []addedLine) []CheatFinding {
	var out []CheatFinding
	for _, a := range added {
		for _, re := range deadBranchRe {
			if re.MatchString(a.text) {
				out = append(out, CheatFinding{
					Pattern:  CheatDeadBranch,
					File:     a.file,
					Line:     a.lineNo,
					Snippet:  clip(a.text),
					Severity: "high",
				})
				break
			}
		}
	}
	return out
}

// isCommentOrBlank reports whether a line (pure content, no diff prefix) is a comment or blank
// — a cross-language heuristic.
//
// isCommentOrBlank 判断一行（纯内容，无 diff 前缀）是否注释或空行——跨语言启发式。
func isCommentOrBlank(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return true
	}
	return strings.HasPrefix(t, "//") || // go/rs/js/ts/java/c/zig
		strings.HasPrefix(t, "#") || // py/rb/nim
		strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "*/")
}

// detectCommentOnly: per source file, look at added lines — if a file's added lines are all
// comments/blank with zero logic change, suspected claim-a-fix-but-only-added-comments.
// per-file rather than per-task: more precise (only flags problem files) and less noisy
// (3 files changed with 1 comment-only only flags that 1).
//
// detectCommentOnly：逐源码文件看新增行——若某文件的新增行全是注释/空行、零逻辑
// 变更，疑似"声称修复但只加注释"。per-file 而非 per-task：更精确（只标问题文件），
// 也减少噪声（3 文件改 1 个 comment-only 只标那 1 个）。
func detectCommentOnly(added []addedLine) []CheatFinding {
	byFile := make(map[string][]addedLine)
	for _, a := range added {
		byFile[a.file] = append(byFile[a.file], a)
	}
	var out []CheatFinding
	for f, lines := range byFile {
		allComment := true
		for _, a := range lines {
			if !isCommentOrBlank(a.text) {
				allComment = false
				break
			}
		}
		if allComment {
			out = append(out, CheatFinding{
				Pattern:  CheatCommentOnly,
				File:     f,
				Line:     lines[0].lineNo,
				Snippet:  "新增行全为注释/空行，零逻辑变更",
				Severity: "low",
			})
		}
	}
	return out
}

// detectCommentDebt detects debt markers inside new comment lines — flagging problems without
// resolving them in this change; the reverse-laziness-ladder level 0 (comments replacing action
// → code-rot root). severity=low: legitimate follow-up markers also hit; advisory prompts review
// (convert to a forge task or fix on the spot) rather than convict.
//
// Only comment lines are scanned (isCommentOrBlank): marker words in code lines may be
// variable/string names, not comment debt. Markers concatenate via debtMarkerWords; this
// comment avoids spelling out concrete markers so the scanner does not false-flag its own
// patterns (same sigil handling as typeSuppressionRe).
//
// detectCommentDebt 检测新增注释行里的"债务标记"——标识问题但不在本变更解决，是懒惰
// 懒惰阶梯反第 0 级（注释替代行动 → 屎山根源）。severity=low：合理的后续跟踪标记也会命中，
// advisory 提示核查（转 forge task 跟踪 或 当场修）而非定罪。
//
// 只扫注释行（isCommentOrBlank）：代码行里的标记词可能是变量/字符串名，不算注释债务。
// 标记词用 debtMarkerWords 拼接 + 本注释不连写具体标记词，避免扫描器扫自身源码时把
// 模式定义/注释里的词误判为债务（同 typeSuppressionRe 的 sigil 处理）。
func detectCommentDebt(added []addedLine) []CheatFinding {
	var out []CheatFinding
	for _, a := range added {
		if !isCommentOrBlank(a.text) {
			continue
		}
		if !commentDebtRe.MatchString(a.text) {
			continue
		}
		out = append(out, CheatFinding{
			Pattern:  CheatCommentDebt,
			File:     a.file,
			Line:     a.lineNo,
			Snippet:  clip(a.text),
			Severity: "low",
		})
	}
	return out
}

// debtMarkerWords is the regex fragment for comment-debt markers (the 4 conventional English
// debt words). String concatenation avoids writing the full words in source — the scanner scans
// its own source, and concatenated marker words would be false-flagged as real debt.
//
// debtMarkerWords 是注释债务标记的 regex 片段（英文惯例的 4 类债务词）。字符串拼接
// 避免在源码里连写完整词——本扫描器会扫自身源码，连写的标记词会被当成真债务误报。
const debtMarkerWords = "TO" + "DO" + "|FIX" + "ME" + "|XXX|HACK"

// commentDebtRe matches comment-debt markers. The 4 English words use \b word boundaries
// (case-sensitive — avoids false-fire on lowercase variable names); Chinese has no word boundaries,
// so isolated high-frequency words need collocation noise reduction: the first branch requires a
// following action verb, the second limits to two tail characters — avoiding false-fires in normal
// contexts that dilute the signal. Recall trade-off: collocation under-reports some real debts
// (action verb interrupted by spacing does not hit) in exchange for not false-firing on frequent
// normal words — advisory prefers anti-dilution; under-reports silent, false accusations costly.
// The last branch is an English phrase, case-insensitive, covering sentence-initial capital forms.
// This comment deliberately avoids spelling out examples (Chinese or English), so the scanner does
// not false-flag examples as real debt (same handling as debtMarkerWords concat).
//
// commentDebtRe 匹配注释债务标记。英文 4 词用 \b 词边界（区分大小写——避免小写变量
// 名误报）；中文无词边界，孤立高频词须靠 collocation 降噪——前一分支要求紧跟动作词，
// 后一分支限两个尾字——避免正常语境误报稀释信号。召回权衡：collocation 会漏报少量
// 带间隔动词的真债务（动作词被隔字打断时不命中），换取不误报高频正常词——advisory
// 优先防信号稀释，漏报静默、无误指控代价。最后分支是英文短语、大小写不敏感，覆盖
// 句首大写形态。本注释刻意不连写任何匹配示例（中文或英文），避免扫描器扫自身源码
// 时把示例当成真债务误报（同 debtMarkerWords 拼接的处理）。
var commentDebtRe = regexp.MustCompile(
	`\b(?:` + debtMarkerWords + `)\b` +
		`|稍后(再|处理|补|改|做|写|修|回|看|说|实现|完成|解决|重构|优化)` +
		`|待补(充|完)?` +
		`|(?i)implement\s+later`,
)

// --- Added-line collectors ---
//
// --- 新增行收集器 ---

// collectAddedLines collects the content of all added lines in the task scope. Covers the same
// file set as taskChangedFiles (committed + working-tree tracked + untracked), but takes + line
// content rather than just file names.
//
// collectAddedLines 收集任务范围内所有新增行的内容。覆盖 taskChangedFiles 的同一
// 文件集（已提交 + 工作树已跟踪 + 未跟踪），但取 + 行内容而非仅文件名。
func collectAddedLines(root string, state *TaskState) []addedLine {
	files := taskChangedFiles(root, state)
	if len(files) == 0 {
		return nil
	}
	sourceSet := make(map[string]bool, len(files))
	for _, f := range files {
		nf := filepath.ToSlash(f)
		if isSourceFile(nf) {
			sourceSet[nf] = true
		}
	}
	if len(sourceSet) == 0 {
		return nil
	}

	var out []addedLine
	// Tracked (committed + working tree): git diff -U0 (no context, pure add/del). The base set
	// matches taskChangedFiles — HeadCommit..HEAD first, feature branches fall back to main...HEAD,
	// always layered with HEAD (working tree).
	//
	// 已跟踪（已提交 + 工作树）：git diff -U0（无 context，纯增删）。base 集 与
	// taskChangedFiles 同——HeadCommit..HEAD 优先，feature 分支回退 main...HEAD，
	// 永远叠 HEAD（工作树）。
	for _, spec := range cheatDiffBases(root, state) {
		out = append(out, parseGitAddedLines(root, spec, sourceSet)...)
	}
	// Untracked (agent just created, not yet git-added): read the whole file, every line is added.
	//
	// 未跟踪（agent 刚建、未 git add）：整文件读，每行都是"新增"。
	for f := range sourceSet {
		if isTracked := gitTracked(root, f); isTracked {
			// Tracked — already covered by git diff.
			continue // 已跟踪——git diff 已覆盖
		}
		out = append(out, readFileAddedLines(filepath.Join(root, f), f)...)
	}
	return out
}

// cheatDiffBases builds the list of base arguments for git diff (matching the tracked part of
// taskChangedFiles).
//
// cheatDiffBases 构造 git diff 的 base 参数列表（与 taskChangedFiles 的已跟踪部分一致）。
func cheatDiffBases(root string, state *TaskState) [][]string {
	var specs [][]string
	if state != nil {
		if state.HeadCommit != "" {
			specs = append(specs, []string{"-U0", "--no-color", state.HeadCommit + "..HEAD"})
		} else if state.Branch != "" && state.Branch != "main" && state.Branch != "master" {
			for _, base := range []string{"main", "origin/main", "master", "origin/master"} {
				if hasRef(root, base) {
					specs = append(specs, []string{"-U0", "--no-color", base + "...HEAD"})
					break
				}
			}
		}
	}
	// Working tree (staged + unstaged vs HEAD) — always relevant.
	//
	// 工作树（暂存 + 未暂存 vs HEAD）——始终相关。
	specs = append(specs, []string{"-U0", "--no-color", "HEAD"})
	return specs
}

// parseGitAddedLines runs `git diff <args>` and parses + lines. Only + lines of files in
// sourceSet are recorded. Line numbers come from the hunk header @@ ... +lineno ... @@, and
// each + line increments (-U0 has no context lines interfering).
//
// parseGitAddedLines 跑 `git diff <args>`，解析 + 行。仅记 sourceSet 内文件的 + 行。
// 行号取自 hunk 头 @@ ... +lineno ... @@，每个 + 行递增（-U0 无 context 行干扰）。
func parseGitAddedLines(root string, args []string, sourceSet map[string]bool) []addedLine {
	cmd := exec.Command("git", append([]string{"-C", root, "diff"}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var res []addedLine
	curFile := ""
	lineNo := 0
	for _, raw := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(raw, "+++ "):
			// +++ b/<path>  or  +++ /dev/null (deletion, no + lines)
			//
			// +++ b/<path>  或  +++ /dev/null（删除，无 + 行）
			p := strings.TrimPrefix(raw, "+++ ")
			p = strings.TrimSpace(p)
			if strings.HasPrefix(p, "b/") {
				p = p[2:]
			}
			if p == "/dev/null" {
				curFile = ""
			} else if sourceSet[filepath.ToSlash(p)] {
				curFile = filepath.ToSlash(p)
			} else {
				curFile = ""
			}
			lineNo = 0
		case strings.HasPrefix(raw, "@@"):
			// Starting line number of the next + line.
			lineNo = parseNewStart(raw) // 下一个 + 行的起始行号
		case strings.HasPrefix(raw, "+") && !strings.HasPrefix(raw, "+++"):
			if curFile != "" {
				res = append(res, addedLine{file: curFile, lineNo: lineNo, text: raw[1:]})
			}
			if lineNo > 0 {
				lineNo++
			}
		case strings.HasPrefix(raw, "-") && !strings.HasPrefix(raw, "---"):
			// Deleted lines do not advance the new-file line number.
			//
			// 删除行不推进新文件行号。
		default:
			// Context lines (-U0 theoretically has none; defensive): advance line number.
			//
			// context 行（-U0 理论上无；防御性）：推进行号。
			if lineNo > 0 && raw != "" {
				lineNo++
			}
		}
	}
	return res
}

// parseNewStart extracts the new file's starting line number from `@@ -l,s +l,s @@` (the number
// after +). Returns 0 on failure.
//
// parseNewStart 从 `@@ -l,s +l,s @@` 提取新文件起始行号（+ 后的数）。失败返回 0。
func parseNewStart(hunk string) int {
	i := strings.Index(hunk, " +")
	if i < 0 {
		return 0
	}
	// "l,s @@ ..."
	rest := hunk[i+2:] // "l,s @@ ..."
	end := strings.IndexAny(rest, ", ")
	if end < 0 {
		end = len(rest)
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0
	}
	return n
}

// readFileAddedLines reads an untracked file in full as addedLines (line numbers start at 1).
// Returns nil on read failure.
//
// readFileAddedLines 读未跟踪文件全文为 addedLine（行号从 1 起）。读失败返回 nil。
func readFileAddedLines(full, rel string) []addedLine {
	f, err := os.Open(full)
	if err != nil {
		return nil
	}
	defer f.Close()
	var res []addedLine
	s := bufio.NewScanner(f)
	// Tolerate long lines.
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 长行容忍
	n := 0
	for s.Scan() {
		n++
		res = append(res, addedLine{file: rel, lineNo: n, text: s.Text()})
	}
	return res
}

// gitTracked reports whether a file is already tracked by git (ls-files --error-unmatch exit 0
// = tracked).
//
// gitTracked 报告文件是否已被 git 跟踪（ls-files --error-unmatch 退出码 0=跟踪）。
func gitTracked(root, rel string) bool {
	err := exec.Command("git", "-C", root, "ls-files", "--error-unmatch", rel).Run()
	return err == nil
}

// hasRef reports whether git recognizes a ref (used to avoid diff errors when falling back to
// main/master).
//
// hasRef 报告 git 是否认识某 ref（用于回退 main/master 时避免 diff 报错）。
func hasRef(root, ref string) bool {
	return exec.Command("git", "-C", root, "rev-parse", "--verify", ref).Run() == nil
}

// clip truncates long lines for snippet (checklog detail should not be too long).
//
// clip 截断长行用于 snippet（checklog detail 不宜过长）。
func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

// cheatScanDetail generates the checklog detail (a one-line summary, not the stderr user message).
//
// cheatScanDetail 生成 checklog detail（一行摘要，非 stderr 用户消息）。
func cheatScanDetail(findings []CheatFinding) string {
	if len(findings) == 0 {
		return "no mechanically-detectable cheat patterns"
	}
	byPat := make(map[CheatPattern]int)
	for _, f := range findings {
		byPat[f.Pattern]++
	}
	var parts []string
	for _, p := range []CheatPattern{CheatTypeSuppression, CheatErrorSwallow, CheatDeadBranch, CheatCommentOnly, CheatCommentDebt} {
		if n := byPat[p]; n > 0 {
			parts = append(parts, string(p)+"="+strconv.Itoa(n))
		}
	}
	return "suspected cheat patterns: " + strings.Join(parts, ", ")
}
