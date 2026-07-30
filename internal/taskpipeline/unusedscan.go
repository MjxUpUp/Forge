package taskpipeline

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// unusedscan.go — deterministic "unreferenced exported symbol" scanner (layer-1 wiring detection).
//
// Motivation: "feature code done but never wired" — a function/type is implemented, has unit tests
// (tests pass), but no caller wires it into the real flow. Unit tests verify the implementation
// (the symbol itself, fed canned inputs), not the wiring (whether production code actually calls it
// at runtime); a broken wire leaves tests green and the feature dead. This is exactly Forge's own
// BUG-1: inferDesignPhases had zero production callers — pure dead code — surfaced only by review.
// Extending cheatscan's philosophy ("mechanical → scanner, semantic → LLM reviewer"): this scanner
// owns the mechanical layer-1 (symbol defined but zero references); layer-2 (referenced but
// semantically unwired — registered in a registry but never instantiated in main, a config key read
// but never consumed) is not mechanically decidable and stays with the LLM reviewer / code-review-gate.
//
// Purely deterministic (the gate computes it, agent cannot forge), advisory (never blocks): library
// / reflective / externally-consumed exports legitimately have no in-repo caller, so hits are
// "suspected, please review" not "convicted". Excluded from BuildEvidenceChain — an observation,
// not verification evidence.
//
// 动机："功能代码写完但没接线"——函数/类型实现了、有单测（测试过），但无调用方把它接入
// 真实流程。单测验的是实现（符号本身、喂预制输入），不验接线（生产代码运行时是否真调用
// 它）；接线一断，测试照绿、功能已死。这正是 Forge 自己的 BUG-1：inferDesignPhases 零生产
// 调用方——纯 dead code——靠 review 才浮出。延伸 cheatscan 的哲学（"机械归扫描器，语义归
// LLM reviewer"）：本扫描器负责层 1 机械检测（符号定义了但零引用）；层 2（引用了但语义没接
// 通——registry 注册了但 main 从未实例化、配置 key 读了但从未消费）机械不可判，仍归 LLM
// reviewer / code-review-gate。
//
// 纯 deterministic（gate 实算，agent 无法伪造），advisory（绝不阻塞）：库/反射/外部消费的
// 导出符号合法地无仓内调用方，故命中是"疑似、请核查"非"定罪"。BuildEvidenceChain 中排除——
// 观测，非验证证据。

// UnusedPattern identifies a category of unreferenced-symbol finding.
//
// UnusedPattern 标识一类未引用符号的 finding。
type UnusedPattern string

const (
	// UnusedExport: a newly-added exported symbol (Go func/type/method, TS export, Rust pub) with
	// zero references among this task's production added lines — suspected "implemented but never
	// wired into the real flow". severity=high: a defined-but-unreferenced export is a strong wiring-
	// miss signal (vs comment-debt's low). Advisory still, because legitimate external/reflection
	// consumers exist; the trail is left for review.
	//
	// UnusedExport：本次新增的导出符号（Go func/type/method、TS export、Rust pub）在本任务
	// 生产新增行里零引用——疑似"实现了但没接进真实流程"。severity=high：定义了却零引用的导出
	// 符号是强接线缺失信号（对比 comment-debt 的 low）。仍 advisory，因为合法的外部/反射消费
	// 者存在；留痕供 review 核查。
	UnusedExport UnusedPattern = "unreferenced-export"
)

// UnusedFinding is a single newly-added exported symbol that no production line in this task
// references. Advisory — detection may false-fire (external consumer), leaves a trail for review,
// never blocks.
//
// UnusedFinding 是一个"本任务生产代码零引用"的新增导出符号。advisory——检测有假阳性可能
// （外部消费者），留痕供 review，绝不阻塞。
type UnusedFinding struct {
	Pattern  UnusedPattern `json:"pattern"`
	Symbol   string        `json:"symbol"`
	Kind     string        `json:"kind"` // func / method / type / export / fn / struct
	File     string        `json:"file"`
	Line     int           `json:"line,omitempty"`
	Severity string        `json:"severity"`
}

// symbolDef is a newly-added exported symbol definition extracted from a + line.
//
// symbolDef 是从一条 + 行提取的新增导出符号定义。
type symbolDef struct {
	symbol string
	kind   string
	file   string
	line   int
}

// ScanUnusedSymbols scans task-scoped added production lines and finds newly-added exported
// symbols that no other production line in this task references — suspected "implemented but never
// wired". Purely deterministic, advisory (never blocks).
//
// Layer-1 wiring detection: catches the mechanical case (symbol defined, zero references). Layer-2
// (referenced but semantically unwired) is not mechanically decidable → stays with the LLM reviewer.
//
// Key insight (why NO repo-wide git grep is needed): a newly-added symbol can only be called by
// newly-added code — pre-existing code cannot reference a symbol that did not exist before this task.
// So the search set is exactly this task's production added lines. This is also more precise than a
// repo-wide grep: a symbol referenced only in its own test file (unit test passes) but in zero
// production lines is exactly the "tests green, feature dead" wiring failure — production references
// are what count, test references do not (mirrors cheatscan's test exclusion).
//
// Failure-tolerant (same reliability contract as cheatscan): collectAddedLines returns what it
// gathered on git/file-read errors; ScanUnusedSymbols then operates on that partial set. Never panics.
//
// ScanUnusedSymbols 扫描任务范围的新增生产行，找"本任务生产代码零引用"的新增导出符号——
// 疑似"实现了但没接线"。纯 deterministic，advisory（绝不阻塞）。
//
// 层 1 接线检测：抓机械情形（符号定义了、零引用）。层 2（引用了但语义没接通）机械不可判
// → 仍归 LLM reviewer。
//
// 关键洞察（为何无需全仓 git grep）：新增符号只能被新增代码调用——已有代码无法引用本任务
// 之前还不存在的符号。故搜索集恰好是本任务的新增生产行。这也比全仓 grep 更精确：某符号只在
// 自己的测试文件里被引用（单测过）但在零生产行出现，恰好是"测试绿、功能死"的接线失败——
// 生产引用才算数，测试引用不算（同 cheatscan 排除测试）。
//
// 失败容忍（同 cheatscan 的可靠性契约）：git/读文件出错时 collectAddedLines 返回已收集的；
// ScanUnusedSymbols 在该部分集上运算。绝不 panic。
func ScanUnusedSymbols(root string, state *TaskState) []UnusedFinding {
	added := collectAddedLines(root, state)
	if len(added) == 0 {
		return nil
	}
	// Production added lines only (exclude tests). collectAddedLines already filtered isSourceFile;
	// isTestFile applied here mirrors cheatscan.
	//
	// 仅生产新增行（排除测试）。collectAddedLines 已过 isSourceFile；此处 isTestFile 同 cheatscan。
	var prod []addedLine
	for _, a := range added {
		if !isTestFile(a.file) {
			prod = append(prod, a)
		}
	}
	if len(prod) == 0 {
		return nil
	}
	defs := dedupDefs(extractExportedSymbols(prod))
	if len(defs) == 0 {
		return nil
	}
	// One word-boundary matcher per distinct symbol (a symbol is defined once but may be referenced N times).
	//
	// 每个去重符号一个 word-boundary matcher（一个符号定义一次但可能被引用 N 次）。
	matchers := make(map[string]*regexp.Regexp, len(defs))
	for _, d := range defs {
		if _, ok := matchers[d.symbol]; !ok {
			matchers[d.symbol] = regexp.MustCompile(`\b` + regexp.QuoteMeta(d.symbol) + `\b`)
		}
	}
	var findings []UnusedFinding
	for _, d := range defs {
		re := matchers[d.symbol]
		referenced := false
		for _, a := range prod {
			// Skip the symbol's own definition line (the `func Foo(` / `type Foo` line).
			//
			// 跳过符号自身的定义行（`func Foo(` / `type Foo` 那行）。
			if a.file == d.file && a.lineNo == d.line {
				continue
			}
			if re.MatchString(stripLineComment(a.text)) {
				referenced = true
				break
			}
		}
		if !referenced {
			findings = append(findings, UnusedFinding{
				Pattern:  UnusedExport,
				Symbol:   d.symbol,
				Kind:     d.kind,
				File:     d.file,
				Line:     d.line,
				Severity: "high",
			})
		}
	}
	return findings
}

// stripLineComment removes a trailing `//` line comment from a source line (Go / TS / JS / Rust all
// share `//`). Applied before reference matching so a doc comment mentioning a symbol's own name
// (e.g. `// Foo does X`) is not counted as a "reference" — exactly the BUG-1 shape (a symbol
// referenced only in its doc comment, never called in production). Known trade-off: a `//` inside a
// string literal (e.g. `url := "http://x"`) is also stripped; the residual hit in the code portion of
// such a line is rare for export-name references and is the accepted recall ceiling. Block comments
// (`/* */`) spanning lines are not handled (rare for single-line wiring refs).
//
// stripLineComment 剥去源码行的尾部 `//` 行注释（Go/TS/JS/Rust 都用 `//`）。在引用匹配前应用，
// 故 doc comment 提及符号自身名（如 `// Foo does X`）不算"引用"——恰是 BUG-1 形状（符号只在
// doc comment 被提及、生产从未调用）。已知权衡：字符串字面量内的 `//`（如 `url := "http://x"`）
// 也会被剥；这类行代码段里残留的命中对导出名引用很少见，是接受的召回上限。跨行块注释
// （`/* */`）不处理（单行接线引用罕见）。
func stripLineComment(text string) string {
	if i := strings.Index(text, "//"); i >= 0 {
		return text[:i]
	}
	return text
}

// dedupDefs collapses definitions that share (symbol, file, line). collectAddedLines layers multiple
// diff specs (HeadCommit..HEAD + HEAD), so a def line present in both yields duplicates; without dedup,
// findings and Detail entries get duplicated (cosmetic, but the trail should be clean).
//
// dedupDefs 折叠共享 (symbol, file, line) 的定义。collectAddedLines 叠多个 diff spec
// （HeadCommit..HEAD + HEAD），两 spec 都有的 def 行会重复；不去重的话 finding 与 Detail 条目重复
// （外观问题，但留痕应干净）。
func dedupDefs(defs []symbolDef) []symbolDef {
	seen := make(map[string]bool, len(defs))
	out := make([]symbolDef, 0, len(defs))
	for _, d := range defs {
		key := d.symbol + "|" + d.file + ":" + strconv.Itoa(d.line)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	return out
}

// --- Symbol extractors (per-language) ---
//
// --- 符号提取器（分语言）---

// extractExportedSymbols extracts newly-added exported symbol definitions from production added
// lines, per-language (Go / TypeScript-JavaScript / Rust). Python is intentionally NOT covered:
// its export model (no `export` keyword; any top-level name is importable) makes "unreferenced"
// high false-positive and low signal. Adding a language = add a case here + a regexp-based extractor.
//
// Coverage caveats per language (intentional; reflective/external consumers still trip advisory, so
// gaps lower recall, never cause false convictions): Go covers func/method/type but NOT exported
// const/var (UnusedExport itself is a const, intentionally unscanned). TS/JS covers
// function/const/let/var/class/type/interface but NOT enum / generator (`function*`). Rust covers
// pub fn/struct but NOT pub trait/enum/const/mod nor `pub(crate)`. `.mjs`/`.cjs` are NOT covered —
// collectAddedLines' isSourceFile (sourceExts) excludes them, so they never reach this switch.
//
// extractExportedSymbols 从生产新增行按语言提取新增导出符号定义（Go / TS-JS / Rust）。Python
// 刻意不覆盖：其导出模型（无 `export` 关键字；任意顶层名可 import）使"未引用"高假阳性、低
// 信号。新增语言 = 在此加 case + 一个基于正则的提取器。
//
// 各语言覆盖缺口（刻意；反射/外部消费者仍触发 advisory，故缺口只降召回、绝不误判）：Go 覆盖
// func/method/type，不覆盖导出 const/var（UnusedExport 本身是 const，刻意不被扫描）。TS/JS 覆
// 盖 function/const/let/var/class/type/interface，不覆盖 enum / generator（`function*`）。Rust 覆
// 盖 pub fn/struct，不覆盖 pub trait/enum/const/mod 及 `pub(crate)`。`.mjs`/`.cjs` 不覆盖——
// collectAddedLines 的 isSourceFile（sourceExts）排除它们，故到不了此 switch。
func extractExportedSymbols(prod []addedLine) []symbolDef {
	var defs []symbolDef
	for _, a := range prod {
		switch filepath.Ext(a.file) {
		case ".go":
			defs = append(defs, extractGo(a)...)
		case ".ts", ".tsx", ".js", ".jsx":
			defs = append(defs, extractTS(a)...)
		case ".rs":
			defs = append(defs, extractRust(a)...)
		}
	}
	return defs
}

// isExportedIdent reports whether a Go identifier is exported (starts uppercase ASCII).
//
// isExportedIdent 报告 Go 标识符是否导出（ASCII 大写开头）。
func isExportedIdent(name string) bool {
	if name == "" {
		return false
	}
	c := name[0]
	return c >= 'A' && c <= 'Z'
}

// goFuncRe matches a top-level or method func definition and captures the name: `func Foo(`,
// `func (r T) Foo(`. Name captured generically (any identifier); export filtering is applied by
// the caller via isExportedIdent.
//
// goFuncRe 匹配顶层或方法 func 定义并捕获名字：`func Foo(`、`func (r T) Foo(`。名字通用捕获
// （任意标识符）；导出过滤由调用方用 isExportedIdent 做。
var goFuncRe = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

// goTypeRe matches an exported type definition: `type Foo struct`, `type Bar interface`,
// `type Alias = X` (already uppercase-anchored — only exported types matter).
//
// goTypeRe 匹配导出类型定义：`type Foo struct`、`type Bar interface`、`type Alias = X`
// （已大写锚定——只关心导出类型）。
var goTypeRe = regexp.MustCompile(`^type\s+([A-Z][A-Za-z0-9_]*)\b`)

// extractGo extracts exported func/method/type definitions from a Go + line.
//
// extractGo 从一条 Go + 行提取导出 func/method/type 定义。
func extractGo(a addedLine) []symbolDef {
	t := strings.TrimLeft(a.text, " \t")
	if m := goFuncRe.FindStringSubmatch(t); m != nil {
		name := m[1]
		if !isExportedIdent(name) {
			return nil
		}
		kind := "func"
		if strings.HasPrefix(t, "func (") || strings.HasPrefix(t, "func(") {
			kind = "method"
		}
		return []symbolDef{{symbol: name, kind: kind, file: a.file, line: a.lineNo}}
	}
	if m := goTypeRe.FindStringSubmatch(t); m != nil {
		return []symbolDef{{symbol: m[1], kind: "type", file: a.file, line: a.lineNo}}
	}
	return nil
}

// tsExportRe matches the common TS/JS export forms and captures the exported name:
// `export function Foo`, `export const Foo`, `export class Foo`, `export type Foo`,
// `export interface Foo`, `export default function Foo`. Default-exported anonymous values
// (`export default 42`) have no name and are skipped.
//
// tsExportRe 匹配常见 TS/JS export 形态并捕获导出名：`export function Foo`、`export const Foo`、
// `export class Foo`、`export type Foo`、`export interface Foo`、`export default function Foo`。
// default 导出的匿名值（`export default 42`）无名，跳过。
var tsExportRe = regexp.MustCompile(`^export\s+(?:default\s+)?(?:async\s+)?(?:function\s+([A-Za-z_$][\w$]*)|(?:const|let|var)\s+([A-Za-z_$][\w$]*)|class\s+([A-Za-z_$][\w$]*)|type\s+([A-Za-z_$][\w$]*)|interface\s+([A-Za-z_$][\w$]*))`)

// extractTS extracts exported symbol definitions from a TS/JS + line. kind is normalized to "export"
// (the precise keyword is not load-bearing for an advisory finding's display).
//
// extractTS 从一条 TS/JS + 行提取导出符号定义。kind 归一为 "export"（精确关键字对 advisory
// finding 的展示不关键）。
func extractTS(a addedLine) []symbolDef {
	t := strings.TrimLeft(a.text, " \t")
	m := tsExportRe.FindStringSubmatch(t)
	if m == nil {
		return nil
	}
	for _, name := range m[1:] {
		if name != "" {
			return []symbolDef{{symbol: name, kind: "export", file: a.file, line: a.lineNo}}
		}
	}
	return nil
}

// rustFnRe / rustStructRe match pub items. Rust's dead_code lint already flags private unused items,
// but pub items are treated as public API and NOT linted — so a pub item unused inside the crate is
// exactly a wiring miss this scanner can catch.
//
// rustFnRe / rustStructRe 匹配 pub 项。Rust 的 dead_code lint 已标私有的未用项，但 pub 项被当
// 公共 API 不 lint——故 crate 内未用的 pub 项恰好是本扫描器能抓的接线缺失。
var rustFnRe = regexp.MustCompile(`^\s*pub\s+(?:async\s+)?(?:unsafe\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)`)
var rustStructRe = regexp.MustCompile(`^\s*pub\s+struct\s+([A-Za-z_][A-Za-z0-9_]*)`)

// extractRust extracts pub fn/struct definitions from a Rust + line.
//
// extractRust 从一条 Rust + 行提取 pub fn/struct 定义。
func extractRust(a addedLine) []symbolDef {
	if m := rustFnRe.FindStringSubmatch(a.text); m != nil {
		return []symbolDef{{symbol: m[1], kind: "fn", file: a.file, line: a.lineNo}}
	}
	if m := rustStructRe.FindStringSubmatch(a.text); m != nil {
		return []symbolDef{{symbol: m[1], kind: "struct", file: a.file, line: a.lineNo}}
	}
	return nil
}

// unusedScanDetail generates the checklog detail (a one-line summary, not the stderr user message).
//
// unusedScanDetail 生成 checklog detail（一行摘要，非 stderr 用户消息）。
func unusedScanDetail(findings []UnusedFinding) string {
	if len(findings) == 0 {
		return `no unreferenced exported symbols`
	}
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, f.Symbol+`(`+f.Kind+`)`)
	}
	return `suspected unreferenced exports: ` + strings.Join(parts, ", ")
}
