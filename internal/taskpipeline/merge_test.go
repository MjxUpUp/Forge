package taskpipeline

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// merge_test.go — guards for the shared merge semantics (task import --merge and
// project import TaskUnion). Chinese strings use raw literals (Windows quote rule).
//
// merge_test.go —— 共享合并语义（task import --merge 与 project import 的
// TaskUnion）守卫。中文字符串用 raw 字面量（Windows 引号规则）。

// TestUnionDecisions_EmptyIDNotCollapsed: a malformed bundle whose decisions carry
// empty IDs must NOT have them collapsed into one by the ID-keyed union (silent data
// loss). Empty-ID entries are appended as-is; non-empty duplicates are still deduped.
//
// TestUnionDecisions_EmptyIDNotCollapsed：决策带空 ID 的畸形 bundle 不能被按 ID
// 的并集压成一条（静默丢数据）。空 ID 条目原样追加；非空重复仍去重。
func TestUnionDecisions_EmptyIDNotCollapsed(t *testing.T) {
	local := []Decision{{ID: `d-1`, Content: `local`}}
	incoming := []Decision{
		{ID: ``, Content: `malformed-A`},
		{ID: ``, Content: `malformed-B`},
		{ID: `d-1`, Content: `dup-of-local`},
	}
	got := UnionDecisions(local, incoming)
	if len(got) != 3 {
		t.Fatalf(`空 ID 不应折叠：期望 3 条（local + 2 空 ID），got %d: %+v`, len(got), got)
	}
	contents := map[string]bool{}
	for _, d := range got {
		contents[d.Content] = true
	}
	if !contents[`malformed-A`] || !contents[`malformed-B`] || !contents[`local`] {
		t.Errorf(`空 ID 条目应都保留 + local 保留, got %v`, contents)
	}
	if contents[`dup-of-local`] {
		t.Error(`非空重复 ID（d-1）应去重掉`)
	}
}

// TestGhostForeignSessions_OnlyGhosts: ghosting marks links Imported and touches
// NOTHING else — result fields, control-flow fields, and assignment survive intact.
// This is the contract the lineage-trusted project import relies on (preserve
// results, ghost sessions).
//
// TestGhostForeignSessions_OnlyGhosts：幽灵化只标 Imported，别的什么都不动——
// 结果字段、控制流字段、分派都原样保留。这是 lineage 受信的 project import
// 依赖的契约（保留结果、幽灵化 session）。
func TestGhostForeignSessions_OnlyGhosts(t *testing.T) {
	now := time.Now()
	s := &TaskState{
		TaskRef:      `feat/ghost`,
		CompletedAt:  &now,
		ReviewPassed: true,
		Score:        &scoringtypes.ScoreResult{Grade: `A`},
		Kind:         `generic`,
		Assignment:   &Assignment{Agent: `claude`, Status: `offered`},
		SessionLinks: []SessionLink{{SessionID: `sid-1`, Tool: `claude-code`}, {SessionID: `sid-2`, Tool: `codex`}},
		Acceptance:   []AcceptanceCriterion{{Run: `go test ./...`, Passed: true}},
	}
	GhostForeignSessions(s)
	for i, l := range s.SessionLinks {
		if !l.Imported {
			t.Errorf(`SessionLinks[%d] 应幽灵化（Imported=true）`, i)
		}
		if l.SessionID == `` {
			t.Errorf(`SessionLinks[%d] 内容不应被改（仅标 Imported）`, i)
		}
	}
	if s.CompletedAt == nil || !s.ReviewPassed || s.Score == nil || s.Kind != `generic` || s.Assignment == nil {
		t.Error(`幽灵化不得触碰结果/控制流/分派字段（仅 session 链接）`)
	}
	if len(s.Acceptance) != 1 || !s.Acceptance[0].Passed {
		t.Error(`验收条目不应被幽灵化改动`)
	}
}

// TestMergeTaskState_IdentityAuthoritative: MergeTaskState never overwrites local
// identity/definition fields; collaborative sets union; idempotent on re-merge.
//
// TestMergeTaskState_IdentityAuthoritative：MergeTaskState 绝不覆盖本地身份/定义
// 字段；协作集合并集；重复合并幂等。
func TestMergeTaskState_IdentityAuthoritative(t *testing.T) {
	local := &TaskState{
		TaskRef:   `feat/m`,
		Goal:      `local goal`,
		Kind:      `code`,
		Decisions: []Decision{{ID: `d-1`, Content: `local`}},
		NextSteps: []string{`step-local`},
	}
	incoming := &TaskState{
		TaskRef:      `feat/m`,
		Goal:         `REMOTE GOAL MUST NOT WIN`,
		Kind:         `generic`,
		Decisions:    []Decision{{ID: `d-2`, Content: `remote`}, {ID: `d-1`, Content: `dup`}},
		NextSteps:    []string{`step-remote`, `step-local`},
		SessionLinks: []SessionLink{{SessionID: `sid-r`, Tool: `codex`, Imported: true}},
	}
	MergeTaskState(local, incoming)
	if local.Goal != `local goal` || local.Kind != `code` {
		t.Errorf(`本地身份字段被覆盖：Goal=%q Kind=%q`, local.Goal, local.Kind)
	}
	if len(local.Decisions) != 2 {
		t.Errorf(`Decisions 应按 ID 并集为 2 条，got %+v`, local.Decisions)
	}
	if len(local.NextSteps) != 2 {
		t.Errorf(`NextSteps 应并集为 2 条，got %v`, local.NextSteps)
	}
	if !local.HasAnySession(`sid-r`) {
		t.Error(`传入幽灵链接应并入`)
	}
	// 幂等：重复合并不翻倍
	snapshot := len(local.Decisions)
	MergeTaskState(local, incoming)
	if len(local.Decisions) != snapshot {
		t.Errorf(`重复合并应幂等：%d → %d`, snapshot, len(local.Decisions))
	}
}

// gateResult builds one History entry.
//
// gateResult 造一条 History 条目。
func gateResult(gate string, passed bool) TaskGateResult {
	return TaskGateResult{Gate: gate, Passed: passed, HeadCommit: `abc1234`}
}

// TestMergeTaskStateSync_FailedGateHealedByPassed pins the convergence rule the plain
// local-authoritative union cannot provide: the peer machine re-ran a gate we FAILED
// and it PASSED — executor's prerequisite walk only counts Passed entries, so keeping
// the local Failed would deadlock the task on this machine forever. Prefer-Passed
// takes the incoming Passed entry; CurrentGate is re-derived from the healed history.
//
// TestMergeTaskStateSync_FailedGateHealedByPassed 钉死纯本地权威并集给不了的收敛
// 规则：对端机器重跑了我们 FAILED 且 PASSED 的门禁——executor 前置链只认 Passed
// 条目，保留本地 Failed 会让任务在本机永久卡死。prefer-Passed 取传入的 Passed
// 条目；CurrentGate 从愈合后的 history 重推。
func TestMergeTaskStateSync_FailedGateHealedByPassed(t *testing.T) {
	local := &TaskState{
		TaskRef:   `feat/sync-heal`,
		History:   []TaskGateResult{gateResult(`task-implement`, true), gateResult(`task-verify`, false)},
		Decisions: []Decision{{ID: `d-local`, Content: `kept`}},
	}
	incoming := &TaskState{
		TaskRef:   `feat/sync-heal`,
		History:   []TaskGateResult{gateResult(`task-implement`, true), gateResult(`task-verify`, true)},
		Decisions: []Decision{{ID: `d-remote`, Content: `remote`}},
	}
	MergeTaskStateSync(local, incoming)
	verify := ``
	passedVerify := false
	for _, h := range local.History {
		if h.Gate == `task-verify` {
			verify = h.Gate
			passedVerify = h.Passed
		}
	}
	if verify == `` || !passedVerify {
		t.Errorf(`对端已修复的 task-verify 应取 Passed（本机 Failed 不得永久卡死任务），history=%+v`, local.History)
	}
	if local.CurrentGate != `task-complete` {
		t.Errorf(`愈合后 CurrentGate 应推进到 task-complete，got %q（history=%+v）`, local.CurrentGate, local.History)
	}
	// 协作记录照常并集，本地未丢
	if len(local.Decisions) != 2 {
		t.Errorf(`Decisions 应并集为 2，got %+v`, local.Decisions)
	}
}

// TestMergeTaskStateSync_CompletionMonotonic: an incoming COMPLETED snapshot of the
// same task completes the local incomplete copy (whole completion block adopted —
// CompletedAt/ReviewPassed/Score/Assignment), while a local completion is never
// downgraded by an incoming incomplete snapshot. Both directions converge; re-merge
// is a no-op (idempotent).
//
// TestMergeTaskStateSync_CompletionMonotonic：同任务的传入「已完成」快照使本地未完
// 成副本完成（整块采纳完成字段——CompletedAt/ReviewPassed/Score/Assignment）；
// 本地已完成绝不被传入未完成快照降级。双向收敛；重复合并是 no-op（幂等）。
func TestMergeTaskStateSync_CompletionMonotonic(t *testing.T) {
	done := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	score := &scoringtypes.ScoreResult{Grade: `A`}
	assignment := &Assignment{Agent: `claude`, Status: `delivered`}

	// 方向 1：本地未完成 + 传入已完成 → 采纳完成块
	local := &TaskState{
		TaskRef: `feat/sync-done`,
		History: []TaskGateResult{gateResult(`task-implement`, true)},
	}
	incoming := &TaskState{
		TaskRef:      `feat/sync-done`,
		CompletedAt:  &done,
		ReviewPassed: true,
		Score:        score,
		Assignment:   assignment,
		History:      []TaskGateResult{gateResult(`task-implement`, true), gateResult(`task-verify`, true), gateResult(`task-complete`, true)},
	}
	MergeTaskStateSync(local, incoming)
	if local.CompletedAt == nil || !local.CompletedAt.Equal(done) {
		t.Errorf(`传入已完成应被采纳，got %v`, local.CompletedAt)
	}
	if !local.ReviewPassed || local.Score != score || local.Assignment != assignment {
		t.Error(`完成块（ReviewPassed/Score/Assignment）应随完成一并采纳`)
	}
	if local.CurrentGate != `` {
		t.Errorf(`已完成任务 CurrentGate 应为空（无下一门禁），got %q`, local.CurrentGate)
	}

	// 方向 2：本地已完成 + 传入未完成 → 不降级
	local2 := &TaskState{
		TaskRef:     `feat/sync-done2`,
		CompletedAt: &done,
		Score:       score,
		History:     []TaskGateResult{gateResult(`task-complete`, true)},
	}
	incoming2 := &TaskState{
		TaskRef: `feat/sync-done2`,
		History: []TaskGateResult{gateResult(`task-implement`, true)},
	}
	MergeTaskStateSync(local2, incoming2)
	if local2.CompletedAt == nil {
		t.Error(`本地已完成不得被未完成快照降级`)
	}

	// 幂等：对已采纳状态重复合并，结果不变
	before, _ := json.Marshal(local)
	MergeTaskStateSync(local, incoming)
	after, _ := json.Marshal(local)
	if string(before) != string(after) {
		t.Errorf(`重复合并应幂等：\nbefore=%s\nafter=%s`, before, after)
	}
}
