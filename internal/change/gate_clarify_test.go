package change

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGateTask_NewLayoutRequiresClarify 钉住 verify 与 gate 的同口径：
// 新布局（goal/change）没记录 clarify 的人确认时，按节点 gate 也必须拒绝。
// 之前 gate 只查 DocProblems，于是 `change phase` 说「缺 clarify」、
// `change gate` 却照跑（review-leftovers-204 N3）。
func TestGateTask_NewLayoutRequiresClarify(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	fillScaffold(t, dir)
	writeAcceptance(t, dir, "echo one > one.txt")
	writeTasks(t, dir, "- [ ] 第一步（判据：acceptance #1）")

	if err := os.Remove(filepath.Join(dir, FileClarify)); err != nil {
		t.Fatal(err)
	}
	_, err := GateTask(context.Background(), root, "demo", 1, GateOptions{Timeout: 30 * time.Second})
	if err == nil {
		t.Fatal("缺 clarify.md 时 gate 应拒绝")
	}
	if !strings.Contains(err.Error(), FileClarify) {
		t.Fatalf("错误信息应点名 clarify.md，got %v", err)
	}

	// 补上人确认后放行。
	writeFile(t, filepath.Join(dir, FileClarify), "# 澄清\n\n## 边界\n\n只验证一条判据。\n\n## 非目标\n\n不做别的。\n\n人确认：2026-11-18T00:00:00Z\n")
	res, err := GateTask(context.Background(), root, "demo", 1, GateOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("补 clarify 后应放行：%v", err)
	}
	if !res.Summary.Passed {
		t.Fatalf("节点应通过：%+v", res.Summary.Checks)
	}
}
