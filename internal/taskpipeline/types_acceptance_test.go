package taskpipeline

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestHasAcceptance pins TaskState.HasAcceptance: true only when Acceptance is non-empty.
// Both task-verify advisory and the verify-acceptance command depend on it to decide whether acceptance criteria exist —
// mistaking empty for present triggers false advisories; the reverse skips actual runs.
//
// TestHasAcceptance 钉住 TaskState.HasAcceptance：仅当 Acceptance 非空才 true。
// task-verify advisory 和 verify-acceptance 命令都依赖它判断「有无验收标准」——
// 误判空为有会误发 advisory，误判有为空会跳过实跑。
func TestHasAcceptance(t *testing.T) {
	if (&TaskState{}).HasAcceptance() {
		t.Error(`空 Acceptance 应 HasAcceptance=false`)
	}
	state := &TaskState{Acceptance: ParseAcceptance([]string{`go version :: go version`})}
	if !state.HasAcceptance() {
		t.Error(`有验收标准时 HasAcceptance 应 true`)
	}
}

// TestAllAcceptancePassed pins AllAcceptancePassed: true only when all Passed=true; empty also true
// (nothing to verify). task-verify advisory keys off !AllAcceptancePassed() to decide whether to remind —
// this test isolates the predicate (no commands run); executor wiring is covered by executor_acceptance_test.
//
// TestAllAcceptancePassed 钉住 AllAcceptancePassed：全 Passed=true 才 true，空也 true
// （无可回扣项）。task-verify advisory 据 !AllAcceptancePassed() 决定是否提醒——
// 此处隔离判定逻辑（不跑命令），executor 接线由 executor_acceptance_test 覆盖。
func TestAllAcceptancePassed(t *testing.T) {
	if !(&TaskState{}).AllAcceptancePassed() {
		t.Error(`空 Acceptance 应 AllAcceptancePassed=true（无可回扣项）`)
	}
	allPass := &TaskState{Acceptance: []AcceptanceCriterion{
		{Run: `a`, Passed: true},
		{Run: `b`, Passed: true},
	}}
	if !allPass.AllAcceptancePassed() {
		t.Error(`全 Passed=true 应 AllAcceptancePassed=true`)
	}
	oneFail := &TaskState{Acceptance: []AcceptanceCriterion{
		{Run: `a`, Passed: true},
		{Run: `b`, Passed: false},
	}}
	if oneFail.AllAcceptancePassed() {
		t.Error(`存在 Passed=false 应 AllAcceptancePassed=false`)
	}
	noneRun := &TaskState{Acceptance: ParseAcceptance([]string{`go version :: go version`})} // 解析后 Passed 全 false
	if noneRun.AllAcceptancePassed() {
		t.Error(`未实跑（Passed 全 false）应 AllAcceptancePassed=false——advisory 应据此触发`)
	}
}

// TestExternalOrigin_JSONRoundTrip locks the JSON serialization of the ExternalOrigin struct: forge_task_start
// --from_issue parses the URL to fill these four fields; readers like proof/act_query deserialize by tag. Tag drift (camelCase / missing omitempty) breaks the cross-tool contract — pin field names + round-trip must not drop fields.
//
// TestExternalOrigin_JSONRoundTrip 锁定 ExternalOrigin struct 的 JSON 序列化：forge_task_start
// --from_issue 解析 URL 填这四字段，proof/act_query 等读端按 tag 反序列化。tag 漂移（驼峰/
// 漏 omitempty）破坏跨工具协议——钉住字段名 + round-trip 不丢字段。
func TestExternalOrigin_JSONRoundTrip(t *testing.T) {
	state := &TaskState{
		ExternalOrigin: ExternalOrigin{
			Tracker: "linear", IssueID: "abc-123", Identifier: "ABC-123", URL: "https://linear.app/x",
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(data, []byte(`"external_origin"`)) {
		t.Errorf(`JSON 缺 external_origin tag: %s`, data)
	}
	var back TaskState
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.ExternalOrigin != state.ExternalOrigin {
		t.Errorf("round-trip 丢失: got %+v, want %+v", back.ExternalOrigin, state.ExternalOrigin)
	}
}

// TestAcceptanceCriterion_AcceptedHeadCommitTag locks the JSON tag of AcceptedHeadCommit: the proof v2
// fast path reads accepted_head_commit to decide whether Passed is fresh — tag drift makes proof read empty values and falsely mark everything stale, forever falling back to v1 re-runs (correct but slow), or reading the wrong field.
//
// TestAcceptanceCriterion_AcceptedHeadCommitTag 锁定 AcceptedHeadCommit 的 JSON tag：proof v2
// 快路径读 accepted_head_commit 判 Passed 是否 fresh——tag 漂移会让 proof 读到空值误判全部
// stale，永远走 v1 重跑（正确但低效），或读到错字段。
func TestAcceptanceCriterion_AcceptedHeadCommitTag(t *testing.T) {
	c := AcceptanceCriterion{Run: `go test`, AcceptedHeadCommit: "abc1234"}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(data, []byte(`"accepted_head_commit":"abc1234"`)) {
		t.Errorf(`JSON 缺/错 accepted_head_commit tag: %s`, data)
	}
}
