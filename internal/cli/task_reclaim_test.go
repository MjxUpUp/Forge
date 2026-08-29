package cli

// task_reclaim_test.go — `task reclaim` (§3 TTL recovery): stale-claim
// detection seeded in-process (saveClaimedAgo), dry-run immutability, the
// flip-to-offered recovery, and the stable empty-JSON contract.
// Migrated from task_assignment_test.go when that file was split by domain.
//
// task_reclaim_test.go —— `task reclaim`（§3 TTL 回收）：进程内种入的 stale
// 认领检测（saveClaimedAgo）、dry-run 不变性、翻回 offered 的回收、稳定的
// 空 JSON 契约。自 task_assignment_test.go 按域拆分时迁入。

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// saveClaimedAgo writes a task claimed by agent with ClaimedAt forced to `ago` and NO checklog
// activity — the exact shape of a claimed zombie (claimer gone) IsClaimedStale detects. The CLI
// stamps time.Now(), so a deterministic >7d-old claim can only be set in-process.
//
// saveClaimedAgo 写一个被 agent 认领的任务，ClaimedAt 强制为 ago 且无 checklog 活动——正是
// IsClaimedStale 检测的 claimed 僵尸（认领方失联）形态。CLI 盖当前时间，确定性的 >7d 认领只能在进程内设。
func saveClaimedAgo(t *testing.T, dir, ref, agent string, ago time.Time) {
	t.Helper()
	s := &taskpipeline.TaskState{TaskRef: ref, Summary: ref + ` 任务`}
	if err := s.AssignTo(agent, `backend`, `claude-code`); err != nil {
		t.Fatalf(`AssignTo %s: %v`, ref, err)
	}
	if err := s.Claim(agent); err != nil {
		t.Fatalf(`Claim %s: %v`, ref, err)
	}
	s.Assignment.ClaimedAt = &ago
	if err := taskpipeline.SaveTaskState(dir, s); err != nil {
		t.Fatalf(`SaveTaskState %s: %v`, ref, err)
	}
}

// TestTaskReclaim end-to-end exercises forge task reclaim — the §3 TTL recovery trigger. Runs as a
// single linear flow because reclaim mutates state (subtests would couple on order): dry-run lists
// the stale claim without mutating → reclaim flips it to offered (AbandonedCount++) and leaves a
// fresh claim + an offered task untouched → a second reclaim finds nothing.
//
// Coverage note: this exercises the HAPPY path — state does not drift between the IsClaimedStale
// scan and the per-task lock, so the in-lock IsClaimedStale re-check (the M2 TOCTOU guard) is a
// no-op here (it returns the same verdict as the outer scan). Pinning that re-check would require
// mutating checklog activity in the sub-millisecond window between ListTaskStates and LockTask,
// which is not deterministic from the CLI; do NOT remove the in-lock check assuming the outer scan
// suffices — it does not.
//
// TestTaskReclaim 端到端跑 forge task reclaim——§3 的 TTL 回收触发。以单一线性流程跑（reclaim
// 改状态，子测试会顺序耦合）：dry-run 列出 stale 认领但不动 → 回收把它翻为 offered
// （AbandonedCount++）且不动刚认领 + offered 任务 → 第二次回收无候选。
//
// 覆盖说明：本测试只覆盖「快乐路径」——状态在 IsClaimedStale 扫描与按 task 加锁之间不漂移，故
// 锁内的 IsClaimedStale 复检（M2 TOCTOU 守卫）在此是 no-op（与外层扫描返回同样判定）。钉住该复检
// 需在 ListTaskStates 与 LockTask 之间的亚毫秒窗口内写入 checklog 活动，CLI 无法确定性触发；
// 切勿以外层扫描已过滤为由删除锁内复检——并不冗余。
func TestTaskReclaim(t *testing.T) {
	dir := setupDelegateProject(t)

	// Stale claimed zombie (8d ago, no checklog) → reclaim candidate.
	//
	// claimed 僵尸（8d 前，无 checklog）→ 回收候选。
	saveClaimedAgo(t, dir, `feat/stale`, `kimi`, time.Now().Add(-8*24*time.Hour))
	// Fresh claim (now) → NOT stale, must be left alone.
	//
	// 刚认领（当前）→ 非僵尸，须不动。
	saveClaimedAgo(t, dir, `feat/fresh`, `cursor`, time.Now())
	// Offered task → not claimed, must be left alone (Abandon requires claimed).
	//
	// offered 任务 → 非 claimed，须不动（Abandon 要求 claimed）。
	saveOfferedAgo(t, dir, `feat/offered`, `reasonix`, time.Now())

	// 1) --dry-run lists only the stale claim and does NOT mutate.
	//
	// 1) --dry-run 只列 stale 认领且不改状态。
	dryOut, _, dcode := runForge(t, dir, `task`, `reclaim`, `--dry-run`, `--json`)
	if dcode != 0 {
		t.Fatalf(`reclaim --dry-run --json exit %d: %s`, dcode, dryOut)
	}
	var dry reclaimResult
	if err := json.Unmarshal([]byte(dryOut), &dry); err != nil {
		t.Fatalf(`解析 reclaim dry-run JSON 失败: %v`+"\n"+`输出: %s`, err, dryOut)
	}
	if !dry.DryRun || dry.Count != 1 || len(dry.Reclaimed) != 1 || dry.Reclaimed[0] != `feat/stale` {
		t.Fatalf(`dry-run 应 dry_run=true/count=1/仅 feat/stale, got %+v`, dry)
	}
	if st, err := taskpipeline.LoadTaskState(dir, `feat/stale`); err != nil {
		t.Fatalf(`LoadTaskState feat/stale: %v`, err)
	} else if st.Assignment.Status != taskpipeline.AssignClaimed || st.Assignment.AbandonedCount != 0 {
		t.Fatalf(`dry-run 不应改状态, got status=%s count=%d`, st.Assignment.Status, st.Assignment.AbandonedCount)
	}

	// 2) Real reclaim flips feat/stale → offered, bumps AbandonedCount, clears ClaimedAt, sets
	//    AbandonedAt; feat/fresh (claimed now) and feat/offered are untouched.
	//
	// 2) 真回收把 feat/stale → offered、AbandonedCount++、清 ClaimedAt、置 AbandonedAt；
	//    feat/fresh（刚认领）与 feat/offered 不受影响。
	out, _, code := runForge(t, dir, `task`, `reclaim`)
	if code != 0 {
		t.Fatalf(`reclaim exit %d: %s`, code, out)
	}
	if !strings.Contains(out, `已回收`) || !strings.Contains(out, `feat/stale`) {
		t.Errorf(`reclaim 输出应含「已回收」+ feat/stale, got:`+"\n"+`%s`, out)
	}
	st, err := taskpipeline.LoadTaskState(dir, `feat/stale`)
	if err != nil {
		t.Fatalf(`LoadTaskState feat/stale: %v`, err)
	}
	if st.Assignment.Status != taskpipeline.AssignOffered {
		t.Fatalf(`feat/stale 应回 offered, got %s`, st.Assignment.Status)
	}
	if st.Assignment.AbandonedCount != 1 {
		t.Errorf(`feat/stale AbandonedCount 应为 1, got %d`, st.Assignment.AbandonedCount)
	}
	if st.Assignment.AbandonedAt == nil {
		t.Error(`feat/stale AbandonedAt 应已设置`)
	}
	if st.Assignment.ClaimedAt != nil {
		t.Error(`feat/stale ClaimedAt 应被清空`)
	}
	if fresh, err := taskpipeline.LoadTaskState(dir, `feat/fresh`); err != nil {
		t.Fatalf(`LoadTaskState feat/fresh: %v`, err)
	} else if fresh.Assignment.Status != taskpipeline.AssignClaimed {
		t.Errorf(`feat/fresh 应仍 claimed 不被动, got %s`, fresh.Assignment.Status)
	}
	if off, err := taskpipeline.LoadTaskState(dir, `feat/offered`); err != nil {
		t.Fatalf(`LoadTaskState feat/offered: %v`, err)
	} else if off.Assignment.Status != taskpipeline.AssignOffered || off.Assignment.AbandonedCount != 0 {
		t.Errorf(`feat/offered 应不动, got status=%s count=%d`, off.Assignment.Status, off.Assignment.AbandonedCount)
	}

	// 3) Second reclaim finds no candidates — feat/stale is now offered (not claimed).
	//
	// 3) 第二次回收无候选——feat/stale 已 offered（非 claimed）。
	out2, _, code2 := runForge(t, dir, `task`, `reclaim`)
	if code2 != 0 {
		t.Fatalf(`second reclaim exit %d: %s`, code2, out2)
	}
	if !strings.Contains(out2, `无 claimed 僵尸`) {
		t.Errorf(`第二次 reclaim 应报告无候选, got:`+"\n"+`%s`, out2)
	}
}

// TestTaskReclaim_EmptyJSON pins the M1 fix: reclaim --json with NO stale tasks must emit
// "reclaimed": [] (a stable empty array), not the Go-default null. Sibling commands (mine/health
// JSON) share the same convention so consumers can range over the field unconditionally.
//
// TestTaskReclaim_EmptyJSON 钉住 M1 修复：无 stale 任务时 reclaim --json 必须输出
// "reclaimed": []（稳定空数组），而非 Go 默认的 null。兄弟命令（mine/health JSON）同约定，
// 使消费者可无条件遍历该字段。
func TestTaskReclaim_EmptyJSON(t *testing.T) {
	dir := setupDelegateProject(t)
	// A fresh (non-stale) claim → reclaim finds nothing.
	//
	// 刚认领（非僵尸）的任务 → reclaim 无候选。
	saveClaimedAgo(t, dir, `feat/fresh`, `kimi`, time.Now())

	out, _, code := runForge(t, dir, `task`, `reclaim`, `--json`)
	if code != 0 {
		t.Fatalf(`reclaim --json exit %d: %s`, code, out)
	}
	if !strings.Contains(out, `"reclaimed": []`) {
		t.Errorf(`空结果应序列化为 "reclaimed": [] 而非 null, got:`+"\n"+`%s`, out)
	}
	if strings.Contains(out, `null`) {
		t.Errorf(`空结果不应含 null, got:`+"\n"+`%s`, out)
	}
	var res reclaimResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf(`解析失败: %v`+"\n"+`输出: %s`, err, out)
	}
	if res.Count != 0 || len(res.Reclaimed) != 0 {
		t.Errorf(`应 count=0/reclaimed 空, got %+v`, res)
	}
}
