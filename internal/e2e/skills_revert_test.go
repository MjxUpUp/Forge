package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillscanonical"
	"github.com/MjxUpUp/Forge/internal/skillsdecisions"
)

// TestSkillsRevert_AutoAppendsRejectDecision: after a successful scoped revert, a reject decision is auto-appended (spec-as-executable, replacing the advisory print relying on agent self-discipline — dogfood iron rule: pure self-discipline will miss).
//
// TestSkillsRevert_AutoAppendsRejectDecision：成功 scoped revert 后自动追加 reject 决策
// （spec-as-executable，取代靠 agent 自觉的 advisory print——dogfood 铁律：纯靠自觉必漏）。
// external canonical（--canonical 指向 temp git repo 的 skills/）下，revert 决策关联的 commit
// 成功后，decisions.md 多一条 outcome=reject 决策——下轮 agent 知悉此优化被否决，避免重复探索。
func TestSkillsRevert_AutoAppendsRejectDecision(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test")

	// skills/mySk/SKILL.md + commit C1（决策 CommitHash 锚点）
	writeFile(t, repo, "skills/mySk/SKILL.md", "# mySk\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "add mySk")
	c1short := strings.TrimSpace(git(t, repo, "rev-parse", "--short", "HEAD"))

	canonical := filepath.Join(repo, "skills")
	// forge decide 写一条 accept 决策（commit=C1）到 decisions.md
	forge(t, repo, "skills", "decide", "--canonical", canonical, "--skill", "mySk",
		"--outcome", "accept", "--diagnosis", "init", "--revision", "v1",
		"--evidence", "probe ok", "--commit", c1short)
	// decide 写的 decisions.md 是工作区变更，commit 它让后续 revert 工作区干净（git revert 要求）
	git(t, repo, "add", "skills/mySk/decisions.md")
	git(t, repo, "commit", "-m", "record accept decision")

	decs, err := skillsdecisions.LoadDecisions(canonical, "mySk")
	if err != nil || len(decs) != 1 {
		t.Fatalf("LoadDecisions: err=%v len=%d", err, len(decs))
	}
	decID := decs[0].ID

	// scoped revert（revert C1）→ 成功 + auto reject 决策
	forge(t, repo, "skills", "revert", "--canonical", canonical,
		"--skill", "mySk", "--decision", decID)

	// 断言 decisions.md 多一条 reject（append 到末尾）
	decsAfter, err := skillsdecisions.LoadDecisions(canonical, "mySk")
	if err != nil {
		t.Fatalf("LoadDecisions after: %v", err)
	}
	if len(decsAfter) != 2 {
		t.Fatalf("revert 后应 2 条决策（accept+reject），got %d", len(decsAfter))
	}
	auto := decsAfter[1]
	if auto.Outcome != skillsdecisions.OutcomeReject {
		t.Errorf("auto 决策 outcome=%q, want reject", auto.Outcome)
	}
	if auto.CommitHash == "" {
		t.Error("auto 决策应记 revert commit hash（CommitHash 非空）")
	}
	if !strings.Contains(auto.Diagnosis, decID) {
		t.Errorf("auto 决策 Diagnosis 应含被 revert 的决策 id %q, got %q", decID, auto.Diagnosis)
	}
}

// TestSkillsRevert_EmbedCache_ActionableError: embed source (isExternal=false, extracted to the OS temp dir) is a read-only snapshot, not in a git repo — scoped revert must give an actionable error (containing --canonical guiding to the forge source repo), rather than a generic not-a-git-repository. embedded skill reverts should operate in the source repo.
//
// TestSkillsRevert_EmbedCache_ActionableError：embed 源（isExternal=false，解压到系统临时目录）
// 是只读快照、不在 git repo——scoped revert 须给可行动错误（含 --canonical 引导到 forge
// 源码 repo），而非通用 not-a-git-repository。embedded skill 的 revert 应在源码 repo 操作。
func TestSkillsRevert_EmbedCache_ActionableError(t *testing.T) {
	// home 隔离：eval/决策等用户级数据仍可能落到 home，重定向避免污染真实环境。
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	dir := freshProject(t) // forge init，不传 --canonical → 内置 embed 源
	// 阶段 D 收尾后 embed 源解压到系统临时目录（不再写 ~/.forge）：路径取
	// skillscanonical.EmbeddedCacheDir，与 CLI 侧同一真相源。
	embedCache, err := skillscanonical.EmbeddedCacheDir()
	if err != nil {
		t.Fatalf("EmbeddedCacheDir: %v", err)
	}
	const skill = "test-revert-embed-probe"
	// 临时缓存跨测试进程共享（系统临时目录），用例结束清掉播种的探针目录，
	// 避免污染后续 validate/audit 对 canonical 的枚举。
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(embedCache, skill)) })

	// 先物化 embed 缓存：解压是惰性的——`forge init` 与 `skills list` 都不落盘，
	// 首个需要真实路径的 skills 命令才解压（Resolve → EnsureEmbeddedCache）。
	// `skills validate` 的退出码可能是 2（规范失败），但不影响物化；只看副作用。
	_, _ = forgeErr(t, dir, "skills", "validate")

	// 直接写 decisions.md 播种 fixture（skillsdecisions.AppendDecision），不走 `skills decide`
	// CLI：decide 现在拒绝 embed 缓存 canonical（快照会被版本重建抹掉——写进去的决策报过 ✅
	// 后静默丢失；见 runSkillsDecide 守卫）。CLI 的拒绝是正确行为，本测试不得依赖绕过它；
	// fixture 模拟的是"缓存里恰有一条决策"，被测对象是 REVERT 的行为。
	if err := skillsdecisions.AppendDecision(embedCache, skill, skillsdecisions.SkillDecision{
		Skill: skill, Diagnosis: "init", Revision: "v1", Evidence: "probe ok",
		Outcome: "accept", CommitHash: "fake123",
	}); err != nil {
		t.Fatalf("seed decision into embed cache: %v", err)
	}
	decs, err := skillsdecisions.LoadDecisions(embedCache, skill)
	if err != nil || len(decs) != 1 {
		t.Fatalf("LoadDecisions: err=%v len=%d", err, len(decs))
	}

	// revert（embed 场景，isExternal=false）→ 期望非 0 + 可行动错误
	out, err := forgeErr(t, dir, "skills", "revert", "--skill", skill, "--decision", decs[0].ID)
	if err == nil {
		t.Fatalf("embed 场景 revert 应失败（非 0），got success: %s", out)
	}
	if !strings.Contains(out, "embed 缓存") {
		t.Errorf("错误应说明 embed 缓存场景, got %q", out)
	}
	if !strings.Contains(out, "--canonical") {
		t.Errorf("错误应含 --canonical 引导, got %q", out)
	}
}
