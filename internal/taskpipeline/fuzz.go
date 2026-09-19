package taskpipeline

// fuzz.go — oracle-pipeline L2b：Go 原生 Fuzz 的预算化执行器（机器批量出题）。
// 发现任务改动包里的 Fuzz 目标，按预算逐个实跑——fuzz 是无记忆的对抗者，
// 解的是 edge case 出题想象力问题：清单再全也是人枚举的，fuzz 不是。
// 失败时 go test 把崩溃输入写进 testdata/（FuzzXxx/ 目录）——证据即语料，
// 修完直接 go test 回归。

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameFuzzRun 是一次 fuzz 实跑的 checklog 条目（deterministic——forge 自己
// 拉起 fuzz 引擎并观察退出码）。
const CheckNameFuzzRun checklog.CheckName = "fuzz-run"

// FuzzTarget 是一个可执行的 fuzz 目标。
type FuzzTarget struct {
	Pkg  string // go test 的包参数（"./internal/x"）
	Func string // FuzzXxx
	File string // 所在 _test.go（repo 相对）
}

// FuzzResult 是一次预算化实跑的结果。
type FuzzResult struct {
	Targets  int
	Run      int
	Failed   []FuzzTarget
	Skipped  []FuzzTarget
	Findings []string // 失败目标语料路径提示
}

// DiscoverFuzzTargets 扫描任务改动文件所在目录的 _test.go（含改动测试文件
// 本身），收集 FuzzXxx 函数（纯发现，不执行）。确定性排序。
func DiscoverFuzzTargets(root string, state *TaskState) []FuzzTarget {
	changed := taskChangedFiles(root, state)
	dirs := map[string]bool{}
	for _, f := range changed {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		dirs[filepath.ToSlash(filepath.Dir(filepath.FromSlash(f)))] = true
	}
	var targets []FuzzTarget
	for dir := range dirs {
		entries, err := os.ReadDir(filepath.Join(filepath.FromSlash(root), filepath.FromSlash(dir)))
		if err != nil {
			continue
		}
		for _, ent := range entries {
			name := ent.Name()
			if !strings.HasSuffix(name, "_test.go") {
				continue
			}
			rel := dir + "/" + name
			funcs := scanFuzzFuncs(filepath.Join(filepath.FromSlash(root), filepath.FromSlash(rel)))
			pkg := "./" + dir
			if pkg == "./." {
				pkg = "."
			}
			for _, fn := range funcs {
				targets = append(targets, FuzzTarget{Pkg: pkg, Func: fn, File: rel})
			}
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Pkg != targets[j].Pkg {
			return targets[i].Pkg < targets[j].Pkg
		}
		return targets[i].Func < targets[j].Func
	})
	return targets
}

// scanFuzzFuncs 解析单个测试文件收集 Fuzz 开头的导出函数名。
func scanFuzzFuncs(absPath string) []string {
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
		if !ok || fd.Recv != nil {
			continue
		}
		if strings.HasPrefix(fd.Name.Name, "Fuzz") && fd.Name.IsExported() {
			names = append(names, fd.Name.Name)
		}
	}
	return names
}

// RunFuzz 按预算逐目标实跑：`go test -run '^$' -fuzz '^FuzzX$' -fuzztime Ns pkg`
// （-fuzz 每次恰一个目标；-run '^$' 排除其余普通测试）。超预算/无 Go 工具链
// 的目标记 Skipped。落一条确定性 checklog 行。失败分型（审查 P2-3）：退出码
// 1 = 真 crasher（go 把崩溃输入写进 testdata/，证据即语料）；其他非零 = 构建
// 失败/预算中断（无语料，措辞不谎称）。
func RunFuzz(root string, state *TaskState, targets []FuzzTarget, perTarget time.Duration, maxTargets int) (FuzzResult, error) {
	res := FuzzResult{Targets: len(targets)}
	if maxTargets <= 0 {
		maxTargets = 3
	}
	if perTarget <= 0 {
		perTarget = 30 * time.Second
	}
	for i, t := range targets {
		if i >= maxTargets {
			res.Skipped = append(res.Skipped, t)
			continue
		}
		code, out := runOneFuzz(root, t, perTarget)
		res.Run++
		if code != 0 {
			res.Failed = append(res.Failed, t)
			if code == 1 {
				res.Findings = append(res.Findings, fmt.Sprintf(
					"%s.%s 发现 crasher（退出码 1）——崩溃输入已写入 %s 的 testdata/，修完以 go test %s 回归",
					t.Pkg, t.Func, t.File, t.Pkg))
			} else {
				res.Findings = append(res.Findings, fmt.Sprintf(
					"%s.%s 失败（退出码 %d，非 crasher——构建失败或预算中断，无崩溃语料）：%s",
					t.Pkg, t.Func, code, truncateFuzzOut(out)))
			}
		}
	}
	recordFuzzRow(root, state.TaskRef, res)
	return res, nil
}

// truncateFuzzOut 取输出尾部 ~200 字节（失败诊断进 findings，全文在重跑里）。
func truncateFuzzOut(s string) string {
	const max = 200
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return "…" + s[len(s)-max:]
}

// runOneFuzz 执行单个 fuzz 目标，返回退出码与输出（0=预算内无发现）。ctx
// 超时强杀单独判（返回 -1）：Windows 上被杀进程退出码也是 1，不与真 crasher
// 混分（复审 nit——措辞不谎称语料）。
func runOneFuzz(root string, t FuzzTarget, perTarget time.Duration) (int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), perTarget+30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-run", "^$", "-fuzz", "^"+t.Func+"$", "-fuzztime", perTarget.String(), t.Pkg)
	cmd.Dir = root
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	if ctx.Err() != nil {
		return -1, string(out) + " [forge] fuzz 预算超限被强杀"
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out)
	}
	return 2, string(out) + " [forge] fuzz 进程异常: " + err.Error()
}

// recordFuzzRow 落证据行：有失败目标 Level=fail（deterministic 事实），否则
// pass。
func recordFuzzRow(root, taskRef string, res FuzzResult) {
	e := &checklog.Entry{
		Check:   CheckNameFuzzRun,
		Passed:  len(res.Failed) == 0,
		Checked: res.Run > 0,
		TaskRef: taskRef,
		Source:  checklog.EvidenceDeterministic,
	}
	if len(res.Failed) > 0 {
		e.Level = checklog.LevelFail
		var parts []string
		for _, f := range res.Failed {
			parts = append(parts, f.Pkg+"."+f.Func)
		}
		e.Detail = fmt.Sprintf("fuzz 实跑：targets %d（run %d / skip %d），失败: %s", res.Targets, res.Run, len(res.Skipped), strings.Join(parts, ", "))
	} else {
		e.Level = checklog.LevelPass
		e.Detail = fmt.Sprintf("fuzz 实跑：targets %d（run %d / skip %d），预算内零发现", res.Targets, res.Run, len(res.Skipped))
	}
	recordAudit(root, e)
}
