package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

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

	// master 上暂存代码变更且无活跃任务 → master-without-task 检查使 MESSAGES
	// 非空（与 TestHook_TaskVerify_ChecklogToDataDir 同一触发）。
	writeFile(t, dir, "extra.go", "package main\n")
	git(t, dir, "add", "extra.go")

	var stdout, stderr strings.Builder

	// escape-hatch-hardening P1(审查项 4):有界阻断段在 kimi 早退**之前**——
	// 活跃任务 + 未提交代码(FORGE_TASK_REF 注入 + extra.go 暂存未提交)时
	// kimi 也被均匀执法:exit 2 + 指引走 stdout(block 面不受 advisory 通道
	// 路由影响)。先钉新契约,再去掉任务注入钉 advisory 的 WARN-stdout 路由
	// (无任务 → 不拦;master + 暂存代码 → master-without-task advisory)。
	runHook := func(withTaskRef bool) error {
		stdout.Reset()
		stderr.Reset()
		c := exec.Command("bash", filepath.Join(forgedata.DataDirFor(dir), "hooks", "task-verify.sh"))
		c.Dir = dir
		env := append(os.Environ(),
			"FORGE_AGENT=kimi",
			"FORGE_SESSION_ID=sess-kimi-tv",
			"PATH="+filepath.Dir(forgeBin)+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
		if withTaskRef {
			env = append(env, "FORGE_TASK_REF=feat/kimi-tv")
		}
		c.Env = env
		c.Stdout = &stdout
		c.Stderr = &stderr
		return c.Run()
	}
	if err := runHook(true); err == nil {
		t.Fatalf("kimi + active task + uncommitted code must bounded-block (uniform enforcement), stdout=%s", stdout.String())
	} else if !strings.Contains(stdout.String(), "有未提交代码变更") {
		t.Fatalf("block guidance must ride stdout, got stdout=%s stderr=%s", stdout.String(), stderr.String())
	}

	_ = os.Remove(filepath.Join(forgedata.DataDirFor(dir), ".task-verify-throttle.last"))
	if err := runHook(false); err != nil {
		t.Fatalf("no-task + staged code + kimi advisory must exit 0: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	// advisory 以 WARN 行上 **stdout**（Go 层剥 WARN 前缀后入队）；stderr 不得
	// 携带——kimi 的 Stop stderr 对模型不可见，那条路正是 100% 丢失通道。
	if !strings.Contains(stdout.String(), "WARN [task-verify]") {
		t.Errorf("kimi branch must print WARN [task-verify] on stdout, got stdout=%q", stdout.String())
	}
	if strings.Contains(stderr.String(), "[task-verify]") {
		t.Errorf("kimi branch must NOT write the advisory to stderr (model-invisible on kimi), got stderr=%q", stderr.String())
	}

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
