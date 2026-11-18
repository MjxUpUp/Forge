package clitask

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

func redFirstHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestCheckRedFirst 钉住 red-first-evidence 的完成侧校验：没有红快照就没有
// 「测试先行」的机械证据；红记录必须是当前历史的祖先；同一 Run 必须随后转绿。
func TestRedAttempts_CheckRedFirst(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	head := redFirstHead(t, dir)

	green := func() *tasktypes.TaskState {
		return &tasktypes.TaskState{
			TaskRef:    "feat/x",
			Acceptance: []tasktypes.AcceptanceCriterion{{Run: "true", Passed: true}},
		}
	}

	t.Run("没有红声明时不受约束", func(t *testing.T) {
		// 红先绿是声明式事实：没登记 --accept-red 不因缺证据被拦（否则文档/重构
		// 类任务被迫编造红灯）。缺证据由 report 的「未登记」披露。
		if err := CheckRedFirst(dir, green()); err != nil {
			t.Fatalf("没有 --accept-red 声明不应被拦: %v", err)
		}
	})

	t.Run("allow-green 放行", func(t *testing.T) {
		s := green()
		s.AllowGreen = true
		if err := CheckRedFirst(dir, s); err != nil {
			t.Fatalf("--allow-green 应放行: %v", err)
		}
	})

	t.Run("红且已转绿放行", func(t *testing.T) {
		s := green()
		s.RedAttempts = []tasktypes.RedAttempt{{Run: "true", Exit: 1, Head: head, At: time.Now().UTC().Format(time.RFC3339)}}
		if err := CheckRedFirst(dir, s); err != nil {
			t.Fatalf("红→绿应放行: %v", err)
		}
	})

	t.Run("红但未转绿拒绝", func(t *testing.T) {
		s := green()
		s.Acceptance[0].Passed = false
		s.RedAttempts = []tasktypes.RedAttempt{{Run: "true", Exit: 1, Head: head, At: time.Now().UTC().Format(time.RFC3339)}}
		err := CheckRedFirst(dir, s)
		if err == nil || !strings.Contains(err.Error(), "未闭合") {
			t.Fatalf("红但没转绿必须拒绝, got %v", err)
		}
	})

	t.Run("开工即全绿拒绝", func(t *testing.T) {
		s := green()
		s.RedAttempts = []tasktypes.RedAttempt{{Run: "true", Exit: 0, Head: head, At: time.Now().UTC().Format(time.RFC3339)}}
		err := CheckRedFirst(dir, s)
		if err == nil || !strings.Contains(err.Error(), "先失败证明缺失") {
			t.Fatalf("开工即全绿必须拒绝, got %v", err)
		}
	})

	t.Run("红记录来自改写掉的历史拒绝", func(t *testing.T) {
		s := green()
		s.RedAttempts = []tasktypes.RedAttempt{{Run: "true", Exit: 1, Head: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", At: time.Now().UTC().Format(time.RFC3339)}}
		err := CheckRedFirst(dir, s)
		if err == nil || !strings.Contains(err.Error(), "不在当前分支历史里") {
			t.Fatalf("非祖先红记录必须拒绝, got %v", err)
		}
	})
}

// TestRequireCriteriaForChange 钉住 F1：带 --change 的任务必须在开工时就有判据；
// --allow-empty-criteria 是显式留痕例外。
func TestTaskStart_ChangeRequiresCriteria(t *testing.T) {
	one := []tasktypes.AcceptanceCriterion{{Run: "go build ./..."}}

	if err := requireCriteriaForChange("", nil, false, &tasktypes.TaskState{}); err != nil {
		t.Fatalf("无 --change 不应受 F1 约束: %v", err)
	}
	if err := requireCriteriaForChange("c1", one, false, &tasktypes.TaskState{}); err != nil {
		t.Fatalf("有判据应放行: %v", err)
	}
	s := &tasktypes.TaskState{}
	err := requireCriteriaForChange("c1", nil, false, s)
	if err == nil || !strings.Contains(err.Error(), "至少登记一条验收判据") {
		t.Fatalf("带 --change 零判据必须拒绝, got %v", err)
	}
	if s.AllowEmptyCriteria {
		t.Error("被拒绝时不得置位 allow_empty_criteria")
	}
	s2 := &tasktypes.TaskState{}
	if err := requireCriteriaForChange("c1", nil, true, s2); err != nil {
		t.Fatalf("--allow-empty-criteria 应放行: %v", err)
	}
	if !s2.AllowEmptyCriteria {
		t.Error("留痕例外必须写进任务状态（report 要能看见）")
	}
}

// gitHeadForTest 取当前 HEAD（测试里只用来造红快照的祖先校验）。
func gitHeadForTest(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestTaskComplete_RedFirst 钉住 task complete 的红先绿前置：红判据必须已经在同一
// 命令上转绿；开工即全绿的判据要么拒绝，要么 --allow-green 显式留痕。
func TestTaskComplete_RedFirst(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	head := gitHeadForTest(t, dir)

	green := func() *tasktypes.TaskState {
		return &tasktypes.TaskState{
			Acceptance:  []tasktypes.AcceptanceCriterion{{Run: "test -f f.txt", Passed: true}},
			RedAttempts: []tasktypes.RedAttempt{{Run: "test -f f.txt", Exit: 1, Head: head}},
		}
	}
	if err := CheckRedFirst(dir, green()); err != nil {
		t.Fatalf("红→绿闭环应放行: %v", err)
	}

	onlyGreen := green()
	onlyGreen.RedAttempts[0].Exit = 0
	if err := CheckRedFirst(dir, onlyGreen); err == nil {
		t.Fatal("开工即全绿的判据必须拒绝")
	} else if !strings.Contains(err.Error(), "先失败证明缺失") {
		t.Fatalf("拒绝文案不对: %v", err)
	}
	onlyGreen.AllowGreen = true
	if err := CheckRedFirst(dir, onlyGreen); err != nil {
		t.Fatalf("--allow-green 留痕后应放行: %v", err)
	}
}
