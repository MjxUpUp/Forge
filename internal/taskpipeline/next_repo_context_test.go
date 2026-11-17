package taskpipeline

import (
	"os"
	"path/filepath"
	"testing"

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
