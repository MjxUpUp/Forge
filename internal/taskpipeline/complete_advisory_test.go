package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// setupCompletableTask builds a temp git repo (initial commit) and a task state ready
// for the task-complete gate: both earlier gates recorded, review hard-prerequisite
// satisfied with an empty baseline (snapshot check skipped). The caller shapes branch
// layout / changed files / title per scenario.
//
// setupCompletableTask 建临时 git 仓库（initial commit）和一个可直接过
// task-complete 门禁的任务状态：前两道 gate 已记录、review 硬前置以空基线满足
// （跳过快照检查）。分支布局/变更文件/标题由调用方按场景构造。
func setupCompletableTask(t *testing.T, ref string) (string, *TaskState) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	state := &TaskState{TaskRef: ref, Branch: "master"}
	state.RecordGateResult("task-implement", true, "")
	state.RecordGateResult("task-verify", true, "")
	state.MarkReviewPassed("", "") // 满足 review 硬前置（空基线=跳过快照检查）
	return dir, state
}

// findCheckEntry reports whether checklog contains an entry with the given check name
// for the task.
//
// findCheckEntry 报告 checklog 中是否存在本任务给定 check 名的条目。
func findCheckEntry(t *testing.T, dir, ref string, name checklog.CheckName) bool {
	t.Helper()
	entries, err := checklog.LoadForTask(dir, ref)
	if err != nil {
		t.Fatalf("LoadForTask: %v", err)
	}
	for _, e := range entries {
		if e.Check == name {
			return true
		}
	}
	return false
}

// TestTaskComplete_BranchUnmergedAdvisory pins the branch-merged advisory: completing
// a task whose feature branch has commits NOT in the mainline must emit a stderr
// advisory + audit entry ('complete' ≠ 'delivered'); once the branch is merged, the
// advisory must disappear. git-probe failures fail open (covered implicitly: the
// merged case runs the same probe and stays silent).
//
// TestTaskComplete_BranchUnmergedAdvisory 钉住分支归属 advisory：feature 分支有主干
// 没有的 commit 时完成任务必须发 stderr advisory + 审计条目（「完成」≠「交付」）；
// 分支合入后 advisory 必须消失。git 探测失败 fail-open（合并案例跑同一探测且保持
// 静默，隐式覆盖）。
func TestTaskComplete_BranchUnmergedAdvisory(t *testing.T) {
	dir, state := setupCompletableTask(t, "branch-advisory")

	runGit(t, dir, "checkout", "-b", "feat/pending")
	os.WriteFile(filepath.Join(dir, "feature.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "feature work")
	state.Branch = "feat/pending"

	// Unmerged → advisory + audit entry.
	//
	// 未合入 → advisory + 审计条目
	stderr := captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
			t.Fatalf("task-complete 应通过（advisory 不阻断）: %v", err)
		}
	})
	if !strings.Contains(stderr, "尚未合入主干") {
		t.Errorf("未合入分支应触发分支归属 advisory，stderr: %q", stderr)
	}
	if !findCheckEntry(t, dir, "branch-advisory", CheckNameBranchUnmerged) {
		t.Error("checklog 应有 CheckNameBranchUnmerged 审计条目")
	}

	// Merge into the mainline → advisory gone.
	//
	// 合入主干 → advisory 消失
	mainline := resolveMainlineRef(dir)
	if mainline == "" {
		t.Fatal("测试仓库应有 main 或 master 主干 ref")
	}
	runGit(t, dir, "checkout", mainline)
	runGit(t, dir, "merge", "feat/pending")

	stderr = captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
			t.Fatalf("合入后 task-complete 应通过: %v", err)
		}
	})
	if strings.Contains(stderr, "尚未合入主干") {
		t.Errorf("分支已合入主干，不应再触发分支归属 advisory，stderr: %q", stderr)
	}
}

// TestTaskComplete_GoalOutputMismatchAdvisory pins the goal↔output coarse-match
// advisory: a title sharing NO keyword with the changed files must trigger the
// advisory + audit entry; a title sharing a keyword must stay silent. Empty title or
// no changed files skip the check entirely.
//
// TestTaskComplete_GoalOutputMismatchAdvisory 钉住目标↔产出粗匹配 advisory：标题与
// 实改文件零关键词交集时必须触发 advisory + 审计条目；有交集时必须静默。空标题或
// 无变更文件完全跳过本检查。
func TestTaskComplete_GoalOutputMismatchAdvisory(t *testing.T) {
	dir, state := setupCompletableTask(t, "goal-mismatch")

	// Untracked new source file: tokens {src, widget, button}.
	//
	// 未跟踪的新源文件：token {src, widget, button}
	os.MkdirAll(filepath.Join(dir, "src", "widget"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "widget", "button.go"), []byte("package widget\n"), 0644)

	// Zero intersection: {refactor, executor, pipeline} ∩ {src, widget, button} = ∅.
	//
	// 零交集：{refactor, executor, pipeline} ∩ {src, widget, button} = ∅
	state.Summary = "Refactor executor pipeline"
	stderr := captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
			t.Fatalf("task-complete 应通过（advisory 不阻断）: %v", err)
		}
	})
	if !strings.Contains(stderr, "无明显关联") {
		t.Errorf("零交集应触发目标↔产出 advisory，stderr: %q", stderr)
	}
	if !findCheckEntry(t, dir, "goal-mismatch", CheckNameGoalOutputMismatch) {
		t.Error("checklog 应有 CheckNameGoalOutputMismatch 审计条目")
	}

	// Intersection: {widget, button} ∩ {src, widget, button} ≠ ∅ → silent.
	//
	// 有交集：{widget, button} ∩ {src, widget, button} ≠ ∅ → 静默
	state.Summary = "Add widget button"
	stderr = captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-complete", state); err != nil {
			t.Fatalf("task-complete 应通过: %v", err)
		}
	})
	if strings.Contains(stderr, "无明显关联") {
		t.Errorf("有交集时不应触发目标↔产出 advisory，stderr: %q", stderr)
	}
}

// TestGoalKeywords pins the tokenization contract: non-alphanumeric split, >=4-char
// lowercase ASCII words kept, short words and pure-CJK tokens dropped, CJK acting as
// a separator (no segmentation dependency).
//
// TestGoalKeywords 钉住切词契约：非字母数字切分、保留 >=4 字符小写 ASCII 词、丢弃
// 短词与纯中文词、中文充当分隔符（不引入分词依赖）。
func TestGoalKeywords(t *testing.T) {
	words := goalKeywords("Fix executor gate 修复门禁 abc")
	for _, want := range []string{"executor", "gate"} {
		if !words[want] {
			t.Errorf("应含关键词 %q，got %v", want, words)
		}
	}
	for _, reject := range []string{"fix", "abc", "修复门禁"} {
		if words[reject] {
			t.Errorf("不应含关键词 %q（短词/纯中文词应跳过），got %v", reject, words)
		}
	}
	if len(goalKeywords("")) != 0 {
		t.Error("空标题应切出零关键词")
	}
}

// TestPathSegmentKeywords pins the path-side tokenization: extension stripped, glob
// metacharacters stripped, segments split on non-alphanumeric boundaries.
//
// TestPathSegmentKeywords 钉住路径侧切词：去扩展名、去 glob 元字符、segment 按非
// 字母数字边界细分。
func TestPathSegmentKeywords(t *testing.T) {
	words := pathSegmentKeywords([]string{`internal\taskpipeline\executor_test.go`}, []string{"docs/plans/*.md"})
	for _, want := range []string{"internal", "taskpipeline", "executor", "test", "docs", "plans", "md"} {
		if !words[want] {
			t.Errorf("应含 token %q，got %v", want, words)
		}
	}
}
