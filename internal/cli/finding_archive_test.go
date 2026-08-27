package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestTaskFinding_ArchivesTriggeringFinding pins the dogfood fix (2026-08-27): the
// attempt archive snapshot must be taken INSIDE the mutation — the triggering finding
// itself must appear in its own round archive, not just pre-existing ones.
//
// TestTaskFinding_ArchivesTriggeringFinding 钉住 dogfood 修复（2026-08-27）：归档快照
// 必须取自 mutation 内——触发归档的这条 finding 自己必须出现在它引发的轮次归档里，
// 而非只有先前已存在的。
func TestTaskFinding_ArchivesTriggeringFinding(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	tmpDir := t.TempDir()
	runGit(t, tmpDir, "init", "-b", "main")
	runGit(t, tmpDir, "config", "user.email", "t@t.t")
	runGit(t, tmpDir, "config", "user.name", "t")
	if stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n"), 0644)
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "init")
	if stdout, _, code := runForge(t, tmpDir, "task", "start", "--ref", "feat/arch-probe", "--title", "arch", "--kind", "generic"); code != 0 {
		t.Fatalf("task start: %s", stdout)
	}

	if stdout, _, code := runForge(t, tmpDir, "task", "finding", "--content", "触发归档的第一条", "--source", "zcode"); code != 0 {
		t.Fatalf("task finding: %s", stdout)
	}
	if stdout, _, code := runForge(t, tmpDir, "task", "finding", "--content", "同轮第二条也应并入", "--source", "zcode"); code != 0 {
		t.Fatalf("task finding 2: %s", stdout)
	}

	// 同轮归档一次写入：首条触发建归档，快照须含首条自身（修复点）。
	arch := filepath.Join(forgedata.DataDirFor(tmpDir), "specs", "feat-arch-probe", "attempts", "round-001", "findings.md")
	data, err := os.ReadFile(arch)
	if err != nil {
		t.Fatalf("读取归档: %v", err)
	}
	if !strings.Contains(string(data), "触发归档的第一条") {
		t.Fatalf("dogfood 修复回归：触发归档的 finding 漏掉自己的归档:\n%s", data)
	}
}
