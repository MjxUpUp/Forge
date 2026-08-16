package skillseval

// weakness_test.go — AnalyzeWeaknesses 的测试：四簇信号 join（维度弱点频次过滤、
// 盲区率、从未触发交叉、低成效过滤）与数据 caveat 的覆盖诚实性。

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// recordConclusionWithDims appends a conclusion carrying LowDimensions (recordConclusion
// does not fill them; weakness mining reads them).
//
// recordConclusionWithDims 追加带 LowDimensions 的 conclusion（recordConclusion 不填
// 该字段；弱点挖掘读它）。
func recordConclusionWithDims(t *testing.T, p *forgedata.Project, taskRef string, score float64, strength string, ratio float64, lowDims []string) {
	t.Helper()
	mustWrite(t, act.Append(p, &act.Conclusion{
		TaskRef:       taskRef,
		Score:         score,
		Strength:      strength,
		Ratio:         ratio,
		CompletedAt:   fixedTime,
		LowDimensions: lowDims,
	}))
}

func TestAnalyzeWeaknesses_EmptyData(t *testing.T) {
	proj, _ := newTestProject(t)
	canonical := t.TempDir()
	makeCanonicalSkill(t, canonical, "alpha")
	makeCanonicalSkill(t, canonical, "beta")

	rep, err := AnalyzeWeaknesses(proj, canonical)
	if err != nil {
		t.Fatalf("AnalyzeWeaknesses: %v", err)
	}
	if rep.TotalTasks != 0 || len(rep.WeakDims) != 0 || len(rep.LowEffectiveness) != 0 {
		t.Errorf("空数据应零信号: %+v", rep)
	}
	// 全量 canonical 都从未触发（但 caveat 必须声明这不是证据）。
	if len(rep.NeverTriggered) != 2 {
		t.Errorf("NeverTriggered = %v, want [alpha beta]", rep.NeverTriggered)
	}
	joined := strings.Join(rep.DataCaveats, "\n")
	for _, want := range []string{"无任务结论数据", "无 skill 触达事件", "无 skill-任务成效关联"} {
		if !strings.Contains(joined, want) {
			t.Errorf("caveats 缺 %q: %v", want, rep.DataCaveats)
		}
	}
}

func TestAnalyzeWeaknesses_Clusters(t *testing.T) {
	proj, root := newTestProject(t)
	canonical := t.TempDir()
	makeCanonicalSkill(t, canonical, "good-skill")
	makeCanonicalSkill(t, canonical, "bad-skill")
	makeCanonicalSkill(t, canonical, "silent-skill")

	// 三个任务：t1/t2 低分维度 testing（复现 2 次 → 列出）；t3 低分维度 scope（1 次 → 噪声过滤）。
	// t2 为 Unverified（盲区 1/3）。
	//
	// Three tasks: t1/t2 low dim "testing" (recurs twice → listed); t3 low dim "scope"
	// (once → filtered as noise). t2 is Unverified (blind spot 1/3).
	pt := proj
	recordConclusionWithDims(t, pt, "t1", 90, "Strong", 1.0, []string{"testing"})
	recordConclusionWithDims(t, pt, "t2", 60, "Unverified", 0.2, []string{"testing"})
	recordConclusionWithDims(t, pt, "t3", 85, "Strong", 0.9, []string{"scope"})

	// good-skill 涉及两个高分强证据任务 → 不列；bad-skill 涉及 t2/t3（t2 弱证据）→ 弱占比 0.5 → 列出。
	//
	// good-skill touches two high-score strong tasks → not listed; bad-skill touches
	// t2/t3 (t2 weak) → weak rate 0.5 → listed.
	recordSkillCall(t, root, "good-skill", "t1")
	recordSkillCall(t, root, "good-skill", "t3")
	recordSkillCall(t, root, "bad-skill", "t2")
	recordSkillCall(t, root, "bad-skill", "t3")

	rep, err := AnalyzeWeaknesses(proj, canonical)
	if err != nil {
		t.Fatalf("AnalyzeWeaknesses: %v", err)
	}
	if rep.TotalTasks != 3 {
		t.Fatalf("TotalTasks = %d, want 3", rep.TotalTasks)
	}
	if len(rep.WeakDims) != 1 || rep.WeakDims[0].Dimension != "testing" || rep.WeakDims[0].Count != 2 {
		t.Errorf("WeakDims = %+v, want [testing×2]（scope 1 次应过滤）", rep.WeakDims)
	}
	if rep.BlindSpotCount != 1 || rep.BlindSpotRate < 0.33 || rep.BlindSpotRate > 0.34 {
		t.Errorf("盲区 = %d/%.2f, want 1/0.33", rep.BlindSpotCount, rep.BlindSpotRate)
	}
	if len(rep.NeverTriggered) != 1 || rep.NeverTriggered[0] != "silent-skill" {
		t.Errorf("NeverTriggered = %v, want [silent-skill]", rep.NeverTriggered)
	}
	if len(rep.LowEffectiveness) != 1 || rep.LowEffectiveness[0].Skill != "bad-skill" {
		t.Errorf("LowEffectiveness = %+v, want [bad-skill]（good-skill 高分强证据不列）", rep.LowEffectiveness)
	}
}

func TestAnalyzeWeaknesses_LowEffectFilters(t *testing.T) {
	proj, root := newTestProject(t)
	canonical := t.TempDir()
	makeCanonicalSkill(t, canonical, "one-shot")
	makeCanonicalSkill(t, canonical, "low-score")

	// one-shot 只涉及 1 个任务（全弱也不列——n=1 无证明力）；low-score 涉及 2 个任务、
	// 弱占比 0 但均分 65 <70 → 经分数分支列出。
	//
	// one-shot touches 1 task (even all-weak not listed — n=1 proves nothing); low-score
	// touches 2 tasks, weak rate 0 but avg 65 <70 → listed via the score branch.
	recordConclusionWithDims(t, proj, "t1", 30, "Weak", 0.1, nil)
	recordConclusionWithDims(t, proj, "t2", 65, "Strong", 0.9, nil)
	recordConclusionWithDims(t, proj, "t3", 65, "Strong", 0.9, nil)
	recordSkillCall(t, root, "one-shot", "t1")
	recordSkillCall(t, root, "low-score", "t2")
	recordSkillCall(t, root, "low-score", "t3")

	rep, err := AnalyzeWeaknesses(proj, canonical)
	if err != nil {
		t.Fatalf("AnalyzeWeaknesses: %v", err)
	}
	if len(rep.LowEffectiveness) != 1 || rep.LowEffectiveness[0].Skill != "low-score" {
		t.Errorf("LowEffectiveness = %+v, want [low-score]（one-shot n=1 应过滤）", rep.LowEffectiveness)
	}
}
