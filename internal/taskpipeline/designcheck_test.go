package taskpipeline

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// writeTree writes files (repo-relative slash paths) under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDesignCheck 钉住 W2.3 方案事实核查：存在的引用不报；不存在的路径、越界行号、
// 找不到的 Go 符号、不在 go.mod 的模块逐类报出；new: 行与代码围栏豁免；命令不当路径。
func TestDesignCheck(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":          "module example.com/m\n\ngo 1.22\n\nrequire (\n\tgithub.com/spf13/cobra v1.8.0\n)\n",
		"internal/a/a.go": "package a\n\nfunc Foo() {}\n\ntype Bar struct{}\n\nfunc (b Bar) Baz() {}\n\nconst (\n\tLimit = 3\n)\n",
	})
	design := strings.Join([]string{
		"## 触点",
		"- `internal/a/a.go:Foo` 改造",
		"- `internal/a/a.go:Bar.Baz`、`internal/a/a.go:Limit`、`internal/a/a.go:3`",
		"- `internal/a/missing.go` 删除",
		"- `internal/a/a.go:999` 附近",
		"- `internal/a/a.go:Nope` 新逻辑",
		"- new: `internal/b/new.go`",
		"## 依赖",
		"沿用 `github.com/spf13/cobra` 与 `example.com/m/internal/a`，引入 `github.com/fake/pkg`。",
		"验证：`go test ./...`",
		"```go",
		"// `internal/zzz/ghost.go`",
		"```",
	}, "\n")

	got := CheckDesignGrounding(root, design)
	kinds := map[string]string{}
	for _, f := range got {
		kinds[f.Ref] = f.Kind
	}
	want := map[string]string{
		"internal/a/missing.go": "path",
		"internal/a/a.go:999":   "line",
		"internal/a/a.go:Nope":  "symbol",
		"github.com/fake/pkg":   "module",
	}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("findings = %v, want %v", kinds, want)
	}

	t.Run("无 go.mod 时不做模块核查", func(t *testing.T) {
		bare := t.TempDir()
		if got := CheckDesignGrounding(bare, "引入 `github.com/fake/pkg`"); len(got) != 0 {
			t.Fatalf("非 Go 仓库不应报模块: %+v", got)
		}
	})

	t.Run("落 design-check 行", func(t *testing.T) {
		RecordDesignCheck(root, "feat/dc", got)
		entries, _ := checklog.LoadForTask(root, "feat/dc")
		var e *checklog.Entry
		for i := range entries {
			if entries[i].Check == CheckNameDesignCheck {
				e = &entries[i]
			}
		}
		if e == nil || e.Passed || e.Meta["findings"] != "4" || e.Meta["kind.symbol"] != "1" {
			t.Fatalf("design-check 行不符: %+v", e)
		}
	})
}

// TestTouchpointsPlanScope 钉住 W2.4：触点一节解析成 PlanScope；手工 scope 优先；
// 来自方案的 scope 可随方案重登记刷新；节外与围栏内的路径不算触点。
func TestTouchpointsPlanScope(t *testing.T) {
	design := strings.Join([]string{
		"## 背景",
		"- `internal/outside/x.go` 不是触点",
		"## 设计",
		"### 触点",
		"- `internal/a/a.go:Foo` 改造",
		"- new: `internal/b/new.go`",
		"| `internal/c/c.go` | 表格行 |",
		"- internal/d/d.go:12 裸写",
		"### 接口",
		"- `internal/after/y.go`",
	}, "\n")
	want := []string{"internal/a/a.go", "internal/b/new.go", "internal/c/c.go", "internal/d/d.go"}
	if got := ParseTouchpoints(design); !reflect.DeepEqual(got, want) {
		t.Fatalf("touchpoints = %v, want %v", got, want)
	}

	t.Run("空 scope 时写入并标来源", func(t *testing.T) {
		st := &TaskState{}
		if !ApplyDesignTouchpoints(st, design) {
			t.Fatal("应写入 PlanScope")
		}
		if !reflect.DeepEqual(st.PlanScope, want) || st.PlanScopeSource != PlanScopeSourceDesign {
			t.Fatalf("got %v / %q", st.PlanScope, st.PlanScopeSource)
		}
	})
	t.Run("手工 scope 优先", func(t *testing.T) {
		st := &TaskState{PlanScope: []string{"internal"}}
		if ApplyDesignTouchpoints(st, design) || !reflect.DeepEqual(st.PlanScope, []string{"internal"}) {
			t.Fatalf("手工 scope 不应被覆盖: %v", st.PlanScope)
		}
	})
	t.Run("来自方案的 scope 随重登记刷新", func(t *testing.T) {
		st := &TaskState{PlanScope: []string{"old/x.go"}, PlanScopeSource: PlanScopeSourceDesign}
		if !ApplyDesignTouchpoints(st, design) || !reflect.DeepEqual(st.PlanScope, want) {
			t.Fatalf("应刷新: %v", st.PlanScope)
		}
	})
	t.Run("无触点节不改", func(t *testing.T) {
		st := &TaskState{}
		if ApplyDesignTouchpoints(st, "## TL;DR\n采用 A\n") || len(st.PlanScope) != 0 {
			t.Fatal("无触点节不应写 scope")
		}
	})
	t.Run("scope-drift 把方案外改动标出来", func(t *testing.T) {
		t.Setenv("FORGE_DATA_HOME", t.TempDir())
		root := t.TempDir()
		st := &TaskState{TaskRef: "feat/tp", PlanScope: want, PlanScopeSource: PlanScopeSourceDesign}
		if err := checkVerifyScopeDrift(root, st, []string{"internal/a/a.go", "internal/zz/rogue.go"}); err != nil {
			t.Fatal(err)
		}
		entries, _ := checklog.LoadForTask(root, st.TaskRef)
		var detail string
		for _, e := range entries {
			if e.Check == checklog.CheckScopeDrift {
				detail = e.Detail
			}
		}
		if !strings.HasPrefix(detail, "方案外改动") || !strings.Contains(detail, "internal/zz/rogue.go") {
			t.Fatalf("drift detail = %q", detail)
		}
	})
}

// TestDesignCheckBoundaries 钉住审查轮修复：方案是半可信输入——越出仓库根的引用
// 不读、不探测（不泄露外部文件的存在与行数）；缩进的调用不算声明；Type.Method 按
// 接收者核对。
func TestDesignCheckBoundaries(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	writeTree(t, parent, map[string]string{
		"secret.txt":       "a\nb\nc\nd\n",
		"repo/x/x.go":      "package x\n\ntype A struct{}\ntype B struct{}\n\nfunc (a *A) Run() {}\n\nfunc use() {\n\tHelper(1)\n}\n",
		"repo/x/README.md": "readme\n",
	})
	got := CheckDesignGrounding(root, strings.Join([]string{
		"- `../secret.txt:99`",
		"- `../secret.txt`",
		"- `x/x.go:Helper`",
		"- `x/x.go:A.Run`",
		"- `x/x.go:B.Run`",
	}, "\n"))
	byRef := map[string]DesignCheckFinding{}
	for _, f := range got {
		byRef[f.Ref] = f
	}
	for _, ref := range []string{"../secret.txt:99", "../secret.txt"} {
		f, ok := byRef[ref]
		if !ok || !strings.Contains(f.Detail, "越出仓库根目录") {
			t.Errorf("%s 应报越界且不核查, got %+v", ref, f)
		}
		if strings.Contains(f.Detail, "文件共") {
			t.Errorf("越界引用不得泄露外部文件行数: %q", f.Detail)
		}
	}
	if _, ok := byRef["x/x.go:Helper"]; !ok {
		t.Error("只被调用、未声明的符号应报 symbol")
	}
	if _, ok := byRef["x/x.go:A.Run"]; ok {
		t.Error("A.Run 已声明，不应报")
	}
	if _, ok := byRef["x/x.go:B.Run"]; !ok {
		t.Error("B 没有 Run 方法，应按接收者报 symbol")
	}
}

// TestDesignCheckInterfaceMethods 钉住复审轮修复：接口方法（形如调用）在 interface
// 块内算声明，按接口名核对；块外的同形调用仍不算。
func TestDesignCheckInterfaceMethods(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"s/s.go": "package s\n\ntype Store interface {\n\tGet(key string) (string, error)\n\tClose()\n}\n\nfunc use(st Store) {\n\tPut(1)\n}\n",
	})
	got := CheckDesignGrounding(root, "- `s/s.go:Store.Get`\n- `s/s.go:Close`\n- `s/s.go:Other.Get`\n- `s/s.go:Put`\n")
	refs := map[string]bool{}
	for _, f := range got {
		refs[f.Ref] = true
	}
	if refs["s/s.go:Store.Get"] || refs["s/s.go:Close"] {
		t.Errorf("接口方法应算声明: %+v", got)
	}
	if !refs["s/s.go:Other.Get"] || !refs["s/s.go:Put"] {
		t.Errorf("错接口名与块外调用应报: %+v", got)
	}
}
