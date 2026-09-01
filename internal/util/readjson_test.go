package util

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadJSONFile pins the P3-6 helper contract: missing → bare
// fs.ErrNotExist (errors.Is-branchable), success decodes, parse failure wraps
// with the path.
//
// TestReadJSONFile 钉住 P3-6 助手契约：缺失 → 裸 fs.ErrNotExist（可被
// errors.Is 分支）、成功解码、解析失败带路径包装。
func TestReadJSONFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	if err := os.WriteFile(p, []byte(`{"name":"x"}`), 0644); err != nil {
		t.Fatal(err)
	}

	var v struct {
		Name string `json:"name"`
	}
	if err := ReadJSONFile(p, &v); err != nil {
		t.Fatalf("valid file: %v", err)
	}
	if v.Name != "x" {
		t.Fatalf("decoded name = %q, want %q", v.Name, "x")
	}

	// 缺失：errors.Is(err, fs.ErrNotExist) 可分支（哨兵/nil/continue 语义的公共判据）。
	err := ReadJSONFile(filepath.Join(dir, "absent.json"), &v)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file err = %v, want fs.ErrNotExist chain", err)
	}

	// 解析失败：带路径包装、非 ErrNotExist。
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{oops"), 0644); err != nil {
		t.Fatal(err)
	}
	err = ReadJSONFile(bad, &v)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("corrupt file err = %v, want non-ErrNotExist parse error", err)
	}
	if !strings.Contains(err.Error(), bad) {
		t.Errorf("parse error 应带路径上下文, got: %v", err)
	}

	// 读失败（目录当文件）：带路径包装、非 ErrNotExist。
	err = ReadJSONFile(dir, &v)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read-dir err = %v, want non-ErrNotExist read error", err)
	}
}
