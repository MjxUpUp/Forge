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
	// 16 用例集（auto-compile 2 + task-guard 5 + file-sentinel 5 + read-before-edit 2
	// + hazard-guard 2 历史反哺）：recall 7/7、fpr 0/9、全部确定性重放一致。
	if !strings.Contains(out, "recall 7/7") || !strings.Contains(out, "fpr 0/9") || !strings.Contains(out, "rbe-blocks-unread-edit") {
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

// TestEvalTrapsE2E 钉住三陷阱的 capture 现状（两洞闭环后 3/3；任何一门检测被
// 删除，本测试或 trap FINDING 立刻转红——对抗审查 I1：此前只有手工首跑无回归钉）。
func TestEvalTrapsE2E(t *testing.T) {
	out, _, code := runForge(t, repoRoot, "eval", "traps", "run")
	if code != 0 {
		t.Fatalf("traps run 应通过（exit %d）：%s", code, out)
	}
	if !strings.Contains(out, "capture 3/3") {
		t.Fatalf("三陷阱应全部识破: %s", out)
	}
	if strings.Contains(out, "FINDING") {
		t.Fatalf("闭环后不应有 FINDING: %s", out)
	}
}

// TestEvalAuditVerifyE2E 钉住验签读侧：真实仓库历史行为 legacy/valid、零伪造、
// 零重放（重放检测的回归钉——见 countReplayedStamps）。
func TestEvalAuditVerifyE2E(t *testing.T) {
	out, _, code := runForge(t, repoRoot, "eval", "audit-verify")
	if code != 0 {
		t.Fatalf("audit-verify 应通过（exit %d）：%s", code, out)
	}
	if !strings.Contains(out, "审计行验签") {
		t.Fatalf("输出缺验签统计: %s", out)
	}
}

// TestEvalResumeDrillE2E 钉住接续演练全套通过（含内联 git 身份修复——
// 对抗审查 I1：干净机器上 git 身份缺失曾致 2/3 FAIL 而完成度声明仍为 3 条）。
func TestEvalResumeDrillE2E(t *testing.T) {
	out, _, code := runForge(t, repoRoot, "eval", "resume-drill")
	if code != 0 {
		t.Fatalf("resume-drill 应通过（exit %d）：%s", code, out)
	}
	if !strings.Contains(out, "3/3 passed") {
		t.Fatalf("三条演练应全过: %s", out)
	}
}
