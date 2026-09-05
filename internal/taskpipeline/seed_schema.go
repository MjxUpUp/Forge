package taskpipeline

// seed_schema.go — TaskState 的序列化 schema 种子（compat 面 5 消费，经 cli
// 注入到 compat.SeedTaskStateForSchema 接缝）。全填充形态让 schemaKeys 能取到
// 每个可选字段（omitempty 字段零值时不出现在序列化面——种子必须填满，对抗
// 审查 should-fix：未填的键恰是承诺表"序列化键只增不删"的强承诺面，删改
// 它们不会触发 golden 棘轮）。

import (
	"time"

	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// SeedTaskStateForSchema 返回全填充 TaskState（schema 键提取用，值无意义）。
//
// SeedTaskStateForSchema returns a fully-populated TaskState for schema key extraction.
func SeedTaskStateForSchema() any {
	now := time.Now()
	s := &tasktypes.TaskState{
		TaskRef: "seed/ref", Summary: "s", Branch: "b", HeadCommit: "abc",
		StartedAt: now, OriginTool: "seed", Goal: "g", Plan: "p",
		Checklist: []tasktypes.ChecklistItem{{ID: 1, Desc: "d", Done: true, DoneAt: &now}},
		PlanScope: []string{"x.go"},
		Acceptance: []tasktypes.AcceptanceCriterion{{
			Run: "r", Expected: "e", Passed: true, Output: "o",
			AcceptedHeadCommit: "abc", AcceptedBaseCommit: "abc", AcceptedChangeHash: "h",
		}},
	}
	s.CompletedAt = &now
	s.SessionID = "sess"
	s.History = []tasktypes.TaskGateResult{{Gate: "g", Passed: true, HeadCommit: "abc"}}
	s.ReviewPassed = true
	s.ReviewRounds = []tasktypes.ReviewRound{{HeadCommit: "abc", ChangeHash: "h", ReviewedAt: now}}
	s.DesignPhases = []tasktypes.DesignPhase{"frontend"}
	s.IntentLog = []tasktypes.IntentEntry{{TS: now, Text: "t", Session: "sess"}}
	s.Findings = []tasktypes.Finding{{ID: "f", Content: "c", Source: "s", Evidence: "e", Severity: "minor", Status: "open"}}
	s.ReportedFindings = []string{"fp"}
	s.DocReview = &tasktypes.DocReview{
		Passed: true, RubricScore: 88, Round: 1, Reviewer: "r", ReviewedAt: now,
		HeadCommit: "abc", DocsFingerprint: "fp",
	}
	s.DocReviewHistory = []tasktypes.DocReview{*s.DocReview}
	s.Integrity = &tasktypes.StateIntegrity{KeyID: "k", Alg: "a", Sig: "s"}
	s.Overrides = tasktypes.TaskOverrides{
		WorkActivity: "disable", TestCoverage: "disable", AcceptanceGate: "disable",
		SkillDecisions: "disable", DocGate: "disable",
	}
	s.ExternalOrigin = tasktypes.ExternalOrigin{Tracker: "github", IssueID: "1", Identifier: "org/repo#1", URL: "u"}
	s.Assignment = &tasktypes.Assignment{
		Agent: "claude-code", Role: "r", Status: "offered",
		OfferedBy: "x", OfferedAt: &now, ClaimedAt: &now, QuestionAt: &now,
		DeliveredAt: &now, LastQuestion: "q", FailReason: "f", CancelReason: "c",
		NotifiedAt: &now, AbandonedCount: 1, AbandonedAt: &now, AutoDelivered: true,
	}
	s.Lease = &tasktypes.Lease{HolderNode: "n", TsHLC: "t", TTLSec: 60, Fencing: 1, ClaimedAt: 1}
	return s
}
