package clitask

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/artifactchain"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// setupChainTask 建 session 绑定的活跃任务（task_chain CLI 动作的作用对象），
// 可选写入 schema.yaml（human 档审批测试需要）。CLI 动作经 projectroot.Find()
// 解析项目根——t.Chdir 到临时项目 + `.forge/` 标记（legacyFind 兜底路径），
// 让 Find 落在临时目录而非测试进程的真实 cwd。
func setupChainTask(t *testing.T, schema string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	const sid = `test-session-chain`
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, sid)
	const taskRef = `feat/chain-cli`
	if err := taskpipeline.SetActiveTaskRef(dir, sid, taskRef); err != nil {
		t.Fatal(err)
	}
	if err := taskpipeline.SaveTaskState(dir, &taskpipeline.TaskState{
		TaskRef:   taskRef,
		SessionID: sid,
		Branch:    `feat/chain-cli`,
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if schema != "" {
		p := artifactchain.SchemaPath(dir)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(schema), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, taskRef
}
