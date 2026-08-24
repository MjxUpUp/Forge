package cli

// skills_decide_test.go — pins the embed-cache write guard on `skills decide`:
// canonical resolving to the embed extraction cache must FAIL LOUDLY (it is a
// regenerated distribution snapshot — version ping-pong between two forge binaries
// wipes it on every foreign-version invocation, silently destroying decisions that
// were reported as ✅-recorded), while an explicit external canonical passes.
//
// skills_decide_test.go — 钉死 `skills decide` 的 embed 缓存写入守卫：canonical 解析到
// embed 解压缓存必须响亮失败（它是可再生成的分发快照——两个 forge 二进制版本交替运行时
// 每次异版调用都会抹掉它，静默销毁已报 ✅ 记录的决策），显式外部 canonical 则放行。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunSkillsDecide_RejectsEmbedCache verifies the guard fires when canonical resolves
// to the embed cache (no env/flag override): the command must error with the remedy
// (point at a real source), not append into a snapshot that the next foreign-version
// forge invocation will RemoveAll.
//
// TestRunSkillsDecide_RejectsEmbedCache 钉死守卫：canonical 解析到 embed 缓存（无
// env/flag 覆盖）时报错并给出补救指引（指向真实源），而不是把追加写进一个下一次异版
// forge 调用就会被 RemoveAll 的快照。
func TestRunSkillsDecide_RejectsEmbedCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("FORGE_SKILLS_CANONICAL", "")

	// Unique sentinel: the guard must fire BEFORE any append, so this string must not
	// appear in ANY file under the isolated home (the embed extraction itself contains
	// real decisions.md files with generic headers — a sentinel avoids colliding with
	// extracted content, the flaw of the earlier header-based check).
	//
	// 唯一哨兵：守卫必须在任何追加之前触发，故该字符串不得出现在隔离 home 下的任何
	// 文件里（embed 解压本身就带含通用头部的真实 decisions.md——哨兵避免与解压内容
	// 碰撞，这正是早先基于头部断言的缺陷）。
	const sentinel = "zz-embed-cache-guard-probe-diagnosis"
	skDecSkill = "demo"
	skDecDiagnosis = sentinel
	skDecRevision = "r"
	skDecEvidence = "e"
	skDecOutcome = "accept"
	defer func() {
		skDecSkill = ""
		skDecDiagnosis = ""
		skDecRevision = ""
		skDecEvidence = ""
		skDecOutcome = ""
	}()

	err := runSkillsDecide(nil, nil)
	if err == nil {
		t.Fatal("decide against the embed cache must fail loudly, got nil error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "embed") || !strings.Contains(msg, "FORGE_SKILLS_CANONICAL") {
		t.Fatalf("error must name the embed-cache cause and the FORGE_SKILLS_CANONICAL remedy, got: %s", msg)
	}
	// Nothing may have been appended anywhere under the isolated home — the guard fires
	// before any mutation (Resolve may have extracted the snapshot; that is read-side
	// cache population, not a decision write).
	//
	// 隔离 home 下不得有任何决策被追加——守卫在任何变更之前触发（Resolve 可能已解压
	// 快照；那是读侧缓存填充，不是决策写入）。
	var leaked []string
	filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr == nil && strings.Contains(string(data), sentinel) {
			leaked = append(leaked, path)
		}
		return nil
	})
	if len(leaked) > 0 {
		t.Fatalf("guard must fire before append; sentinel leaked into: %v", leaked)
	}
}

// TestRunSkillsDecide_AcceptsExternalCanonical is the pass-through control: with an
// explicit external canonical the guard stays silent and the append lands.
//
// TestRunSkillsDecide_AcceptsExternalCanonical 是放行对照：显式外部 canonical 下守卫
// 静默、追加照常落盘。
func TestRunSkillsDecide_AcceptsExternalCanonical(t *testing.T) {
	canonical := t.TempDir()
	skillDir := filepath.Join(canonical, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORGE_SKILLS_CANONICAL", canonical)

	skDecSkill = "demo"
	skDecDiagnosis = "guard-control"
	skDecRevision = "r"
	skDecEvidence = "e"
	skDecOutcome = "accept"
	defer func() {
		skDecSkill = ""
		skDecDiagnosis = ""
		skDecRevision = ""
		skDecEvidence = ""
		skDecOutcome = ""
	}()

	if err := runSkillsDecide(nil, nil); err != nil {
		t.Fatalf("decide with external canonical must succeed, got: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "decisions.md"))
	if err != nil {
		t.Fatalf("decisions.md missing after successful decide: %v", err)
	}
	if !strings.Contains(string(data), "guard-control") {
		t.Fatal("decision text must be appended to the external canonical decisions.md")
	}
}
