package srclint

// rawstring_test.go — repo-wide guard: single-line RAW string literals must not
// contain the two-character sequence `\n`. In a raw string that sequence never
// becomes a newline, so every Fprintf/Errorf that flows through one prints a
// literal backslash-n to the user (observed 2026-08: `forge trust list` printed
// `require_signed: false\n（无已登记节点）` — 108 sites repo-wide, fixed in
// fix/dsh-review-followup). The fix convention is raw content + `+"\n"`
// concatenation (same as internal/cli/hook.go).
//
// rawstring_test.go —— 全仓守卫：单行 raw string 字面量不得含 `\n` 两字符序列
// （raw string 里它永远不会变成换行，经 Fprintf/Errorf 输出就是字面反斜杠+n；
// 2026-08 实测 `forge trust list` 输出 `require_signed: false\n（无已登记节点）`，
// 全仓 108 处，已在 fix/dsh-review-followup 修复）。约定写法：raw 内容 + `+"\n"`
// 拼接（与 internal/cli/hook.go 同款）。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// intentionalRawNL whitelists literals where a literal `\n` IS the payload:
// regexes (`[^\n]` char classes), a JSON fixture (`"out":"x\n"` needs the JSON
// escape), a shell printf fixture (the shell interprets \n), and a Windows
// path (`Z:\no\...`). Matched by prefix; line numbers are deliberately not
// used so the whitelist survives edits around the sites.
//
// intentionalRawNL 白名单：字面 `\n` 本身就是内容的场景——正则（`[^\n]` 字符类）、
// JSON fixture（`"out":"x\n"` 需要的是 JSON 转义）、shell printf fixture（\n 由
// shell 解释）、Windows 路径（`Z:\no\...`）。按前缀匹配；刻意不用行号，白名单
// 不受周边编辑影响。
var intentionalRawNL = []string{
	`printf '{"cancel":false,"contextModification":"%s"}\n' "$context"`, // shell fixture
	`(?m)^\s*gh workflow run release\.yml\b[^\n]*--repo\b`,              // regex
	`Z:\no\such\dir\anywhere`,                                           // windows path
	`{"hook_event_name":"PostToolUse"`,                                  // JSON fixture (escaped \n inside)
	`(?s)^---\s*\n(.*?)\n---\s*\n?`,                                     // regex
	`\bexcept\b[^\n]*:\s*pass`,                                          // regex
	`C:\Users\u`,                                                        // windows path fixture (update_channel_test.go: \npm / \node_modules 序列是路径本身)
	`C:\npm-global`,                                                     // windows path fixture (update_channel_test.go 混合分隔符行)
	`{"command":"*** Begin Patch`,                                       // JSON fixture（attribution_test.go：patch 文本的 \n 是 JSON 转义，解码后成真换行）
}

func whitelisted(lit string) bool {
	inner := strings.TrimSuffix(strings.TrimPrefix(lit, "`"), "`") // lit.Value carries its backticks
	for _, p := range intentionalRawNL {
		if strings.HasPrefix(inner, p) {
			return true
		}
	}
	return false
}

func TestNoLiteralBackslashNInSingleLineRawStrings(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile))) // internal/srclint → repo root

	var offenders []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		// repo-wide, but skip VCS/tool dirs and the guard's own source (the matcher
		// itself contains raw `\n` literals).
		if info.IsDir() {
			name := info.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		// self-exclusion by BASENAME: runtime.Caller reports forward-slash paths on
		// windows while filepath.Walk yields backslash ones — a full-path compare
		// silently fails to exclude the guard itself there (first windows CI run).
		if err != nil || !strings.HasSuffix(path, ".go") || filepath.Base(path) == filepath.Base(thisFile) {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if len(lit.Value) >= 2 && lit.Value[0] == '`' &&
					!strings.Contains(lit.Value, "\n") && strings.Contains(lit.Value, `\n`) &&
					!whitelisted(lit.Value) {
					p := fset.Position(lit.Pos())
					offenders = append(offenders, filepath.Base(path)+":"+strconv.Itoa(p.Line)+": "+clip(lit.Value))
				}
			}
			return true
		})
		return nil
	})
	if len(offenders) > 0 {
		t.Fatalf("raw string 含字面 \\n（不会换行，输出即腐蚀）——改成 raw 内容 + \"+\\n\" 拼接（见 internal/cli/hook.go 风格）；若确属故意（正则/JSON/shell fixture）请加入 intentionalRawNL 白名单并注明:\n%s",
			strings.Join(offenders, "\n"))
	}
}

func clip(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}
