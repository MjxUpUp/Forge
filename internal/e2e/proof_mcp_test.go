package e2e

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// proof-of-work 闭环 e2e：验证本次 PR 两个新持久化点（ExternalOrigin + AcceptedHeadCommit）跨 forge
// 子进程端到端落盘——forge task start --from-issue 子进程写 ExternalOrigin，forge task verify-acceptance
// 子进程写实跑快照 AcceptedHeadCommit，e2e 进程 LoadTaskState 读回断言。
//
// proof 的 done/drift 逻辑（v2 快路径 + review drift）在 mcpserver/tools_proof_git_test.go 单测覆盖
// （需真 git + SourceChangesSince + 直接调 taskProofCore）；complete/评分在 mcpserver/tools_complete_test.go
// + taskpipeline/scoring 单测覆盖。本文件聚焦"新字段跨子进程持久化"这一 e2e 独有价值——证明 spawn 式
// 编排器经 forge CLI 起的 task，ExternalOrigin/验收快照真落盘可读回。

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
