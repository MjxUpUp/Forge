package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestTaskVerifyHook_KimiBranchRoutesToStdout pins the kimi branch of the
// task-verify Stop script (2026-08-24): on kimi the advisory must go to STDOUT
// (as "WARN [task-verify] ..." — the Go layer's extractDetail picks it up and
// queues it for the UserPromptSubmit drain; kimi's Stop stderr/stdout never
// reach the model directly), exit 0, and the hand-written checklog line must
// carry the real task_ref/session_id context (the dead-record fix) with the
// MESSAGES summary as detail. Runs the reference script copy directly (same
// discipline as TestTaskVerifyHook_SurfacesTestDisciplineAdvisory) with
// FORGE_AGENT=kimi injected — the env the Go dispatcher sets from --agent.
//
// TestTaskVerifyHook_KimiBranchRoutesToStdout 钉住 task-verify Stop 脚本的
// kimi 分支（2026-08-24）：kimi 下 advisory 必须打到 **stdout**（形如
// "WARN [task-verify] ..."——Go 层的 extractDetail 拾取后入队，留待
// UserPromptSubmit 攒发；kimi 的 Stop stderr/stdout 都直达不了模型）、exit 0，
// 且手写 checklog 行必须携带真实 task_ref/session_id 上下文（死记录修复）并以
// MESSAGES 摘要为 detail。直接运行参考副本脚本（与
// TestTaskVerifyHook_SurfacesTestDisciplineAdvisory 同一纪律），注入
// FORGE_AGENT=kimi——即 Go 分发器按 --agent 设置的 env。
func TestTaskVerifyHook_KimiBranchRoutesToStdout(t *testing.T) {
	dir := freshProject(t)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "initial")

	// Staged code change on master with no active task → the master-without-task
	// check fills MESSAGES (same trigger as TestHook_TaskVerify_ChecklogToDataDir).
	//
	// master 上暂存代码变更且无活跃任务 → master-without-task 检查使 MESSAGES
	// 非空（与 TestHook_TaskVerify_ChecklogToDataDir 同一触发）。
	writeFile(t, dir, "extra.go", "package main\n")
	git(t, dir, "add", "extra.go")

	hookPath := filepath.Join(forgedata.DataDirFor(dir), "hooks", "task-verify.sh")
	cmd := exec.Command("bash", hookPath)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"FORGE_AGENT=kimi",
		"FORGE_TASK_REF=feat/kimi-tv",
		"FORGE_SESSION_ID=sess-kimi-tv",
		"PATH="+filepath.Dir(forgeBin)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("task-verify hook must exit 0 on kimi (advisory, never blocks): %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	// The advisory rides STDOUT as a WARN line (the Go layer strips the WARN
	// prefix and queues the rest); stderr must NOT carry it — kimi's Stop
	// stderr is model-invisible, that path was the 100%-loss channel.
	//
	// advisory 以 WARN 行上 **stdout**（Go 层剥 WARN 前缀后入队）；stderr 不得
	// 携带——kimi 的 Stop stderr 对模型不可见，那条路正是 100% 丢失通道。
	if !strings.Contains(stdout.String(), "WARN [task-verify]") {
		t.Errorf("kimi branch must print WARN [task-verify] on stdout, got stdout=%q", stdout.String())
	}
	if strings.Contains(stderr.String(), "[task-verify]") {
		t.Errorf("kimi branch must NOT write the advisory to stderr (model-invisible on kimi), got stderr=%q", stderr.String())
	}

	// The hand-written checklog line carries the injected context verbatim
	// (task_ref/session_id) and a real detail summary instead of the old fixed
	// "advisory: non-blocking issues surfaced to stderr" dead record.
	//
	// 手写 checklog 行原样携带注入的上下文（task_ref/session_id），detail 为
	// 真实摘要而非旧的固定串「advisory: non-blocking issues surfaced to
	// stderr」死记录。
	checklogData, err := os.ReadFile(filepath.Join(forgedata.DataDirFor(dir), "checklog.jsonl"))
	if err != nil {
		t.Fatalf("checklog not written: %v", err)
	}
	content := string(checklogData)
	for _, want := range []string{`"task_ref":"feat/kimi-tv"`, `"session_id":"sess-kimi-tv"`, "Code changes on master"} {
		if !strings.Contains(content, want) {
			t.Errorf("checklog line missing %s:\n%s", want, content)
		}
	}
}
