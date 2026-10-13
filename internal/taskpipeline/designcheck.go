package taskpipeline

// designcheck.go —— 方案断言级事实核查（design-stage-hardening W2.3）与触点 →
// PlanScope（W2.4）。
//
// designcheck.go — claim-level grounding check of a design doc (W2.3) and
// touch points → PlanScope (W2.4).
//
// 动机：AI 生成的方案约 1/3 断言在需求里找不到依据、包名幻觉率 5.2%-21.7%
// （R2ABench 2026；USENIX Sec 2025）。散文规则拦不住，只能机械核对：方案里点名的
// 路径、file:line、Go 符号、Go 模块必须真实存在。确定性、零 LLM；advisory 起步。
//
// 豁免：同一行带 `new:` 标记的引用是计划新建的文件/依赖，不要求存在。代码围栏内
// 是示例，不核查。

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameDesignCheck is the checklog name of the design grounding check.
//
// CheckNameDesignCheck 是方案事实核查的 checklog 名。
const CheckNameDesignCheck checklog.CheckName = "design-check"

// PlanScopeSourceDesign marks a PlanScope parsed from the design artifact's touch points.
//
// PlanScopeSourceDesign 标记从 design 产物触点解析出的 PlanScope。
const PlanScopeSourceDesign = "design"

// DesignCheckFinding is one unresolvable reference in a design doc.
//
// DesignCheckFinding 是方案中一条无法落地核实的引用。
type DesignCheckFinding struct {
	Kind   string `json:"kind"` // path | line | symbol | module
	Ref    string `json:"ref"`
	Line   int    `json:"line"`
	Detail string `json:"detail"`
}

var (
	// backtickRe captures inline code spans.
	backtickRe = regexp.MustCompile("`([^`]+)`")
	// pathTokenRe accepts repo-relative path-like tokens: segments with a slash, or a
	// bare filename with an extension.
	pathTokenRe = regexp.MustCompile(`^[\w.\-]+(?:/[\w.\-]+)*$`)
	// fileRefRe splits `path:rest` (rest = line number or Go symbol).
	fileRefRe = regexp.MustCompile(`^([\w.\-/]+\.[A-Za-z0-9]+):([A-Za-z_][\w.]*|\d+)$`)
	// goModuleRe matches a Go module path (host with a dot + at least one path segment).
	goModuleRe = regexp.MustCompile(`^(?:[a-z0-9\-]+\.)+[a-z]{2,}(?:/[\w.\-]+)+$`)
	// newMarkerRe marks a line whose references are planned new files/deps.
	newMarkerRe = regexp.MustCompile(`(?i)\bnew(?:-dep)?:`)
	// touchpointsHeadingRe matches the touch-point section heading.
	touchpointsHeadingRe = regexp.MustCompile(`(?i)^#{2,6}\s*(触点|touch[ -]?points?)`)
)

// proseLines returns the non-fenced lines of text with 1-based line numbers.
//
// proseLines 返回 text 中围栏外的行及其 1 起行号。
func proseLines(text string) (lines []string, numbers []int) {
	openMarker := ""
	for i, raw := range strings.Split(text, "\n") {
		t := strings.TrimSpace(raw)
		if isFenceMarker(t) && (openMarker == "" || strings.HasPrefix(t, openMarker)) {
			if openMarker == "" {
				openMarker = t[:3]
			} else {
				openMarker = ""
			}
			continue
		}
		if openMarker != "" {
			continue
		}
		lines = append(lines, raw)
		numbers = append(numbers, i+1)
	}
	return lines, numbers
}

// looksLikeRepoPath filters inline-code tokens down to plausible repo paths
// (excludes commands with spaces, URLs, flags, globs and bare words).
//
// looksLikeRepoPath 把行内代码筛到像仓库路径的 token（排除含空格的命令、URL、
// flag、glob 与裸单词）。
func looksLikeRepoPath(tok string) bool {
	if strings.ContainsAny(tok, " *?<>{}$") || strings.Contains(tok, "://") || strings.HasPrefix(tok, "-") {
		return false
	}
	if !pathTokenRe.MatchString(tok) {
		return false
	}
	if goModuleRe.MatchString(tok) && !strings.Contains(filepath.Base(tok), ".go") {
		return false // 模块路径单独核查
	}
	return strings.Contains(tok, "/") && (filepath.Ext(tok) != "" || strings.HasSuffix(tok, "/"))
}

// goDeclRe builds the declaration regex for a Go symbol: func, method (with
// receiver type when given), type/var/const, a `Name = …` entry of a const/var
// block, or a `Name Type` entry of a type/var block or struct. An indented call
// `\tFoo(1)` is NOT a declaration (no whitespace between name and paren).
//
// goDeclRe 构造 Go 符号的声明正则：func、方法（给了接收者类型时一并匹配）、
// type/var/const、const/var 块里的 `Name = …`、type/var 块或 struct 里的
// `Name Type`。缩进的调用 `\tFoo(1)` 不算声明（名字与括号之间无空白）。
func goDeclRe(recv, sym string) *regexp.Regexp {
	q := regexp.QuoteMeta(sym)
	method := `func\s+\([^)]*\)\s*` + q + `\b`
	if recv != "" {
		method = `func\s+\([^)]*\b\*?` + regexp.QuoteMeta(recv) + `(?:\[[^\]]*\])?\)\s*` + q + `\b`
	}
	return regexp.MustCompile(`(?m)^(?:func\s+` + q + `\b|` + method + `|(?:type|var|const)\s+` + q + `\b|\s+` + q + `\s*=|\s+` + q + `\s+[A-Za-z*\[(])`)
}

// interfaceBlockRe captures `type Name interface { … }` blocks (closing brace at line start).
// Type parameters are allowed (`type Set[T any] interface {`); the opening brace
// end its line (a trailing // comment and CRLF are fine), so one-line `interface{}`
// never opens a block that would
// swallow the following function bodies.
var interfaceBlockRe = regexp.MustCompile(`(?ms)^type\s+(\w+)(?:\[[^\]]*\])?\s+interface\s*\{[ \t]*(?://[^\n]*)?\r?\n(.*?)^\}`)

// interfaceDeclares reports whether an interface block declares method sym
// (restricted to interface recv when given). Interface methods have the call
// shape `\tName(…)`, so they are matched only inside interface blocks.
//
// interfaceDeclares 报告是否有 interface 块声明了方法 sym（给了 recv 时只看该
// interface）。接口方法形如调用 `\tName(…)`，故只在 interface 块内匹配。
func interfaceDeclares(body []byte, recv, sym string) bool {
	method := regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(sym) + `\(`)
	for _, m := range interfaceBlockRe.FindAllSubmatch(body, -1) {
		if (recv == "" || string(m[1]) == recv) && method.Match(m[2]) {
			return true
		}
	}
	return false
}

// maxDesignRefBytes caps how much of a referenced file the check reads.
//
// maxDesignRefBytes 是核查读取被引用文件的字节上限。
const maxDesignRefBytes = 4 << 20

// repoPath resolves a doc-supplied repo-relative path inside root. Absolute
// paths and anything escaping root via ".." are refused (the design doc is
// semi-trusted input — the check must never probe files outside the repo).
//
// repoPath 把方案给出的仓库相对路径解析到 root 内。绝对路径与经 ".." 越出 root
// 的路径一律拒绝（方案文档是半可信输入——核查绝不能探测仓库外的文件）。
func repoPath(root, rel string) (string, bool) {
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", false
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	r, err := filepath.Rel(root, abs)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return abs, true
}

// readRepoFile reads a regular file (no symlinks, pipes or devices) up to the cap.
//
// readRepoFile 读取普通文件（不跟随符号链接、不读管道或设备），受字节上限约束。
func readRepoFile(abs string) ([]byte, error) {
	fi, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxDesignRefBytes))
}

// readGoModRequires returns the module path and required modules of go.mod
// (empty when absent).
func readGoModRequires(root string) (module string, required map[string]bool) {
	required = map[string]bool{}
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", required
	}
	defer f.Close()
	inBlock := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "module "):
			module = strings.TrimSpace(strings.TrimPrefix(line, "module "))
		case strings.HasPrefix(line, "require ("):
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock && line != "" && !strings.HasPrefix(line, "//"):
			required[strings.Fields(line)[0]] = true
		case strings.HasPrefix(line, "require "):
			if fs := strings.Fields(strings.TrimPrefix(line, "require ")); len(fs) > 0 {
				required[fs[0]] = true
			}
		}
	}
	return module, required
}

// moduleKnown reports whether mod (or a package under it) is the main module or a
// required module.
func moduleKnown(mod, mainModule string, required map[string]bool) bool {
	if mainModule != "" && (mod == mainModule || strings.HasPrefix(mod, mainModule+"/")) {
		return true
	}
	for r := range required {
		if mod == r || strings.HasPrefix(mod, r+"/") {
			return true
		}
	}
	return false
}

// CheckDesignGrounding verifies every reference in the design text against the
// repo at root. Pure apart from read-only filesystem access.
//
// CheckDesignGrounding 对照 root 仓库核查方案文本中的每条引用。除只读文件系统
// 访问外无副作用。
func CheckDesignGrounding(root, text string) []DesignCheckFinding {
	var findings []DesignCheckFinding
	mainModule, required := readGoModRequires(root)
	hasGoMod := mainModule != ""
	seen := map[string]bool{}
	add := func(f DesignCheckFinding) {
		key := f.Kind + "\x1f" + f.Ref
		if !seen[key] {
			seen[key] = true
			findings = append(findings, f)
		}
	}
	lines, numbers := proseLines(text)
	for i, line := range lines {
		lineNo := numbers[i]
		if newMarkerRe.MatchString(line) {
			continue
		}
		for _, m := range backtickRe.FindAllStringSubmatch(line, -1) {
			tok := strings.TrimSpace(m[1])
			if fm := fileRefRe.FindStringSubmatch(tok); fm != nil {
				checkFileRef(root, fm[1], fm[2], lineNo, add)
				continue
			}
			if hasGoMod && goModuleRe.MatchString(tok) && !strings.HasSuffix(tok, ".go") {
				if !moduleKnown(tok, mainModule, required) {
					add(DesignCheckFinding{Kind: "module", Ref: tok, Line: lineNo,
						Detail: "Go 模块不在 go.mod（主模块或 require）——确认名称；计划新增的依赖在该行标 new-dep:"})
				}
				continue
			}
			if looksLikeRepoPath(tok) {
				abs, ok := repoPath(root, tok)
				if !ok {
					add(DesignCheckFinding{Kind: "path", Ref: tok, Line: lineNo, Detail: "引用越出仓库根目录，未核查"})
					continue
				}
				if _, err := os.Lstat(abs); err != nil {
					add(DesignCheckFinding{Kind: "path", Ref: tok, Line: lineNo,
						Detail: "路径在仓库中不存在——计划新建的文件在该行标 new:"})
				}
			}
		}
	}
	sort.SliceStable(findings, func(a, b int) bool { return findings[a].Line < findings[b].Line })
	return findings
}

// checkFileRef verifies `path:line` (line within file length) or `path:Symbol`
// (Go declaration present in the file).
func checkFileRef(root, path, rest string, lineNo int, add func(DesignCheckFinding)) {
	ref := path + ":" + rest
	abs, ok := repoPath(root, path)
	if !ok {
		add(DesignCheckFinding{Kind: "path", Ref: ref, Line: lineNo, Detail: "引用越出仓库根目录，未核查"})
		return
	}
	body, err := readRepoFile(abs)
	if err != nil {
		add(DesignCheckFinding{Kind: "path", Ref: ref, Line: lineNo, Detail: "引用的文件在仓库中不存在或不是普通文件"})
		return
	}
	if n, convErr := strconv.Atoi(rest); convErr == nil {
		if total := strings.Count(string(body), "\n") + 1; n < 1 || n > total {
			add(DesignCheckFinding{Kind: "line", Ref: ref, Line: lineNo,
				Detail: fmt.Sprintf("行号超出文件范围（文件共 %d 行）", total)})
		}
		return
	}
	if filepath.Ext(path) != ".go" {
		return // 非 Go 文件的符号引用不做声明核查
	}
	recv, sym := "", rest
	if dot := strings.LastIndex(sym, "."); dot >= 0 {
		recv, sym = sym[:dot], sym[dot+1:] // Type.Method → 接收者 Type 的方法 Method
	}
	if !goDeclRe(recv, sym).Match(body) && !interfaceDeclares(body, recv, sym) {
		add(DesignCheckFinding{Kind: "symbol", Ref: ref, Line: lineNo, Detail: "文件中找不到该 Go 符号的声明"})
	}
}

// ParseTouchpoints extracts repo paths from the touch-point section (触点 /
// Touch points): the first path-like token of each list item or table row, with a
// trailing `:symbol`/`:line` stripped. `new:` items are kept — they are planned
// changes too.
//
// ParseTouchpoints 从触点一节（触点 / Touch points）提取仓库路径：每个列表项或
// 表格行的首个路径 token，去掉末尾的 `:符号`/`:行号`。`new:` 项保留——它们也是
// 计划内改动。
func ParseTouchpoints(text string) []string {
	var out []string
	seen := map[string]bool{}
	inSection, sectionLevel := false, 0
	lines, _ := proseLines(text)
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			level := len(line) - len(strings.TrimLeft(line, "#"))
			if touchpointsHeadingRe.MatchString(line) {
				inSection, sectionLevel = true, level
			} else if inSection && level <= sectionLevel {
				inSection = false
			}
			continue
		}
		if !inSection || !(strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "|")) {
			continue
		}
		for _, cand := range touchpointCandidates(line) {
			if fm := fileRefRe.FindStringSubmatch(cand); fm != nil {
				cand = fm[1]
			}
			if looksLikeRepoPath(cand) || (strings.Contains(cand, "/") && pathTokenRe.MatchString(cand)) {
				if !seen[cand] {
					seen[cand] = true
					out = append(out, cand)
				}
				break
			}
		}
	}
	return out
}

// touchpointCandidates yields backticked tokens first, then whitespace/pipe
// separated words with the new: marker stripped.
func touchpointCandidates(line string) []string {
	var cands []string
	for _, m := range backtickRe.FindAllStringSubmatch(line, -1) {
		cands = append(cands, strings.TrimSpace(m[1]))
	}
	for _, w := range strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '|' || r == '\t' || r == ',' }) {
		w = strings.Trim(w, "`-*")
		if i := strings.Index(strings.ToLower(w), "new:"); i == 0 {
			w = w[len("new:"):]
		}
		if w != "" {
			cands = append(cands, w)
		}
	}
	return cands
}

// ApplyDesignTouchpoints sets PlanScope from the design text's touch points when
// the task has no hand-declared scope (or the existing scope itself came from the
// design). Returns whether PlanScope changed. Hand-declared scope always wins.
//
// ApplyDesignTouchpoints 在任务没有手工声明 scope（或现有 scope 本就来自方案）时，
// 用方案触点设置 PlanScope。返回是否改动。手工声明的 scope 永远优先。
func ApplyDesignTouchpoints(state *TaskState, designText string) bool {
	if len(state.PlanScope) > 0 && state.PlanScopeSource != PlanScopeSourceDesign {
		return false
	}
	tps := ParseTouchpoints(designText)
	if len(tps) == 0 {
		return false
	}
	state.PlanScope = tps
	state.PlanScopeSource = PlanScopeSourceDesign
	return true
}

// RecordDesignCheck lands the design-check checklog row (advisory: Passed reflects
// the findings, the gate never blocks on it in v1).
//
// RecordDesignCheck 落 design-check 的 checklog 行（advisory：Passed 反映
// findings，v1 门禁不因它阻断）。
func RecordDesignCheck(root, taskRef string, findings []DesignCheckFinding) {
	counts := map[string]int{}
	for _, f := range findings {
		counts[f.Kind]++
	}
	meta := map[string]string{"findings": strconv.Itoa(len(findings))}
	for k, n := range counts {
		meta["kind."+k] = strconv.Itoa(n)
	}
	e := &checklog.Entry{
		Check: CheckNameDesignCheck, Passed: len(findings) == 0, Checked: true, TaskRef: taskRef,
		Level: checklog.LevelPass, Meta: meta,
		Detail: "design grounding: all references resolve",
	}
	if len(findings) > 0 {
		e.Level = checklog.LevelAdvisory
		refs := make([]string, 0, len(findings))
		for i, f := range findings {
			if i >= 5 {
				refs = append(refs, fmt.Sprintf("…+%d", len(findings)-5))
				break
			}
			refs = append(refs, f.Kind+":"+f.Ref)
		}
		e.Detail = "design grounding: unresolved " + strings.Join(refs, ", ")
	}
	recordAudit(root, e)
}
