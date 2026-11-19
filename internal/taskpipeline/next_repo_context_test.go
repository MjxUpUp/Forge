package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/change"
	"github.com/MjxUpUp/Forge/internal/specroot"
)

// 回归（代码评审 P2）：老 change（没有 goal.md）可以走到 ready，但它不能 --change
// （钉契约要五段 goal）。NextRepoContext 必须把它排除在 ReadyChanges 之外，否则
// next / gate / status 会建议一条照抄就报错的命令。
func TestNextRepoContext_ReadyChangeWithoutGoalIsNotRecommended(t *testing.T) {
	root := t.TempDir()
	if _, err := change.Init(root, specroot.ProfileMini); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := change.New(root, "demo", "demo"); err != nil {
		t.Fatalf("New: %v", err)
	}
	dir := filepath.Join(root, "spec", "changes", "demo")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("clarify.md", "## 边界\n\n只改 A 的渲染路径\n\n## 非目标\n\n不动 B\n\n人确认：2026-11-16T00:00:00Z\n")
	write("proposal.md", "## 问题\n\nx\n\n## 提案\n\ny\n")
	write("design.md", "## 方案\n\nz\n")
	write("tasks.md", "- [ ] 改 A（判据：acceptance #1）\n")
	write("acceptance.md", "```bash\n# 1. build\ngo build ./...\n```\n")
	// 退化成老 change：删掉 goal.md 与 .goal-required。
	for _, name := range []string{change.FileGoal, change.GoalRequiredFile, change.FileChange} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatalf("remove %s: %v", name, err)
		}
	}

	phase, _, err := change.ComputePhase(root, "demo")
	if err != nil {
		t.Fatalf("ComputePhase: %v", err)
	}
	if phase != change.PhaseReady {
		t.Fatalf("老 change 应能到 ready，实际 %s", phase)
	}

	ctx := NextRepoContext(root, "main", true, 0)
	for _, id := range ctx.ReadyChanges {
		if id == "demo" {
			t.Fatalf("ready 但无 goal.md 的老 change 不应进 ReadyChanges（--change 会失败）")
		}
	}
	if len(ctx.OpenChanges) != 1 || ctx.OpenChanges[0] != "demo" {
		t.Fatalf("OpenChanges = %v，应仍含 demo", ctx.OpenChanges)
	}
}

// next 不该把用户拖回旧活：ready 但三天没动过的 change 进 StaleChanges，
// 决策给新任务入口（change new），而不是 task start --change <stale>。
// 2026-11-19 实证：一个 2h17m 的会话开头被 next 指向已放弃的 ready change。
func TestNextRepoContext_StaleReadyChangeIsNotRecommended(t *testing.T) {
	root := t.TempDir()
	if _, err := change.Init(root, specroot.ProfileMini); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := change.New(root, "doudou", "doudou"); err != nil {
		t.Fatalf("New: %v", err)
	}
	dir := filepath.Join(root, "spec", "changes", "doudou")
	if err := os.WriteFile(filepath.Join(dir, "clarify.md"), []byte("## 边界\n\nb\n\n## 非目标\n\nn\n\n人确认：2026-11-16T00:00:00Z\n"), 0o644); err != nil {
		t.Fatalf("clarify: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "goal.md"), []byte("# t\n\n## Objective\n\n做 x\n\n## Out of Scope\n\n不做 y\n\n## Check\n\n- 判据\n"), 0o644); err != nil {
		t.Fatalf("goal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "change.md"), []byte("## 改动\n\nx\n\n## 影响\n\ny\n\n## 验证\n\nz\n"), 0o644); err != nil {
		t.Fatalf("change.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "acceptance.md"), []byte("```bash\ngo build ./...\n```\n"), 0o644); err != nil {
		t.Fatalf("acceptance: %v", err)
	}
	old := time.Now().Add(-5 * 24 * time.Hour)
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		return os.Chtimes(path, old, old)
	}); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	ctx := NextRepoContext(root, "main", true, 0)
	if len(ctx.ReadyChanges) != 0 {
		t.Fatalf("陈旧 change 不应进 ReadyChanges: %v", ctx.ReadyChanges)
	}
	if len(ctx.StaleChanges) != 1 || ctx.StaleChanges[0] != "doudou" {
		t.Fatalf("StaleChanges = %v，应含 doudou", ctx.StaleChanges)
	}
	got := NextDecisionCtx(ctx, nil)
	if got.Next != "forge change new <id>" {
		t.Fatalf("陈旧 change 场景应给新任务入口，实际 %q", got.Next)
	}
	if !strings.Contains(got.Reason, "三天") {
		t.Fatalf("理由应说明陈旧，实际 %q", got.Reason)
	}
}

// 新鲜的 ready change 仍按原规则推荐 task start --change。
func TestNextRepoContext_FreshReadyChangeIsRecommended(t *testing.T) {
	root := t.TempDir()
	if _, err := change.Init(root, specroot.ProfileMini); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := change.New(root, "fresh", "fresh"); err != nil {
		t.Fatalf("New: %v", err)
	}
	dir := filepath.Join(root, "spec", "changes", "fresh")
	for name, body := range map[string]string{
		"clarify.md":    "## 边界\n\nb\n\n## 非目标\n\nn\n\n人确认：2026-11-19T00:00:00Z\n",
		"goal.md":       "# t\n\n## Objective\n\n做 x\n\n## Out of Scope\n\n不做 y\n\n## Check\n\n- 判据\n",
		"change.md":     "## 改动\n\nx\n\n## 影响\n\ny\n\n## 验证\n\nz\n",
		"acceptance.md": "```bash\ngo build ./...\n```\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	ctx := NextRepoContext(root, "main", true, 0)
	if len(ctx.ReadyChanges) != 1 || ctx.ReadyChanges[0] != "fresh" {
		t.Fatalf("新鲜 ready change 应进 ReadyChanges: %v", ctx.ReadyChanges)
	}
	if len(ctx.StaleChanges) != 0 {
		t.Fatalf("StaleChanges 应为空: %v", ctx.StaleChanges)
	}
	got := NextDecisionCtx(ctx, nil)
	if !strings.Contains(got.Next, "--change fresh") {
		t.Fatalf("应推荐 task start --change fresh，实际 %q", got.Next)
	}
}
