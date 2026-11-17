package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/change"
	"github.com/MjxUpUp/Forge/internal/specroot"
)

const readyGoal = "## Objective\n\n目标。\n\n## Why\n\n原因。\n\n## Constraints\n\n约束。\n\n## Out of Scope\n\n范围外。\n\n## Check\n\n检查。\n"

// fillReady 把 change 补齐到 ready（clarify + proposal/design/acceptance/tasks）。
func fillReady(t *testing.T, dir, id string) {
	t.Helper()
	base := filepath.Join(dir, "spec", "changes", id)
	if err := os.WriteFile(filepath.Join(base, change.FileGoal), []byte(readyGoal), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, change.FileClarify), []byte(change.RenderClarify(id, "只改 A", "不动 B", time.Now())), 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"change.md":     "# 改动\n\n改 A 的渲染路径。\n\n## 影响\n\n无。\n\n## 验证\n\nacceptance #1。\n",
		"proposal.md":   "# 提案\n\n为什么做。\n",
		"design.md":     "# 设计\n\n怎么改。\n",
		"acceptance.md": "# 验收\n\n```\ntrue\n```\n",
		"tasks.md":      "# 任务\n\n- [ ] 1. 做一件事（判据：acceptance #1）\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(base, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// 老 change（无 goal.md）即使 ready 也不能进 ReadyChanges——--change 会被
// resolveGoalContract 拒绝，推荐它就是推荐一条必然失败的命令。
func TestNextContext_ReadyChangesRequireGoal(t *testing.T) {
	dir := t.TempDir()
	if _, err := change.Init(dir, specroot.ProfileMini); err != nil {
		t.Fatal(err)
	}
	if _, err := change.New(dir, "modern", "modern"); err != nil {
		t.Fatal(err)
	}
	if _, err := change.New(dir, "legacy", "legacy"); err != nil {
		t.Fatal(err)
	}
	fillReady(t, dir, "modern")
	fillReady(t, dir, "legacy")
	for _, f := range []string{change.FileGoal, change.GoalRequiredFile} {
		if err := os.Remove(filepath.Join(dir, "spec", "changes", "legacy", f)); err != nil {
			t.Fatal(err)
		}
	}

	ctx := nextContext(dir, "main", true, 0)
	if len(ctx.OpenChanges) != 2 {
		t.Fatalf("open changes = %v, want both", ctx.OpenChanges)
	}
	if len(ctx.ReadyChanges) != 1 || ctx.ReadyChanges[0] != "modern" {
		t.Fatalf("ready changes = %v, want [modern]（legacy 不能带 --change）", ctx.ReadyChanges)
	}
}
