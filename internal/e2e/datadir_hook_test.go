package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestHook_TaskVerify_ChecklogToDataDir pins the real broken-link fix from refactor-data-home commit D end-to-end:
// TaskVerifyHook must write checklog + throttle stamp to the user-level DataDir,
// no longer to the project-level .forge/. Before the migration the hook wrote .forge/checklog.jsonl while Go checklog.LoadForTask
// read DataDir/checklog*.jsonl → task-verify events were lost in forge trace (a real broken link).
//
// Trigger: code changes staged on master with no active task — the hook's
// master-without-task check fills MESSAGES, so TaskVerifyHook writes one advisory
// task-verify checklog entry; the redirect target is $_DATA_DIR/checklog.jsonl. The
// forge data-dir subcommand (PATH includes forgeBin) resolves DataDir; freshProject
// sets FORGE_DATA_HOME for isolation. Mux assertion: DataDir must diverge from
// <dir>/.forge, otherwise the test is moot.
//
// TestHook_TaskVerify_ChecklogToDataDir 端到端钉死 refactor-data-home commit D 的
// 真断链修复：TaskVerifyHook 必须把 checklog + 节流 stamp 写到用户级 DataDir，
// 不再写项目级 .forge/。迁移前 hook 写 .forge/checklog.jsonl 而 Go checklog.LoadForTask
// 读 DataDir/checklog*.jsonl → task-verify 事件在 forge trace 丢失（真断链）。
//
// 触发：master 上有已暂存代码变更且无活跃任务——hook 的 master-without-task 检查
// 使 MESSAGES 非空，TaskVerifyHook 必写一条 advisory task-verify checklog 记录，
// 重定向目标为 $_DATA_DIR/checklog.jsonl。forge data-dir 子命令（PATH 含 forgeBin）
// 解析 DataDir；freshProject 设 FORGE_DATA_HOME 隔离。mux 断言：DataDir 必须与
// <dir>/.forge 分叉，否则测试无意义。
func TestHook_TaskVerify_ChecklogToDataDir(t *testing.T) {
	dir := freshProject(t)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "initial")
	git(t, dir, "branch", "-M", "master")

	// Stage a code change on master with no active task → the hook's
	// master-without-task check fills MESSAGES → advisory checklog entry written.
	//
	// master 上暂存一处代码变更且无活跃任务 → hook 的 master-without-task 检查
	// 使 MESSAGES 非空 → 必写 advisory checklog 条目。
	if err := os.WriteFile(filepath.Join(dir, "extra.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write extra.go: %v", err)
	}
	git(t, dir, "add", "extra.go")

	stdout, stderr, err := forgeHook(t, dir, "task-verify", hookStdin(t, "sess-dd", "PostToolUse", "Edit", map[string]any{
		"file_path": "main.go",
	}))
	_ = stdout
	// TaskVerifyHook always PASSes (advisory, does not block); a non-nil err means dispatch failure.
	//
	// TaskVerifyHook 永远 PASS（advisory，不拦）；非 nil err 才是 dispatch 失败。
	if err != nil {
		t.Fatalf("forge hook task-verify: %v\n%s", err, stderr)
	}

	dataDir := forgedata.DataDirFor(dir)
	if dataDir == filepath.Join(dir, ".forge") {
		t.Fatalf("DataDir fell back to <dir>/.forge; git project must resolve user-level — test is moot")
	}

	// The advisory task-verify checklog must land in DataDir (the core of commit D broken-link fix).
	// Staged code change on master without an active task → MESSAGES non-empty → the hook
	// records the advisory entry.
	//
	// advisory task-verify checklog 必须落 DataDir（commit D 真断链修复核心）。
	// master 上有已暂存代码变更且无活跃任务 → MESSAGES 非空 → hook 记下 advisory 条目。
	checklog, err := os.ReadFile(filepath.Join(dataDir, "checklog.jsonl"))
	if err != nil {
		t.Fatalf("checklog not written to DataDir/checklog.jsonl: %v — TaskVerifyHook must use forge data-dir", err)
	}
	if !strings.Contains(string(checklog), `"check":"task-verify"`) {
		t.Errorf("DataDir/checklog.jsonl missing task-verify advisory entry:\n%s", checklog)
	}

	// The throttle stamp must also land in DataDir (the _STAMP change).
	//
	// 节流 stamp 也必须落 DataDir（_STAMP 改造）。
	if _, err := os.Stat(filepath.Join(dataDir, ".task-verify-throttle.last")); err != nil {
		t.Errorf("throttle stamp not written to DataDir/.task-verify-throttle.last: %v", err)
	}

	// The legacy .forge/checklog.jsonl must not exist (broken-link fix: the hook no longer writes project-level).
	//
	// legacy .forge/checklog.jsonl 必须不存在（断链修复：hook 不再写项目级）。
	if _, err := os.Stat(filepath.Join(dir, ".forge", "checklog.jsonl")); err == nil {
		t.Error("legacy .forge/checklog.jsonl must NOT be written — TaskVerifyHook must use DataDir, not project .forge/")
	}
}
