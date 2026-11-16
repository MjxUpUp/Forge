package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTaskStart_EmptyExpectedHint 钉住声明期提示：空 expected（`::`）只校验退出码，
// task start 必须说一次，否则容易被读成「输出也验过了」（1.87.2 dogfood 修复点 4）。
func TestTaskStart_EmptyExpectedHint(t *testing.T) {
	dir := t.TempDir()
	mkGitProjCLI(t, dir)
	if out, _, code := runForge(t, dir, "init"); code != 0 {
		t.Fatalf("forge init 失败: %s", out)
	}

	out, _, code := runForge(t, dir, "task", "start", "--ref", "main", "--kind", "task", "--title", "空期望提示",
		"--accept", "go build ./... ::")
	if code != 0 {
		t.Fatalf("task start 应成功，exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "只校验退出码") {
		t.Fatalf("空 expected 应提示只校验退出码，got:\n%s", out)
	}
}

// TestTaskStart_NoEmptyExpectedHint 反向：带期望输出时不提示，避免噪音。
func TestTaskStart_NoEmptyExpectedHint(t *testing.T) {
	dir := t.TempDir()
	mkGitProjCLI(t, dir)
	if out, _, code := runForge(t, dir, "init"); code != 0 {
		t.Fatalf("forge init 失败: %s", out)
	}

	out, _, code := runForge(t, dir, "task", "start", "--ref", "main", "--kind", "task", "--title", "有期望",
		"--accept", "go version :: go version")
	if code != 0 {
		t.Fatalf("task start 应成功，exit=%d out=%s", code, out)
	}
	if strings.Contains(out, "只校验退出码") {
		t.Fatalf("带期望输出时不应提示，got:\n%s", out)
	}
}

// TestTaskStart_PlanFileEmptyExpectedHint 钉住提示在 --plan-file 合流之后统计：
// 只在 --accept 阶段统计会漏掉 plan 提取出来的裸 Run（1.87.2 代码评审 P2-1）。
func TestTaskStart_PlanFileEmptyExpectedHint(t *testing.T) {
	dir := t.TempDir()
	mkGitProjCLI(t, dir)
	if out, _, code := runForge(t, dir, "init"); code != 0 {
		t.Fatalf("forge init 失败: %s", out)
	}
	plan := "# plan\n\n## 步骤\n\nRun: go build ./...\n\nRun: go vet ./...\n"
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte(plan), 0644); err != nil {
		t.Fatal(err)
	}

	out, _, code := runForge(t, dir, "task", "start", "--ref", "main", "--kind", "task", "--title", "plan 空期望",
		"--plan-file", "plan.md")
	if code != 0 {
		t.Fatalf("task start 应成功，exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "只校验退出码") {
		t.Fatalf("plan 提取的空期望也应提示，got:\n%s", out)
	}
}

// TestTaskAccept_EmptyExpectedHint 事后补登同样要提示。
func TestTaskAccept_EmptyExpectedHint(t *testing.T) {
	dir := t.TempDir()
	mkGitProjCLI(t, dir)
	if out, _, code := runForge(t, dir, "init"); code != 0 {
		t.Fatalf("forge init 失败: %s", out)
	}
	if out, _, code := runForge(t, dir, "task", "start", "--ref", "main", "--kind", "task", "--title", "补登"); code != 0 {
		t.Fatalf("task start 应成功，exit=%d out=%s", code, out)
	}
	out, _, code := runForge(t, dir, "task", "accept", "go vet ./... ::")
	if code != 0 {
		t.Fatalf("task accept 应成功，exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "只校验退出码") {
		t.Fatalf("补登空期望应提示，got:\n%s", out)
	}
}
