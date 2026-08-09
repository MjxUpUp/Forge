package taskpipeline

import (
	"errors"
	"testing"
)

// offeredTask builds a TaskState delegated (offered) to agent — the common starting point for
// claim/deliver state-transition tests.
//
// offeredTask 构造一个已派发（offered）给 agent 的 TaskState——claim/deliver 状态转换测试的常用起点。
func offeredTask(agent string) *TaskState {
	s := &TaskState{TaskRef: `feat/x`}
	if err := s.AssignTo(agent, `frontend`, `claude-code`); err != nil {
		panic(`offeredTask: AssignTo failed: ` + err.Error())
	}
	return s
}

func TestAssignTo(t *testing.T) {
	t.Run(`creates offered assignment`, func(t *testing.T) {
		s := &TaskState{}
		if err := s.AssignTo(`kimi`, `frontend`, `claude-code`); err != nil {
			t.Fatalf(`AssignTo 应成功，错误: %v`, err)
		}
		a := s.Assignment
		if a == nil || a.Agent != `kimi` || a.Role != `frontend` || a.Status != AssignOffered || a.OfferedBy != `claude-code` {
			t.Fatalf(`assignment 字段未正确填充: %+v`, a)
		}
		if a.OfferedAt == nil || a.OfferedAt.IsZero() {
			t.Fatal(`OfferedAt 应为非 nil 指针且时间非零`)
		}
		if !s.HasAssignment() {
			t.Fatal(`HasAssignment 应为 true`)
		}
	})
	t.Run(`rejects empty agent`, func(t *testing.T) {
		s := &TaskState{}
		if !errors.Is(s.AssignTo(``, `frontend`, `claude-code`), errAssignmentEmptyAgent) {
			t.Fatal(`空 agent 应返回 errAssignmentEmptyAgent`)
		}
	})
	t.Run(`rejects duplicate`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		if !errors.Is(s.AssignTo(`reasonix`, `backend`, `claude-code`), errAssignmentExists) {
			t.Fatal(`已有 assignment 应返回 errAssignmentExists（改派走 reassign 路径）`)
		}
	})
}

func TestClaim(t *testing.T) {
	t.Run(`offered matching agent -> claimed`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		if err := s.Claim(`kimi`); err != nil {
			t.Fatalf(`Claim 应成功: %v`, err)
		}
		if s.Assignment.Status != AssignClaimed {
			t.Fatal(`状态应为 claimed`)
		}
		if s.Assignment.ClaimedAt == nil {
			t.Fatal(`ClaimedAt 应被设置`)
		}
	})
	t.Run(`wrong agent rejected`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		if !errors.Is(s.Claim(`reasonix`), errClaimWrongAgent) {
			t.Fatal(`不匹配的 agent 认领应返回 errClaimWrongAgent`)
		}
	})
	t.Run(`non-offered rejected`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		if !errors.Is(s.Claim(`kimi`), errClaimNotOffered) {
			t.Fatal(`已 claimed 再 claim 应返回 errClaimNotOffered`)
		}
	})
	t.Run(`no assignment rejected`, func(t *testing.T) {
		s := &TaskState{}
		if !errors.Is(s.Claim(`kimi`), errNoAssignment) {
			t.Fatal(`无 assignment 应返回 errNoAssignment`)
		}
	})
}

func TestDeliver(t *testing.T) {
	t.Run(`claimed -> delivered`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		if err := s.Deliver(); err != nil {
			t.Fatalf(`Deliver 应成功: %v`, err)
		}
		if s.Assignment.Status != AssignDelivered || s.Assignment.DeliveredAt == nil {
			t.Fatal(`状态应为 delivered 且 DeliveredAt 设置`)
		}
	})
	t.Run(`offered->delivered skip rejected`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		if !errors.Is(s.Deliver(), errDeliverNotClaimed) {
			t.Fatal(`offered 直接 deliver（跳过 claim）应返回 errDeliverNotClaimed`)
		}
	})
}

func TestQuestionAnswer(t *testing.T) {
	t.Run(`claim->question->answer records decision`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		if err := s.Question(`API 契约不明`); err != nil {
			t.Fatalf(`Question 应成功: %v`, err)
		}
		if s.Assignment.Status != AssignInputRequired || s.Assignment.LastQuestion != `API 契约不明` {
			t.Fatal(`状态应为 input-required 且 LastQuestion 记录`)
		}
		before := len(s.Decisions)
		if err := s.Answer(`用 REST，字段见 openapi.yaml`); err != nil {
			t.Fatalf(`Answer 应成功: %v`, err)
		}
		if s.Assignment.Status != AssignClaimed {
			t.Fatal(`Answer 后应回 claimed`)
		}
		if len(s.Decisions) != before+1 {
			t.Fatal(`Answer 应追加一条 Decision 使决议可追溯`)
		}
	})
	t.Run(`answer on non-input-required rejected`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		if !errors.Is(s.Answer(`x`), errAnswerNotInputReq) {
			t.Fatal(`非 input-required 的答复应返回 errAnswerNotInputReq`)
		}
	})
	t.Run(`empty answer still resumes without decision`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		_ = s.Question(`q`)
		before := len(s.Decisions)
		if err := s.Answer(``); err != nil {
			t.Fatalf(`空答复应成功（仅恢复 claimed，不记决策）: %v`, err)
		}
		if s.Assignment.Status != AssignClaimed {
			t.Fatal(`空答复后也应回 claimed`)
		}
		if len(s.Decisions) != before {
			t.Fatal(`空答复不应追加 Decision`)
		}
	})
}

func TestFail(t *testing.T) {
	s := offeredTask(`kimi`)
	_ = s.Claim(`kimi`)
	if err := s.Fail(`编译不过`); err != nil {
		t.Fatalf(`Fail 应成功: %v`, err)
	}
	if s.Assignment.Status != AssignFailed || s.Assignment.FailReason != `编译不过` {
		t.Fatal(`状态应为 failed 且 FailReason 记录`)
	}
}

func TestCancel(t *testing.T) {
	for _, st := range []string{AssignOffered, AssignClaimed, AssignInputRequired} {
		s := offeredTask(`kimi`)
		if st == AssignClaimed || st == AssignInputRequired {
			_ = s.Claim(`kimi`)
		}
		if st == AssignInputRequired {
			_ = s.Question(`q`)
		}
		if err := s.Cancel(`需求变了`); err != nil {
			t.Fatalf(`从 %s cancel 应成功: %v`, st, err)
		}
		if s.Assignment.Status != AssignCanceled {
			t.Fatalf(`从 %s cancel 后状态应为 canceled`, st)
		}
	}
	// 终态（delivered/failed/canceled）不能 cancel。
	s := offeredTask(`kimi`)
	_ = s.Claim(`kimi`)
	_ = s.Deliver()
	if !errors.Is(s.Cancel(`x`), errCancelTerminal) {
		t.Fatal(`终态（delivered）cancel 应返回 errCancelTerminal`)
	}
}

func TestReopen(t *testing.T) {
	s := offeredTask(`kimi`)
	_ = s.Claim(`kimi`)
	_ = s.Deliver()
	if err := s.Reopen(`联调发现 bug`); err != nil {
		t.Fatalf(`Reopen 应成功: %v`, err)
	}
	if s.Assignment.Status != AssignClaimed || s.Assignment.DeliveredAt != nil {
		t.Fatal(`Reopen 后应回 claimed 且 DeliveredAt 清空`)
	}
	if s.Assignment.FailReason != `联调发现 bug` {
		t.Fatal(`Reopen 原因应记入 FailReason`)
	}
}

func TestAbandon(t *testing.T) {
	s := offeredTask(`kimi`)
	_ = s.Claim(`kimi`)
	if err := s.Abandon(); err != nil {
		t.Fatalf(`Abandon 应成功: %v`, err)
	}
	if s.Assignment.Status != AssignOffered {
		t.Fatal(`Abandon 后应回 offered`)
	}
	if s.Assignment.ClaimedAt != nil {
		t.Fatal(`Abandon 应清 ClaimedAt`)
	}
	if s.Assignment.AbandonedCount != 1 || s.Assignment.AbandonedAt == nil {
		t.Fatal(`AbandonedCount 应++ 且 AbandonedAt 设置`)
	}
}

func TestIsOfferedTo(t *testing.T) {
	s := &TaskState{}
	if s.IsOfferedTo(`kimi`) {
		t.Fatal(`无 assignment 时 IsOfferedTo 应 false`)
	}
	s = offeredTask(`kimi`)
	if !s.IsOfferedTo(`kimi`) {
		t.Fatal(`offered 给 kimi 时 IsOfferedTo(kimi) 应 true`)
	}
	if s.IsOfferedTo(`reasonix`) {
		t.Fatal(`offered 给 kimi 时 IsOfferedTo(reasonix) 应 false`)
	}
	_ = s.Claim(`kimi`)
	if s.IsOfferedTo(`kimi`) {
		t.Fatal(`claimed 后不再是 offered，IsOfferedTo 应 false`)
	}
}
