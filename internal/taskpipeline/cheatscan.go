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

// CheatPattern 标识扫描器检测的一类 AI 作弊模式（机械可检的子集）。
type CheatPattern string

const (
	// CheatTypeSuppression：新增类型/告警抑制指令——TS 的 ts-ignore/ts-nocheck/ts-expect-error、
	// eslint 的 disable、Rust 的 allow 属性、Python mypy 的 type-ignore、Java 的
	// SuppressWarnings 注解。把警告藏起来而非解决。（注释里不连写指令 sigil，见 typeSuppressionRe 注。）
	CheatTypeSuppression CheatPattern = "type-suppression"
	// CheatErrorSwallow：新增空 catch / except...: pass——静默吞掉错误，问题永不暴露。
	CheatErrorSwallow CheatPattern = "error-swallow"
	// CheatDeadBranch：新增永假分支（if(false)/if(0)/if(1===2)）——看起来处理了边界，实际永不执行。
	CheatDeadBranch CheatPattern = "dead-branch"
	// CheatCommentOnly：某源码文件的新增行全是注释/空行、零逻辑变更——疑似"声称修复但只加了注释"。
	// 启发式（severity=low）：纯文档任务可能误报，advisory 提示核查而非定罪。
	CheatCommentOnly CheatPattern = "comment-only-fix"
	// CheatCommentDebt：新增注释行里的"债务标记"——AI 偷懒用注释标识"这里有问题/待办"
	// 但本变更不解决。是懒惰阶梯反第 0 级（注释替代行动 → 屎山根源）：看起来负责任（标注了），
	// 实际零行动，后续无人跟进。severity=low：合理的后续跟踪标记也会命中，advisory 提示核查
	// （转 forge task 跟踪 或 当场修）而非定罪。detectCommentDebt 只扫注释行；标记词用
	// debtMarkerWords 拼接，避免扫描器扫自身源码时把模式定义/注释里的词误判为债务。
	CheatCommentDebt CheatPattern = "comment-as-debt"
)

// CheatFinding 是一次机械检测到的疑似作弊。advisory——检测有假阳性可能，留痕供
// review 核查，绝不阻塞。
type CheatFinding struct {
	Pattern  CheatPattern `json:"pattern"`
	File     string       `json:"file"`
	Line     int          `json:"line,omitempty"`
	Snippet  string       `json:"snippet"`
	Severity string       `json:"severity"` // "high"（机械高置信）/ "low"（启发式）
}

// addedLine 是任务范围 diff 的一条新增行（仅 + 行内容 + 归属文件 + 新文件行号）。
type addedLine struct {
	file   string
	lineNo int
	text   string
}

// ScanCheatPatterns 扫描任务范围内的新增行，机械检测 4 类 AI 作弊模式。
// 纯 deterministic（gate 实算，agent 无法伪造）。返回 findings（空=干净）。
// 失败容忍：git/读文件出错时跳过该源（返回已收集的），绝不 panic——advisory 检测
// 的可靠性来自"扫到了就准"，不来自"必须扫全"。
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

// --- 检测器 ---

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
	regexp.MustCompile(`@ts-nocheck`),        // TS 整文件抑制
	regexp.MustCompile(`@ts-ignore`),         // TS 单行抑制
	regexp.MustCompile(`@ts-expect-error`),   // TS 期望错误（仍是抑制）
	regexp.MustCompile(`eslint-disable`),     // eslint 行/块禁用
	regexp.MustCompile(`#\s*type:\s*ignore`), // Python mypy（要求 # 前缀）
	regexp.MustCompile(`#\[allow`),           // Rust 属性（要求 #[ 前缀）
	regexp.MustCompile(`@SuppressWarnings`),  // Java 注解
}

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
	// 空 catch 单行：catch {} / catch (e) {} / catch (e: Err) {} —— 跨语言（JS/TS/Java/C#）。
	regexp.MustCompile(`\bcatch\s*(\([^)]*\))?\s*\{\s*\}`),
	// Python except ... : pass（同行的 pass = 真吞）。
	regexp.MustCompile(`\bexcept\b[^\n]*:\s*pass`),
}

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
	// Go/Rust/JS 词法：if false {  /  Python: if False: / if 0:
	regexp.MustCompile(`\bif\s+(?:false|False|0)\b`),
	// while (false) / while (0)
	regexp.MustCompile(`\bwhile\s*\(\s*(?:false|0)\s*\)`),
}

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

// debtMarkerWords 是注释债务标记的 regex 片段（英文惯例的 4 类债务词）。字符串拼接
// 避免在源码里连写完整词——本扫描器会扫自身源码，连写的标记词会被当成真债务误报。
const debtMarkerWords = "TO" + "DO" + "|FIX" + "ME" + "|XXX|HACK"

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

// --- 新增行收集器 ---

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
	// 已跟踪（已提交 + 工作树）：git diff -U0（无 context，纯增删）。base 集 与
	// taskChangedFiles 同——HeadCommit..HEAD 优先，feature 分支回退 main...HEAD，
	// 永远叠 HEAD（工作树）。
	for _, spec := range cheatDiffBases(root, state) {
		out = append(out, parseGitAddedLines(root, spec, sourceSet)...)
	}
	// 未跟踪（agent 刚建、未 git add）：整文件读，每行都是"新增"。
	for f := range sourceSet {
		if isTracked := gitTracked(root, f); isTracked {
			continue // 已跟踪——git diff 已覆盖
		}
		out = append(out, readFileAddedLines(filepath.Join(root, f), f)...)
	}
	return out
}

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
	// 工作树（暂存 + 未暂存 vs HEAD）——始终相关。
	specs = append(specs, []string{"-U0", "--no-color", "HEAD"})
	return specs
}

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
			lineNo = parseNewStart(raw) // 下一个 + 行的起始行号
		case strings.HasPrefix(raw, "+") && !strings.HasPrefix(raw, "+++"):
			if curFile != "" {
				res = append(res, addedLine{file: curFile, lineNo: lineNo, text: raw[1:]})
			}
			if lineNo > 0 {
				lineNo++
			}
		case strings.HasPrefix(raw, "-") && !strings.HasPrefix(raw, "---"):
			// 删除行不推进新文件行号。
		default:
			// context 行（-U0 理论上无；防御性）：推进行号。
			if lineNo > 0 && raw != "" {
				lineNo++
			}
		}
	}
	return res
}

// parseNewStart 从 `@@ -l,s +l,s @@` 提取新文件起始行号（+ 后的数）。失败返回 0。
func parseNewStart(hunk string) int {
	i := strings.Index(hunk, " +")
	if i < 0 {
		return 0
	}
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

// readFileAddedLines 读未跟踪文件全文为 addedLine（行号从 1 起）。读失败返回 nil。
func readFileAddedLines(full, rel string) []addedLine {
	f, err := os.Open(full)
	if err != nil {
		return nil
	}
	defer f.Close()
	var res []addedLine
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 长行容忍
	n := 0
	for s.Scan() {
		n++
		res = append(res, addedLine{file: rel, lineNo: n, text: s.Text()})
	}
	return res
}

// gitTracked 报告文件是否已被 git 跟踪（ls-files --error-unmatch 退出码 0=跟踪）。
func gitTracked(root, rel string) bool {
	err := exec.Command("git", "-C", root, "ls-files", "--error-unmatch", rel).Run()
	return err == nil
}

// hasRef 报告 git 是否认识某 ref（用于回退 main/master 时避免 diff 报错）。
func hasRef(root, ref string) bool {
	return exec.Command("git", "-C", root, "rev-parse", "--verify", ref).Run() == nil
}

// clip 截断长行用于 snippet（checklog detail 不宜过长）。
func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

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
