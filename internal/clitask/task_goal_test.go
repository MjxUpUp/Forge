package clitask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setGoalFlags 设置三条互斥 flag，并在用例结束时清空——taskStartCmd 是包级变量，
// 不清会把 flag 状态漏给同包其它用例。
func setGoalFlags(t *testing.T, change, goalFile, goal string) {
	t.Helper()
	for name, val := range map[string]string{"change": change, "goal-file": goalFile, "goal": goal} {
		if err := taskStartCmd.Flags().Set(name, val); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, name := range []string{"change", "goal-file", "goal"} {
			_ = taskStartCmd.Flags().Set(name, "")
		}
	})
}

// TestTaskStart_RegistersGoalContractFlags 钉住两个入口 flag 存在：删任意一个，
// --change/--goal-file 就会静默变成 unknown flag，契约无法钉。
func TestTaskStart_RegistersGoalContractFlags(t *testing.T) {
	for _, name := range []string{"change", "goal-file"} {
		if taskStartCmd.Flags().Lookup(name) == nil {
			t.Fatalf("task start 缺 --%s flag", name)
		}
	}
}

func TestResolveGoalContract_RejectsTwoSources(t *testing.T) {
	setGoalFlags(t, "task-goal-contract", "goal.md", "")
	if _, _, err := resolveGoalContract(taskStartCmd, t.TempDir()); err == nil || !strings.Contains(err.Error(), "只能选一个") {
		t.Fatalf("--change 与 --goal-file 同时给应报互斥，got %v", err)
	}
}

func TestResolveGoalContract_RejectsFreeTextWithContract(t *testing.T) {
	setGoalFlags(t, "", "goal.md", "随手写个目标")
	if _, _, err := resolveGoalContract(taskStartCmd, t.TempDir()); err == nil || !strings.Contains(err.Error(), "互斥") {
		t.Fatalf("--goal 与 --goal-file 应互斥，got %v", err)
	}
}

func TestResolveGoalContract_RejectsPathOutsideRepo(t *testing.T) {
	root := t.TempDir()
	// 让仓外文件真实存在：否则测试只能证明「读不到」，不能证明边界检查生效。
	outside := filepath.Join(filepath.Dir(root), "outside.md")
	if err := os.WriteFile(outside, []byte(completeGoalForTest), 0o644); err != nil {
		t.Fatal(err)
	}
	setGoalFlags(t, "", "../outside.md", "")
	if _, _, err := resolveGoalContract(taskStartCmd, root); err == nil || !strings.Contains(err.Error(), "必须位于仓库内") {
		t.Fatalf("仓外 goal 文件应被拒绝，got %v", err)
	}
}

func TestResolveGoalContract_MissingChangeGoalFails(t *testing.T) {
	setGoalFlags(t, "no-such-change", "", "")
	if _, _, err := resolveGoalContract(taskStartCmd, t.TempDir()); err == nil {
		t.Fatal("change 不存在时读 goal.md 应报错，而不是静默放行")
	}
}

// TestResolveGoalContract_RejectsSymlinkEscape 钉住符号链接逃逸：路径在仓库内但
// 链接指向仓外时，git 里只有链接本身，契约内容不可评审——必须拒绝。
func TestResolveGoalContract_RejectsSymlinkEscape(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "goal.md")
	if err := os.WriteFile(target, []byte(completeGoalForTest), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	link := filepath.Join(root, "goal.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("本机不支持符号链接：%v", err)
	}
	setGoalFlags(t, "", "goal.md", "")
	if _, _, err := resolveGoalContract(taskStartCmd, root); err == nil || !strings.Contains(err.Error(), "仓库外") {
		t.Fatalf("指向仓外的符号链接应被拒绝，got %v", err)
	}
}

const completeGoalForTest = `# 目标契约

## Objective

交付一件事。

## Why

因为要做。

## Constraints

- 无。

## Out of Scope

- 无。

## Check

- acceptance #1 通过。
`

// TestResolveGoalContract_RejectsTraversalChangeID 钉住 --change 的路径边界：
// change id 会直接拼进 spec/changes/<id>/goal.md，`..`/绝对路径能让契约钉到
// 仓库外（Code Review P1-1）。校验必须在拼路径之前。
func TestResolveGoalContract_RejectsTraversalChangeID(t *testing.T) {
	root := t.TempDir()
	// 让「没有 ValidID 守卫时能成功」：每个越界 id 在 Join 之后的位置都放一份
	// 合法五段 goal。否则所有分支都只是「文件读不到」，测试对守卫是空转的
	// （Code Review P3-1）。
	for _, rel := range []string{
		"spec/changes/demo/goal.md", // demo/../demo
		"spec/goal.md",              // ..
		"abs/path/goal.md",          // /abs/path（Join 后成为仓库内相对路径）
	} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(completeGoalForTest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// ../../../outside 的落点是 root 的父目录；单独建一份，证明在没有守卫时
	// 它会读到仓外内容。
	outside := filepath.Join(filepath.Dir(root), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "goal.md"), []byte(completeGoalForTest), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"../../../outside", "demo/../demo", "/abs/path", ".."} {
		setGoalFlags(t, id, "", "")
		_, _, err := resolveGoalContract(taskStartCmd, root)
		if err == nil {
			t.Errorf("--change %q 不应被接受", id)
			continue
		}
		// 断言拒绝理由来自 id 校验，而不是碰巧读不到文件。
		if !strings.Contains(err.Error(), "不合法") {
			t.Errorf("--change %q 应以「change id 不合法」拒绝，got %v", id, err)
		}
	}
}
