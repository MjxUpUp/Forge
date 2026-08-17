package dashboard

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestPulseCache_ReloadsOnlyOnChange pins the fingerprint-gating contract: repeated
// aggregations over unchanged files reuse the cached parse (one real load), and any
// source-file change (here: a new act conclusion) forces a reload whose result
// immediately reflects the new data — no stale reads.
//
// TestPulseCache_ReloadsOnlyOnChange 钉住指纹门控契约：文件未变的重复聚合复用缓存
// 解析（只发生一次真实加载）；任何源文件变更（此处：新增 act 结论）强制重载，且
// 重载结果立即反映新数据——不可读到旧值。
func TestPulseCache_ReloadsOnlyOnChange(t *testing.T) {
	root, _, base := feedFixture(t)
	before := sharedPulseCache.loadCount(root)

	if _, err := AggregateFeed(Options{Root: root}, base.Add(5*time.Hour), FeedQuery{}); err != nil {
		t.Fatal(err)
	}
	if _, err := AggregateFeed(Options{Root: root}, base.Add(6*time.Hour), FeedQuery{}); err != nil {
		t.Fatal(err)
	}
	if got := sharedPulseCache.loadCount(root); got != before+1 {
		t.Fatalf("两次聚合未变文件应只加载 1 次，got %d（前 %d）", got, before)
	}

	// 新增结论 → 指纹变化 → 重载，且新事件立即可见。
	p, err := forgedata.ProjectFor(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := act.Append(p, &act.Conclusion{
		TaskRef: "feat/new", Score: 70, Grade: "C", Strength: "Weak",
		CompletedAt: base.Add(7 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	res, err := AggregateFeed(Options{Root: root}, base.Add(8*time.Hour), FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if got := sharedPulseCache.loadCount(root); got != before+2 {
		t.Fatalf("源文件变更后应重载 1 次，总加载 got %d（前 %d）", got, before)
	}
	found := false
	for _, e := range res.Events {
		if e.TaskRef == "feat/new" && e.Kind == FeedKindConclusion {
			found = true
		}
	}
	if !found {
		t.Error("缓存失效后新结论必须立即可见（防过度缓存）")
	}
}

// TestPulseCache_TimeProjectionStaysFresh pins the cache-layer split: a cache HIT must
// still re-run the time-dependent projection. Zombie escalation is a pure function of
// `now` — a task turns zombie precisely when nothing changes on disk, so caching
// projected events would freeze it forever.
//
// TestPulseCache_TimeProjectionStaysFresh 钉住缓存分层：缓存命中仍须重算时间相关投影。
// 僵尸升级是 now 的纯函数——任务恰恰在盘上毫无变化时变僵尸，若缓存投影结果将永远冻结。
func TestPulseCache_TimeProjectionStaysFresh(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	now := time.Now()
	stalled := &taskpipeline.TaskState{TaskRef: "feat/stalled", Branch: "feat/stalled", StartedAt: now.Add(-9 * 24 * time.Hour)}
	if err := stalled.AssignTo("kimi", "backend", "claude-code"); err != nil {
		t.Fatal(err)
	}
	offered := now.Add(-8 * 24 * time.Hour)
	stalled.Assignment.OfferedAt = &offered
	if err := taskpipeline.SaveTaskState(root, stalled); err != nil {
		t.Fatal(err)
	}
	before := sharedPulseCache.loadCount(root)

	// offered+6d：未达僵尸阈值（7d）。
	res, err := AggregateFeed(Options{Root: root}, offered.Add(6*24*time.Hour), FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 1 || res.Events[0].Severity != FeedSeverityInfo {
		t.Fatalf("offered+6d 应为 info 进行中: %+v", res.Events)
	}

	// offered+8d：盘上零变化（缓存命中），投影必须升级为 warn。
	res, err = AggregateFeed(Options{Root: root}, offered.Add(8*24*time.Hour), FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if got := sharedPulseCache.loadCount(root); got != before+1 {
		t.Fatalf("两次聚合之间无文件变化，应只加载 1 次，got %d（前 %d）", got, before)
	}
	if len(res.Events) != 1 || res.Events[0].Severity != FeedSeverityWarn {
		t.Fatalf("缓存命中后投影未随 now 刷新——offered+8d 应为 warn 僵尸: %+v", res.Events)
	}
}

// TestSkillEvalCache_ReloadsOnlyOnChange pins the same contract for the per-skill eval
// cache: unchanged runs/baselines/decisions hit the cache; an appended run invalidates.
//
// TestSkillEvalCache_ReloadsOnlyOnChange 为单 skill eval 缓存钉住同一契约：
// runs/baselines/decisions 未变命中缓存；追加 run 触发失效。
func TestSkillEvalCache_ReloadsOnlyOnChange(t *testing.T) {
	evalDir := t.TempDir()
	base := time.Unix(1700000000, 0).UTC()
	run1 := skillseval.EvalRun{
		RunID: "run-1", Skill: "alpha", Timestamp: base,
		ForgeVersion: "v1", AgentModel: "m1", DescHash: "dh1",
		Results:     []skillseval.CaseResult{{CaseID: "t1", Kind: skillseval.KindTrigger, Pass: true}},
		HealthScore: 90,
	}
	writeEvalRuns(t, evalDir, "alpha", []skillseval.EvalRun{run1})

	key := "|" + evalDir + "|alpha" // canonical 为 "" 时的缓存键形态
	before := sharedPulseCache.loadCount(key)

	d, err := LoadSkillDetail("", evalDir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(d.Runs))
	}
	if _, err := LoadSkillDetail("", evalDir, "alpha"); err != nil {
		t.Fatal(err)
	}
	if got := sharedPulseCache.loadCount(key); got != before+1 {
		t.Fatalf("两次详情读取未变文件应只加载 1 次，got %d（前 %d）", got, before)
	}

	// 追加一条 run（文件重写，size 变化）→ 失效重载，新 run 立即可见。
	run2 := run1
	run2.RunID = "run-2"
	run2.Timestamp = base.Add(time.Hour)
	run2.HealthScore = 55
	writeEvalRuns(t, evalDir, "alpha", []skillseval.EvalRun{run1, run2})
	d, err = LoadSkillDetail("", evalDir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got := sharedPulseCache.loadCount(key); got != before+2 {
		t.Fatalf("runs 文件变更后应重载 1 次，总加载 got %d（前 %d）", got, before)
	}
	if len(d.Runs) != 2 || d.TriggerQuality == nil || d.TriggerQuality.FromRun != "run-2" {
		t.Errorf("失效后新 run 必须立即可见: %+v", d.Runs)
	}
}
