package cliskills

// skills_decide_test.go — 钉死 `skills decide` 的 embed 缓存写入守卫：canonical 解析到
// embed 解压缓存必须响亮失败（它是可再生成的分发快照——两个 forge 二进制版本交替运行时
// 每次异版调用都会抹掉它，静默销毁已报 ✅ 记录的决策），显式外部 canonical 则放行。

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/skillseval"
)

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
	skDecWarrant = ""
	t.Cleanup(func() {
		skDecSkill = ""
		skDecDiagnosis = ""
		skDecRevision = ""
		skDecEvidence = ""
		skDecOutcome = ""
		skDecWarrant = ""
	})
}

// TestRunSkillsDecide_RejectsEmbedCache verifies the guard fires when canonical resolves to the embed cache.
//
// TestRunSkillsDecide_RejectsEmbedCache 钉死守卫：canonical 解析到 embed 缓存（无
// env/flag 覆盖）时报错并给出补救指引（指向真实源），而不是把追加写进一个下一次异版
// forge 调用就会被 RemoveAll 的快照。
func TestRunSkillsDecide_RejectsEmbedCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("FORGE_SKILLS_CANONICAL", "")

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

	// 位置参数与 --skill 同名：一致，放行。
	setSkDecVars(t, "demo", "positional-consistent")
	if err := runSkillsDecide(nil, []string{"demo"}); err != nil {
		t.Fatalf("positional == --skill must be accepted, got: %v", err)
	}
}

// TestRunSkillsDecide_PositionalConflictsFlag pins the ambiguity guard.
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

// TestRunSkillsDecide_InRepoCanonicalDefault pins the in-repo canonical default.
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

// TestRunSkillsDecide_NoSkillsTreeStillFailsLoud is the negative path of the in-repo default.
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

// TestRunSkillsDecide_AcceptsExternalCanonical is the pass-through control: with an explicit external canonical the guard stays silent and the append lands.
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

// TestRunSkillsDecide_RequiresEachTupleField: leaving out any single one of
// diagnosis/revision/evidence is rejected before anything is written — the
// guard must not only fire when all three are empty.
//
// TestRunSkillsDecide_RequiresEachTupleField：diagnosis/revision/evidence 任缺一项
// 都在写入前被拒——守卫不能只在三项全空时才生效。
func TestRunSkillsDecide_RequiresEachTupleField(t *testing.T) {
	canonical := t.TempDir()
	if err := os.MkdirAll(filepath.Join(canonical, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORGE_SKILLS_CANONICAL", canonical)
	for _, missing := range []string{"diagnosis", "revision", "evidence"} {
		setSkDecVars(t, "demo", "tuple-probe")
		switch missing {
		case "diagnosis":
			skDecDiagnosis = ""
		case "revision":
			skDecRevision = ""
		case "evidence":
			skDecEvidence = ""
		}
		err := runSkillsDecide(nil, nil)
		if err == nil || !strings.Contains(err.Error(), "均必填") {
			t.Errorf(`缺 --%s 应报四元组必填错误，got %v`, missing, err)
		}
	}
	if _, err := os.Stat(filepath.Join(canonical, "demo", "decisions.md")); !os.IsNotExist(err) {
		t.Error(`缺字段被拒时不得写出 decisions.md`)
	}
}

// TestRunSkillsDecide_OrphanWarning: writing to a skill name without SKILL.md
// warns about the orphan directory; a real skill does not get that warning.
//
// TestRunSkillsDecide_OrphanWarning：写入无 SKILL.md 的 skill 名时提示孤儿目录；
// 真实 skill 不出现该提示。
func TestRunSkillsDecide_OrphanWarning(t *testing.T) {
	canonical := t.TempDir()
	if err := os.MkdirAll(filepath.Join(canonical, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "real", "SKILL.md"), []byte("---\nname: real\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORGE_SKILLS_CANONICAL", canonical)
	for _, c := range []struct {
		skill      string
		wantOrphan bool
	}{{"ghost", true}, {"real", false}} {
		setSkDecVars(t, c.skill, "orphan-probe")
		skDecOutcome = "reject" // 非 accept 且无 --warrant：不触发理由核验，stderr 只看孤儿提示
		var stderr bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetErr(&stderr)
		if err := runSkillsDecide(cmd, nil); err != nil {
			t.Fatalf(`%s：decide 应成功，got %v`, c.skill, err)
		}
		if got := strings.Contains(stderr.String(), "孤儿目录"); got != c.wantOrphan {
			t.Errorf(`%s：孤儿提示应为 %v，stderr=%q`, c.skill, c.wantOrphan, stderr.String())
		}
	}
}

// warrantFixture builds an isolated forge project (registered in a throwaway
// registry, git repo with docs/evidence.md committed), an external canonical
// tree with skill "demo", and an empty eval dir. It chdirs into the project and
// returns (projectRoot, canonical, evalDir, headSHA).
//
// warrantFixture 搭隔离的 forge 项目（登记在一次性 registry、git 仓库已提交
// docs/evidence.md）、含 skill "demo" 的外部 canonical 树和空 eval 目录；cwd 切到
// 项目根，返回 (项目根, canonical, eval 目录, HEAD sha)。
func warrantFixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("FORGE_DATA_HOME", filepath.Join(home, ".forge"))

	proj := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", proj}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "user.email", "t@t.com")
	git("config", "user.name", "T")
	if err := os.MkdirAll(filepath.Join(proj, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "docs", "evidence.md"), []byte("# evidence\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "init")
	head := git("rev-parse", "HEAD")
	if err := registry.Add(proj); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	t.Chdir(proj)

	canonical := t.TempDir()
	if err := os.MkdirAll(filepath.Join(canonical, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "demo", "SKILL.md"), []byte("---\nname: demo\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORGE_SKILLS_CANONICAL", canonical)
	evalDir := t.TempDir()
	t.Setenv("FORGE_EVAL_DIR", evalDir)
	return proj, canonical, evalDir, head
}

// warrantRows returns the decision-warrant rows recorded for root, in order.
//
// warrantRows 返回 root 下按序记录的 decision-warrant 行。
func warrantRows(t *testing.T, root string) []checklog.Entry {
	t.Helper()
	all, err := checklog.LoadAllAll(root)
	if err != nil {
		t.Fatalf("LoadAllAll: %v", err)
	}
	var rows []checklog.Entry
	for _, e := range all {
		if e.Check == checklog.CheckDecisionWarrant {
			rows = append(rows, e)
		}
	}
	return rows
}

// runDecideCaptured runs decide in-process with the given outcome/warrant and
// returns stderr; the decision text is the diagnosis so callers can find it.
//
// runDecideCaptured 进程内跑一次 decide（指定 outcome/warrant），返回 stderr；
// diagnosis 作为可检索的决策文本。
func runDecideCaptured(t *testing.T, diagnosis, outcome, warrant string) (string, error) {
	t.Helper()
	setSkDecVars(t, "demo", diagnosis)
	skDecOutcome = outcome
	skDecWarrant = warrant
	var stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&stderr)
	err := runSkillsDecide(cmd, nil)
	return stderr.String(), err
}

// TestSkillsDecideWarrant (REQ-2): an accept decision without a checkable
// anchor, or with one that fails its check, warns and records a decision-warrant
// row with Passed=false — the decision is still written and decide returns nil.
// Valid commit/doc anchors are weak and pass; the anchor round-trips into
// decisions.md. Non-accept outcomes without a warrant are not checked, and a
// multi-line warrant is rejected before anything is written.
//
// TestSkillsDecideWarrant（REQ-2）：accept 决策缺可核验锚点或锚点核验失败时告警并
// 落 Passed=false 的 decision-warrant 行——决策照常写入、decide 返回 nil。合法的
// commit/doc 锚点为弱且通过；锚点写入 decisions.md。非 accept 且无锚点不核验；多行
// 锚点在写入前被拒。
func TestSkillsDecideWarrant(t *testing.T) {
	proj, canonical, _, head := warrantFixture(t)
	decisionsPath := filepath.Join(canonical, "demo", "decisions.md")

	cases := []struct {
		name        string
		outcome     string
		warrant     string
		wantRow     bool
		wantVerdict string
		wantPassed  bool
		wantStderr  string
	}{
		{"accept 缺锚点", "accept", "", true, "missing", false, "未带可核验锚点"},
		{"doc 锚点存在", "accept", "doc:docs/evidence.md", true, "weak", true, "弱锚点核验通过"},
		{"doc 不存在", "accept", "doc:docs/nope.md", true, "invalid", false, "不存在"},
		{"doc 跳出仓库", "accept", "doc:../outside.md", true, "invalid", false, "跳出了仓库根"},
		{"doc 绝对路径", "accept", "doc:/etc/hosts", true, "invalid", false, "绝对路径"},
		{"commit 存在", "accept", "commit:" + head, true, "weak", true, "弱锚点核验通过"},
		{"commit 不存在", "accept", "commit:deadbeefdeadbeef", true, "invalid", false, "不存在"},
		{"commit 选项注入", "accept", "commit:--output=x", true, "invalid", false, "十六进制"},
		{"未知类型", "accept", "jira:ABC-1", true, "invalid", false, "未知锚点类型"},
		{"缺值", "accept", "run:", true, "invalid", false, "格式应为"},
		{"run 不存在", "accept", "run:nope", true, "invalid", false, "不存在"},
		{"reject 无锚点不核验", "reject", "", false, "", false, ""},
	}
	for i, c := range cases {
		before := len(warrantRows(t, proj))
		diagnosis := "warrant-case-" + string(rune('a'+i))
		stderr, err := runDecideCaptured(t, diagnosis, c.outcome, c.warrant)
		if err != nil {
			t.Fatalf("%s：decide 必须照常成功（advisory），got %v", c.name, err)
		}
		data, rerr := os.ReadFile(decisionsPath)
		if rerr != nil || !strings.Contains(string(data), diagnosis) {
			t.Fatalf("%s：决策必须照常写入 decisions.md（err=%v）", c.name, rerr)
		}
		if c.warrant != "" && !strings.Contains(string(data), "- **Warrant**: "+c.warrant+"\n") {
			t.Errorf("%s：锚点应写成 Warrant 字段行", c.name)
		}
		rows := warrantRows(t, proj)
		if !c.wantRow {
			if len(rows) != before {
				t.Errorf("%s：不应落 decision-warrant 行（%d → %d）", c.name, before, len(rows))
			}
			continue
		}
		if len(rows) != before+1 {
			t.Fatalf("%s：应恰好新增 1 行 decision-warrant（%d → %d）", c.name, before, len(rows))
		}
		row := rows[len(rows)-1]
		if row.Meta["verdict"] != c.wantVerdict {
			t.Errorf("%s：verdict 应为 %q，got %q（detail=%s）", c.name, c.wantVerdict, row.Meta["verdict"], row.Detail)
		}
		if row.Passed != c.wantPassed {
			t.Errorf("%s：Passed 应为 %v，got %v", c.name, c.wantPassed, row.Passed)
		}
		if row.Meta["skill"] != "demo" || row.Meta["outcome"] != c.outcome {
			t.Errorf("%s：meta 应记 skill=demo outcome=%s，got %v", c.name, c.outcome, row.Meta)
		}
		if !strings.Contains(stderr, c.wantStderr) {
			t.Errorf("%s：stderr 应含 %q，got %q", c.name, c.wantStderr, stderr)
		}
	}

	// 非 accept 但显式给了锚点：同样核验并落行（核验失败 → Passed=false）。
	rowsBefore := len(warrantRows(t, proj))
	if _, err := runDecideCaptured(t, "warrant-reject-with-anchor", "reject", "doc:docs/nope.md"); err != nil {
		t.Fatalf("reject + --warrant 必须照常成功，got %v", err)
	}
	if rows := warrantRows(t, proj); len(rows) != rowsBefore+1 || rows[len(rows)-1].Passed || rows[len(rows)-1].Meta["outcome"] != "reject" {
		t.Errorf("reject + 无效锚点应落 1 行 Passed=false outcome=reject，got %d 行", len(rows)-rowsBefore)
	}

	// 值的首尾空白被规整：写入 decisions.md 的值与 meta 一致。
	if _, err := runDecideCaptured(t, "warrant-normalized", "accept", "doc:  docs/evidence.md "); err != nil {
		t.Fatalf("带空白的锚点必须照常成功，got %v", err)
	}
	if data, _ := os.ReadFile(decisionsPath); !strings.Contains(string(data), "- **Warrant**: doc:docs/evidence.md\n") {
		t.Error("锚点值应规整为 doc:docs/evidence.md 后写入")
	}
	if rows := warrantRows(t, proj); rows[len(rows)-1].Meta["warrant"] != "doc:docs/evidence.md" {
		t.Errorf("meta.warrant 应与写入值一致，got %q", rows[len(rows)-1].Meta["warrant"])
	}

	// 超长锚点：输入校验错误，决策不得写入。
	beforeLong, _ := os.ReadFile(decisionsPath)
	if _, err := runDecideCaptured(t, "warrant-too-long", "accept", "doc:"+strings.Repeat("a", maxWarrantLen)); err == nil {
		t.Fatal("超长 --warrant 必须报错")
	}
	if afterLong, _ := os.ReadFile(decisionsPath); !bytes.Equal(beforeLong, afterLong) {
		t.Error("超长 --warrant 被拒时 decisions.md 必须不变")
	}

	// 多行锚点：输入校验错误，决策不得写入（伪造决策头的注入面）。
	before, _ := os.ReadFile(decisionsPath)
	if _, err := runDecideCaptured(t, "warrant-multiline", "accept", "doc:docs/evidence.md\n## [d-evil] accept"); err == nil {
		t.Fatal("多行 --warrant 必须报错")
	}
	after, _ := os.ReadFile(decisionsPath)
	if !bytes.Equal(before, after) {
		t.Error("多行 --warrant 被拒时 decisions.md 必须不变")
	}
}

// TestSkillsDecideWarrantRegression (REQ-3): a run anchor that regresses
// against the skill's baseline warns with the regressed cases listed and a
// Passed=false row; a clean run and the baseline run itself are strong.
//
// TestSkillsDecideWarrantRegression（REQ-3）：run 锚点相对该 skill 的 baseline 有
// 回归时告警、逐条列出回归 case、行 Passed=false；无回归的 run 与 baseline 本身为强。
func TestSkillsDecideWarrantRegression(t *testing.T) {
	proj, _, evalDir, _ := warrantFixture(t)
	mk := func(id string, c1, c2 bool) {
		run := &skillseval.EvalRun{RunID: id, Skill: "demo", Results: []skillseval.CaseResult{
			{CaseID: "c1", Kind: skillseval.KindTrigger, Pass: c1},
			{CaseID: "c2", Kind: skillseval.KindTrigger, Pass: c2},
		}}
		if err := skillseval.AppendRun(evalDir, "demo", run); err != nil {
			t.Fatal(err)
		}
	}
	mk("r-base", true, true)
	mk("r-bad", false, true)
	mk("r-good", true, true)
	if err := skillseval.SetBaseline(evalDir, "demo", "r-base", "test"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		warrant     string
		wantVerdict string
		wantPassed  bool
	}{
		{"run:r-bad", "regression", false},
		{"run:r-good", "strong", true},
		// 锚点就是 baseline 本身：没有对照，只算弱（不能靠"和自己比没回归"拿强）。
		{"run:r-base", "weak", true},
	}
	for i, c := range cases {
		stderr, err := runDecideCaptured(t, "regression-case-"+string(rune('a'+i)), "accept", c.warrant)
		if err != nil {
			t.Fatalf("%s：decide 必须照常成功，got %v", c.warrant, err)
		}
		rows := warrantRows(t, proj)
		if len(rows) == 0 {
			t.Fatalf("%s：应落 decision-warrant 行", c.warrant)
		}
		row := rows[len(rows)-1]
		if row.Meta["verdict"] != c.wantVerdict || row.Passed != c.wantPassed {
			t.Errorf("%s：应为 verdict=%s Passed=%v，got %s/%v（detail=%s）", c.warrant, c.wantVerdict, c.wantPassed, row.Meta["verdict"], row.Passed, row.Detail)
		}
		if c.wantVerdict == "regression" {
			if !strings.Contains(row.Detail, "case c1") || strings.Contains(row.Detail, "case c2") {
				t.Errorf("回归明细应只列 c1：%s", row.Detail)
			}
			if !strings.Contains(stderr, "1 条回归") {
				t.Errorf("stderr 应报 1 条回归，got %q", stderr)
			}
		}
		if c.wantVerdict == "strong" && row.Level != checklog.LevelPass {
			t.Errorf("%s：强锚点行 Level 应为 pass，got %q", c.warrant, row.Level)
		}
	}
}

// TestSkillsDecideEvidenceSource (REQ-2): a decision whose evidence or rationale cites external figures without any URL warns and records a Passed=false decision-evidence row; with a URL the row passes silently; without external figures no row is written. The decision is always written and decide returns nil.
//
// TestSkillsDecideEvidenceSource（REQ-2）：Evidence / Rationale 引外部数字却无 URL 时告警并
// 落 Passed=false 的 decision-evidence 行；有 URL 时静默落 Passed=true 行；没有外部数字时
// 不落行。决策总是照常写入、decide 返回 nil。
func TestSkillsDecideEvidenceSource(t *testing.T) {
	proj, _, _, _ := warrantFixture(t)
	evidenceRows := func() []checklog.Entry {
		t.Helper()
		all, err := checklog.LoadAllAll(proj)
		if err != nil {
			t.Fatalf("LoadAllAll: %v", err)
		}
		var rows []checklog.Entry
		for _, e := range all {
			if e.Check == checklog.CheckDecisionEvidence {
				rows = append(rows, e)
			}
		}
		return rows
	}
	cases := []struct {
		name, evidence, rationale string
		wantRow, wantPassed       bool
		wantStderr, wantFigures   string
	}{
		{"百分比无 URL", "审查阶段占 59.4% token（Tokenomics）", "", true, false, "没有来源", "59.4%"},
		{"倍数与 arXiv 无 URL", "作弊率反升 43×", "见 arXiv:2510.20270", true, false, "没有来源", "43×、arXiv:2510.20270"},
		{"有 URL", "审查阶段占 59.4% token", "https://arxiv.org/abs/2601.14470（仅 ChatDev）", true, true, "", "59.4%"},
		{"倍与 Nx 无来源", "成本涨了 3 倍，吞吐 2.5x", "", true, false, "没有来源", "3 倍、2.5x"},
		{"全角百分号与大写 X", "覆盖率 85％，提速 4X", "", true, false, "没有来源", "85％、4X"},
		{"内部指标引用 run", "trigger 通过率 100%（run-20261015-a1）", "", true, true, "", "100%"},
		{"普通词 run-time 不算来源", "run-time 开销降了 30%", "", true, false, "没有来源", "30%"},
		{"无外部数字", "改了 SKILL.md 的措辞", "复盘结论", false, false, "", ""},
	}
	for i, c := range cases {
		before := len(evidenceRows())
		setSkDecVars(t, "demo", "evidence-case-"+string(rune('a'+i)))
		skDecEvidence = c.evidence
		skDecRationale = c.rationale
		skDecWarrant = "doc:docs/evidence.md"
		var stderr bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetErr(&stderr)
		if err := runSkillsDecide(cmd, nil); err != nil {
			t.Fatalf("%s: decide must succeed (advisory), got %v", c.name, err)
		}
		skDecRationale = ""
		rows := evidenceRows()
		if !c.wantRow {
			if len(rows) != before || strings.Contains(stderr.String(), "decision-evidence") {
				t.Errorf("%s: no figures must leave no row and no warning", c.name)
			}
			continue
		}
		if len(rows) != before+1 {
			t.Fatalf("%s: want exactly one new decision-evidence row (%d → %d)", c.name, before, len(rows))
		}
		row := rows[len(rows)-1]
		if row.Passed != c.wantPassed || row.Meta["figures"] != c.wantFigures || row.Meta["skill"] != "demo" {
			t.Errorf("%s: row = passed %v figures %q skill %q", c.name, row.Passed, row.Meta["figures"], row.Meta["skill"])
		}
		if c.wantStderr != "" && !strings.Contains(stderr.String(), c.wantStderr) {
			t.Errorf("%s: stderr must warn %q, got %q", c.name, c.wantStderr, stderr.String())
		}
		if c.wantStderr == "" && strings.Contains(stderr.String(), "decision-evidence") {
			t.Errorf("%s: sourced figures must not warn, got %q", c.name, stderr.String())
		}
	}
}

// TestDecisionEvidenceRunWarrant pins that a run warrant counts as a checkable source for internal metrics.
//
// TestDecisionEvidenceRunWarrant 钉住 run 锚点算作内部指标的可核验来源。
func TestDecisionEvidenceRunWarrant(t *testing.T) {
	if c := checkDecisionEvidence("通过率 92%", "", "run:20261015-a1"); len(c.Figures) != 1 || !c.HasURL {
		t.Fatalf("run warrant must count as a source, got %+v", c)
	}
	if c := checkDecisionEvidence("通过率 92%", "", "doc:docs/x.md"); c.HasURL {
		t.Fatalf("a doc warrant is not a source for an external figure, got %+v", c)
	}
}
