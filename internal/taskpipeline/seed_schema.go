package taskpipeline

// seed_schema.go — TaskState 的序列化 schema 种子（compat 面 5 消费，经 cli
// 注入到 compat.SeedTaskStateForSchema 接缝）。全填充形态让 schemaKeys 能取到
// 每个可选字段（omitempty 字段零值时不出现在序列化面——种子必须填满）。

import "time"

// SeedTaskStateForSchema 返回全填充 TaskState（schema 键提取用，值无意义）。
//
// SeedTaskStateForSchema returns a fully-populated TaskState for schema key extraction.
func SeedTaskStateForSchema() any {
	now := time.Now()
	s := &TaskState{
		TaskRef: "seed/ref", Summary: "s", Branch: "b", HeadCommit: "abc",
		StartedAt: now, OriginTool: "seed",
		Checklist: []ChecklistItem{{ID: 1, Desc: "d", Done: true, DoneAt: &now}},
		PlanScope: []string{"x.go"}, Goal: "g", Plan: "p",
		Acceptance: []AcceptanceCriterion{{
			Run: "r", Expected: "e", Passed: true, Output: "o",
			AcceptedHeadCommit: "abc", AcceptedBaseCommit: "abc", AcceptedChangeHash: "h",
		}},
	}
	s.Assignment = &Assignment{
		Agent: "claude-code", Role: "r", Status: AssignOffered,
		OfferedBy: "x", OfferedAt: &now, ClaimedAt: &now, QuestionAt: &now,
		DeliveredAt: &now, LastQuestion: "q", FailReason: "f", CancelReason: "c",
		NotifiedAt: &now, AbandonedCount: 1, AbandonedAt: &now, AutoDelivered: true,
	}
	return s
}
