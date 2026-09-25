package skillmetrics

import "testing"

// TestCompareTriggerSets_Detected replays the 2026-08-16 incident shape: prod 15, repo 33; the repo-only names are exactly MissingInProd — v1.32.0 was built 8h46m before the trigger-top-up commit.
//
// TestCompareTriggerSets_Detected 2026-08-16 现场重放：生产 15、仓库 33，仓库独有的
// 18 个就是 MissingInProd——v1.32.0 build 早于 triggers 提交 8h46m 的事故形状。
func TestCompareTriggerSets_Detected(t *testing.T) {
	prod := map[string]bool{"a": true, "b": true}
	repo := map[string]bool{"a": true, "b": true, "c": true, "d": true}
	d := CompareTriggerSets(prod, repo)
	if !d.RepoCompared || !d.HasDrift() {
		t.Fatalf("应检出漂移: %+v", d)
	}
	if d.ProdDeclared != 2 || d.RepoDeclared != 4 {
		t.Errorf("两侧计数错: prod=%d repo=%d, want 2/4", d.ProdDeclared, d.RepoDeclared)
	}
	if len(d.MissingInProd) != 2 || d.MissingInProd[0] != "c" || d.MissingInProd[1] != "d" {
		t.Errorf("MissingInProd 应排序为 [c d], got %v", d.MissingInProd)
	}
}

// TestCompareTriggerSets_NoDrift 两侧一致 → RepoCompared 但无漂移。
// TestCompareTriggerSets_NoDrift: identical sets → compared, no drift.
func TestCompareTriggerSets_NoDrift(t *testing.T) {
	set := map[string]bool{"a": true}
	if d := CompareTriggerSets(set, set); d.HasDrift() || len(d.MissingInProd) != 0 {
		t.Errorf("一致集不应报漂移: %+v", d)
	}
}

// TestCompareTriggerSets_RepoNilNotCompared pins that repo=nil means RepoCompared=false.
//
// TestCompareTriggerSets_RepoNilNotCompared repo=nil（非仓库内运行/无 skills/ 目录）→
// RepoCompared=false，MissingInProd 必须为空且不得解读为无漂移——HasDrift 恒 false，
// 渲染层因此走「不可比较」话术而非「一致」。
func TestCompareTriggerSets_RepoNilNotCompared(t *testing.T) {
	d := CompareTriggerSets(map[string]bool{"a": true}, nil)
	if d.RepoCompared || d.HasDrift() || len(d.MissingInProd) != 0 {
		t.Errorf("repo=nil 应不可比较且无 MissingInProd: %+v", d)
	}
	if d.ProdDeclared != 1 {
		t.Errorf("ProdDeclared 应仍有效=1, got %d", d.ProdDeclared)
	}
}

// TestCompareTriggerSets_ProdSuperset: prod being a superset of repo (post-release repo rollback / skill deletion) is NOT drift — drift only answers "did repo-declared triggers make it into production".
//
// TestCompareTriggerSets_ProdSuperset 生产是仓库超集（发版后仓库回退/删 skill）不算漂移
// ——漂移只回答「仓库声明的判定是否进了生产」。
func TestCompareTriggerSets_ProdSuperset(t *testing.T) {
	d := CompareTriggerSets(map[string]bool{"a": true, "ghost": true}, map[string]bool{"a": true})
	if d.HasDrift() {
		t.Errorf("生产超集不算漂移: %+v", d)
	}
}
