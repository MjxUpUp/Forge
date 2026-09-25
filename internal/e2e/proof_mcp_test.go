package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// proof-of-work 闭环 e2e：验证本次 PR 两个新持久化点（ExternalOrigin + AcceptedHeadCommit）跨 forge
// 子进程端到端落盘——forge task start --from-issue 子进程写 ExternalOrigin，forge task verify-acceptance
// 子进程写实跑快照 AcceptedHeadCommit，e2e 进程 LoadTaskState 读回断言。
//
// proof 的 done/drift 逻辑（v2 快路径信任 AcceptedHeadCommit + review drift）下沉在 taskpipeline
// 单测覆盖（JudgeAcceptance/ScoreTask/AppendConclusion）；本文件聚焦「新字段跨子进程持久化」这一 e2e
// 独有价值——证明经 forge CLI 起的 task，ExternalOrigin/验收快照真落盘可读回。（MCP 层已全拆，
// CLI 子进程持久化是 proof 闭环的现有载体。）

// TestE2E_FromIssue_PersistsExternalOrigin: forge task start --from-issue <linear url> → ExternalOrigin is parsed and persisted.
//
// TestE2E_FromIssue_PersistsExternalOrigin：forge task start --from-issue <linear url> → ExternalOrigin
// 解析并落盘。spawn 式编排器从外部 issue 起 task，ExternalOrigin 是衔接锚（解耦 mount 式 agent 自起 task
// 与 spawn 式从 issue 起 task 的 origin）。e2e 验证 CLI --from-issue 路径 + 持久化跨进程可读回。
func TestE2E_FromIssue_PersistsExternalOrigin(t *testing.T) {
	dir := freshProject(t)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "initial")

	forge(t, dir, "task", "start", "--ref", "feat/pow-ext", "--title", "pow ext origin",
		"--from-issue", "https://linear.app/acme/issue/ENG-42", "--branch")

	state, err := taskpipeline.LoadTaskState(dir, "feat/pow-ext")
	if err != nil {
		t.Fatalf("LoadTaskState: %v", err)
	}
	if state.ExternalOrigin.Tracker != "linear" {
		t.Errorf("ExternalOrigin.Tracker = %q, want linear", state.ExternalOrigin.Tracker)
	}
	if !strings.Contains(state.ExternalOrigin.Identifier, "ENG-42") {
		t.Errorf("ExternalOrigin.Identifier = %q，应含 ENG-42（linear issue 段）", state.ExternalOrigin.Identifier)
	}
	if state.ExternalOrigin.URL == "" {
		t.Error("ExternalOrigin.URL 应非空（原始 URL 回填）")
	}
}

// TestE2E_VerifyAcceptance_RecordsAcceptedHeadCommit: forge task verify-acceptance backfills AcceptedHeadCommit (= current HEAD) after actually running acceptance.
//
// TestE2E_VerifyAcceptance_RecordsAcceptedHeadCommit：forge task verify-acceptance 实跑验收后回填
// AcceptedHeadCommit（= 当前 HEAD）。这是 proof v2 快路径的快照源——verify 实跑留 head+passed，
// proof 信任快照不重跑（防重跑 flaky + 快照绑 HEAD 防验收后改码假绿）。e2e 验证快照跨子进程落盘可读回。
func TestE2E_VerifyAcceptance_RecordsAcceptedHeadCommit(t *testing.T) {
	dir := freshProject(t)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "initial")

	forge(t, dir, "task", "start", "--ref", "feat/pow-acc", "--title", "pow acceptance",
		"--accept", "go version :: go version", "--branch")
	forge(t, dir, "task", "verify-acceptance")

	state, err := taskpipeline.LoadTaskState(dir, "feat/pow-acc")
	if err != nil {
		t.Fatalf("LoadTaskState: %v", err)
	}
	if len(state.Acceptance) != 1 {
		t.Fatalf("acceptance 条数 = %d, want 1", len(state.Acceptance))
	}
	c := state.Acceptance[0]
	if !c.Passed {
		t.Error("go version 验收应 Passed=true（exit 0 + 输出含 'go version' 子串）")
	}
	if c.AcceptedHeadCommit == "" {
		t.Error("verify-acceptance 实跑后应记 AcceptedHeadCommit（v2 快路径快照源，防验收后改码假绿）")
	}
}

// TestE2E_TaskComplete_BlockedWhenAcceptanceNotRun pins that acceptance pre-flight is truly wired into task-complete: a task declaring acceptance criteria but never verified must BLOCK.
//
// TestE2E_TaskComplete_BlockedWhenAcceptanceNotRun 钉住 acceptance pre-flight 真接到 task-complete：
// task 声明了验收标准但没跑 forge task verify-acceptance（AcceptedHeadCommit 空）时，forge task complete
// 必须 BLOCKED——这是给 AcceptedHeadCommit 补的消费方（MCP 拆除后该字段只写不读成孤儿）。对应
// Emergence World Proof of Work：声称「验收过」须有 deterministic consumer，否则 complete 击穿。
// 第二段验证逃生舱 FORGE_ACCEPTANCE_GATE=disable 接线（落 checklog 审计后放行，对冲「硬门禁 +
// 全局逃生 = 假硬门禁」——逃生有 checklog 代价 + evidence cap Weak）。
// 成功路径（verify-acceptance 回填后 complete 放行）见 TestE2E_TaskComplete_PassAfterVerifyAcceptance。
func TestE2E_TaskComplete_BlockedWhenAcceptanceNotRun(t *testing.T) {
	dir := freshProject(t)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "initial")

	forge(t, dir, "task", "start", "--ref", "feat/pow-block", "--title", "pow block",
		"--accept", "go version :: go version", "--branch")
	passAllGates(t, dir, "feat/pow-block") // 过 task-implement/task-verify/review-pass/task-complete gate

	// forge task complete 应被 acceptance pre-flight BLOCKED（验收未实跑，AcceptedHeadCommit 空）
	out, err := forgeErr(t, dir, "task", "complete", "--ref", "feat/pow-block")
	if err == nil {
		t.Fatalf(`forge task complete 应被 acceptance pre-flight 拦截（验收未实跑），却成功。output: %s`, out)
	}
	if !strings.Contains(out, "acceptance pre-flight") {
		t.Errorf(`BLOCKED 输出应含 "acceptance pre-flight" 提示，got:`+"\n"+`%s`, out)
	}

	// 逃生舱（env）：FORGE_ACCEPTANCE_GATE=disable 落 checklog 审计后放行——证明逃生舱接线。
	t.Setenv("FORGE_ACCEPTANCE_GATE", "disable") // t.Setenv 自动还原，优于 os.Setenv+defer
	out, err = forgeErr(t, dir, "task", "complete", "--ref", "feat/pow-block")
	if err != nil {
		t.Fatalf(`escape 后 forge task complete 应放行，got err: %v output: %s`, err, out)
	}
}

// TestE2E_TaskComplete_PassAfterVerifyAcceptance pins the happy path: declare acceptance → pass gates → verify-acceptance actually runs backfilling AcceptedHeadCommit==HEAD + Passed → forge task complete allows.
//
// TestE2E_TaskComplete_PassAfterVerifyAcceptance 钉住 happy path：声明验收 → 过门禁 → verify-acceptance
// 实跑回填 AcceptedHeadCommit==HEAD + Passed → forge task complete 放行。proof-of-work 闭环核心主张
// （spec-as-gate：验收标准真跑通过才能 complete），与 BlockedWhenAcceptanceNotRun 对照。
//
// 关键时序：verify-acceptance 必须在 task-complete gate **之前**。task-complete gate 通过会调
// state.MarkComplete()（task.go:717）设 CompletedAt，之后 ActiveTaskState 把 CompletedAt!=nil 的 task
// 当 stale ref fall through（state.go:75），verify-acceptance 找不到 active task。所以本测试手动过
// implement/verify/review gate（不含 task-complete gate），verify-acceptance，再 task-complete gate + complete。
// passAllGates 含 task-complete gate 会早 MarkComplete，不适用此场景。
func TestE2E_TaskComplete_PassAfterVerifyAcceptance(t *testing.T) {
	dir := freshProject(t)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "initial")
	forge(t, dir, "task", "start", "--ref", "feat/pow-ok", "--title", "pow ok",
		"--accept", "go version :: go version", "--branch")

	// 过 implement/verify gate + review pass（不含 task-complete gate——它 MarkComplete 后 verify 找不到 active task）。
	t.Setenv("FORGE_GATE_MIN_INTERVAL", "0s")
	t.Setenv("FORGE_WORK_ACTIVITY", "disable")
	// 真实文件变更 + commit——task-implement 比对内容（git diff
	// HeadCommit..HEAD），空 commit 无法满足。
	if err := os.WriteFile(filepath.Join(dir, "e2e-scratch.txt"), []byte("change for pow-ok\n"), 0644); err != nil {
		t.Fatalf("write scratch file: %v", err)
	}
	git(t, dir, "add", "e2e-scratch.txt")
	git(t, dir, "commit", "-m", "e2e: code change for task-implement")
	for _, g := range []string{"task-implement", "task-verify"} {
		if out, err := forgeErr(t, dir, "task", "gate", g, "--ref", "feat/pow-ok"); err != nil {
			t.Fatalf("forge task gate %s: %v\n%s", g, err, out)
		}
	}
	if out, err := forgeErr(t, dir, "review", "pass"); err != nil {
		t.Fatalf("forge review pass: %v\n%s", err, out)
	}

	// verify-acceptance 在 task-complete gate 前（task 还 active）：回填 AcceptedHeadCommit==HEAD + Passed。
	forge(t, dir, "task", "verify-acceptance")

	// task-complete gate → MarkComplete；complete → acceptance pre-flight fresh（AcceptedHeadCommit==HEAD）放行。
	if out, err := forgeErr(t, dir, "task", "gate", "task-complete", "--ref", "feat/pow-ok"); err != nil {
		t.Fatalf("forge task gate task-complete: %v\n%s", err, out)
	}
	out, err := forgeErr(t, dir, "task", "complete", "--ref", "feat/pow-ok")
	if err != nil {
		t.Fatalf(`verify-acceptance 后 forge task complete 应放行（acceptance fresh），got err: %v output: %s`, err, out)
	}
}
