package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRepoTriggerSet 三态：有 triggers 的 skills/ → 名集；零 triggers 的 skills/ → nil
// （审查 LOW-1：恰好有 skills/ 目录的非 Forge 项目不得渲染假「一致」）；无 skills/ → nil。
//
// TestRepoTriggerSet three states: skills/ with triggers → name set; skills/ with zero
// triggers → nil (review LOW-1: a non-Forge project that happens to carry skills/ must not
// render a fake "consistent"); no skills/ → nil.
func TestRepoTriggerSet(t *testing.T) {
	root := t.TempDir()

	// 无 skills/ 目录 → 不可比较。
	// No skills/ dir → not comparable.
	if rs := repoTriggerSet(root); rs != nil {
		t.Errorf("无 skills/ 应返 nil, got %v", rs)
	}

	// 零 triggers 的 skills/（仅一个无 triggers 声明的 SKILL.md）→ 同样不可比较。
	// skills/ with zero triggers (one SKILL.md without declarations) → also not comparable.
	skillsDir := filepath.Join(root, "skills")
	os.MkdirAll(filepath.Join(skillsDir, "no-meta"), 0755)
	os.WriteFile(filepath.Join(skillsDir, "no-meta", "SKILL.md"), []byte("---\nname: no-meta\n---\n"), 0644)
	if rs := repoTriggerSet(root); rs != nil {
		t.Errorf("零 triggers 的 skills/ 应返 nil（假一致守卫）, got %v", rs)
	}

	// 有 triggers 声明 → 返回名集。
	// With trigger declarations → the name set.
	writeSkill(t, skillsDir, "td", `[{"event":"Stop"}]`)
	rs := repoTriggerSet(root)
	if rs == nil || !rs["td"] || len(rs) != 1 {
		t.Errorf("应返回 {td: true}, got %v", rs)
	}

	// skills/ 零 triggers 但 skills-forge/ 有（2026-08 迁移后的并集扫描）：可比且
	// forge 原生声明计入——不并集的话这些 skill 会渲染假漂移行（review W7）。
	//
	// skills/ zero triggers but skills-forge/ has some (union scan since the
	// 2026-08 migration): comparable and forge-native declarations counted —
	// without the union those skills render fake drift lines (review W7).
	writeSkill(t, filepath.Join(root, "skills-forge"), "skill-evolution", `[{"event":"UserPromptSubmit"}]`)
	rs = repoTriggerSet(root)
	if rs == nil || !rs["skill-evolution"] {
		t.Errorf("skills-forge/ 的 triggers 声明应计入并集, got %v", rs)
	}
}
