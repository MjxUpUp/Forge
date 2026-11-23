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
func ScanDecls(root string) ([]Decl, error) {
	mod := modulePath(root)
	var decls []Decl
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
		fs2 := token.NewFileSet()
		f, perr := parser.ParseFile(fs2, p, nil, 0)
		if perr != nil {
			return nil // 单个文件解析失败不废整次扫描；账本少一行比整本读不出来好
		}
		pkg := dirPackage(mod, path.Dir(rel))
		for _, dec := range f.Decls {
			fn, ok := dec.(*ast.FuncDecl)
			if !ok || fn.Name == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			hasSkip, reason, guarded := skipInfo(fn.Body)
			decls = append(decls, Decl{
				Test:        fn.Name.Name,
				Package:     pkg,
				File:        rel,
				Line:        fs2.Position(fn.Pos()).Line,
				HasSkip:     hasSkip,
				SkipReason:  reason,
				SkipGuarded: guarded,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(decls, func(i, j int) bool {
		if decls[i].Package != decls[j].Package {
			return decls[i].Package < decls[j].Package
		}
		return decls[i].Line < decls[j].Line
	})
	return decls, nil
}

// skipInfo 判断函数体里有几个 t.Skip* 调用：有没有、第一参数是不是非空字符串
// 字面量，以及它们是否【全都】写在 if 条件里。
//
// “在 if 里”这一维是账本能说实话的关键：绝大多数 Skip 是环境闸门
// （if runtime.GOOS != "windows"、if testing.Short()、if _, err := exec.LookPath("git"); err != nil），
// 这些测试平时照常跑，不该被算成“被静默关掉的测试”。真正的静默删除是函数体里
// 无条件的 t.Skip——那种才需要理由与审批。
func skipInfo(body *ast.BlockStmt) (has bool, reason string, guarded bool) {
	if body == nil {
		return false, "", false
	}
	guarded = true
	var walk func(n ast.Node, inIf bool)
	walk = func(n ast.Node, inIf bool) {
		switch x := n.(type) {
		case nil:
			return
		case *ast.BlockStmt:
			for _, s := range x.List {
				walk(s, inIf)
			}
		case *ast.IfStmt:
			walk(x.Init, true)
			walk(x.Cond, true)
			walk(x.Body, true)
			if x.Else != nil {
				walk(x.Else, true)
			}
		case *ast.ForStmt:
			walk(x.Body, inIf)
		case *ast.RangeStmt:
			walk(x.Body, inIf)
		case *ast.SwitchStmt:
			walk(x.Body, inIf)
		case *ast.TypeSwitchStmt:
			walk(x.Body, inIf)
		case *ast.SelectStmt:
			walk(x.Body, inIf)
		case *ast.CaseClause:
			for _, s := range x.Body {
				walk(s, inIf)
			}
		case *ast.CommClause:
			for _, s := range x.Body {
				walk(s, inIf)
			}
		case *ast.LabeledStmt:
			walk(x.Stmt, inIf)
		case *ast.GoStmt:
			walk(x.Call, inIf)
		case *ast.DeferStmt:
			walk(x.Call, inIf)
		case *ast.ExprStmt:
			walk(x.X, inIf)
		case *ast.AssignStmt:
			for _, e := range x.Lhs {
				walk(e, inIf)
			}
			for _, e := range x.Rhs {
				walk(e, inIf)
			}
		case *ast.ReturnStmt:
			for _, e := range x.Results {
				walk(e, inIf)
			}
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "Skip", "SkipNow", "Skipf":
					has = true
					if !inIf {
						guarded = false
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
				walk(a, inIf)
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
		key := r.Package + "." + r.Test
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
