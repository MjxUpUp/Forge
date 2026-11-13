package artifactchain

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDigest_StableAndSensitive 钉住链指纹的三条契约（docs/design/task-tree-guard.md §6）：
// 同内容（仅行尾不同）→ 同指纹；内容一变→ 变；无 schema → 空串（缺失绝不读成漂移）。
func TestDigest_StableAndSensitive(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", filepath.Join(root, "data"))

	if got := Digest(root); got != "" {
		t.Fatalf("无 schema 必须返回空串，got %q", got)
	}

	p := SchemaPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("stages:\r\n  - design\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	crlf := Digest(root)
	if crlf == "" {
		t.Fatal("有 schema 时指纹不得为空")
	}
	if err := os.WriteFile(p, []byte("stages:\n  - design\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if lf := Digest(root); lf != crlf {
		t.Fatalf("仅行尾不同必须同指纹：CRLF=%s LF=%s", crlf, lf)
	}
	if err := os.WriteFile(p, []byte("stages:\n  - design\n  - verify\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed := Digest(root); changed == crlf {
		t.Fatal("内容变更必须改变指纹")
	}
}
