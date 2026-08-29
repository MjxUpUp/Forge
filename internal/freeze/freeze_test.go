package freeze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// testProject 构造 DataDir 隔离在临时 root 下的 Project——无需
// FORGE_DATA_HOME（Project 字段就是纯路径）。
func testProject(t *testing.T) *forgedata.Project {
	t.Helper()
	root := t.TempDir()
	return &forgedata.Project{
		Root:    root,
		DataDir: filepath.Join(root, ".forge-data"),
	}
}

func TestActivateLoadRoundTrip(t *testing.T) {
	p := testProject(t)
	st, err := Activate(p, p.Root, []string{"src"})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if len(st.Paths) != 1 {
		t.Fatalf("Paths = %v, want 1 entry", st.Paths)
	}
	want := Canonical(p.Root, "src")
	if st.Paths[0] != want {
		t.Errorf("stored path = %q, want canonical %q", st.Paths[0], want)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil || len(loaded.Paths) != 1 || loaded.Paths[0] != want {
		t.Errorf("Load = %+v, want paths [%q]", loaded, want)
	}
}

func TestActivateMultiPathAndDedupe(t *testing.T) {
	p := testProject(t)
	st, err := Activate(p, p.Root, []string{"src", "docs/guide", "./src"})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if len(st.Paths) != 2 {
		t.Fatalf("Paths = %v, want 2 entries (duplicate ./src collapsed)", st.Paths)
	}
	// Re-activation replaces the scope.
	st2, err := Activate(p, p.Root, []string{"internal"})
	if err != nil {
		t.Fatalf("re-Activate: %v", err)
	}
	if len(st2.Paths) != 1 || st2.Paths[0] != Canonical(p.Root, "internal") {
		t.Errorf("re-Activate Paths = %v, want [internal]", st2.Paths)
	}
}

func TestActivateRejectsEmpty(t *testing.T) {
	p := testProject(t)
	if _, err := Activate(p, p.Root, nil); err == nil {
		t.Error("Activate(nil) should fail")
	}
	if _, err := Activate(p, p.Root, []string{"  "}); err == nil {
		t.Error("Activate(blank) should fail")
	}
}

func TestDeactivateIdempotent(t *testing.T) {
	p := testProject(t)
	if _, err := Activate(p, p.Root, []string{"src"}); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if err := Deactivate(p); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	st, err := Load(p)
	if err != nil || st != nil {
		t.Errorf("after Deactivate Load = %+v, %v; want nil, nil", st, err)
	}
	// Second --off on no state is a clean no-op.
	if err := Deactivate(p); err != nil {
		t.Errorf("Deactivate again: %v, want nil (idempotent)", err)
	}
}

func TestCheckNoFreezeAllowsEverything(t *testing.T) {
	p := testProject(t)
	allowed, st, err := Check(p, "anywhere/file.go")
	if err != nil || !allowed || st != nil {
		t.Errorf("Check without state = %v, %+v, %v; want true, nil, nil", allowed, st, err)
	}
}

func TestCheckBlocksOutsideFrozenPath(t *testing.T) {
	p := testProject(t)
	if _, err := Activate(p, p.Root, []string{"src"}); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	cases := []struct {
		target string
		want   bool
	}{
		{"src/main.go", true},          // 冻结路径内（相对项目根）
		{"src", true},                  // 冻结路径本身
		{"src/deep/nested/x.go", true}, // 深层嵌套
		{"docs/readme.md", false},      // 冻结路径外
		{"src2/x.go", false},           // 兄弟前缀不得误命中
		{"main.go", false},             // 项目根直挂文件
	}
	for _, c := range cases {
		allowed, _, err := Check(p, c.target)
		if err != nil {
			t.Fatalf("Check(%q): %v", c.target, err)
		}
		if allowed != c.want {
			t.Errorf("Check(%q) allowed = %v, want %v", c.target, allowed, c.want)
		}
	}
}

func TestCheckMultiPathAllowsAny(t *testing.T) {
	p := testProject(t)
	if _, err := Activate(p, p.Root, []string{"src", "docs"}); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	for _, target := range []string{"src/a.go", "docs/b.md"} {
		allowed, _, err := Check(p, target)
		if err != nil || !allowed {
			t.Errorf("Check(%q) = %v, %v; want allowed", target, allowed, err)
		}
	}
	if allowed, _, _ := Check(p, "scripts/c.sh"); allowed {
		t.Error("Check(scripts/c.sh) should be blocked with src+docs frozen")
	}
}

func TestCheckAbsoluteTarget(t *testing.T) {
	p := testProject(t)
	if _, err := Activate(p, p.Root, []string{"src"}); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	inside := filepath.Join(p.Root, "src", "x.go")
	if allowed, _, _ := Check(p, inside); !allowed {
		t.Errorf("Check(abs inside) should allow: %q", inside)
	}
	outside := filepath.Join(p.Root, "other", "x.go")
	if allowed, _, _ := Check(p, outside); allowed {
		t.Errorf("Check(abs outside) should block: %q", outside)
	}
}

func TestCheckCorruptStateFailsOpen(t *testing.T) {
	p := testProject(t)
	if err := os.MkdirAll(p.FreezeDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.FreezeStatePath(), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	allowed, _, err := Check(p, "anywhere/x.go")
	if err == nil {
		t.Error("corrupt state should surface an error")
	}
	if !allowed {
		t.Error("corrupt state must fail open (allowed), never hard-stop every edit")
	}
}

// TestContainsCaseFold 在任意宿主机上钉住 Windows 语义（foldCase=true）：
// 不同大小写的同路径是同一目录；不同前缀仍拒绝。
func TestContainsCaseFold(t *testing.T) {
	sep := string(filepath.Separator)
	cases := []struct {
		frozen, target string
		foldCase       bool
		want           bool
	}{
		{filepath.Join("E:", sep, "Forge", "src"), filepath.Join("e:", sep, "forge", "SRC", "x.go"), true, true},
		{filepath.Join("E:", sep, "Forge", "src"), filepath.Join("e:", sep, "forge", "SRC", "x.go"), false, false},
		{filepath.Join("E:", sep, "Forge", "src"), filepath.Join("E:", sep, "Forge", "src2", "x.go"), true, false},
	}
	for _, c := range cases {
		if got := contains(c.frozen, c.target, c.foldCase); got != c.want {
			t.Errorf("contains(%q, %q, fold=%v) = %v, want %v", c.frozen, c.target, c.foldCase, got, c.want)
		}
	}
}

// TestCanonicalNormalization 钉住相对路径归一化："./x"、"a/../b" 与冗余
// 分隔符全部收敛到同一 canonical 绝对形式。
func TestCanonicalNormalization(t *testing.T) {
	base := t.TempDir()
	a := Canonical(base, "src")
	b := Canonical(base, "./src")
	c := Canonical(base, "docs/../src")
	if a != b || a != c {
		t.Errorf("Canonical divergence: %q vs %q vs %q", a, b, c)
	}
	if !filepath.IsAbs(a) {
		t.Errorf("Canonical should be absolute: %q", a)
	}
	if strings.HasSuffix(a, string(filepath.Separator)) {
		t.Errorf("Canonical should have no trailing separator: %q", a)
	}
}
