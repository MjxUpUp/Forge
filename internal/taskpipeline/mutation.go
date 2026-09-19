package taskpipeline

// mutation.go — oracle-pipeline L2a：mutation 抽样引擎（机器出题治假绿）。
// 对任务改动的 Go 源文件做确定性算子变异（比较翻转 / 逻辑翻转），逐个实跑
// 所在包的 go test——杀不死变异体的测试是装饰品（断言弱化/空断言无论覆盖率
// 多高都查不出）。每个变异体就是一道注入的 bad case：测试套件必须逐个抓住。
//
// 设计约束：
//   - deterministic：候选扫描与抽样均按 (file, offset) 排序稳定取样，无随机
//   - 原地变异 + sidecar 备份：变异前把原始字节写到 <file>.forge-mutbak，
//     跑完还原并删备份；还原失败立即报错指路备份文件（git checkout 会连带
//     销毁任务未提交改动，不作指引）；进程被杀时备份残留——下次起跑检测
//     残留即拒绝执行（如实披露，不自动恢复——备份可能早于其后的合法编辑）
//   - 证据：一次抽样落一条 checklog 行（deterministic 源，计入证据强度白名单）；
//     零有效样本（killed+survived==0）= 无信号，判 no-signal 不产假 PASS
//   - advisory：永不阻断（v1）——独立命令按需执行，不是每任务强制

import (
	"bytes"
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

// CheckNameMutationSampling 是一次 mutation 抽样的 checklog 条目（deterministic
// ——forge 自己变异、自己跑测试、自己数杀灭，agent 无法伪造结果侧）。
const CheckNameMutationSampling checklog.CheckName = "mutation-sampling"

// mutationOpFlips 是 v1 算子表：比较翻转（经典 relational/operator mutation）
// 与逻辑翻转（&&↔||）。全部同宽或宽体替换在 applyMutation 处理；刻意不含
// 算术（+↔- 在字符串/指针上下文会产出编译失败型无效体）与布尔字面量
// （等价变异体率高）——v1 宁少勿滥：每道题都必须真的在考。
var mutationOpFlips = map[string]string{
	"==": "!=",
	"!=": "==",
	"<":  ">=",
	">=": "<",
	">":  "<=",
	"<=": ">",
	"&&": "||",
	"||": "&&",
}

// MutationSite 是一个可变异位点：repo 相对路径 + 算子字节偏移 + 翻转对。
type MutationSite struct {
	File   string
	Offset int
	Line   int
	Op     string
	Flip   string
	// PkgDir 是 go test 的包目录参数（"./internal/x" 形态；根包为 "."）。
	PkgDir string
}

// MutationResult 是一次抽样的结构化结果。
type MutationResult struct {
	Sampled   int
	Killed    int
	Survived  int
	Invalid   int
	Survivors []MutationSite
	// Score = killed / (killed + survived)；Invalid（编译不过的变异体）不计分
	// ——编译器抓住的变异计入杀灭会虚高分数，而它考的是编译器不是测试。
	Score float64
}

// mutationTimeoutPerMutant 限制单个变异体的测试时长（挂死循环的变异体由
// go test 超时机制转成失败——被"杀灭"，不再吃后续预算）。FORGE_MUTATION_TIMEOUT
// 可调（Go duration）。
var mutationTimeoutPerMutant = 3 * time.Minute

func init() {
	if v := os.Getenv("FORGE_MUTATION_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			mutationTimeoutPerMutant = d
		}
	}
}

// changedGoSourceFiles 列出任务改动的非测试 Go 源文件（repo 相对 slash 路径）。
func changedGoSourceFiles(root string, state *TaskState) []string {
	var files []string
	for _, f := range taskChangedFiles(root, state) {
		if strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") && !strings.Contains(f, "/vendor/") && !strings.HasPrefix(f, "vendor/") {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	return files
}

// ScanMutationSites 解析单个 Go 文件收集变异位点（纯函数可单测）。absPath 是
// 文件绝对路径，relFile 是 repo 相对路径——PkgDir（go test 的包参数）必须从
// relFile 推导：从绝对路径推会产出 "./C:/..." 形态的坏包路径。只收 BinaryExpr
// 的算子——一元/赋值上下文的 token 不碰（渲染歧义）。
func ScanMutationSites(absPath, relFile string) ([]MutationSite, error) {
	body, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, relFile, body, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", relFile, err)
	}
	dir := "./" + filepath.ToSlash(filepath.Dir(filepath.FromSlash(relFile)))
	if dir == "./." {
		dir = "."
	}
	var sites []MutationSite
	ast.Inspect(f, func(n ast.Node) bool {
		if be, ok := n.(*ast.BinaryExpr); ok {
			flip, has := mutationOpFlips[be.Op.String()]
			if !has {
				return true
			}
			pos := fset.Position(be.OpPos)
			sites = append(sites, MutationSite{
				File:   relFile,
				Offset: pos.Offset,
				Line:   pos.Line,
				Op:     be.Op.String(),
				Flip:   flip,
				PkgDir: dir,
			})
		}
		return true
	})
	return sites, nil
}

// SampleMutationSites 确定性抽样：按 (file, offset) 排序后均匀步进取 n 个
// （n<=0 或 n>=len 时全取）——无随机源，同状态重跑同样本。
func SampleMutationSites(sites []MutationSite, n int) []MutationSite {
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].File != sites[j].File {
			return sites[i].File < sites[j].File
		}
		return sites[i].Offset < sites[j].Offset
	})
	if n <= 0 || n >= len(sites) {
		return sites
	}
	out := make([]MutationSite, 0, n)
	step := float64(len(sites)) / float64(n)
	for i := 0; i < n; i++ {
		out = append(out, sites[int(float64(i)*step)])
	}
	return out
}

// mutationBakSuffix 是 sidecar 备份后缀：变异前落原始字节，还原成功即删；
// 残留（进程被杀）由下次起跑检测并拒绝执行。
const mutationBakSuffix = ".forge-mutbak"

// applyMutation 就地把 file 的 [offset, offset+len(op)) 替换为 flip 并写盘。
// 写盘前把原始字节落到 sidecar 备份（<file>.forge-mutbak）——进程被杀时它是
// 唯一的恢复依据（审查 P1-4）。返回恢复闭包：写回原始字节 + 删备份。
func applyMutation(file string, offset int, op, flip string) (func() error, error) {
	path := filepath.FromSlash(file)
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if offset+len(op) > len(original) || string(original[offset:offset+len(op)]) != op {
		return nil, fmt.Errorf("变异位点失配：%s@%d 期望 %q", file, offset, op)
	}
	mutated := append([]byte{}, original[:offset]...)
	mutated = append(mutated, flip...)
	mutated = append(mutated, original[offset+len(op):]...)
	if err := os.WriteFile(path+mutationBakSuffix, original, 0o644); err != nil {
		return nil, fmt.Errorf("备份原始字节失败（不变异）: %w", err)
	}
	if err := os.WriteFile(path, mutated, 0o644); err != nil {
		_ = os.Remove(path + mutationBakSuffix)
		return nil, err
	}
	return func() error {
		if err := os.WriteFile(path, original, 0o644); err != nil {
			return err
		}
		return os.Remove(path + mutationBakSuffix)
	}, nil
}

// detectLeftoverBackups 起跑前扫描改动文件所在目录的 .forge-mutbak 残留
// （上次抽样被进程中断的铁证）——存在即拒绝执行并指路手动恢复，绝不自动
// 覆盖（备份可能早于其后的合法编辑）。
func detectLeftoverBackups(root string, files []string) []string {
	var leftovers []string
	seen := map[string]bool{}
	for _, f := range files {
		dir := filepath.Dir(filepath.Join(filepath.FromSlash(root), filepath.FromSlash(f)))
		if seen[dir] {
			continue
		}
		seen[dir] = true
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), mutationBakSuffix) {
				leftovers = append(leftovers, filepath.Join(dir, e.Name()))
			}
		}
	}
	return leftovers
}

// runPkgTests 在 root 下跑单个包的测试（-count=1 防缓存假绿——虽然内容哈希
// 变化本应击穿缓存，显式禁缓存让契约不依赖实现细节）。返回退出码与输出：
// go 工具约定 1=测试失败、2=构建/设置失败——编译器抓住的变异体（2）与测试
// 抓住的（1）由此机械区分，不做输出字符串嗅探。
func runPkgTests(ctx context.Context, root, pkgDir string, timeout time.Duration) (int, string) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "go", "test", "-count=1", pkgDir)
	cmd.Dir = root
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out)
	}
	return 2, string(out) + "\n[forge] 变异体测试进程异常: " + err.Error()
}

// RunMutationSampling 是引擎入口：扫描任务改动源文件的变异位点 → 确定性
// 抽样 → 逐个原地变异、实跑所在包测试、立即还原 → 汇总杀灭率并落 checklog
// 证据行。sample<=0 时取默认 6；无改动源文件/无位点时 Checked=false 返回。
func RunMutationSampling(root string, state *TaskState, sample int) (MutationResult, bool, error) {
	res := MutationResult{}
	if sample <= 0 {
		sample = 6
	}
	files := changedGoSourceFiles(root, state)
	if len(files) == 0 {
		return res, false, nil
	}
	var sites []MutationSite
	for _, f := range files {
		abs := filepath.Join(filepath.FromSlash(root), filepath.FromSlash(f))
		ss, err := ScanMutationSites(abs, f)
		if err != nil {
			// 单文件解析失败（生成代码等）：跳过该文件，不废整个抽样。
			continue
		}
		sites = append(sites, ss...)
	}
	if len(sites) == 0 {
		return res, false, nil
	}
	picked := SampleMutationSites(sites, sample)
	// 起跑护栏（审查 P1-4）：上次被中断的抽样留下的备份残留 = 工作区可能有
	// 未还原的变异体——拒绝执行，指路手动恢复（比对后决定取舍）。
	if leftovers := detectLeftoverBackups(root, files); len(leftovers) > 0 {
		return res, true, fmt.Errorf("检测到变异备份残留（上次抽样被中断的铁证）：%s——先人工比对 .forge-mutbak 与当前文件后手动恢复/删除备份，再重跑（绝不自动覆盖：备份可能早于其后的合法编辑）", strings.Join(leftovers, ", "))
	}
	for _, site := range picked {
		abs := filepath.Join(filepath.FromSlash(root), filepath.FromSlash(site.File))
		restore, err := applyMutation(abs, site.Offset, site.Op, site.Flip)
		if err != nil {
			res.Invalid++
			continue
		}
		exitCode, _ := runPkgTests(context.Background(), root, site.PkgDir, mutationTimeoutPerMutant)
		if rerr := restore(); rerr != nil {
			return res, true, fmt.Errorf("变异体还原失败（%s）——原始字节已留在 %s%s，从它恢复（勿用 git checkout：会连带销毁任务未提交改动）: %w", site.File, site.File, mutationBakSuffix, rerr)
		}
		res.Sampled++
		switch {
		case exitCode == 0:
			res.Survived++
			res.Survivors = append(res.Survivors, site)
			fmt.Fprintf(os.Stderr, "  ☠ 存活 %s:%d（%s→%s）——测试没抓住这道 bad case\n", site.File, site.Line, site.Op, site.Flip)
		case exitCode == 2:
			// 编译失败型变异体：编译器抓住的，考的是编译器不是测试，不计分。
			res.Invalid++
		default:
			// 测试失败（含测试超时/panic）：测试套件抓住了这道 bad case。
			res.Killed++
		}
	}
	if k := res.Killed + res.Survived; k > 0 {
		res.Score = float64(res.Killed) / float64(k)
	}
	recordMutationRow(root, state.TaskRef, res)
	return res, true, nil
}

// recordMutationRow 落确定性证据行。零有效样本（killed+survived==0：工具链
// 缺失或任务窗口构建已坏）= 无信号——Passed=false / verdict=no-signal，
// 绝不产出"零验证却 deterministic PASS"的假绿行（审查 P1-3）；survived>0 时
// Level=warn——advisory 披露，不阻断。
func recordMutationRow(root, taskRef string, res MutationResult) {
	e := &checklog.Entry{
		Check:   CheckNameMutationSampling,
		Passed:  res.Killed+res.Survived > 0 && res.Survived == 0,
		Checked: true,
		TaskRef: taskRef,
		Source:  checklog.EvidenceDeterministic,
		Meta: map[string]string{
			"sampled":  fmt.Sprintf("%d", res.Sampled),
			"killed":   fmt.Sprintf("%d", res.Killed),
			"survived": fmt.Sprintf("%d", res.Survived),
			"invalid":  fmt.Sprintf("%d", res.Invalid),
			"score":    fmt.Sprintf("%.2f", res.Score),
			"verdict":  mutationVerdict(res),
		},
	}
	switch {
	case res.Killed+res.Survived == 0:
		e.Level = checklog.LevelWarn
		e.Detail = fmt.Sprintf("mutation 抽样：零有效样本（sampled %d 全为无效体）——无验证信号，检查 go 工具链与任务窗口构建后重跑", res.Sampled)
	case res.Survived > 0:
		e.Level = checklog.LevelWarn
		var parts []string
		for _, s := range res.Survivors {
			parts = append(parts, fmt.Sprintf("%s:%d(%s→%s)", s.File, s.Line, s.Op, s.Flip))
		}
		e.Detail = fmt.Sprintf("mutation 抽样：杀灭率 %.0f%%（killed %d / survived %d / invalid %d）——存活位点: %s", res.Score*100, res.Killed, res.Survived, res.Invalid, strings.Join(parts, ", "))
	default:
		e.Level = checklog.LevelPass
		e.Detail = fmt.Sprintf("mutation 抽样：杀灭率 100%%（killed %d / invalid %d）", res.Killed, res.Invalid)
	}
	recordAudit(root, e)
}

// mutationVerdict 渲染一行人读结论（CLI 复用）。
func mutationVerdict(res MutationResult) string {
	switch {
	case res.Sampled == 0:
		return "no-sites"
	case res.Killed+res.Survived == 0:
		return "no-signal"
	case res.Survived == 0:
		return "all-killed"
	default:
		return "has-survivors"
	}
}

// FormatMutationSummary 供 CLI 输出复用的渲染（纯函数）。
func FormatMutationSummary(res MutationResult) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "mutation 抽样结果：样本 %d（killed %d / survived %d / invalid %d），杀灭率 %.0f%%\n",
		res.Sampled, res.Killed, res.Survived, res.Invalid, res.Score*100)
	for _, s := range res.Survivors {
		fmt.Fprintf(&b, "  ☠ 存活 %s:%d（%s→%s）\n", s.File, s.Line, s.Op, s.Flip)
	}
	if res.Survived > 0 {
		b.WriteString("存活 = 注入的 bad case 测试没抓住——按位点补断言（修测试，不是删变异）\n")
	}
	return b.String()
}
