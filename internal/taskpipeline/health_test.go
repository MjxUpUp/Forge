package taskpipeline

import (
	"errors"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// health_test.go: covers the read-only zombie/deadlock detection in health.go. All time-based
// checks use a FIXED `now` (never time.Now) so the 7-day windows are deterministic. Checklog
// activity is seeded with checklog.AppendEntries (which preserves RecordedAt) so the "is anyone
// working?" signal can be placed at an exact instant.
//
// health_test.go：覆盖 health.go 的只读僵尸/死锁检测。所有基于时间的检查用固定 now
// （绝不用 time.Now）使 7 天窗口确定。checklog 活动用 checklog.AppendEntries（保留 RecordedAt）
// 种入，使「是否仍有人在推进?」信号可置于精确时刻。

// fixedNow is the deterministic reference instant for age math (1e9 Unix sec ≈ 2001-09-09).
// All "old"/"recent" offsets derive from it so no test reads the wall clock.
//
// fixedNow 是年龄计算的确定性参考时刻（1e9 Unix 秒 ≈ 2001-09-09）。所有「老」「近」偏移
// 都由它派生，故无任何测试读取墙上时钟。
var fixedNow = time.Unix(1_000_000_000, 0).UTC()

// eightDaysAgo / oneHourAgo bracket the 7-day zombie TTL: old enough to trip it, fresh enough not to.
//
// eightDaysAgo / oneHourAgo 夹住 7 天僵尸 TTL：前者足以触发，后者不足以。
var (
	eightDaysAgo = fixedNow.Add(-8 * 24 * time.Hour)
	oneHourAgo   = fixedNow.Add(-1 * time.Hour)
)

// seedChecklog writes one entry for taskRef at the given time so the activity-based TTL checks
// (claimed/input-required) have a known latest-activity instant. Uses AppendEntries to keep the
// supplied RecordedAt (Record would stamp now). root is an arbitrary dir; FORGE_DATA_HOME (set
// per-test) determines where the file actually lands.
//
// seedChecklog 在 taskRef 的指定时间写一条条目，使基于活动的 TTL 检查（claimed/input-required）
// 有已知的最新活动时刻。用 AppendEntries 保留所给 RecordedAt（Record 会盖当前时间）。root 为
// 任意目录；实际落点由 per-test 设置的 FORGE_DATA_HOME 决定。
func seedChecklog(t *testing.T, root, taskRef string, at time.Time) {
	t.Helper()
	if err := checklog.AppendEntries(root, []checklog.Entry{
		{Check: `compile`, TaskRef: taskRef, RecordedAt: at, Detail: `seeded activity`},
	}); err != nil {
		t.Fatalf(`seedChecklog: %v`, err)
	}
}

// ptrTime is the test-local helper to build a *time.Time from a value (mirrors how Assignment
// stores OfferedAt/ClaimedAt/AbandonedAt as pointers).
//
// ptrTime 是测试内把 time.Time 值包成 *time.Time 的助手（镜像 Assignment 把 OfferedAt/
// ClaimedAt/AbandonedAt 存为指针的方式）。
func ptrTime(t time.Time) *time.Time { return &t }

func TestIsOfferedZombie(t *testing.T) {
	t.Run(`offered older than TTL is zombie`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		s.Assignment.OfferedAt = ptrTime(eightDaysAgo)
		if ok, _ := IsOfferedZombie(s, fixedNow); !ok {
			t.Fatal(`offered 8 天前应判僵尸`)
		}
	})
	t.Run(`offered fresh is not`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		s.Assignment.OfferedAt = ptrTime(oneHourAgo)
		if ok, _ := IsOfferedZombie(s, fixedNow); ok {
			t.Fatal(`offered 1 小时前不应判僵尸`)
		}
	})
	t.Run(`recent abandon resets the baseline`, func(t *testing.T) {
		// OfferedAt is old, but Abandon() just reclaimed it (AbandonedAt=recent): the task was
		// re-offered fresh, so it must NOT be flagged until it idles again.
		//
		// OfferedAt 虽老，但 Abandon() 刚回收（AbandonedAt=近期）：任务被重新 offered，故在再次
		// 空闲前不应被标记。
		s := offeredTask(`kimi`)
		s.Assignment.OfferedAt = ptrTime(eightDaysAgo)
		s.Assignment.AbandonedAt = ptrTime(oneHourAgo)
		if ok, _ := IsOfferedZombie(s, fixedNow); ok {
			t.Fatal(`近期 abandon 回收应重置基线，不应判僵尸`)
		}
	})
	t.Run(`missing OfferedAt cannot age`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		s.Assignment.OfferedAt = nil // legacy state with no offer timestamp
		if ok, _ := IsOfferedZombie(s, fixedNow); ok {
			t.Fatal(`无 OfferedAt 应无法老化，不判僵尸（避免假阳性）`)
		}
	})
	t.Run(`non-offered is not checked`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		if ok, _ := IsOfferedZombie(s, fixedNow); ok {
			t.Fatal(`claimed 任务不应被 offered 检查命中`)
		}
	})
}

func TestIsClaimedStale(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	root := t.TempDir()
	t.Run(`claimed old with no activity is stale`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		s.Assignment.ClaimedAt = ptrTime(eightDaysAgo)
		if ok, _ := IsClaimedStale(root, s, fixedNow); !ok {
			t.Fatal(`claimed 8 天前且无 checklog 活动应判僵尸`)
		}
	})
	t.Run(`recent checklog activity resets window`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		s.Assignment.ClaimedAt = ptrTime(eightDaysAgo)
		seedChecklog(t, root, s.TaskRef, oneHourAgo) // worked 1h ago → not abandoned
		if ok, _ := IsClaimedStale(root, s, fixedNow); ok {
			t.Fatal(`1 小时前仍有 checklog 活动不应判僵尸`)
		}
	})
	t.Run(`claimed fresh is not stale`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		s.Assignment.ClaimedAt = ptrTime(oneHourAgo)
		if ok, _ := IsClaimedStale(root, s, fixedNow); ok {
			t.Fatal(`claimed 1 小时前不应判僵尸`)
		}
	})
	t.Run(`non-claimed is not checked`, func(t *testing.T) {
		s := offeredTask(`kimi`) // offered, not claimed
		if ok, _ := IsClaimedStale(root, s, fixedNow); ok {
			t.Fatal(`offered 任务不应被 claimed 检查命中`)
		}
	})
}

func TestIsInputReqStale(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	root := t.TempDir()
	raiseQuestion := func() *TaskState {
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		_ = s.Question(`需要设计稿?`)
		return s
	}
	t.Run(`claimed old, never worked, unanswered is stale`, func(t *testing.T) {
		// The ClaimedAt fallback (MEDIUM-1 fix): with NO checklog activity, a question asked long
		// ago and never progressed IS flagged — ClaimedAt anchors the age. Before the fix this was a
		// false negative (the canonical stuck question: a worker claims, raises it, then both sides
		// go silent with no gate ever run to write a checklog entry).
		//
		// ClaimedAt 兜底（MEDIUM-1 修复）：无 checklog 活动时，一个很久前提出、后再无推进的问题「仍」
		// 被标记——ClaimedAt 锚定年龄。修复前此例为假阴性（典型卡问题：工作方认领后回抛，双方随后静默，
		// 从未跑过门禁写 checklog）。
		s := raiseQuestion()
		s.Assignment.ClaimedAt = ptrTime(eightDaysAgo)
		if ok, _ := IsInputReqStale(root, s, fixedNow); !ok {
			t.Fatal(`input-required，claimed 8 天前且无后续活动应判僵尸（ClaimedAt 兜底基线）`)
		}
	})
	t.Run(`recent checklog activity resets window`, func(t *testing.T) {
		s := raiseQuestion()
		s.Assignment.ClaimedAt = ptrTime(eightDaysAgo)
		seedChecklog(t, root, s.TaskRef, oneHourAgo) // worked 1h ago → not abandoned
		if ok, _ := IsInputReqStale(root, s, fixedNow); ok {
			t.Fatal(`1 小时前仍有 checklog 活动不应判僵尸`)
		}
	})
	t.Run(`claimed fresh is not stale`, func(t *testing.T) {
		s := raiseQuestion()
		s.Assignment.ClaimedAt = ptrTime(oneHourAgo)
		if ok, _ := IsInputReqStale(root, s, fixedNow); ok {
			t.Fatal(`claimed 1 小时前不应判僵尸`)
		}
	})
	t.Run(`non-input-required is not checked`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		if ok, _ := IsInputReqStale(root, s, fixedNow); ok {
			t.Fatal(`offered 任务不应被 input-required 检查命中`)
		}
	})
}

func TestIsRepeatAbandon(t *testing.T) {
	t.Run(`in-flight with count>=2 is zombie`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		s.Assignment.AbandonedCount = 2
		if !IsRepeatAbandon(s) {
			t.Fatal(`offered 且 AbandonedCount=2 应判反复回收僵尸`)
		}
	})
	t.Run(`terminal status not flagged`, func(t *testing.T) {
		// A delivered task with a messy abandonment history is NOT currently stuck.
		//
		// 已交付但带混乱回收历史的任务并非此刻卡住。
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		_ = s.Deliver()
		s.Assignment.AbandonedCount = 5
		if IsRepeatAbandon(s) {
			t.Fatal(`delivered（终态）即便 AbandonedCount 高也不应判僵尸`)
		}
	})
	t.Run(`count below threshold not flagged`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		s.Assignment.AbandonedCount = 1
		if IsRepeatAbandon(s) {
			t.Fatal(`AbandonedCount=1 低于阈值不应判僵尸`)
		}
	})
}

func TestIsZombie(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	root := t.TempDir()
	t.Run(`non-delegated never zombie`, func(t *testing.T) {
		s := &TaskState{TaskRef: `feat/plain`}
		if ok, _ := IsZombie(root, s, fixedNow); ok {
			t.Fatal(`无分派的普通任务永不是僵尸`)
		}
	})
	t.Run(`terminal status never zombie`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		_ = s.Deliver()
		s.Assignment.AbandonedCount = 9
		if ok, _ := IsZombie(root, s, fixedNow); ok {
			t.Fatal(`delivered 终态即便 AbandonedCount 高也不是僵尸`)
		}
	})
	t.Run(`offered zombie carries reason`, func(t *testing.T) {
		s := offeredTask(`kimi`)
		s.Assignment.OfferedAt = ptrTime(eightDaysAgo)
		ok, reasons := IsZombie(root, s, fixedNow)
		if !ok || len(reasons) != 1 || reasons[0] != `offered>7d` {
			t.Fatalf(`应为 offered>7d 僵尸，得到 ok=%v reasons=%v`, ok, reasons)
		}
	})
	t.Run(`multiple signals collect multiple reasons`, func(t *testing.T) {
		// claimed 8d ago (stale) AND abandoned twice → two distinct reasons.
		//
		// claimed 8 天前（失联）且被回收两次 → 两条不同 reason。
		s := offeredTask(`kimi`)
		_ = s.Claim(`kimi`)
		s.Assignment.ClaimedAt = ptrTime(eightDaysAgo)
		s.Assignment.AbandonedCount = 2
		ok, reasons := IsZombie(root, s, fixedNow)
		if !ok || len(reasons) != 2 {
			t.Fatalf(`应收集两条 reason（claimed>TTL + abandoned_count≥2），得到 ok=%v reasons=%v`, ok, reasons)
		}
	})
}

func TestDeadlockedDependency(t *testing.T) {
	build := func(items map[string]*TaskState) func(string) (*TaskState, error) {
		return func(ref string) (*TaskState, error) {
			if s, ok := items[ref]; ok {
				return s, nil
			}
			return nil, errors.New(`lookup: not found`) // missing → dead-chain root
		}
	}
	t.Run(`failed dependency deadlocks`, func(t *testing.T) {
		a := &TaskState{TaskRef: `feat/a`, DependsOn: []string{`feat/b`}}
		b := offeredTask(`kimi`)
		_ = b.Claim(`kimi`)
		_ = b.Fail(`boom`)
		lookup := build(map[string]*TaskState{`feat/b`: b})
		if ref, dead := DeadlockedDependency(a, lookup); !dead || ref != `feat/b` {
			t.Fatalf(`依赖 failed 应死锁，得到 ref=%q dead=%v`, ref, dead)
		}
	})
	t.Run(`canceled dependency deadlocks`, func(t *testing.T) {
		a := &TaskState{TaskRef: `feat/a`, DependsOn: []string{`feat/b`}}
		b := offeredTask(`kimi`)
		_ = b.Cancel(`撤回`)
		lookup := build(map[string]*TaskState{`feat/b`: b})
		if _, dead := DeadlockedDependency(a, lookup); !dead {
			t.Fatal(`依赖 canceled 应死锁`)
		}
	})
	t.Run(`missing dependency deadlocks`, func(t *testing.T) {
		a := &TaskState{TaskRef: `feat/a`, DependsOn: []string{`feat/gone`}}
		lookup := build(map[string]*TaskState{}) // feat/gone absent (aborted)
		if ref, dead := DeadlockedDependency(a, lookup); !dead || ref != `feat/gone` {
			t.Fatalf(`依赖缺失应死锁，得到 ref=%q dead=%v`, ref, dead)
		}
	})
	t.Run(`pending-but-alive dependency is not a deadlock`, func(t *testing.T) {
		// A dependency that is claimed (not yet delivered) but still alive is PENDING, not dead —
		// task health must not cry deadlock over normal in-flight dependencies.
		//
		// 依赖 claimed（未交付）但仍存活是 PENDING 而非死锁——task health 不得对正常在途依赖误报。
		a := &TaskState{TaskRef: `feat/a`, DependsOn: []string{`feat/b`}}
		b := offeredTask(`kimi`)
		_ = b.Claim(`kimi`)
		lookup := build(map[string]*TaskState{`feat/b`: b})
		if _, dead := DeadlockedDependency(a, lookup); dead {
			t.Fatal(`依赖 claimed 在途（未交付但存活）不应判死锁`)
		}
	})
	t.Run(`delivered dependency is not a deadlock`, func(t *testing.T) {
		a := &TaskState{TaskRef: `feat/a`, DependsOn: []string{`feat/b`}}
		b := offeredTask(`kimi`)
		_ = b.Claim(`kimi`)
		_ = b.Deliver()
		lookup := build(map[string]*TaskState{`feat/b`: b})
		if _, dead := DeadlockedDependency(a, lookup); dead {
			t.Fatal(`依赖 delivered 不应判死锁`)
		}
	})
}

func TestHasDependencyCycle(t *testing.T) {
	t.Run(`two-node cycle detected`, func(t *testing.T) {
		a := &TaskState{TaskRef: `feat/a`, DependsOn: []string{`feat/b`}}
		b := &TaskState{TaskRef: `feat/b`, DependsOn: []string{`feat/a`}}
		lookup := func(ref string) *TaskState {
			m := map[string]*TaskState{`feat/a`: a, `feat/b`: b}
			return m[ref]
		}
		if !HasDependencyCycle(a, lookup) {
			t.Fatal(`A→B→A 环应被检测`)
		}
	})
	t.Run(`acyclic chain not flagged`, func(t *testing.T) {
		a := &TaskState{TaskRef: `feat/a`, DependsOn: []string{`feat/b`}}
		b := &TaskState{TaskRef: `feat/b`, DependsOn: []string{`feat/c`}}
		c := &TaskState{TaskRef: `feat/c`}
		lookup := func(ref string) *TaskState {
			m := map[string]*TaskState{`feat/a`: a, `feat/b`: b, `feat/c`: c}
			return m[ref]
		}
		if HasDependencyCycle(a, lookup) {
			t.Fatal(`A→B→C 无环不应误报`)
		}
	})
	t.Run(`self-dependency is a cycle`, func(t *testing.T) {
		a := &TaskState{TaskRef: `feat/a`, DependsOn: []string{`feat/a`}}
		lookup := func(ref string) *TaskState { return a }
		if !HasDependencyCycle(a, lookup) {
			t.Fatal(`自依赖应判环`)
		}
	})
	t.Run(`missing dependency is a leaf not a cycle`, func(t *testing.T) {
		a := &TaskState{TaskRef: `feat/a`, DependsOn: []string{`feat/gone`}}
		lookup := func(ref string) *TaskState { return nil } // feat/gone absent
		if HasDependencyCycle(a, lookup) {
			t.Fatal(`依赖缺失应是叶子而非环`)
		}
	})
	t.Run(`reachable non-start cycle still blocks`, func(t *testing.T) {
		// A→B, B→C, C→B: A is not itself in the B↔C cycle, but its dependency subtree contains one,
		// so A can never complete. Gray-set DFS finds the back-edge C→B and reports A as stuck (not
		// just B). Cycles are rejected at AddDependency write time, so this only arises from import
		// or corruption — but when it does, every transitively-blocked task should surface.
		//
		// A→B, B→C, C→B：A 自身不在 B↔C 环中，但其依赖子树含环，故 A 永不可完成。gray-set DFS 发现回边
		// C→B 并把 A 也判为受阻塞（不止 B）。环在 AddDependency 写入时即拒，故此情形仅来自 import 或
		// 损坏——但一旦发生，每个传递受阻的任务都应上浮。
		a := &TaskState{TaskRef: `feat/a`, DependsOn: []string{`feat/b`}}
		b := &TaskState{TaskRef: `feat/b`, DependsOn: []string{`feat/c`}}
		c := &TaskState{TaskRef: `feat/c`, DependsOn: []string{`feat/b`}}
		lookup := func(ref string) *TaskState {
			m := map[string]*TaskState{`feat/a`: a, `feat/b`: b, `feat/c`: c}
			return m[ref]
		}
		if !HasDependencyCycle(a, lookup) {
			t.Fatal(`A 的依赖子树含 B↔C 环，A 应被判受环阻塞`)
		}
	})
}
