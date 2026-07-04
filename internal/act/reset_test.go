package act

import (
	"os"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

// TestResetForRebuild_NoFile 验证旧项目本就没有 conclusions.jsonl 的合法情形：
// 不报错、返空备份路径（rebuild 命令据此决定是否打印"已备份"）。
func TestResetForRebuild_NoFile(t *testing.T) {
	p := forgedatatest.ForDataDir(t.TempDir())
	backup, err := ResetForRebuild(p)
	if err != nil {
		t.Fatalf("ResetForRebuild no-file: %v", err)
	}
	if backup != "" {
		t.Fatalf("backup path not empty when no file: %q", backup)
	}
}

// TestResetForRebuild_BackupClear 验证核心契约：有现有文件时备份到 .bak 且原位清空
// （os.Rename 既备份又腾位，Append 随后在原位重建）。备份内容须等于原内容。
func TestResetForRebuild_BackupClear(t *testing.T) {
	p := forgedatatest.ForDataDir(t.TempDir())
	orig := p.ActConclusionsPath()
	if err := os.MkdirAll(p.ActDir(), 0755); err != nil {
		t.Fatal(err)
	}
	old := []byte("{\"task\":\"x\"}\n")
	if err := os.WriteFile(orig, old, 0644); err != nil {
		t.Fatal(err)
	}
	backup, err := ResetForRebuild(p)
	if err != nil {
		t.Fatalf("ResetForRebuild: %v", err)
	}
	want := orig + ".bak"
	if backup != want {
		t.Fatalf("backup path = %q, want %q", backup, want)
	}
	if _, err := os.Stat(orig); !os.IsNotExist(err) {
		t.Fatalf("original file still exists after reset: %v", err)
	}
	got, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(got) != string(old) {
		t.Fatalf("backup content = %q, want %q", got, old)
	}
}

// TestResetForRebuild_IdempotentOverwrites 验证幂等安全：再次 reset 时 .bak 被最新内容覆盖
// （rebuild 可重复跑，不会因残留 .bak 报错或留住过时备份）。
func TestResetForRebuild_IdempotentOverwrites(t *testing.T) {
	p := forgedatatest.ForDataDir(t.TempDir())
	orig := p.ActConclusionsPath()
	if err := os.MkdirAll(p.ActDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orig, []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResetForRebuild(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orig, []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	backup, err := ResetForRebuild(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2\n" {
		t.Fatalf("backup should hold latest (v2), got %q", got)
	}
}
