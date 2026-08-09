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

// TestAddDependency covers DependsOn append with dedup + cycle detection. The lookup is a plain
// map (no storage), so the cycle DFS runs in-process — this is why AddDependency takes a lookup
// func instead of reaching into the storage layer (testability + no import cycle).
//
// TestAddDependency 覆盖 DependsOn 追加的去重 + 环检测。lookup 是普通 map（无存储），故环 DFS 在进程内
// 跑——这正是 AddDependency 取 lookup func 而非直连存储层的原因（可测 + 无 import 环）。
func TestAddDependency(t *testing.T) {
	t.Run(`accepts + dedups`, func(t *testing.T) {
		a := &TaskState{TaskRef: `A`}
		lookup := func(string) *TaskState { return nil } // 无预存边 → 无环
		if err := a.AddDependency([]string{`B`, `B`, `C`}, lookup); err != nil {
			t.Fatalf(`无环依赖应接受, got %v`, err)
		}
		if len(a.DependsOn) != 2 || a.DependsOn[0] != `B` || a.DependsOn[1] != `C` {
			t.Fatalf(`应去重为 [B C], got %v`, a.DependsOn)
		}
	})
	t.Run(`rejects self-reference`, func(t *testing.T) {
		a := &TaskState{TaskRef: `A`}
		if err := a.AddDependency([]string{`A`}, nil); err == nil {
			t.Fatal(`自引用应被拒绝（A 不能依赖 A）`)
		}
		if len(a.DependsOn) != 0 {
			t.Fatalf(`拒绝后 DependsOn 不应变, got %v`, a.DependsOn)
		}
	})
	t.Run(`rejects direct cycle`, func(t *testing.T) {
		// 预存 B→A 边：A 试图依赖 B 会闭合环 A→B→A
		a := &TaskState{TaskRef: `A`}
		b := &TaskState{TaskRef: `B`, DependsOn: []string{`A`}}
		states := map[string]*TaskState{`A`: a, `B`: b}
		lookup := func(ref string) *TaskState { return states[ref] }
		if err := a.AddDependency([]string{`B`}, lookup); err == nil {
			t.Fatal(`引入环 A→B（B 已依赖 A）应被拒绝`)
		}
		if len(a.DependsOn) != 0 {
			t.Fatalf(`拒绝环后 DependsOn 不应变, got %v`, a.DependsOn)
		}
	})
	t.Run(`rejects transitive cycle`, func(t *testing.T) {
		// C→B→A：A 依赖 C 闭合 A→C→B→A
		a := &TaskState{TaskRef: `A`}
		b := &TaskState{TaskRef: `B`, DependsOn: []string{`A`}}
		c := &TaskState{TaskRef: `C`, DependsOn: []string{`B`}}
		states := map[string]*TaskState{`A`: a, `B`: b, `C`: c}
		lookup := func(ref string) *TaskState { return states[ref] }
		if err := a.AddDependency([]string{`C`}, lookup); err == nil {
			t.Fatal(`传递环 A→C→B→A 应被拒绝`)
		}
	})
	t.Run(`accepts diamond no cycle`, func(t *testing.T) {
		// A→B, A→C, B→D, C→D（菱形，D 是共享下游，无环）。A 依赖 B 和 C 合法。
		d := &TaskState{TaskRef: `D`}
		b := &TaskState{TaskRef: `B`, DependsOn: []string{`D`}}
		c := &TaskState{TaskRef: `C`, DependsOn: []string{`D`}}
		a := &TaskState{TaskRef: `A`}
		states := map[string]*TaskState{`A`: a, `B`: b, `C`: c, `D`: d}
		lookup := func(ref string) *TaskState { return states[ref] }
		if err := a.AddDependency([]string{`B`, `C`}, lookup); err != nil {
			t.Fatalf(`菱形依赖（无环）应接受, got %v`, err)
		}
	})
}

// TestIsDelivered covers the three dependency-target shapes (design §4): assignment-delivered
// (Status==delivered before own gates done), complete (all gates passed / generic marked), and the
// non-delivered states (offered/claimed/input-required/plain). The complete branch delegates to
// IsComplete (independently tested), but is exercised here so the delegation call cannot be
// silently removed.
//
// TestIsDelivered 覆盖三种依赖目标形态（设计 §4）：分派已交付（Status==delivered，自身门禁可能未过）、
// complete（所有 gate 通过 / generic 标记）、非交付态（offered/claimed/input-required/裸）。complete
// 分支委托 IsComplete（已有独立测试），此处仍执行以使委托调用无法被静默删除。
func TestIsDelivered(t *testing.T) {
	t.Run(`assignment-delivered`, func(t *testing.T) {
		s := &TaskState{Assignment: &Assignment{Status: AssignDelivered}}
		if !s.IsDelivered() {
			t.Error(`assignment-delivered 应判 delivered（即便自身 gate 未全过）`)
		}
	})
	t.Run(`complete via all gates passed`, func(t *testing.T) {
		s := &TaskState{TaskRef: `feat/c`}
		for _, g := range DefaultGates() {
			s.RecordGateResult(g.ID, true, `abc123`)
		}
		if !s.IsComplete() {
			t.Fatal(`前置失败：所有 gate 标 passed 后 IsComplete 应 true`)
		}
		if !s.IsDelivered() {
			t.Error(`complete task 应判 delivered（IsComplete 委托）`)
		}
	})
	t.Run(`non-delivered states`, func(t *testing.T) {
		for _, st := range []string{AssignOffered, AssignClaimed, AssignInputRequired} {
			s := offeredTask(`kimi`)
			if st == AssignClaimed {
				_ = s.Claim(`kimi`)
			}
			if st == AssignInputRequired {
				_ = s.Claim(`kimi`)
				_ = s.Question(`q`)
			}
			if s.IsDelivered() {
				t.Errorf(`%s 不应判 delivered`, st)
			}
		}
		// 无 assignment 且未完成的裸 task
		if (&TaskState{}).IsDelivered() {
			t.Error(`无 assignment 未完成 task 不应判 delivered`)
		}
	})
}

// TestIsDelivered_ReopenRevokesAssignmentTask is the M1 regression: an assigned task that has
// finished all its gates (IsComplete=true) AND been delivered, when reopened (bug found in
// integration), must drop IsDelivered back to false. IsComplete stays true (gate history is
// retained), so naively using IsComplete as the delivery signal would falsely unblock dependents
// while the upstream is actually being redone. The fix: assigned tasks consult Assignment.Status
// only, not IsComplete.
//
// TestIsDelivered_ReopenRevokesAssignmentTask 是 M1 回归：一个过完所有 gate（IsComplete=true）且已
// deliver 的分派 task，被 reopen（联调发现 bug）后 IsDelivered 必须回 false。IsComplete 仍 true
// （gate 历史保留），故天真地用 IsComplete 作交付信号会在上游「实际正在重做」时假放行依赖方。修复：
// 分派 task 只看 Assignment.Status，不看 IsComplete。
func TestIsDelivered_ReopenRevokesAssignmentTask(t *testing.T) {
	s := offeredTask(`kimi`)
	_ = s.Claim(`kimi`)
	for _, g := range DefaultGates() {
		s.RecordGateResult(g.ID, true, `abc`)
	}
	if !s.IsComplete() {
		t.Fatal(`前置：所有 gate passed 后 IsComplete 应 true`)
	}
	_ = s.Deliver()
	if !s.IsDelivered() {
		t.Fatal(`deliver 后 IsDelivered 应 true`)
	}
	if err := s.Reopen(`联调发现 bug`); err != nil {
		t.Fatalf(`Reopen 应成功: %v`, err)
	}
	if s.IsDelivered() {
		t.Fatal(`M1：reopen 后 IsDelivered 应 false（分派 task 认 Status 非 IsComplete），否则依赖方假放行`)
	}
}

// TestIsDelivered_FailedCanceledNotDelivered pins that the terminal diversion states (failed /
// canceled) are not a delivery — a dependent must not unblock on an upstream that failed or was
// withdrawn. Status≠delivered and (for these) IsComplete is false, so both branches agree.
//
// TestIsDelivered_FailedCanceledNotDelivered 钉住终态分流（failed/canceled）不是交付——依赖方不该
// 因上游失败或撤回而放行。Status≠delivered 且（这些态）IsComplete 为 false，两分支一致。
func TestIsDelivered_FailedCanceledNotDelivered(t *testing.T) {
	t.Run(`failed`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		_ = s.Fail(`编译不过`)
		if s.IsDelivered() {
			t.Error(`failed task 不应判 delivered`)
		}
	})
	t.Run(`canceled`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		_ = s.Cancel(`需求变了`)
		if s.IsDelivered() {
			t.Error(`canceled task 不应判 delivered`)
		}
	})
}

// TestAddDependency_PartialWriteRollback is the L2 regression: when a multi-ref batch hits a cycle
// mid-way, the already-validated refs must NOT be left in DependsOn (all-or-nothing). Without this,
// a caller that persists on a different success path could record a partial dependency edge.
//
// TestAddDependency_PartialWriteRollback 是 L2 回归：多 ref 批次中途撞环时，已校验的 ref 不能残留进
// DependsOn（all-or-nothing）。否则在另一种成功路径上 persist 的调用方可能记下半截依赖边。
func TestAddDependency_PartialWriteRollback(t *testing.T) {
	a := &TaskState{TaskRef: `A`}
	c := &TaskState{TaskRef: `C`, DependsOn: []string{`A`}} // C→A 预存
	states := map[string]*TaskState{`A`: a, `C`: c}
	lookup := func(ref string) *TaskState { return states[ref] }
	// [B, C]：B 通过校验，C 引入环 A→C→A → 整批回滚
	err := a.AddDependency([]string{`B`, `C`}, lookup)
	if err == nil {
		t.Fatal(`C 引入环应报错`)
	}
	if len(a.DependsOn) != 0 {
		t.Fatalf(`all-or-nothing：环错误后 DependsOn 应空（B 不残留）, got %v`, a.DependsOn)
	}
}
