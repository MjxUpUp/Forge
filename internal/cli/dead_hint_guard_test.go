package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// deletedTopCommands 是 core-only 这轮砍掉的顶层命令。它们不该再出现在任何
// 打印给用户的字符串里——否则 agent 照做时会撞上 unknown command（core-only
// 收尾时真实踩到过：task complete 提示跑已删的 act retro-done）。
var deletedTopCommands = []string{
	"act", "bridge", "config", "conventions", "dashboard", "enforcement", "eval",
	"gate", "harness", "health", "migrate", "node", "policy", "project", "registry",
	"skill", "skills", "trace", "trust", "update", "verify", "workspace", "worktree",
}

// benignPhrases 让「说明某命令已被移除」的文案通过：它们提到死命令是为了告知，
// 不是为了让人去跑。
var benignPhrases = []string{"已删除", "已随 core-only 移除", "命令不存在", "已移除", "removed"}

func deadHintPattern() *regexp.Regexp {
	return regexp.MustCompile("forge (" + strings.Join(deletedTopCommands, "|") + ")([^a-z0-9_-]|$)")
}

// TestNoDeadCommandHints 扫仓内非测试 Go 源码：fmt 系调用的字符串实参里不得出现
// forge 加已删命令。只看 fmt 调用（打印面），注释与历史文档不管——注释不执行。
func TestNoDeadCommandHints(t *testing.T) {
	root := filepath.Join("..", "..")
	re := deadHintPattern()
	fset := token.NewFileSet()
	var offenders []string
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "testdata", ".tmp":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil // 解析失败交给 go build 去报
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if !isFmtCall(call) {
				return true
			}
			for _, arg := range call.Args {
				ast.Inspect(arg, func(m ast.Node) bool {
					lit, ok := m.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					v := lit.Value
					match := re.FindString(v)
					if match == "" {
						return true
					}
					for _, b := range benignPhrases {
						if strings.Contains(v, b) {
							return true
						}
					}
					offenders = append(offenders, fset.Position(lit.Pos()).String()+" "+strings.TrimSpace(match))
					return true
				})
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
	if len(offenders) > 0 {
		t.Fatalf("用户可见文案里还指向已删命令（core-only 已砍）：\n  %s\n要么改指活命令，要么在文案里写明「已随 core-only 移除」。", strings.Join(offenders, "\n  "))
	}
}

func isFmtCall(c *ast.CallExpr) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "fmt" {
		return false
	}
	switch sel.Sel.Name {
	case "Sprintf", "Errorf", "Fprintf", "Printf", "Println", "Print", "Fprintln", "Fprint", "Sprint", "Sprintln":
		return true
	}
	return false
}
