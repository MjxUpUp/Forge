package taskpipeline

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// writeCommitSource 写文件并提交（夹具用）。
func writeCommitSource(t *testing.T, dir string, files map[string]string, msg string) {
	t.Helper()
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", msg)
}

// writeUntracked 只写文件、不 add/commit（工作区未跟踪形态）。
func writeUntracked(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// initRepoWithMaster 建 master（空提交）并切 feat/testcov 分支——
// 镜像 forge task start --branch 的形状。
func initRepoWithMaster(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "commit", "--allow-empty", "-m", "master init")
	runGit(t, dir, "checkout", "-b", "feat/testcov")
}

// newVerifyState 构造「implement 已过、feature 分支」状态，并播一条 Read 让
// work-activity 不先拦。
func newVerifyState(t *testing.T, dir, ref string) *TaskState {
	t.Helper()
	state := &TaskState{TaskRef: ref, Branch: "feat/testcov"}
	state.RecordGateResult("task-implement", true, "")
	rr := toolusage.ToolCall{ToolName: "Read", TaskRef: ref, Timestamp: time.Now().Add(2 * time.Second)}
	if err := toolusage.Record(dir, &rr); err != nil {
		t.Fatalf("seed Read: %v", err)
	}
	return state
}

// captureStderr 临时重定向 os.Stderr 捕获 advisory 输出。
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// setupRecurrentRepo 建仓库并把 FORGE_DATA_HOME 指到仓库内 .forge。
func setupRecurrentRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0o755); err != nil {
		t.Fatalf("mkdir .forge: %v", err)
	}
	t.Setenv("FORGE_DATA_HOME", dir)
	return dir
}
