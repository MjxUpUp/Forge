package clitask

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// setupChainTask 建 session 绑定的活跃任务（回环/编排类测试的公共夹具）。
//
// core-only：声明式产物链（schema.yaml）已删，schema 参数保留为空壳——调用方仍传
// 字符串，但不再写任何 artifact-chain 文件；测试只关心活跃任务本身。
func setupChainTask(t *testing.T, schema string) (string, string) {
	t.Helper()
	_ = schema
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	const sid = "test-session-chain"
	t.Setenv("CLAUDE_CODE_SESSION_ID", sid)
	const taskRef = "feat/chain-cli"
	if err := taskpipeline.SetActiveTaskRef(dir, sid, taskRef); err != nil {
		t.Fatal(err)
	}
	if err := taskpipeline.SaveTaskState(dir, &taskpipeline.TaskState{
		TaskRef:   taskRef,
		SessionID: sid,
		Branch:    "feat/chain-cli",
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	return dir, taskRef
}
