package checklog

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hostEraCheckNames 是 hook 时代（1.85 之前）留下、在 core-only 清理中删除的
// CheckName 常量。它们没有生产者：命令与 hook 都已不在，留着只会让 compat 面
// 与看板继续展示永远不会出现的检查名。
var hostEraCheckNames = []string{
	"CheckBashGuard", "CheckFileSentinel", "CheckScopeDrift", "CheckUnusedScan",
	"CheckKimiPluginStale", "CheckPlanFirst", "CheckTestNudge", "CheckSelfcheckPairing",
	"CheckSelfcheckScope", "CheckTestNudgeState", "CheckTaskDrift", "CheckConventionsInject",
	"CheckCrossRepoImpact", "CheckArtifactChain", "CheckArtifactDrift", "CheckArtifactDrill",
	"CheckGateCmdForm", "CheckHookInfra",
}

// TestNoHostEraCheckNames 钉住 core-only 的删除：这 18 个常量不得再出现在
// checklog 包里（常量、roster、白名单都算）。
//
// 用源扫描而不是直接引用：如果它们被加回来，引用式断言编译不过，报错会像
// "undefined: X" 而不是"你把删掉的东西加回来了"——源扫描把意图写进失败信息。
func TestNoHostEraCheckNames(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("没有找到 checklog 包源文件")
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		astFile, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		ast.Inspect(astFile, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			for _, dead := range hostEraCheckNames {
				if id.Name == dead {
					t.Errorf("%s:%d 仍声明/引用宿主时代检查名 %s——core-only 已删，别再引回来",
						filepath.Base(path), fset.Position(id.Pos()).Line, dead)
				}
			}
			return true
		})
	}
}
