package e2e

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillsdecisions"
)

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

// TestSkillsRevert_EmbedCache_ActionableError：embed 缓存（isExternal=false，~/.forge/skills-cache/
// embedded）是只读快照、不在 git repo——scoped revert 须给可行动错误（含 --canonical 引导到 forge
// 源码 repo），而非通用 not-a-git-repository。embedded skill 的 revert 应在源码 repo 操作。
func TestSkillsRevert_EmbedCache_ActionableError(t *testing.T) {
	// home 隔离（C1）：resolveCanonical 走 os.UserHomeDir()（不走 freshProject 设的 FORGE_DATA_HOME）。
	// forge init 的 EnsureEmbeddedCache 解压 embed 缓存到 ~/.forge/skills-cache/embedded（版本不匹配
	// 时 RemoveAll+重建），decide 写 decisions.md 到该缓存——必须重定向 USERPROFILE（Windows）/HOME
	// （Unix）到 temp，否则污染真实 ~/.forge。temp home 整个由 t.TempDir 自动清理，无需手动 cleanup。
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	dir := freshProject(t) // forge init，不传 --canonical → embed 缓存（隔离到 temp home）
	embedCache := filepath.Join(home, ".forge", "skills-cache", "embedded")
	const skill = "test-revert-embed-probe"

	// 先 decide 写一条带 commit 的决策，否则 revert 在 loadDecisions 阶段返「无 decisions.md」
	forge(t, dir, "skills", "decide", "--skill", skill,
		"--outcome", "accept", "--diagnosis", "init", "--revision", "v1",
		"--evidence", "probe ok", "--commit", "fake123")
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
