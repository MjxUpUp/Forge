package taskpipeline

// finding_regression.go — oracle-pipeline L4：修复必须自证（finding resolve 的
// 回归测试前置）。bug 一旦出现过就永久变成免疫记忆——没有「修前红、修后绿」
// 的回归测试就不算修完；finding 的存在作证「红」半边，--with-test 指向本任务
// 改动的测试文件作证「绿」半边。无可测形态走 --no-test 落审计行（逃生不静默）。

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

// TaskChangedFiles 导出任务改动文件清单（repo 相对 slash 路径）——L4 的
// --with-test 校验消费（必须落在任务窗口内：改动是任务的一部分，测试同理）。
func TaskChangedFiles(root string, state *TaskState) []string {
	return taskChangedFiles(root, state)
}

// IsTestFilePath 导出测试文件判定（testcoverage.go isTestFile 的单一真相源
// 包装——cli 的 test-diff 披露面复用它而非复制弱化版后缀表：复制即漂移，
// pytest 的 test_*.py 前缀形态正是会被弱化版漏掉的对象）。
func IsTestFilePath(path string) bool {
	return isTestFile(path)
}

// IsGoTestFileWithTests 报告 rel（repo 相对）是否为含 Test/Fuzz 函数的 _test.go。
// 三重校验合一：后缀（是测试文件）、在给定改动集内（属本任务）、内容真有
// 测试函数（防拿无关旧测试文件冒充回归）。
func IsGoTestFileWithTests(root, rel string, changed []string) bool {
	if !strings.HasSuffix(rel, "_test.go") {
		return false
	}
	inWindow := false
	for _, c := range changed {
		if filepath.ToSlash(c) == filepath.ToSlash(rel) {
			inWindow = true
			break
		}
	}
	if !inWindow {
		return false
	}
	return hasTestFuncs(filepath.Join(filepath.FromSlash(root), filepath.FromSlash(rel)))
}

// hasTestFuncs 解析文件确认存在 Test*/Fuzz*/Benchmark* 函数（宽松前缀——
// 回归载体不限于 Test 前缀的形态）。
func hasTestFuncs(absPath string) bool {
	body, err := os.ReadFile(absPath)
	if err != nil {
		return false
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, absPath, body, 0)
	if err != nil {
		return false
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv != nil {
			continue
		}
		n := fd.Name.Name
		if strings.HasPrefix(n, "Test") || strings.HasPrefix(n, "Fuzz") || strings.HasPrefix(n, "Benchmark") {
			return true
		}
	}
	return false
}

// RequireFindingRegression 是 resolve 的 L4 前置：resolveID 须已通过
// RecordFindingRegression 登记回归绑定（测试文件路径，或 none:<理由> 的审计
// 逃生）。缺失即拒绝并指路 forge task regression。手改 state 的对抗面由
// IntegrityBroken 前置封死（审查 P1-2：regression 绑定是 gate-satisfying 字段，
// 其消费方必须验签——否则后续 MutateTaskState 的重签名会把手改内容洗白）；
// 文件绑定在消费侧复验（审查 P2-2：登记后文件可能被删/掏空——「绿」半边
// 不能是幽灵）。
func RequireFindingRegression(root string, state *TaskState, findingID string) error {
	if state.IntegrityBroken() {
		return fmt.Errorf(`finding resolve 拒绝：任务状态文件完整性校验失败（在 forge 之外被修改——手改的回归绑定不被采信）。恢复路径：forge task abort 后重新走门禁`)
	}
	binding := state.RegressionTests[findingID]
	if binding == "" {
		return fmt.Errorf(`finding %s 无回归绑定（oracle-pipeline L4：修前红由 finding 作证、修后绿由回归测试作证——没有回归的 fixed 会复发）。登记：forge task regression --finding %s --test <本任务改动的 _test.go>；确无可测形态：forge task regression --finding %s --none --note <理由>（落审计行）`,
			findingID, findingID, findingID)
	}
	if strings.HasPrefix(binding, "none:") {
		return nil
	}
	if !IsGoTestFileWithTests(root, binding, TaskChangedFiles(root, state)) {
		return fmt.Errorf("finding %s 的回归绑定 %q 已失效（文件被删/移出任务窗口/不再含测试函数）——重新登记：forge task regression --finding %s --test <本任务改动的 _test.go>", findingID, binding, findingID)
	}
	return nil
}

// RecordFindingRegression 登记 finding 的回归绑定（锁内写 TaskState.
// RegressionTests）。--none 形态落审计行（无可测形态的显式声明：理由必填，
// 绝不静默——「标注≠解决」纪律的执法面）；--test 形态先做三重校验。
func RecordFindingRegression(root string, state *TaskState, findingID, testFile, noneNote string) error {
	if findingID == "" {
		return fmt.Errorf("--finding <id> 必填（forge task resume 查看现有 ID）")
	}
	hasFinding := false
	for _, f := range state.Findings {
		if f.ID == findingID {
			hasFinding = true
		}
	}
	if !hasFinding {
		return fmt.Errorf("未找到发现 ID %q（forge task resume 查看现有 ID）", findingID)
	}
	switch {
	case noneNote != "":
		if err := MutateTaskState(root, state.TaskRef, func(s *TaskState) error {
			if s.RegressionTests == nil {
				s.RegressionTests = map[string]string{}
			}
			s.RegressionTests[findingID] = "none:" + noneNote
			return nil
		}); err != nil {
			return err
		}
		RecordFindingRegressionEscape(root, state, findingID, noneNote)
		return nil
	case testFile == "":
		return fmt.Errorf(`--test <本任务改动的测试文件> 与 --none --note <理由> 二选一`)
	default:
		if !IsGoTestFileWithTests(root, testFile, TaskChangedFiles(root, state)) {
			return fmt.Errorf("--test %q 未通过校验：须是本任务改动的 _test.go 且含 Test/Fuzz 函数（拿无关旧测试冒充回归不算数；forge task scope show 看任务改动集）", testFile)
		}
		return MutateTaskState(root, state.TaskRef, func(s *TaskState) error {
			if s.RegressionTests == nil {
				s.RegressionTests = map[string]string{}
			}
			s.RegressionTests[findingID] = testFile
			return nil
		})
	}
}

// RecordFindingRegressionEscape 落 --none 逃生审计行（无可测形态的显式
// 声明：理由必填，绝不静默——「标注≠解决」纪律的执法面）。
func RecordFindingRegressionEscape(root string, state *TaskState, findingID, note string) {
	recordAudit(root, &checklog.Entry{
		Check:   checklog.CheckEscapeHatch,
		Passed:  true,
		Checked: true,
		Level:   checklog.LevelWarn,
		TaskRef: state.TaskRef,
		Detail:  fmt.Sprintf("escape-hatch: finding %s resolved WITHOUT regression test (--none: %s)", findingID, note),
		Meta:    map[string]string{"escape.gate": "finding-regression", "escape.reason": checklog.EscapeReasonOverride, "escape.owner": state.TaskRef},
	})
}
