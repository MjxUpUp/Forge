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
// 不是为了让人去跑。deadhint:allow 是逐行逃生舱（确实要举一个反例时用）。
var benignPhrases = []string{"已删除", "已随 core-only 移除", "命令不存在", "已移除", "removed", "deadhint:allow"}

func deadHintPattern() *regexp.Regexp {
	return regexp.MustCompile("forge (" + strings.Join(deletedTopCommands, "|") + ")([^a-z0-9_-]|$)")
}

func benignLine(s string) bool {
	for _, b := range benignPhrases {
		if strings.Contains(s, b) {
			return true
		}
	}
	return false
}

// TestNoDeadCommandHints 扫仓内所有非测试 Go 源码的字符串字面量，以及
// internal/**/templates 下的模板文档：不得出现 forge 加已删命令。
//
// 只看字符串字面量（常量、错误文案、模板正文），注释不执行也不进产物；
// 字符串字面量覆盖了「打印给用户看」与「拼进模板/文件」两条路径——第一版只扫
// fmt 系调用，漏掉 Errorf 之外的构造（字符串常量、模板拼接）。
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
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		lines := strings.Split(string(src), "\n")

		if strings.HasSuffix(path, ".md") {
			if !strings.Contains(rel, "templates/") {
				return nil
			}
			for i, line := range lines {
				if re.FindString(line) == "" || benignLine(line) {
					continue
				}
				offenders = append(offenders, rel+":"+itoa(i+1)+" "+strings.TrimSpace(re.FindString(line)))
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			return nil // 解析失败交给 go build 去报
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			match := re.FindString(lit.Value)
			if match == "" {
				return true
			}
			// 只按字面量本身判豁免：同一行别处出现「已删除」也可能是无关语句，
			// 整行豁免会把真正教用户跑死命令的文案一起放过。
			if benignLine(lit.Value) {
				return true
			}
			offenders = append(offenders, fset.Position(lit.Pos()).String()+" "+strings.TrimSpace(match))
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

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
