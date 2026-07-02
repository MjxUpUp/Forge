package taskpipeline

import (
	"reflect"
	"testing"
)

// TestMatchesScope 钉住覆盖语义：空 scope 全覆盖；精确/前缀递归/glob 三种命中；测试随源码免查。
func TestMatchesScope(t *testing.T) {
	cases := []struct {
		name  string
		file  string
		scope []string
		want  bool
	}{
		{`空 scope 全覆盖`, `internal/cli/task.go`, nil, true},
		{`精确匹配`, `internal/cli/task.go`, []string{`internal/cli/task.go`}, true},
		{`精确不匹配`, `internal/cli/task.go`, []string{`internal/cli/hook.go`}, false},
		{`目录前缀递归（无尾斜杠）`, `internal/cli/sub/x.go`, []string{`internal/cli`}, true},
		{`目录前缀递归（尾斜杠）`, `internal/cli/task.go`, []string{`internal/cli/`}, true},
		{`前缀不误匹配同名片段`, `internal/cli2/x.go`, []string{`internal/cli`}, false},
		{`glob 直接子文件`, `internal/cli/task.go`, []string{`internal/cli/*.go`}, true},
		// path.Match 的 * 不跨 /：internal/cli/*.go 不该匹配多层级子目录文件。
		{`glob 不跨层级`, `internal/cli/sub/x.go`, []string{`internal/cli/*.go`}, false},
		{`通配根级 ext`, `task.go`, []string{`*.go`}, true},
		{`通配根级不跨层级`, `cli/task.go`, []string{`*.go`}, false},
		{`多 entry 任一命中`, `internal/cli/task.go`, []string{`internal/act/*`, `internal/cli/*`}, true},
		// 测试文件随声明源码免查（a.go 声明 → a_test.go 覆盖）。
		{`Go 测试随源码覆盖`, `internal/cli/task_test.go`, []string{`internal/cli/task.go`}, true},
		{`TS 测试随源码覆盖`, `src/app.test.ts`, []string{`src/app.ts`}, true},
		{`Python 测试随源码覆盖`, `test_app.py`, []string{`app.py`}, true},
		{`测试文件源码不在 scope`, `internal/cli/hook_test.go`, []string{`internal/cli/task.go`}, false},
		// 生成/类型白名单恒覆盖（testcoverage 单一真相源复用）。
		{`生成文件恒覆盖`, `api/client.gen.go`, []string{`internal/cli/*`}, true},
		{`main.go 恒覆盖`, `main.go`, []string{`internal/cli/*`}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MatchesScope(c.file, c.scope); got != c.want {
				t.Errorf(`MatchesScope(%q, %v) = %v, want %v`, c.file, c.scope, got, c.want)
			}
		})
	}
}

// TestScopeDrift 钉住 drift 计算：仅源码计数；非源码忽略；测试文件被声明的源码覆盖不计数。
func TestScopeDrift(t *testing.T) {
	scope := []string{`internal/cli/task.go`, `internal/cli/scope.go`}
	changed := []string{
		`internal/cli/task.go`,         // 在 scope
		`internal/cli/task_test.go`,    // 测试，源码在 scope → 不 drift
		`internal/cli/scope.go`,        // 在 scope
		`internal/dashboard/server.go`, // 源码，不在 scope → drift
		`README.md`,                    // 非源码 → 忽略
		`internal/cli/embed.go`,        // 白名单（embed.go）→ 覆盖，不 drift
		`cmd/forge/main.go`,            // 白名单（cmd/）→ 覆盖，不 drift
	}
	got := ScopeDrift(changed, scope)
	want := []string{`internal/dashboard/server.go`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf(`ScopeDrift = %v, want %v`, got, want)
	}
}

// TestScopeDrift_EmptyScopeNil 空 scope 永远返回 nil——无声明即无 drift（advisory 前提）。
func TestScopeDrift_EmptyScopeNil(t *testing.T) {
	if got := ScopeDrift([]string{`a.go`, `b.go`}, nil); got != nil {
		t.Errorf(`空 scope 应返回 nil，got %v`, got)
	}
}

// TestScopeDrift_PreservesOrder 多个 drift 文件保持输入顺序（便于稳定展示/比对）。
func TestScopeDrift_PreservesOrder(t *testing.T) {
	changed := []string{`a/x.go`, `b/y.go`, `c/z.go`}
	got := ScopeDrift(changed, []string{`other.go`})
	want := []string{`a/x.go`, `b/y.go`, `c/z.go`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf(`order = %v, want %v`, got, want)
	}
}

// TestSourcePathsForTest 钉住测试→源码反推约定（Go/TS/JS/Python）。
func TestSourcePathsForTest(t *testing.T) {
	cases := []struct {
		test string
		want []string
	}{
		{`internal/cli/task_test.go`, []string{`internal/cli/task.go`}},
		{`src/app.test.ts`, []string{`src/app.ts`}},
		{`src/app.spec.jsx`, []string{`src/app.jsx`}},
		{`test_app.py`, []string{`app.py`}},
		{`foo.go`, nil}, // 非测试文件无标记 → 不反推
	}
	for _, c := range cases {
		got := sourcePathsForTest(c.test)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf(`sourcePathsForTest(%q) = %v, want %v`, c.test, got, c.want)
		}
	}
}
