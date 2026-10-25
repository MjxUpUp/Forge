package doctor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeExecFile 写文件并指定是否带执行位。
func writeExecFile(t *testing.T, path, content string, exec bool) {
	t.Helper()
	mode := os.FileMode(0o644)
	if exec {
		mode = 0o755
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// TestShebangInterpreter 钉死解释器识别：env 形态（含 -S / VAR=val）取其后的第一个名字，
// 直接路径形态返回基名与原路径，原生二进制与无 shebang 文本返回空。
func TestShebangInterpreter(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		content  string
		wantName string
		wantAbs  string
	}{
		{"env-node", "#!/usr/bin/env node\nx\n", "node", ""},
		{"env-S-flag", "#!/usr/bin/env -S node --no-warnings\nx\n", "node", ""},
		{"env-assign", "#!/usr/bin/env NODE_ENV=prod node\nx\n", "node", ""},
		{"direct", "#!/opt/homebrew/bin/node\nx\n", "node", "/opt/homebrew/bin/node"},
		{"native", "\x7fELF\x02\x01", "", ""},
		{"plain-text", "just text\n", "", ""},
		{"empty-shebang", "#!\n", "", ""},
	}
	for _, c := range cases {
		p := filepath.Join(dir, c.name)
		writeExecFile(t, p, c.content, true)
		gotName, gotAbs := shebangInterpreter(p)
		if gotName != c.wantName || gotAbs != c.wantAbs {
			t.Errorf("%s: got (%q,%q), want (%q,%q)", c.name, gotName, gotAbs, c.wantName, c.wantAbs)
		}
	}
	if n, a := shebangInterpreter(filepath.Join(dir, "missing")); n != "" || a != "" {
		t.Errorf("缺失文件应返回空，got (%q,%q)", n, a)
	}
}

// TestResolveInPath 钉死 sh 查找的复现：第一个带执行位的普通文件胜出；缺执行位的项不算命中
// （仅在最终无命中时单独上报）；相对与空 PATH 元素跳过；目录不算命中。
func TestResolveInPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX 执行位语义")
	}
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	writeExecFile(t, filepath.Join(first, "forge"), "x", false) // 缺执行位：不算命中
	writeExecFile(t, filepath.Join(second, "forge"), "x", true)
	if err := os.MkdirAll(filepath.Join(root, "dirhit", "forge"), 0o755); err != nil { // 同名目录不算命中
		t.Fatal(err)
	}

	pathVar := "relative/dir" + string(os.PathListSeparator) + "" + string(os.PathListSeparator) +
		filepath.Join(root, "dirhit") + string(os.PathListSeparator) + first + string(os.PathListSeparator) + second
	found, noExec := resolveInPath("forge", pathVar)
	if found != filepath.Join(second, "forge") {
		t.Errorf("应跳过缺执行位项与目录，命中 second，got %q", found)
	}
	if noExec != "" {
		t.Errorf("后面有可执行命中时 sh 不会报 126，不应上报缺执行位项，got %q", noExec)
	}

	found, noExec = resolveInPath("forge", first)
	if found != "" || noExec != filepath.Join(first, "forge") {
		t.Errorf("只有缺执行位项时应未命中并记下（126），got (%q,%q)", found, noExec)
	}
	if found, noExec = resolveInPath("forge", "relative/only"); found != "" || noExec != "" {
		t.Errorf("相对 PATH 元素应跳过，got (%q,%q)", found, noExec)
	}
}

// TestUnderAny 钉死临时目录判定：目录内含自身，前缀相近的兄弟目录不误判，符号链接
// 解析后的形态同样命中。
func TestUnderAny(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "a", "forge")
	writeExecFile(t, inside, "x", true)
	sibling := root + "-sibling"
	if !underAny(inside, []string{root}) {
		t.Error("目录内的路径应命中")
	}
	if underAny(filepath.Join(sibling, "forge"), []string{root}) {
		t.Error("仅前缀相近的兄弟目录不应命中")
	}
	if underAny(inside, []string{"", filepath.Join(root, "other")}) {
		t.Error("空项与无关目录不应命中")
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(root, link); err != nil {
			t.Fatal(err)
		}
		if !underAny(filepath.Join(link, "a", "forge"), []string{root}) {
			t.Error("符号链接解析后的形态应命中")
		}
	}
}
