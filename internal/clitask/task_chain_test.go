package clitask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/artifactchain"
	"github.com/MjxUpUp/Forge/internal/forgedata"
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

// TestArtifactCmdSetApproveExtractList 钉住 `forge task artifact` 的动作链：
// set 登记折引用 → approve 签内容哈希 → extract 编译验收 → list 渲染 →
// verify 报漂移。全进程内直调（与 task_acceptance_test 同范式）。
func TestArtifactCmdSetApproveExtractList(t *testing.T) {
	dir, taskRef := setupChainTask(t, "version: 1\nstages:\n  - name: spec\n    mode: human\n")

	// 1. set：登记产物（内容含可提取的验收标准行）。
	src := filepath.Join(dir, "spec-src.md")
	content := "# 规格\n\n- accept: echo chain-cli :: chain-cli\n"
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := runArtifactSet("spec", src, false); err != nil {
			t.Fatalf("set: %v", err)
		}
	})
	if !strings.Contains(out, "产物已登记：spec") {
		t.Errorf("set 输出应确认登记: %q", out)
	}
	state, err := taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatal(err)
	}
	ref, has := state.SpecArtifacts["spec"]
	if !has || ref.Hash == "" {
		t.Fatalf("set 应折哈希引用进 state: %+v", state.SpecArtifacts)
	}

	// 2. approve（human 档）：签当前内容哈希。
	out = captureStdout(t, func() {
		if err := runArtifactApprove("spec", "human-reviewer"); err != nil {
			t.Fatalf("approve: %v", err)
		}
	})
	if !strings.Contains(out, "已审批 spec") {
		t.Errorf("approve 输出应确认: %q", out)
	}

	// 3. extract：产物里的 accept 行编译成验收标准。
	out = captureStdout(t, func() {
		if err := runArtifactExtract(); err != nil {
			t.Fatalf("extract: %v", err)
		}
	})
	if !strings.Contains(out, "提取 1 条") {
		t.Errorf("extract 应提取 1 条: %q", out)
	}
	state, err = taskpipeline.LoadTaskState(dir, taskRef)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range state.Acceptance {
		if c.Run == "echo chain-cli" && c.Expected == "chain-cli" {
			found = true
		}
	}
	if !found {
		t.Errorf("提取的验收标准应入库: %+v", state.Acceptance)
	}

	// 4. list：渲染链表。
	out = captureStdout(t, func() {
		if err := runArtifactList(taskArtifactCmd, false); err != nil {
			t.Fatalf("list: %v", err)
		}
	})
	if !strings.Contains(out, "spec") || !strings.Contains(out, "human") || !strings.Contains(out, "[已审批]") {
		t.Errorf("list 应含 stage/档位/审批态: %q", out)
	}

	// 5. verify：未漂移时通过；手改文件后报漂移。
	out = captureStdout(t, func() {
		if err := runArtifactVerify(); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})
	if !strings.Contains(out, "无漂移") {
		t.Errorf("登记即校验应通过: %q", out)
	}
	artifactPath := filepath.Join(forgedata.DataDirFor(dir), filepath.FromSlash(ref.Path))
	if err := os.WriteFile(artifactPath, []byte(content+"\n手改行\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() {
		if err := runArtifactVerify(); err != nil {
			t.Fatalf("verify(漂移): %v", err)
		}
	})
	if !strings.Contains(out, "1 处漂移") {
		t.Errorf("手改后 verify 应报漂移: %q", out)
	}
}

// TestArtifactApproveRejectsWrongTier 钉住审批的档位边界：advisory 档审批被拒
// （审批只在 human 档有意义），漂移产物拒绝审批（审批的是登记内容）。
func TestArtifactApproveRejectsWrongTier(t *testing.T) {
	t.Run("advisory 档拒绝审批", func(t *testing.T) {
		setupChainTask(t, "") // 默认链全 advisory
		if err := runArtifactApprove("spec", "h"); err == nil || !strings.Contains(err.Error(), "human") {
			t.Fatalf("advisory 档审批应被拒: %v", err)
		}
	})
	t.Run("漂移产物拒绝审批", func(t *testing.T) {
		dir, taskRef := setupChainTask(t, "version: 1\nstages:\n  - name: spec\n    mode: human\n")
		src := filepath.Join(dir, "spec.md")
		if err := os.WriteFile(src, []byte("# v1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := runArtifactSet("spec", src, false); err != nil {
			t.Fatal(err)
		}
		state, err := taskpipeline.LoadTaskState(dir, taskRef)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(forgedata.DataDirFor(dir), filepath.FromSlash(state.SpecArtifacts["spec"].Path))
		if err := os.WriteFile(p, []byte("# v2 手改\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := runArtifactApprove("spec", "h"); err == nil || !strings.Contains(err.Error(), "漂移") {
			t.Fatalf("漂移产物审批应被拒: %v", err)
		}
	})
}

// TestArtifactSetActionExclusivity 钉住动作互斥：零动作或双动作都拒绝。
func TestArtifactSetActionExclusivity(t *testing.T) {
	setupChainTask(t, "")
	if err := runArtifactSet("spec", "", false); err == nil {
		t.Fatal("--file 与 --stdin 双空应拒绝")
	}
	if err := runArtifactSet("spec", "x.md", true); err == nil {
		t.Fatal("--file 与 --stdin 双给应拒绝")
	}
}
