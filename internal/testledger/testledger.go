// Package testledger 把「仓库里有哪些测试、上一次真的跑过吗、哪些被跳过了」
// 变成一份可核对的账本。
//
// 设计口径（刻意保守）：
//   - 真相源是执行记录（go test -json 落到 .forge/test-exec.jsonl），不是覆盖率
//     报告、也不是人写的清单。没有记录的测试一律判 orphaned——"不确定它跑没跑"
//     在账本里就是一种需要处理的状态，不是可以默认通过的状态。
//   - Skip 分两件事：有没有写理由（源码里 t.Skip 的参数必须是非空字符串字面量），
//     以及这条理由有没有被审批（spec/test-skips.yaml 里的清单）。两者都满足才算
//     合法的跳过；只满足一半就进违规清单。
//   - 只有无条件 Skip 才算“被跳过的测试”。写在 if 里的 Skip（平台/环境/短模式闸门）
//     是条件跳过：测试平时照常跑，把它算成 skipped 会让账本满屏噪声——第一版就是
//     这样，59 条“未审批”里有 50 多条其实是 if runtime.GOOS != "windows" 这类闸门。
//     “信号里掺噪声”和“没有信号”一样糟。
//   - 记录里存在、源码里已经没有的测试判 phantom——测试被删是允许的，但删除要
//     留痕，而不是悄悄从账本里消失。
package testledger

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// State 是账本里一条测试的四种状态之一。
type State string

const (
	// StateLive：源码里有声明，且有 MaxAge 内的执行记录。
	StateLive State = "live"
	// StateOrphaned：源码里有声明，但没有（足够新的）执行记录。
	StateOrphaned State = "orphaned"
	// StateSkipped：源码里调用了 t.Skip*。
	StateSkipped State = "skipped"
	// StatePhantom：执行记录里有，源码里已经没有这个测试。
	StatePhantom State = "phantom"
)

// Decl 是一个测试声明（来自 AST 扫描，不执行代码）。
type Decl struct {
	Test       string
	Package    string // import path
	File       string // repo 相对 slash 路径
	Line       int
	HasSkip    bool
	SkipReason string // 只认非空字符串字面量参数；变量/空串都算"没写理由"
	// SkipGuarded 表示这条 t.Skip* 全部写在 if 条件里（环境闸门）。条件跳过的测试
	// 平时照常跑，不是"被静默关掉的测试"，因此不计入 skipped、也不需要审批。
	SkipGuarded bool
}

// Record 是一条执行记录（来自 go test -json 的 pass/fail/skip 事件）。
type Record struct {
	Package string    `json:"package"`
	Test    string    `json:"test"`
	Action  string    `json:"action"`
	At      time.Time `json:"at"`
}

// SkipApproval 是 spec/test-skips.yaml 里的一条审批。
type SkipApproval struct {
	Test       string `yaml:"test" json:"test"`
	Reason     string `yaml:"reason" json:"reason"`
	ApprovedBy string `yaml:"approved_by" json:"approved_by"`
	At         string `yaml:"at" json:"at"`
}

// Entry 是账本里的一行（一条测试或一条幽灵记录）。
type Entry struct {
	Test         string    `json:"test"`
	Package      string    `json:"package"`
	File         string    `json:"file,omitempty"`
	Line         int       `json:"line,omitempty"`
	State        State     `json:"state"`
	SkipReason   string    `json:"skip_reason,omitempty"`
	SkipApproved bool      `json:"skip_approved,omitempty"`
	GuardedSkip  bool      `json:"guarded_skip,omitempty"`
	LastSeen     time.Time `json:"last_seen,omitempty"`
}

// Ledger 是一次分类的结果。
type Ledger struct {
	Entries []Entry       `json:"entries"`
	Counts  map[State]int `json:"counts"`
}

// Options 控制分类口径。
type Options struct {
	// MaxAge 是"记录还算新鲜"的上限；<=0 取默认 7 天。
	MaxAge time.Duration
	// Now 是判定基准时间；零值取 time.Now()（测试里可注入固定时间）。
	Now time.Time
	// Approvals 是按 "<import path>.<TestName>" 索引的 Skip 审批。
	Approvals map[string]SkipApproval
}

// DefaultMaxAge 是一条执行记录默认的新鲜期。
const DefaultMaxAge = 7 * 24 * time.Hour

// ScanDecls 遍历仓库里的 *_test.go，收集 Test* 函数及其 Skip 情况。
//
// 跳过 vendor / 隐藏目录 / node_modules / testdata：前者不是本仓代码，后者
// 是测试夹具（里面的 *_test.go 常是故意写坏的反例，不该进账本）。
//
// 跳过来源分两层：函数体里直接写的 t.Skip*（按语法位置判断是否在条件里），
// 以及调用同目录 helper 时的间接跳过（helper 里写着 Skip，测试本身一行 Skip
// 都没有——纯看调用点根本看不出来，只能把 helper 也解析一遍）。
func ScanDecls(root string) ([]Decl, error) {
	mod := modulePath(root)
	byDir := map[string][]parsedTestFile{}
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == root {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return nil // 单个文件解析失败不废整次扫描；账本少一行比整本读不出来好
		}
		dir := path.Dir(rel)
		if _, seen := byDir[dir]; !seen {
			dirs = append(dirs, dir)
		}
		byDir[dir] = append(byDir[dir], parsedTestFile{rel: rel, fset: fset, file: f})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	var decls []Decl
	for _, dir := range dirs {
		files := byDir[dir]
		helpers := helperSkips(files)
		pkg := dirPackage(mod, dir)
		for _, pf := range files {
			for _, dec := range pf.file.Decls {
				fn, ok := dec.(*ast.FuncDecl)
				if !ok || fn.Name == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
					continue
				}
				hasSkip, reason, guarded := skipInfo(fn.Body)
				if !hasSkip {
					if h, ok := callsHelper(fn.Body, helpers); ok {
						hs := helpers[h]
						hasSkip, guarded = true, hs.guarded
						reason = "经 helper 跳过（" + h + " 内含 t.Skip）"
					}
				}
				decls = append(decls, Decl{
					Test:        fn.Name.Name,
					Package:     pkg,
					File:        pf.rel,
					Line:        pf.fset.Position(fn.Pos()).Line,
					HasSkip:     hasSkip,
					SkipReason:  reason,
					SkipGuarded: guarded,
				})
			}
		}
	}
	sort.Slice(decls, func(i, j int) bool {
		if decls[i].Package != decls[j].Package {
			return decls[i].Package < decls[j].Package
		}
		if decls[i].File != decls[j].File {
			return decls[i].File < decls[j].File
		}
		return decls[i].Line < decls[j].Line
	})
	return decls, nil
}

// parsedTestFile 是一个已解析的测试文件（同目录的 helper 要一起看，所以按目录攒）。
type parsedTestFile struct {
	rel  string
	fset *token.FileSet
	file *ast.File
}

// helperSkip 是一个 helper 的跳过信息：理由，以及它内部的 Skip 是否全在
// 条件里（决定调用点算"条件跳过"还是"无条件跳过"）。
type helperSkip struct {
	reason  string
	guarded bool
}

// helperSkips 找出同目录里"可能跳过"的 helper 函数，返回 函数名 -> 信息。
//
// 判据很粗：非 Test* 的函数体里出现 t.Skip* 就算 helper（helper 存在的意义
// 就是"某些条件下不起作用"）。再跑几轮不动点，让"helper 调 helper"也能传递
// 到调用点。guarded 跟着传播：helper 里是无条件 Skip，调用点就必须按无条件
// 跳过记，否则账本会把"永远不跑"洗成"条件跳过"。
func helperSkips(files []parsedTestFile) map[string]helperSkip {
	own := map[string]helperSkip{}
	for _, pf := range files {
		for _, dec := range pf.file.Decls {
			fn, ok := dec.(*ast.FuncDecl)
			if !ok || fn.Name == nil || strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			if has, reason, guarded := skipInfo(fn.Body); has {
				if strings.TrimSpace(reason) == "" {
					reason = "t.Skip"
				}
				own[fn.Name.Name] = helperSkip{reason: reason, guarded: guarded}
			}
		}
	}
	for round := 0; round < 8; round++ {
		changed := false
		for _, pf := range files {
			for _, dec := range pf.file.Decls {
				fn, ok := dec.(*ast.FuncDecl)
				if !ok || fn.Name == nil || strings.HasPrefix(fn.Name.Name, "Test") {
					continue
				}
				if _, done := own[fn.Name.Name]; done {
					continue
				}
				if h, ok := callsHelper(fn.Body, own); ok {
					own[fn.Name.Name] = helperSkip{reason: "经 " + h + " 跳过", guarded: own[h].guarded}
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	return own
}

// callsHelper 找一个函数体里对 helper 集合中函数的调用（只认裸标识符调用）。
func callsHelper[T any](body *ast.BlockStmt, helpers map[string]T) (string, bool) {
	if body == nil || len(helpers) == 0 {
		return "", false
	}
	found := ""
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		if _, isHelper := helpers[id.Name]; isHelper && found == "" {
			found = id.Name
		}
		return true
	})
	return found, found != ""
}

// skipInfo 判断函数体里有几个 t.Skip* 调用：有没有、第一参数是不是非空字符串
// 字面量，以及它们是否【全都】写在条件上下文里。
//
// "在条件里"这一维是账本能说实话的关键：绝大多数 Skip 是环境闸门
// （if runtime.GOOS != "windows"、if testing.Short()、if _, err := exec.LookPath("git"); err != nil），
// 这些测试平时照常跑，不该被算成"被静默关掉的测试"。真正的静默删除是函数体里
// 无条件的那一句 t.Skip("我先不管了")。
//
// 条件上下文包括 if / for / range / switch / type switch / select 的体与分支：
// "case 里 Skip" 与 "if 里 Skip" 在语义上没区别——都是某条件下才不跑。
func skipInfo(body *ast.BlockStmt) (has bool, reason string, guarded bool) {
	if body == nil {
		return false, "", false
	}
	guarded = true
	var walk func(n ast.Node, inCond bool)
	walk = func(n ast.Node, inCond bool) {
		switch x := n.(type) {
		case nil:
			return
		case *ast.BlockStmt:
			for _, s := range x.List {
				walk(s, inCond)
			}
		case *ast.IfStmt:
			walk(x.Init, true)
			walk(x.Cond, true)
			walk(x.Body, true)
			walk(x.Else, true)
		case *ast.ForStmt:
			walk(x.Init, true)
			walk(x.Cond, true)
			walk(x.Post, true)
			walk(x.Body, true)
		case *ast.RangeStmt:
			walk(x.Key, true)
			walk(x.Value, true)
			walk(x.X, true)
			walk(x.Body, true)
		case *ast.SwitchStmt:
			walk(x.Init, true)
			walk(x.Tag, true)
			walk(x.Body, true)
		case *ast.TypeSwitchStmt:
			walk(x.Init, true)
			walk(x.Assign, true)
			walk(x.Body, true)
		case *ast.SelectStmt:
			walk(x.Body, true)
		case *ast.CaseClause:
			for _, e := range x.List {
				walk(e, true)
			}
			for _, s := range x.Body {
				walk(s, true)
			}
		case *ast.CommClause:
			walk(x.Comm, true)
			for _, s := range x.Body {
				walk(s, true)
			}
		case *ast.LabeledStmt:
			walk(x.Stmt, inCond)
		case *ast.FuncLit:
			// t.Run/子 goroutine 里的闭包同样要扫：跳过写在闭包里也是"这条
			// 测试可能不跑"，不能因为多了一层函数字面量就看不见。
			walk(x.Body, inCond)
		case *ast.GoStmt:
			walk(x.Call, inCond)
		case *ast.DeferStmt:
			walk(x.Call, inCond)
		case *ast.ExprStmt:
			walk(x.X, inCond)
		case *ast.AssignStmt:
			for _, e := range x.Lhs {
				walk(e, inCond)
			}
			for _, e := range x.Rhs {
				walk(e, inCond)
			}
		case *ast.ReturnStmt:
			for _, e := range x.Results {
				walk(e, inCond)
			}
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "Skip", "SkipNow", "Skipf":
					has = true
					if !inCond {
						guarded = false
					}
					// SkipNow 不接受理由参数：它本身不是"没写理由"，用一个
					// 固定文案占位，避免被 缺理由 规则误判。
					if sel.Sel.Name == "SkipNow" {
						reason = "t.SkipNow（该 API 不接受理由）"
					}
					if len(x.Args) > 0 {
						if lit, ok := x.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if v, uerr := strconv.Unquote(lit.Value); uerr == nil && strings.TrimSpace(v) != "" {
								reason = strings.TrimSpace(v)
							}
						}
					}
				}
			}
			for _, a := range x.Args {
				walk(a, inCond)
			}
		}
	}
	walk(body, false)
	return has, reason, has && guarded
}

// ParseExecJSON 读 go test -json 的事件流，只保留带 Test 的 pass/fail/skip。
// run/output 事件没有"这条测试最后怎么了"的信息，丢弃。
func ParseExecJSON(r io.Reader) ([]Record, error) {
	var out []Record
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var ev struct {
			Action  string
			Package string
			Test    string
			Time    string
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		switch ev.Action {
		case "pass", "fail", "skip":
		default:
			continue
		}
		if ev.Test == "" {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, ev.Time)
		out = append(out, Record{Package: ev.Package, Test: ev.Test, Action: ev.Action, At: at})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// LoadRecords 读 .forge/test-exec.jsonl。文件不存在返回空账本（不是错误：
// 第一次用账本本来就是空的，全仓判 orphaned 正是"还没有证据"的如实表达）。
// 损坏行被跳过并计数，不让一行坏数据吞掉整本账。
func LoadRecords(p string) ([]Record, int, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	var out []Record
	bad := 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil || rec.Test == "" {
			bad++
			continue
		}
		out = append(out, rec)
	}
	return out, bad, nil
}

// AppendRecords 追加记录到 JSONL 文件（父目录自动创建）。
func AppendRecords(p string, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

// LoadSkipApprovals 读 spec/test-skips.yaml。文件不存在返回空表（不是错误——
// 没有任何审批时，所有 Skip 都算未审批，这正是应该被看见的状态）。
func LoadSkipApprovals(p string) (map[string]SkipApproval, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]SkipApproval{}, nil
		}
		return nil, err
	}
	var doc struct {
		Skips []SkipApproval `yaml:"skips"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", p, err)
	}
	out := map[string]SkipApproval{}
	for _, a := range doc.Skips {
		if strings.TrimSpace(a.Test) == "" {
			continue
		}
		out[strings.TrimSpace(a.Test)] = a
	}
	return out, nil
}

// Build 把声明与记录合成账本。
func Build(decls []Decl, recs []Record, opts Options) *Ledger {
	maxAge := opts.MaxAge
	if maxAge <= 0 {
		maxAge = DefaultMaxAge
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	latest := map[string]Record{}
	for _, r := range recs {
		// 子测试（Test="TestFoo/sub"）是父测试的一次运行，不是独立声明：按父名
		// 归并，否则每个 subtest 都会掉进 phantom 循环，账本被自己跑出来的记录
		// 判成"测试已删除"。
		name := r.Test
		if i := strings.Index(name, "/"); i > 0 {
			name = name[:i]
		}
		key := r.Package + "." + name
		if prev, ok := latest[key]; !ok || r.At.After(prev.At) {
			latest[key] = r
		}
	}
	l := &Ledger{Counts: map[State]int{}}
	seen := map[string]bool{}
	for _, d := range decls {
		key := d.Package + "." + d.Test
		seen[key] = true
		e := Entry{Test: d.Test, Package: d.Package, File: d.File, Line: d.Line}
		rec, hasRec := latest[key]
		if hasRec {
			e.LastSeen = rec.At
		}
		e.GuardedSkip = d.HasSkip && d.SkipGuarded
		switch {
		case d.HasSkip && !d.SkipGuarded:
			e.State = StateSkipped
			e.SkipReason = d.SkipReason
			if ap, ok := opts.Approvals[key]; ok && strings.TrimSpace(ap.ApprovedBy) != "" {
				e.SkipApproved = true
			}
		case hasRec && rec.Action != "skip" && now.Sub(rec.At) <= maxAge:
			e.State = StateLive
		default:
			e.State = StateOrphaned
		}
		l.Entries = append(l.Entries, e)
		l.Counts[e.State]++
	}
	// 记录里有、声明里没有 = phantom（测试被删或改名，记录还在）。
	for key, rec := range latest {
		if seen[key] {
			continue
		}
		l.Entries = append(l.Entries, Entry{
			Test: rec.Test, Package: rec.Package, State: StatePhantom, LastSeen: rec.At,
		})
		l.Counts[StatePhantom]++
	}
	sort.Slice(l.Entries, func(i, j int) bool {
		if l.Entries[i].Package != l.Entries[j].Package {
			return l.Entries[i].Package < l.Entries[j].Package
		}
		if l.Entries[i].File != l.Entries[j].File {
			return l.Entries[i].File < l.Entries[j].File
		}
		return l.Entries[i].Line < l.Entries[j].Line
	})
	return l
}

// Violations 返回需要处理的三类问题：Skip 没写理由、Skip 没审批、phantom 记录。
// orphaned 不进违规清单——它是"缺证据"的报告项，不是错误（是否要阻断由调用方
// 用 --strict 之外的策略决定；把它塞进违规会让第一次使用永远失败）。
func (l *Ledger) Violations() []string {
	var out []string
	for _, e := range l.Entries {
		key := e.Package + "." + e.Test
		switch {
		case e.State == StateSkipped && strings.TrimSpace(e.SkipReason) == "":
			out = append(out, key+"：Skip 缺理由——t.Skip 的参数必须写清为什么跳过（空参/变量都不算）")
		case e.State == StateSkipped && !e.SkipApproved:
			out = append(out, key+"：Skip 未审批——理由「"+e.SkipReason+"」需要在 spec/test-skips.yaml 里由人登记 approved_by")
		case e.State == StatePhantom:
			out = append(out, key+"：phantom——执行记录里有、源码里已经没有；测试被删/改名要留痕，别让它从账本里悄悄消失")
		}
	}
	sort.Strings(out)
	return out
}

// modulePath 从 go.mod 读 module 行；读不到返回空串（此时 Package 退化为相对目录，
// 账本仍可用，只是键不是 import path）。
func modulePath(root string) string {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// dirPackage 把 repo 相对目录变成 import path。
func dirPackage(mod, dir string) string {
	dir = strings.TrimPrefix(filepath.ToSlash(dir), "./")
	if dir == "." || dir == "" {
		if mod == "" {
			return "."
		}
		return mod
	}
	if mod == "" {
		return dir
	}
	return mod + "/" + dir
}
