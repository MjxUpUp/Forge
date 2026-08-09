package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// task_health_test.go: end-to-end `forge task health` over an isolated project. The detection
// logic is unit-covered in taskpipeline/health_test.go; this test wires it through the CLI and
// asserts the SCAN surfaces zombies + deadlocks and OMITS healthy tasks (the milestone: 卡住的
// 任务主动暴露). Zombie timestamps are seeded directly via taskpipeline.SaveTaskState because the
// CLI always stamps time.Now() — a deterministic 8-days-ago offer can only be set in-process.
//
// task_health_test.go：在隔离项目上端到端跑 forge task health。检测逻辑已在 taskpipeline/
// health_test.go 单元覆盖；本测试把它接到 CLI 并断言扫描上浮僵尸 + 死锁、且「省略」健康任务
// （里程碑：卡住的任务主动暴露）。僵尸时间戳经 taskpipeline.SaveTaskState 直接种入，因 CLI 总盖
// time.Now()——确定性的「8 天前 offered」只能在进程内设置。

// saveOfferedAgo writes a task offered to agent with OfferedAt forced to `ago` (simulating an
// offer that has sat unclaimed past the TTL — impossible to produce via the CLI, which stamps now).
//
// saveOfferedAgo 写一个 offered 给 agent 的任务，OfferedAt 强制为 ago（模拟无人认领超过 TTL 的
// 派发——CLI 盖当前时间，产不出此态）。
func saveOfferedAgo(t *testing.T, dir, ref, agent string, ago time.Time) {
	t.Helper()
	s := &taskpipeline.TaskState{TaskRef: ref, Summary: ref + ` 任务`}
	if err := s.AssignTo(agent, `frontend`, `claude-code`); err != nil {
		t.Fatalf(`AssignTo %s: %v`, ref, err)
	}
	s.Assignment.OfferedAt = &ago
	if err := taskpipeline.SaveTaskState(dir, s); err != nil {
		t.Fatalf(`SaveTaskState %s: %v`, ref, err)
	}
}

// saveFailedDependency writes a claimed-then-failed task, the kind a DependsOn edge can never
// satisfy — the root of an abandoned chain (设计 §12 废弃链).
//
// saveFailedDependency 写一个 claimed 后 failed 的任务，DependsOn 边永不可达——废弃链的根
// （设计 §12 废弃链）。
func saveFailedDependency(t *testing.T, dir, ref, agent string) {
	t.Helper()
	s := &taskpipeline.TaskState{TaskRef: ref, Summary: ref + ` 失败依赖`}
	if err := s.AssignTo(agent, `backend`, `claude-code`); err != nil {
		t.Fatalf(`AssignTo %s: %v`, ref, err)
	}
	if err := s.Claim(agent); err != nil {
		t.Fatalf(`Claim %s: %v`, ref, err)
	}
	if err := s.Fail(`boom`); err != nil {
		t.Fatalf(`Fail %s: %v`, ref, err)
	}
	if err := taskpipeline.SaveTaskState(dir, s); err != nil {
		t.Fatalf(`SaveTaskState %s: %v`, ref, err)
	}
}

func TestTaskHealth_ReportsZombiesAndDeadlocks(t *testing.T) {
	dir := setupDelegateProject(t)

	// 1) Zombie: offered 8 days ago (offered>7d).
	//
	// 僵尸：8 天前 offered（offered>7d）。
	saveOfferedAgo(t, dir, `feat/zombie`, `kimi`, time.Now().Add(-8*24*time.Hour))

	// 2) Deadlock: feat/deadlocked depends on feat/dep-failed (canceled/failed → permanent block).
	//
	// 死锁：feat/deadlocked 依赖 feat/dep-failed（失败 → 永久阻塞）。
	saveFailedDependency(t, dir, `feat/dep-failed`, `reasonix`)
	dead := &taskpipeline.TaskState{TaskRef: `feat/deadlocked`, Summary: `死锁任务`, DependsOn: []string{`feat/dep-failed`}}
	if err := taskpipeline.SaveTaskState(dir, dead); err != nil {
		t.Fatalf(`SaveTaskState deadlocked: %v`, err)
	}

	// 3) Healthy: offered just now — must be OMITTED from the report.
	//
	// 健康：刚刚 offered——必须从报告中「省略」。
	saveOfferedAgo(t, dir, `feat/healthy`, `cursor`, time.Now())

	t.Run(`json lists only flagged tasks`, func(t *testing.T) {
		out, _, code := runForge(t, dir, `task`, `health`, `--json`)
		if code != 0 {
			t.Fatalf(`task health --json exit %d: %s`, code, out)
		}
		var rows []healthRow
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			t.Fatalf(`解析 health JSON 失败: %v\n输出: %s`, err, out)
		}
		if len(rows) != 2 {
			t.Fatalf(`应仅列出 2 个被标记任务（僵尸+死锁），得到 %d: %+v`, len(rows), rows)
		}
		byRef := map[string]healthRow{}
		for _, r := range rows {
			byRef[r.TaskRef] = r
		}
		z, ok := byRef[`feat/zombie`]
		if !ok {
			t.Fatalf(`僵尸任务 feat/zombie 应在报告中，得到 %+v`, byRef)
		}
		if !z.IsZombie || len(z.ZombieReasons) == 0 || z.ZombieReasons[0] != `offered>7d` {
			t.Errorf(`feat/zombie 应 is_zombie 且 reason=offered>7d, got %+v`, z)
		}
		d, ok := byRef[`feat/deadlocked`]
		if !ok {
			t.Fatalf(`死锁任务 feat/deadlocked 应在报告中，得到 %+v`, byRef)
		}
		if !d.Deadlocked || d.DeadlockReason == `` {
			t.Errorf(`feat/deadlocked 应 deadlocked 且带 reason, got %+v`, d)
		}
		if _, leaked := byRef[`feat/healthy`]; leaked {
			t.Error(`健康任务 feat/healthy 不应出现在报告中`)
		}
	})

	t.Run(`text surfaces markers and omits healthy`, func(t *testing.T) {
		out, _, code := runForge(t, dir, `task`, `health`)
		if code != 0 {
			t.Fatalf(`task health exit %d: %s`, code, out)
		}
		for _, want := range []string{`发现 2 个需关注任务`, `feat/zombie`, `⚠僵尸`, `offered>7d`, `feat/deadlocked`, `🔒死锁`} {
			if !strings.Contains(out, want) {
				t.Errorf(`health 文本输出应含 %q\n输出:\n%s`, want, out)
			}
		}
		if strings.Contains(out, `feat/healthy`) {
			t.Errorf(`健康任务不应出现:\n%s`, out)
		}
	})
}

func TestTaskHealth_CleanProjectReportsNothing(t *testing.T) {
	dir := setupDelegateProject(t)
	// Only a freshly-offered (healthy) task — no zombies, no deadlocks.
	//
	// 仅一个刚 offered（健康）的任务——无僵尸无死锁。
	saveOfferedAgo(t, dir, `feat/fresh`, `kimi`, time.Now())

	out, _, code := runForge(t, dir, `task`, `health`)
	if code != 0 {
		t.Fatalf(`task health exit %d: %s`, code, out)
	}
	if !strings.Contains(out, `未发现`) {
		t.Errorf(`无问题时应输出「未发现」, got:\n%s`, out)
	}
}
