package protocol

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSaveLoadRoundTrip verifies Save persists a loadable protocol.yml: it creates .forge/
// itself (via util.AtomicWrite) and Load reads back an equivalent Protocol.
//
// TestSaveLoadRoundTrip 验证 Save 落盘的 protocol.yml 可加载：自建 .forge/ 目录
// （经 util.AtomicWrite），Load 读回等价 Protocol。
func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := DefaultProtocol()

	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".forge", "protocol.yml")); err != nil {
		t.Fatalf("protocol.yml must exist after Save: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got.Version != want.Version {
		t.Errorf("Version = %q, want %q", got.Version, want.Version)
	}
	if len(got.Standards) != len(want.Standards) {
		t.Fatalf("Standards len = %d, want %d", len(got.Standards), len(want.Standards))
	}
	for i := range want.Standards {
		if got.Standards[i] != want.Standards[i] {
			t.Errorf("Standards[%d] = %+v, want %+v", i, got.Standards[i], want.Standards[i])
		}
	}
	if len(got.SessionRules) != len(want.SessionRules) {
		t.Fatalf("SessionRules len = %d, want %d", len(got.SessionRules), len(want.SessionRules))
	}
	for i := range want.SessionRules {
		if got.SessionRules[i] != want.SessionRules[i] {
			t.Errorf("SessionRules[%d] = %+v, want %+v", i, got.SessionRules[i], want.SessionRules[i])
		}
	}
}

// TestSave_AtomicNoTempLeftover verifies Save leaves no temp files behind in .forge/ —
// AtomicWrite renames its temp file over the target, so a stray .tmp-* file would indicate
// the write path is not atomic (e.g. someone reverted to os.WriteFile + rename by hand).
//
// TestSave_AtomicNoTempLeftover 验证 Save 不在 .forge/ 留临时文件——AtomicWrite 把临时
// 文件 rename 覆盖目标，残留 .tmp-* 说明写路径不再原子（如被改回手写 write+rename）。
func TestSave_AtomicNoTempLeftover(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, DefaultProtocol()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".forge"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "protocol.yml" {
			t.Errorf("unexpected file left in .forge/: %s", e.Name())
		}
	}
}
