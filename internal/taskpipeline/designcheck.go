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
	"path/filepath"
	"regexp"
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

// interfaceBlockRe captures `type Name interface { … }` blocks (closing brace at line start).
// Type parameters are allowed (`type Set[T any] interface {`); the opening brace
// end its line (a trailing // comment and CRLF are fine), so one-line `interface{}`
// never opens a block that would
// swallow the following function bodies.
var interfaceBlockRe = regexp.MustCompile(`(?ms)^type\s+(\w+)(?:\[[^\]]*\])?\s+interface\s*\{[ \t]*(?://[^\n]*)?\r?\n(.*?)^\}`)

// maxDesignRefBytes caps how much of a referenced file the check reads.
//
// maxDesignRefBytes 是核查读取被引用文件的字节上限。
const maxDesignRefBytes = 4 << 20

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
