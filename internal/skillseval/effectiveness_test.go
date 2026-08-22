package skillseval

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestMain isolates the package's data home: every store write in these tests goes
// through forgedata.DataDirFor, which is ALWAYS user-level (~/.forge/projects/<key>/)
// — without isolation each `go test` run spams garbage p* project dirs into the real
// home (measured: thousands accumulated before this). Per-test isolation is
// unnecessary: project keys derive from each test's unique t.TempDir root, so tests
// never collide inside the shared isolated home.
//
// TestMain 隔离本包测试的 data home：这些测试的所有 store 写入都走
// forgedata.DataDirFor——恒为用户级（~/.forge/projects/<key>/），不隔离的话每次
// `go test` 都向真实 home 倾倒垃圾 p* 项目目录（实测：修复前已累计数千个）。
// 无需 per-test 隔离：项目 key 由各测试唯一的 t.TempDir root 派生，共享的隔离
// home 内测试间互不碰撞。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "skillseval-home")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	os.Setenv("FORGE_DATA_HOME", dir)
	os.Exit(m.Run())
}

// newTestProject constructs a Project whose Root==GitRoot==fresh tempdir: DataDirFor(root)
// finds no .git to key on, so it lands on PathKey — the SAME user-level dir for act
// conclusions (p.DataDir) and both LoadAllAll hit sources, enabling closed-loop
// verification of AnalyzeEffectiveness's two-source (TaskRef) join. The non-git shape
// (GitRoot=="", Root=path) is pinned separately by NonGitRootPin below. The data home
// itself is isolated package-wide by TestMain.
//
// newTestProject 构造 Root==GitRoot==新 tempdir 的 Project：DataDirFor(root) 无 .git
// 可 key，落到 PathKey——act 结论（p.DataDir）与两个 LoadAllAll 命中源解析到同一个
// 用户级目录，AnalyzeEffectiveness 的两源（TaskRef）join 可闭环验证。非 git 形态
// （GitRoot==""、Root=路径）由下方 NonGitRootPin 单独钉死。data home 本身由
// TestMain 做包级隔离。
func newTestProject(t *testing.T) (*forgedata.Project, string) {
	t.Helper()
	root := t.TempDir()
	return &forgedata.Project{
		Root:      root,
		GitRoot:   root,
		DataDir:   forgedata.DataDirFor(root),
		ConfigDir: filepath.Join(root, ".forge"),
	}, root
}

var fixedTime = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func recordConclusion(t *testing.T, p *forgedata.Project, taskRef string, score float64, strength string, ratio float64) {
	t.Helper()
	mustWrite(t, act.Append(p, &act.Conclusion{
		TaskRef:     taskRef,
		Score:       score,
		Strength:    strength,
		Ratio:       ratio,
		CompletedAt: fixedTime,
	}))
}

// TestAnalyzeEffectiveness basic association: two skills each touch one task with conclusion.
// Verifies hit×effectiveness aggregation + weak-evidence ratio.
//
// TestAnalyzeEffectiveness 基础关联：两个 skill 各涉及一个 task，task 有 conclusion。
// 验证 hit×成效聚合 + 弱证据占比。
func TestAnalyzeEffectiveness(t *testing.T) {
	proj, root := newTestProject(t)
	recordSkillCall(t, root, "good-skill", "t1")
	recordSkillCall(t, root, "weak-skill", "t2")
	recordConclusion(t, proj, "t1", 90, "Strong", 1.0)
	recordConclusion(t, proj, "t2", 60, "Weak", 0.2)

	effs, err := AnalyzeEffectiveness(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(effs) != 2 {
		t.Fatalf("len=%d want 2: %+v", len(effs), effs)
	}
	// Same HitCount=1, sort by Skill alphabetical order: good-skill < weak-skill
	//
	// 同 HitCount=1，按 Skill 字母序：good-skill < weak-skill
	good, weak := effs[0], effs[1]
	if good.Skill != "good-skill" || weak.Skill != "weak-skill" {
		t.Fatalf("order/skill mismatch: %+v", effs)
	}
	if good.HitCount != 1 || good.TaskCount != 1 || good.AvgScore != 90 || good.AvgRatio != 1.0 || good.WeakRate != 0 {
		t.Fatalf("good-skill wrong: %+v", good)
	}
	if weak.WeakRate != 1.0 {
		t.Fatalf("weak-skill WeakRate=%v want 1.0（Strength=Weak）", weak.WeakRate)
	}
	if weak.AvgScore != 60 || weak.AvgRatio != 0.2 {
		t.Fatalf("weak-skill avg wrong: %+v", weak)
	}
}

// TestAnalyzeEffectiveness_TaskDedup same skill same task multiple calls: hits accumulate, but effectiveness counts once
// (prevents same-task multiple calls from inflating weight). Tasks without conclusion are excluded from avg denominator.
//
// TestAnalyzeEffectiveness_TaskDedup 同 skill 同 task 多次调用：hits 累加，但成效只累加一次
// （防同 task 多次调用放大权重）。无 conclusion 的 task 不计入 avg 分母。
func TestAnalyzeEffectiveness_TaskDedup(t *testing.T) {
	proj, root := newTestProject(t)
	recordSkillCall(t, root, "s", "t1")
	recordSkillCall(t, root, "s", "t1") // 同 task 重复：hits++，task 去重
	recordSkillCall(t, root, "s", "t2") // t2 带不同分数：去重断言的判别锚——若 t1 被双折，AvgScore=(80+80+60)/3≈73.3 而非 70
	recordSkillCall(t, root, "s", "t3") // t3 无 conclusion：计入 TaskCount 但不计 avg 分母
	recordConclusion(t, proj, "t1", 80, "Strong", 0.8)
	recordConclusion(t, proj, "t2", 60, "Strong", 0.6)
	// t3 intentionally has no conclusion
	//
	// t3 故意无 conclusion

	effs, err := AnalyzeEffectiveness(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(effs) != 1 {
		t.Fatalf("len=%d want 1", len(effs))
	}
	e := effs[0]
	if e.HitCount != 4 {
		t.Fatalf("HitCount=%d want 4（每次调用都累加）", e.HitCount)
	}
	if e.TaskCount != 3 {
		t.Fatalf("TaskCount=%d want 3（t1+t2+t3，去重）", e.TaskCount)
	}
	if e.AvgScore != 70 {
		t.Fatalf("AvgScore=%v want 70（t1 只折一次：(80+60)/2；双折=(80+80+60)/3≈73.3）", e.AvgScore)
	}
	if e.AvgRatio != 0.7 {
		t.Fatalf("AvgRatio=%v want 0.7（(0.8+0.6)/2）", e.AvgRatio)
	}
}

// TestAnalyzeEffectiveness_Empty no toollog and no conclusion: returns empty slice without error
// (agent-neutral principle: evaluation system still works when data is missing).
//
// TestAnalyzeEffectiveness_Empty 无 toollog 无 conclusion：返回空切片不报错
// （agent-neutral 原则：数据缺失时评估体系仍工作）。
func TestAnalyzeEffectiveness_Empty(t *testing.T) {
	proj, _ := newTestProject(t)
	effs, err := AnalyzeEffectiveness(proj)
	if err != nil {
		t.Fatalf("空数据应无错: %v", err)
	}
	if len(effs) != 0 {
		t.Fatalf("want empty, got %+v", effs)
	}
}

// TestAnalyzeEffectiveness_WeakIncludesUnverified Strength=Unverified also counts as weak evidence
// (consistent with act.RetrospectiveNudge criteria).
//
// TestAnalyzeEffectiveness_WeakIncludesUnverified Strength=Unverified 也算弱证据
// （与 act.RetrospectiveNudge 判据一致）。
func TestAnalyzeEffectiveness_WeakIncludesUnverified(t *testing.T) {
	proj, root := newTestProject(t)
	recordSkillCall(t, root, "s", "t1")
	recordConclusion(t, proj, "t1", 95, "Unverified", 0.0)

	effs, err := AnalyzeEffectiveness(proj)
	if err != nil {
		t.Fatal(err)
	}
	if effs[0].WeakRate != 1.0 {
		t.Fatalf("Unverified 应计入弱占比，WeakRate=%v want 1.0", effs[0].WeakRate)
	}
}

// TestAnalyzeEffectiveness_UnscoredTaskExcluded: Score==0 (unscored, act.BuildConclusion
// score==nil sentinel) is excluded from AvgScore denominator — otherwise artificially lowers avg. But its evidence strength still counts in
// AvgRatio/WeakRate denominator (ratio/evidence independent from score).
//
// TestAnalyzeEffectiveness_UnscoredTaskExcluded：Score==0（未评分，act.BuildConclusion
// score==nil 哨兵值）不计入 AvgScore 分母——否则人为拉低 avg。但其证据强度仍计入
// AvgRatio/WeakRate 分母（ratio/证据与 score 独立）。
func TestAnalyzeEffectiveness_UnscoredTaskExcluded(t *testing.T) {
	proj, root := newTestProject(t)
	recordSkillCall(t, root, "s", "t1")
	recordSkillCall(t, root, "s", "t2")
	recordConclusion(t, proj, "t1", 0, "Strong", 0.9) // 未评分（Score=0 哨兵）
	recordConclusion(t, proj, "t2", 90, "Strong", 1.0)

	effs, err := AnalyzeEffectiveness(proj)
	if err != nil {
		t.Fatal(err)
	}
	e := effs[0]
	if e.AvgScore != 90 {
		t.Fatalf("AvgScore=%v want 90（t1 Score=0 不计入分母，非 (0+90)/2=45）", e.AvgScore)
	}
	if e.AvgRatio != 0.95 { // (0.9+1.0)/2，两个 conclusion 都计入
		t.Fatalf("AvgRatio=%v want 0.95（ratio 与 score 独立，未评分 task 仍计入）", e.AvgRatio)
	}
}

// TestAnalyzeEffectiveness_NoDataIsWeak: NoData (zero real-run evidence) counts as weak ratio — effectiveness
// context is exposing blind spots, NoData is blinder than Weak. Different from RetrospectiveNudge criteria (Nudge only
// triggers retrospective on Weak/Unverified).
//
// TestAnalyzeEffectiveness_NoDataIsWeak：NoData（零实跑证据）算入弱占比——effectiveness
// 语境是暴露盲区，NoData 比 Weak 更盲。与 RetrospectiveNudge 判据不同（Nudge 只
// Weak/Unverified 触发回顾）。
func TestAnalyzeEffectiveness_NoDataIsWeak(t *testing.T) {
	proj, root := newTestProject(t)
	recordSkillCall(t, root, "s", "t1")
	recordConclusion(t, proj, "t1", 95, "NoData", 0.0)

	effs, err := AnalyzeEffectiveness(proj)
	if err != nil {
		t.Fatal(err)
	}
	if effs[0].WeakRate != 1.0 {
		t.Fatalf("NoData 应计入弱占比，WeakRate=%v want 1.0", effs[0].WeakRate)
	}
}

// recordTrigger persists one CheckSkillTrigger entry — the passive source the 2026-08-22
// join added. Most skills are hit via the skill-trigger hook and never via an active
// Skill tool call; before the join those skills were absent from effectiveness entirely
// (the "panel shows hit counts only, quality columns empty" root cause).
//
// recordTrigger 落一条 CheckSkillTrigger 条目——2026-08-22 join 新增的被动源。多数
// skill 经 skill-trigger hook 命中、从未有主动 Skill 工具调用；join 前这些 skill 在
// effectiveness 里整体缺席（「面板只显示命中数、质量列全空」的根因）。
func recordTrigger(t *testing.T, root, skill, taskRef string) {
	t.Helper()
	mustWrite(t, checklog.Record(root, &checklog.Entry{
		Check:      checklog.CheckSkillTrigger,
		Passed:     true,
		Checked:    true,
		TaskRef:    taskRef,
		Detail:     checklog.DetailForSkillTrigger(skill, "UserPromptSubmit", "keywords"),
		RecordedAt: fixedTime,
	}))
}

// TestAnalyzeEffectiveness_PassiveTriggerJoin pins the passive join: a skill hit ONLY
// via a CheckSkillTrigger entry (no active Skill call anywhere) must appear in the
// report with its hit counted and the task's conclusion folded in. Entries without a
// TaskRef still count as hits but attach no outcome (TaskCount 0 → avg denominators 0).
//
// TestAnalyzeEffectiveness_PassiveTriggerJoin 钉死被动 join：只经 CheckSkillTrigger
// 条目命中（全程无主动 Skill 调用）的 skill 必须出现在报告里，命中被计数、task 的
// conclusion 被折入。无 TaskRef 的条目仍计命中但不挂成效（TaskCount 0 → avg 分母 0）。
func TestAnalyzeEffectiveness_PassiveTriggerJoin(t *testing.T) {
	proj, root := newTestProject(t)
	recordTrigger(t, root, "passive-skill", "t1")
	recordTrigger(t, root, "no-task-skill", "") // 无 TaskRef：只计命中
	recordConclusion(t, proj, "t1", 85, "Strong", 0.7)

	effs, err := AnalyzeEffectiveness(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(effs) != 2 {
		t.Fatalf("len=%d want 2（被动源必须独立产出条目）: %+v", len(effs), effs)
	}
	// HitCount 同为 1，按字母序 no-task-skill < passive-skill。
	nt, ps := effs[0], effs[1]
	if nt.Skill != "no-task-skill" || ps.Skill != "passive-skill" {
		t.Fatalf("order/skill mismatch: %+v", effs)
	}
	if ps.HitCount != 1 || ps.TaskCount != 1 || ps.AvgScore != 85 || ps.AvgRatio != 0.7 || ps.WeakRate != 0 {
		t.Fatalf("passive-skill 应完整挂上 t1 成效: %+v", ps)
	}
	if nt.HitCount != 1 || nt.TaskCount != 0 || nt.AvgScore != 0 || nt.AvgRatio != 0 {
		t.Fatalf("no-task-skill 只计命中不挂成效: %+v", nt)
	}
}

// TestAnalyzeEffectiveness_PassiveActiveDedup pins the cross-source dedup: the SAME
// (skill, task) hit both passively (trigger entry) and actively (Skill call) counts
// HitCount=2 but folds the outcome ONCE — the aggregator's per-(skill, task) dedup
// spans both sources, so a passive trigger followed by an active load in one task does
// not double the task's weight.
//
// TestAnalyzeEffectiveness_PassiveActiveDedup 钉死跨源去重：同一 (skill, task) 既被
// 动（触发条目）又主动（Skill 调用）命中时 HitCount=2 但成效只折入一次——聚合器的
// per-(skill, task) 去重横跨两源，同 task 内被动触发+主动加载不会让该 task 权重翻倍。
func TestAnalyzeEffectiveness_PassiveActiveDedup(t *testing.T) {
	proj, root := newTestProject(t)
	recordTrigger(t, root, "s", "t1")
	recordSkillCall(t, root, "s", "t1")
	recordSkillCall(t, root, "s", "t2") // t2 带不同分数：判别锚——单一 conclusion task 双折时分子分母同乘（80/1==160/2），断言恒过；异分才锁得住 fold-once
	recordConclusion(t, proj, "t1", 80, "Strong", 0.8)
	recordConclusion(t, proj, "t2", 60, "Strong", 0.6)

	effs, err := AnalyzeEffectiveness(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(effs) != 1 {
		t.Fatalf("len=%d want 1: %+v", len(effs), effs)
	}
	e := effs[0]
	if e.HitCount != 3 {
		t.Fatalf("HitCount=%d want 3（被动+主动都累加）", e.HitCount)
	}
	if e.TaskCount != 2 {
		t.Fatalf("TaskCount=%d want 2（跨源去重）", e.TaskCount)
	}
	if e.AvgScore != 70 || e.AvgRatio != 0.7 {
		t.Fatalf("成效只折入一次（fold-once：(80+60)/2=70；t1 双折则 ≈73.3）: %+v", e)
	}
}

// TestAnalyzeEffectiveness_NonGitRootPin pins the data-dir resolution for non-git
// projects (review M-1): a non-git Project has GitRoot=="" and Root=<registered
// path>. Both hit sources must resolve via Root — resolving via GitRoot would send
// DataDirFor("") to the process CWD's git repo (here: the Forge repo's key under the
// TestMain-isolated home → no logs), silently reading another project's data and
// blanking the join. Fixture mirrors forgedata.ProjectFor's non-git branch.
//
// TestAnalyzeEffectiveness_NonGitRootPin 钉死非 git 项目的数据目录解析（审查 M-1）：
// 非 git Project 的 GitRoot==""、Root=<注册路径>。两个命中源必须经 Root 解析——若经
// GitRoot，DataDirFor("") 会落到进程 CWD 所在 git 仓库（此处：TestMain 隔离 home 下
// 的 Forge 仓库 key → 无日志），静默读到别的项目的数据、join 全空。fixture 对齐
// forgedata.ProjectFor 的非 git 分支。
func TestAnalyzeEffectiveness_NonGitRootPin(t *testing.T) {
	root := t.TempDir()
	proj := &forgedata.Project{
		Root:      root,
		GitRoot:   "",
		DataDir:   forgedata.DataDirFor(root),
		ConfigDir: filepath.Join(root, ".forge"),
	}
	recordTrigger(t, root, "s", "t1")
	recordConclusion(t, proj, "t1", 85, "Strong", 0.7)

	effs, err := AnalyzeEffectiveness(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(effs) != 1 || effs[0].Skill != "s" {
		t.Fatalf("非 git 项目经 Root 解析必须读到被动源: %+v", effs)
	}
	if e := effs[0]; e.HitCount != 1 || e.TaskCount != 1 || e.AvgScore != 85 || e.AvgRatio != 0.7 {
		t.Fatalf("join 应完整挂上 t1 成效: %+v", e)
	}
}
