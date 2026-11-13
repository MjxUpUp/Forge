package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// captureOutput swaps os.Stdout/os.Stderr, runs fn, and returns what each captured.
//
// captureOutput 临时替换 os.Stdout/os.Stderr，运行 fn 后返回两者捕获到的内容。
func captureOutput(t *testing.T, fn func() error) (stdout, stderr string, err error) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()

	err = fn()

	wOut.Close()
	wErr.Close()
	outBytes, _ := io.ReadAll(rOut)
	errBytes, _ := io.ReadAll(rErr)
	return string(outBytes), string(errBytes), err
}

// repoRoot 是仓库根（最近的含 go.mod 的祖先）。cli 测试此前的 repoRoot 变量随
// 宿主层测试文件删除而丢失，这里以包级变量补齐（测试以包目录为 cwd 运行）。
var repoRoot = func() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("no go.mod found above " + dir)
		}
		dir = parent
	}
}()

// convWrite writes rel under root (parent dirs auto-made).
func convWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// chdirAndRestore chdirs into dir for the duration of the test.
func chdirAndRestore(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}
