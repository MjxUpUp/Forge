package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestHook_TaskVerify_ChecklogToDataDir 端到端钉死 refactor-data-home commit D 的
// 真断链修复：TaskVerifyHook 必须把 checklog + 节流 stamp 写到用户级 DataDir，
// 不再写项目级 .forge/。迁移前 hook 写 .forge/checklog.jsonl 而 Go checklog.LoadForTask
// 读 DataDir/checklog*.jsonl → task-verify 事件在 forge trace 丢失（真断链）。
//
// 触发：FORGE_SKIP_VERIFY=1 命中 escape-hatch 分支（TaskVerifyHook 必写一条 escape-hatch
// checklog 记录），重定向目标改造为 $_DATA_DIR/checklog.jsonl。forge data-dir 子命令
// （PATH 含 forgeBin）解析 DataDir；freshProject 设 FORGE_DATA_HOME 隔离。mux 断言：
// DataDir 必须与 <dir>/.forge 分叉，否则测试无意义。
func TestHook_TaskVerify_ChecklogToDataDir(t *testing.T) {
	dir := freshProjectOnBranch(t, "feature/dd-checklog")
	t.Setenv("FORGE_SKIP_VERIFY", "1") // 命中 escape-hatch 分支，必写 checklog

	stdout, stderr, err := forgeHook(t, dir, "task-verify", hookStdin(t, "sess-dd", "PostToolUse", "Edit", map[string]any{
		"file_path": "main.go",
	}))
	_ = stdout
	// TaskVerifyHook 永远 PASS（advisory，不拦）；非 nil err 才是 dispatch 失败。
	if err != nil {
		t.Fatalf("forge hook task-verify: %v\n%s", err, stderr)
	}

	dataDir := forgedata.DataDirFor(dir)
	if dataDir == filepath.Join(dir, ".forge") {
		t.Fatalf("DataDir fell back to <dir>/.forge; git project must resolve user-level — test is moot")
	}

	// escape-hatch checklog 必须落 DataDir（commit D 真断链修复核心）。
	checklog, err := os.ReadFile(filepath.Join(dataDir, "checklog.jsonl"))
	if err != nil {
		t.Fatalf("checklog not written to DataDir/checklog.jsonl: %v — TaskVerifyHook must use forge data-dir", err)
	}
	if !strings.Contains(string(checklog), `"check":"escape-hatch"`) {
		t.Errorf("DataDir/checklog.jsonl missing escape-hatch entry:\n%s", checklog)
	}

	// 节流 stamp 也必须落 DataDir（_STAMP 改造）。
	if _, err := os.Stat(filepath.Join(dataDir, ".task-verify-throttle.last")); err != nil {
		t.Errorf("throttle stamp not written to DataDir/.task-verify-throttle.last: %v", err)
	}

	// legacy .forge/checklog.jsonl 必须不存在（断链修复：hook 不再写项目级）。
	if _, err := os.Stat(filepath.Join(dir, ".forge", "checklog.jsonl")); err == nil {
		t.Error("legacy .forge/checklog.jsonl must NOT be written — TaskVerifyHook must use DataDir, not project .forge/")
	}
}
