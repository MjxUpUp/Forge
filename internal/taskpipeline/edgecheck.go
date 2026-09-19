package taskpipeline

// edgecheck.go — oracle-pipeline L2c：edge case 机械枚举（五维清单生成器）。
// 「出 edge case」从灵感变流水线：任务改动的每个导出函数 × 五个维度
//（基数/值域/时序/环境/故障）生成可勾选的清单骨架，落 <DataDir>/edgecases/
// 供 spec 评审与测试计划消费——机器出骨架，人或 agent 填断言。观察行
// CheckNameEdgeChecklist 是 taskpipeline 字面量（不入 checklog roster——它是
// 产物生成事件，不是验证类检查，进证据白名单反而稀释 deterministic 口径）。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameEdgeChecklist 是一次清单生成的观察行（taskpipeline 字面量，不入
// roster——非验证类）。
const CheckNameEdgeChecklist checklog.CheckName = "edge-checklist"

// edgeDimensions 是五维清单的维度与每维一行提示（Go 语种的通用锚点；跨栈
// 维度语义不变，提示语按栈扩展是后续事）。
var edgeDimensions = []struct {
	Name string
	Hint string
}{
	{"基数", "空集/单元素/N 个/极大批量——空输入与单例是 off-by-one 的主场"},
	{"值域", "零值/最小/最大/负数/NaN/空串/超长串/多字节字符——边界与非法值"},
	{"时序", "并发调用/重入/乱序/超时取消（ctx cancel）/慢依赖——时间维的坏天气"},
	{"环境", "跨平台路径/时区/编码/locale/权限不足/磁盘满——运行环境假设"},
	{"故障", "依赖返回错误/超时/格式突变/部分失败——错误路径是否真的走到"},
}

// EdgeChecklistArtifact 生成清单并落盘，返回相对 DataDir 的产物路径。
type EdgeChecklistArtifact struct {
	Path    string
	Funcs   int
	Items   int
	Changed []string
}

// GenerateEdgeChecklist 对任务改动的非测试源文件生成五维清单（确定性：文件
// 排序 + 函数声明序）。产物写 <DataDir>/edgecases/<ref-safe>.md 并落观察行。
func GenerateEdgeChecklist(root string, state *TaskState) (EdgeChecklistArtifact, bool) {
	files := changedGoSourceFiles(root, state)
	if len(files) == 0 {
		return EdgeChecklistArtifact{}, false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# edge case 清单：%s\n\n", state.TaskRef)
	fmt.Fprintf(&b, "机械枚举五维（基数/值域/时序/环境/故障）——机器出骨架，验收前人或 agent 逐项填断言；\n勾选即对账（checkbox 是进度语义，与 task checklist 同构）。\n\n")
	art := EdgeChecklistArtifact{Changed: files}
	for _, f := range files {
		funcs := scanExportedFuncs(filepath.Join(filepath.FromSlash(root), filepath.FromSlash(f)))
		if len(funcs) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n", f)
		for _, fn := range funcs {
			art.Funcs++
			fmt.Fprintf(&b, "### %s\n\n", fn)
			for _, d := range edgeDimensions {
				art.Items++
				fmt.Fprintf(&b, "- [ ] **%s**：%s\n", d.Name, d.Hint)
			}
			b.WriteString("\n")
		}
	}
	if art.Funcs == 0 {
		return EdgeChecklistArtifact{Changed: files}, false
	}
	dir := filepath.Join(dataHome(root), "edgecases")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return EdgeChecklistArtifact{}, false
	}
	safe := strings.ReplaceAll(state.TaskRef, "/", "__")
	rel := filepath.ToSlash(filepath.Join("edgecases", safe+".md"))
	if err := os.WriteFile(filepath.Join(dir, safe+".md"), []byte(b.String()), 0o644); err != nil {
		return EdgeChecklistArtifact{}, false
	}
	art.Path = rel
	recordAudit(root, &checklog.Entry{
		Check:   CheckNameEdgeChecklist,
		Passed:  true,
		Checked: true,
		TaskRef: state.TaskRef,
		Level:   checklog.LevelPass,
		Detail:  fmt.Sprintf("edge case 清单生成：改动文件 %d / 导出函数 %d / 条目 %d → %s", len(files), art.Funcs, art.Items, rel),
	})
	return art, true
}

// scanExportedFuncs 解析文件收集导出函数签名（pkg.Func 形态渲染）。
func scanExportedFuncs(absPath string) []string {
	body, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, absPath, body, 0)
	if err != nil {
		return nil
	}
	var names []string
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || !fd.Name.IsExported() {
			continue
		}
		names = append(names, fd.Name.Name)
	}
	return names
}
