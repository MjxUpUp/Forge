package cli

// eval_cli_test.go — `forge eval` 命令族的端到端验证：在仓库根（评测资产所在）
// 经预构建二进制跑真实命令表面。目标表面 = CLI 输出 + 退出码（非内部函数）。

import (
	"path/filepath"
	"strings"
	"testing"
)

// 仓库根复用既有常量 repoRoot（docs_consistency_test.go，值 "../.."）——评测资产
// 按约定提交在 <root>/evals/forge/，card/dashboard 的资产解析以 cwd 为基。

func TestEvalCardE2E(t *testing.T) {
	out, _, code := runForge(t, repoRoot, "eval", "card")
	if code != 0 {
		t.Fatalf("eval card 应通过（exit %d）：%s", code, out)
	}
	if !strings.Contains(out, "gates-card") {
		t.Fatalf("输出缺校验行: %s", out)
	}
	out, _, code = runForge(t, repoRoot, "eval", "card", "--render")
	if code != 0 {
		t.Fatalf("eval card --render 应通过（exit %d）：%s", code, out)
	}
	for _, want := range []string{"已知盲区", "占层声明", "逃生舱"} {
		if !strings.Contains(out, want) {
			t.Fatalf("渲染缺节 %q", want)
		}
	}
}

func TestEvalDashboardDryRunE2E(t *testing.T) {
	out, _, code := runForge(t, repoRoot, "eval", "dashboard", "--dry-run")
	if code != 0 {
		t.Fatalf("dashboard --dry-run 应通过（exit %d）：%s", code, out)
	}
	if !strings.Contains(out, "C1-C7 roster 完整") {
		t.Fatalf("输出缺 roster 断言: %s", out)
	}
}

func TestEvalGoldenRunE2E(t *testing.T) {
	// 真实二进制重放 golden 种子集：fixture 里跑真实 go build（需要工具链，
	// CI/开发环境均具备）。报告落隔离 HOME 的 evals（TestMain 隔离）。
	out, _, code := runForge(t, repoRoot, "eval", "golden", "run")
	if code != 0 {
		t.Fatalf("golden run 应通过（exit %d）：%s", code, out)
	}
	// 12 用例集（auto-compile 2 + task-guard 5 + file-sentinel 5）：
	// precision 5/5、fpr 0/7、全部确定性重放一致。
	if !strings.Contains(out, "precision 5/5") || !strings.Contains(out, "fpr 0/7") || !strings.Contains(out, "taskguard-blocks-forge-runtime-write") {
		t.Fatalf("输出缺 precision 基线/用例行: %s", out)
	}
	// 指纹一致性：二次运行不得因 manifest 拒绝。
	_, _, code = runForge(t, repoRoot, "eval", "golden", "run")
	if code != 0 {
		t.Fatalf("二次 golden run 应通过（指纹一致）: %d", code)
	}
}

func TestEvalRunScriptedE2E(t *testing.T) {
	manifest, err := filepath.Abs(filepath.Join(repoRoot, "evals", "forge", "manifests", "smoke-v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	out, _, code := runForge(t, repoRoot, "eval", "run",
		"--manifest", manifest, "--profile", "full", "--model", "smoke-model",
		"--repeats", "2", "--forge-ref", "test-ref")
	if code != 0 {
		t.Fatalf("eval run 应通过（exit %d）：%s", code, out)
	}
	for _, want := range []string{"SCORECARD | profile=full model=smoke-model benchmark=smoke-v1@frozen forge_ref=test-ref sandbox=scripted", "组合评测", "pass^k"} {
		if !strings.Contains(out, want) {
			t.Fatalf("scorecard 缺 %q：%s", want, out)
		}
	}
	// 脚本 runner 按哈希确定性判 pass（忽略命令）——3 任务 2 过，pass@1 应为
	// 0.667 且跨运行稳定（确定性替身的契约：同输入同分数）。
	if !strings.Contains(out, "pass@1 0.667") {
		t.Fatalf("scripted 冒烟 pass@1 应为 0.667：%s", out)
	}
}

func TestEvalReportE2E(t *testing.T) {
	out, _, code := runForge(t, repoRoot, "eval", "report")
	if code != 0 {
		t.Fatalf("eval report 应通过（exit %d）：%s", code, out)
	}
	if !strings.Contains(out, "缺失证据") {
		t.Fatalf("报告应含缺失证据节: %s", out)
	}
}
