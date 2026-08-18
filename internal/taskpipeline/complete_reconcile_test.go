package taskpipeline

import (
	"testing"
)

// complete_reconcile_test.go: covers the 2026-08-18 脱节修复 — the task pipeline
// (gates→complete) and the assignment state machine (offered→claimed→delivered) previously
// did not talk, so a completed task could keep a suspended offered assignment (mine renders
// 待认领 forever; the zombie scan flags it after 7d). MarkComplete now auto-reclaims an
// in-flight assignment to delivered (with an AutoDelivered audit trail), and the zombie
// checks honor the long-declared "a completed task is never a zombie" semantic.
//
// complete_reconcile_test.go：覆盖 2026-08-18 脱节修复——任务管线（gates→complete）与分派
// 状态机（offered→claimed→delivered）此前互不知情，已完成任务可能悬置 offered 分派（mine
// 永久渲染待认领、僵尸扫描 7 天后误报）。MarkComplete 现在把在途分派自动回收为 delivered
// （带 AutoDelivered 审计痕迹），僵尸检查兑现早已声明的「已完成任务永不是僵尸」语义。

// passAllGates marks every default gate as passed so IsComplete() reports true — the exact
// precondition the dogfooded 脱节个案 hit (gates done, assignment suspended).
//
// passAllGates 把全部默认门禁标为 passed，使 IsComplete() 为 true——正是 dogfood 脱节个案
// 命中的前置（门禁全过、分派悬置）。
func passAllGates(s *TaskState) {
	for _, g := range DefaultGates() {
		s.RecordGateResult(g.ID, true, ``)
	}
}

func TestCompleteReconcilesAssignment(t *testing.T) {
	inFlight := []struct {
		name  string
		setup func(s *TaskState)
	}{
		{`offered`, func(s *TaskState) {}},
		{`claimed`, func(s *TaskState) { _ = s.Claim(`kimi`) }},
		{`input-required`, func(s *TaskState) { _ = s.Claim(`kimi`); _ = s.Question(`卡住了`) }},
	}
	for _, tc := range inFlight {
		t.Run(tc.name+` auto-reclaimed to delivered with audit trail`, func(t *testing.T) {
			s := offeredTask(`kimi`)
			tc.setup(s)
			s.MarkComplete()
			a := s.Assignment
			if a.Status != AssignDelivered {
				t.Fatalf(`complete 后 %s 分派应回收为 delivered, got %s`, tc.name, a.Status)
			}
			if a.DeliveredAt == nil || a.DeliveredAt.IsZero() {
				t.Fatal(`应记 DeliveredAt`)
			}
			if !a.AutoDelivered {
				t.Fatal(`应记 AutoDelivered 审计痕迹（区分非人工 deliver）`)
			}
		})
	}

	t.Run(`generic kind path reconciles too`, func(t *testing.T) {
		// Both CLI complete paths — runTaskCompleteAt (code kind) and completeGenericTask —
		// converge on MarkComplete, so a generic task with a suspended assignment reclaims
		// through the same state-machine call point.
		//
		// 两条 CLI 完成路径——runTaskCompleteAt（code kind）与 completeGenericTask——都汇聚到
		// MarkComplete，故带悬置分派的 generic 任务经同一状态机调用点回收。
		s := offeredTask(`kimi`)
		s.Kind = TaskKindGeneric
		s.MarkComplete()
		if s.Assignment.Status != AssignDelivered || !s.Assignment.AutoDelivered {
			t.Fatalf(`generic 任务 complete 应同样回收分派, got status=%s auto=%v`,
				s.Assignment.Status, s.Assignment.AutoDelivered)
		}
	})

	t.Run(`human-delivered terminal untouched`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		_ = s.Deliver()
		humanDeliveredAt := s.Assignment.DeliveredAt
		s.MarkComplete()
		if s.Assignment.Status != AssignDelivered {
			t.Fatal(`delivered 终态应保持 delivered`)
		}
		if s.Assignment.AutoDelivered {
			t.Fatal(`人工 deliver 不得被改标 AutoDelivered`)
		}
		if s.Assignment.DeliveredAt != humanDeliveredAt {
			t.Fatal(`人工 DeliveredAt 不得被覆盖`)
		}
	})

	t.Run(`failed and canceled terminals untouched`, func(t *testing.T) {
		for _, status := range []string{AssignFailed, AssignCanceled} {
			s := offeredTask(`kimi`)
			s.Assignment.Status = status
			s.MarkComplete()
			if s.Assignment.Status != status {
				t.Fatalf(`%s 终态应 no-op, got %s`, status, s.Assignment.Status)
			}
			if s.Assignment.DeliveredAt != nil || s.Assignment.AutoDelivered {
				t.Fatalf(`%s 终态不得记 DeliveredAt/AutoDelivered`, status)
			}
		}
	})

	t.Run(`no assignment is a no-op`, func(t *testing.T) {
		s := &TaskState{TaskRef: `feat/plain`}
		s.MarkComplete() // must not panic
		if s.Assignment != nil {
			t.Fatal(`无分派任务 complete 不得凭空创建分派`)
		}
		if s.CompletedAt == nil {
			t.Fatal(`CompletedAt 应照常记录`)
		}
	})

	t.Run(`reopen after auto-deliver clears the audit flag`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		s.MarkComplete()
		if err := s.Reopen(`发现 bug`); err != nil {
			t.Fatalf(`Reopen 应成功: %v`, err)
		}
		if s.Assignment.AutoDelivered || s.Assignment.DeliveredAt != nil {
			t.Fatal(`Reopen 应清 AutoDelivered 与 DeliveredAt（镜像既有 DeliveredAt 清零）`)
		}
	})
}

func TestZombieSkipsCompleted(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	root := t.TempDir()

	t.Run(`completed offered past TTL is not zombie`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		s.Assignment.OfferedAt = ptrTime(eightDaysAgo)
		passAllGates(s)
		if !s.IsComplete() {
			t.Fatal(`前置：门禁全过应 IsComplete`)
		}
		if ok, reasons := IsZombie(root, s, fixedNow); ok {
			t.Fatalf(`已完成任务即便 offered 超 7d 也永不是僵尸, got reasons=%v`, reasons)
		}
	})

	t.Run(`completed claimed stale is not zombie`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		s.Assignment.ClaimedAt = ptrTime(eightDaysAgo)
		passAllGates(s)
		if ok, reasons := IsZombie(root, s, fixedNow); ok {
			t.Fatalf(`已完成任务即便 claimed 失联超 TTL 也不是僵尸, got reasons=%v`, reasons)
		}
	})

	t.Run(`completed with repeat abandons is not zombie`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		s.Assignment.AbandonedCount = RepeatAbandonThreshold
		passAllGates(s)
		if ok, _ := IsZombie(root, s, fixedNow); ok {
			t.Fatal(`已完成任务即便有反复回收历史也不是僵尸`)
		}
	})

	t.Run(`incomplete offered past TTL still zombie (control)`, func(t *testing.T) {
		// Negative control: the immunity is keyed on IsComplete, not on the assignment shape
		// alone — the same suspended-offered task WITHOUT the gates passed is still flagged.
		//
		// 阴性对照：免疫以 IsComplete 为键而非分派形态本身——同样悬置 offered 但门禁未全过
		// 的任务仍照常上浮。
		s := offeredTask(`kimi`)
		s.Assignment.OfferedAt = ptrTime(eightDaysAgo)
		ok, reasons := IsZombie(root, s, fixedNow)
		if !ok || len(reasons) != 1 || reasons[0] != `offered>7d` {
			t.Fatalf(`未完成的对照任务应仍是 offered>7d 僵尸, got ok=%v reasons=%v`, ok, reasons)
		}
	})
}
