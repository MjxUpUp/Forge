package skillseval

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// newTestProject constructs a Project with GitRoot=non-git tempdir so act.DataDir and
// toolusage.DataDirFor(GitRoot) resolve to the same <root>/.forge/ — act conclusions and toollog coexist,
// enabling closed-loop verification of AnalyzeEffectiveness's two-source (TaskRef) join.
//
// newTestProject 构造一个 GitRoot=非 git tempdir 的 Project，让 act.DataDir 与
// toolusage.DataDirFor(GitRoot) 解析到同一 <root>/.forge/——act 结论与 toollog 同处，
// AnalyzeEffectiveness 的两源连接（TaskRef）可闭环验证。
func newTestProject(t *testing.T) (*forgedata.Project, string) {
	t.Helper()
	root := t.TempDir()
	return &forgedata.Project{
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
	recordSkillCall(t, root, "s", "t3") // t3 无 conclusion：计入 TaskCount 但不计 avg 分母
	recordConclusion(t, proj, "t1", 80, "Strong", 0.8)
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
	if e.HitCount != 3 {
		t.Fatalf("HitCount=%d want 3（每次调用都累加）", e.HitCount)
	}
	if e.TaskCount != 2 {
		t.Fatalf("TaskCount=%d want 2（t1+t3，去重）", e.TaskCount)
	}
	if e.AvgScore != 80 {
		t.Fatalf("AvgScore=%v want 80（只 t1 有 conclusion，分母=1）", e.AvgScore)
	}
	if e.AvgRatio != 0.8 {
		t.Fatalf("AvgRatio=%v want 0.8", e.AvgRatio)
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
