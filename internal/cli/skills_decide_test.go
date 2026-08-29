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

// setSkDecVars pins the `skills decide` package vars for one in-process
// invocation and restores them when the test ends — the vars are flag-bound
// global state, so without the reset a leak couples later tests. revision/
// evidence/outcome are the constants every site here uses.
//
// setSkDecVars 为一次进程内调用钉住 `skills decide` 的包级变量，测试结束时
// 复位——这些变量是 flag 绑定的全局状态，不复位则泄漏耦合后续测试。
// revision/evidence/outcome 取本文件各用例共用的常量。
func setSkDecVars(t *testing.T, skill, diagnosis string) {
	t.Helper()
	skDecSkill = skill
	skDecDiagnosis = diagnosis
	skDecRevision = "r"
	skDecEvidence = "e"
	skDecOutcome = "accept"
	t.Cleanup(func() {
		skDecSkill = ""
		skDecDiagnosis = ""
		skDecRevision = ""
		skDecEvidence = ""
		skDecOutcome = ""
	})
}

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
	setSkDecVars(t, "demo", sentinel)

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

// TestRunSkillsDecide_PositionalSkillArg pins the positional shorthand for --skill
// (usage-log fix: `forge skills decide subagent-orchestration --diagnosis ...` errored
// "需要 --skill NAME" although the intent was unambiguous). A bare positional argument
// must be accepted exactly as --skill would be.
//
// TestRunSkillsDecide_PositionalSkillArg 钉住 --skill 的位置参数简写（usage 日志修复：
// `forge skills decide subagent-orchestration --diagnosis ...` 报「需要 --skill NAME」，
// 但意图本无歧义）。裸位置参数必须与 --skill 等价生效。
func TestRunSkillsDecide_PositionalSkillArg(t *testing.T) {
	canonical := t.TempDir()
	skillDir := filepath.Join(canonical, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORGE_SKILLS_CANONICAL", canonical)
	setSkDecVars(t, "", "positional-probe")

	if err := runSkillsDecide(nil, []string{"demo"}); err != nil {
		t.Fatalf("decide with positional skill arg must succeed, got: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "decisions.md"))
	if err != nil {
		t.Fatalf("decisions.md missing after positional decide: %v", err)
	}
	if !strings.Contains(string(data), "positional-probe") {
		t.Fatal("decision text must be appended when the skill is given positionally")
	}

	// Positional == --skill (same name): consistent, accepted.
	//
	// 位置参数与 --skill 同名：一致，放行。
	setSkDecVars(t, "demo", "positional-consistent")
	if err := runSkillsDecide(nil, []string{"demo"}); err != nil {
		t.Fatalf("positional == --skill must be accepted, got: %v", err)
	}
}

// TestRunSkillsDecide_PositionalConflictsFlag pins the ambiguity guard: a positional
// skill name that disagrees with --skill must error (loudly) instead of silently
// picking one — the two spellings naming different skills is always a user mistake.
//
// TestRunSkillsDecide_PositionalConflictsFlag 钉住歧义守卫：位置参数与 --skill 不一致
// 必须（响亮）报错而非静默二选一——两种拼写指向不同 skill 必是用户笔误。
func TestRunSkillsDecide_PositionalConflictsFlag(t *testing.T) {
	t.Setenv("FORGE_SKILLS_CANONICAL", t.TempDir())
	setSkDecVars(t, "flag-skill", "d")

	err := runSkillsDecide(nil, []string{"other-skill"})
	if err == nil || !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("positional/--skill conflict must error naming the conflict, got: %v", err)
	}
}

// TestRunSkillsDecide_InRepoCanonicalDefault pins the in-repo default (usage-log fix:
// inside a skills-bearing checkout — the Forge repo itself — decide previously
// resolved to the embed cache at ~/.forge/skills-cache/embedded and either failed or,
// worse, older builds wrote there and lost the entries). With no env/flag override,
// running inside a project whose root has skills/CONVENTIONS.md must default to that
// canonical tree.
//
// TestRunSkillsDecide_InRepoCanonicalDefault 钉住仓库内默认（usage 日志修复：在带
// skills 树的 checkout——Forge 本仓——里 decide 之前解析到 ~/.forge/skills-cache/
// embedded 的 embed 缓存，要么报错、要么（更糟）旧版本写进去丢条目）。无 env/flag
// 覆盖时，在项目根带 skills/CONVENTIONS.md 的项目内运行必须默认写该 canonical 树。
func TestRunSkillsDecide_InRepoCanonicalDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("FORGE_DATA_HOME", filepath.Join(home, ".forge"))
	t.Setenv("FORGE_SKILLS_CANONICAL", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "decide-inrepo")

	dir := t.TempDir()
	if stdout, _, code := runForge(t, dir, "init", "--mode", "medium"); code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}
	// Minimal canonical tree marker + one skill — the same CONVENTIONS.md marker
	// EnsureEmbeddedCache uses to recognize a canonical tree.
	//
	// 最小 canonical 树标记 + 一个 skill——EnsureEmbeddedCache 识别 canonical 树用的
	// 同一 CONVENTIONS.md 标记。
	skillDir := filepath.Join(dir, "skills", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills", "CONVENTIONS.md"), []byte("# conventions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, code := runForge(t, dir, "skills", "decide", "demo",
		"--diagnosis", "in-repo-default-probe", "--revision", "r", "--evidence", "e", "--outcome", "accept")
	if code != 0 {
		t.Fatalf("in-repo decide must default to ./skills and succeed, exit %d: %s", code, out)
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "decisions.md"))
	if err != nil {
		t.Fatalf("decision must land in the repo canonical tree: %v", err)
	}
	if !strings.Contains(string(data), "in-repo-default-probe") {
		t.Fatal("decision text must be appended to the repo's ./skills tree")
	}
	// The embed cache under the isolated HOME must NOT carry the decision — the
	// in-repo default kept the write away from the version-ping-pong snapshot.
	//
	// 隔离 HOME 下的 embed 缓存不得带有该决策——仓库内默认把写入挡在了版本 ping-pong
	// 快照之外。
	var leaked []string
	filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if body, rerr := os.ReadFile(path); rerr == nil && strings.Contains(string(body), "in-repo-default-probe") {
			leaked = append(leaked, path)
		}
		return nil
	})
	if len(leaked) > 0 {
		t.Fatalf("decision leaked into the embed cache instead of the repo tree: %v", leaked)
	}
}

// TestRunSkillsDecide_NoSkillsTreeStillFailsLoud is the negative path of the in-repo
// default: a forge-initialized project WITHOUT a real canonical skill tree must NOT be
// misdetected — decide keeps failing loudly with the embed-cache remedy error. Two
// shapes: no skills/ dir at all, and a skills/CONVENTIONS.md whose tree contains no
// */SKILL.md (a lookalike marker alone is not a skill tree). Sentinel discipline
// mirrors TestRunSkillsDecide_RejectsEmbedCache: the probe text must not leak anywhere
// under the isolated HOME.
//
// TestRunSkillsDecide_NoSkillsTreeStillFailsLoud 是仓库内默认的负路径：forge 已 init
// 但无真实 canonical skill 树的项目不得被误判——decide 保持以 embed 缓存补救文案响亮
// 报错。两种形态：完全没有 skills/ 目录；有 skills/CONVENTIONS.md 但树内无任何
// */SKILL.md（光杆标记不是 skill 树）。哨兵纪律同
// TestRunSkillsDecide_RejectsEmbedCache：探针文本不得泄漏到隔离 HOME 下任何文件。
func TestRunSkillsDecide_NoSkillsTreeStillFailsLoud(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("FORGE_DATA_HOME", filepath.Join(home, ".forge"))
	t.Setenv("FORGE_SKILLS_CANONICAL", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "decide-noskills")

	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
	}{
		{"无 skills 目录", func(t *testing.T, dir string) {} /* 什么都不建 */},
		{"光杆 CONVENTIONS.md 无 SKILL.md", func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "skills", "CONVENTIONS.md"), []byte("# conventions\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if stdout, _, code := runForge(t, dir, "init", "--mode", "medium"); code != 0 {
				t.Fatalf("forge init failed: %s", stdout)
			}
			tc.setup(t, dir)

			const sentinel = "zz-no-skill-tree-probe"
			out, _, code := runForge(t, dir, "skills", "decide", "demo",
				"--diagnosis", sentinel, "--revision", "r", "--evidence", "e", "--outcome", "accept")
			if code == 0 {
				t.Fatalf("无真实 skill 树时 decide 必须 fail-loud, got exit 0: %s", out)
			}
			if !strings.Contains(out, "embed") || !strings.Contains(out, "FORGE_SKILLS_CANONICAL") {
				t.Errorf("报错须点名 embed 缓存原因与 FORGE_SKILLS_CANONICAL 补救, got: %s", out)
			}
			// The probe must not have been appended anywhere under the isolated HOME
			// (the embed cache lives there) nor under the project.
			//
			// 探针不得被追加到隔离 HOME（embed 缓存所在）或项目下任何文件。
			for _, root := range []string{home, dir} {
				filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return nil
					}
					if body, rerr := os.ReadFile(path); rerr == nil && strings.Contains(string(body), sentinel) {
						t.Errorf("fail-loud 路径不得写入任何 decisions；哨兵泄漏到: %s", path)
					}
					return nil
				})
			}
		})
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
	setSkDecVars(t, "demo", "guard-control")

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
