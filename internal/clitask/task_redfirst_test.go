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
			HeadCommit: head,
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

	t.Run("红记录的 head 必须等于开工时刻", func(t *testing.T) {
		// 事后补拍最容易的形态：把红快照的 head 指向别处。红快照必须绑定开工
		// 那一刻的 HeadCommit，否则「开工时是红的」无从证明（review-rfe-1 P3-a：
		// 此前这条校验没有测试，删掉它没有任何测试变红）。
		s := green()
		s.RedAttempts = []tasktypes.RedAttempt{{Run: "true", Exit: 1, Head: "deadbeef", At: time.Now().UTC().Format(time.RFC3339)}}
		err := CheckRedFirst(dir, s)
		if err == nil || !strings.Contains(err.Error(), "未绑定开工时刻") {
			t.Fatalf("红记录 head 与开工 head 不一致必须被拒, got %v", err)
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
		// HeadCommit 与红记录 head 相同，才会走到祖先校验；否则先被「未绑定开工时刻」拦。
		s.HeadCommit = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
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
			HeadCommit:  head,
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

// TestTaskComplete_RedFirstBlocks 走 CLI 路径：红判据一直没转绿时 `task complete`
// 必须被拒（review-rfe-1 P1-a 当初就是「检查没接线，测试直调函数」漏掉的）。
func TestTaskComplete_RedFirstBlocks(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	t.Setenv(`HOME`, t.TempDir())
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@example.com")
	runGit(t, dir, "config", "user.name", "t")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	if out, errs, code := runForge(t, dir, "change", "init"); code != 0 {
		t.Fatalf("change init: %s %s", out, errs)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "init")

	if out, errs, code := runForge(t, dir, "task", "start", "--ref", "feat/unclosed", "--branch",
		"--accept", "test -f f.txt ::", "--accept-red", "test -f never.txt"); code != 0 {
		t.Fatalf("task start: %s %s", out, errs)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "feat: f")
	for _, args := range [][]string{
		{"task", "gate", "task-implement"},
		{"task", "verify-acceptance"},
		{"review", "pass", "--ref", "feat/unclosed", "--reviewer", "rev-unclosed", "--score", "80"},
		{"task", "gate", "task-complete"},
	} {
		if out, errs, code := runForge(t, dir, args...); code != 0 {
			t.Fatalf("%v: %s %s", args, out, errs)
		}
	}
	out, errs, code := runForge(t, dir, "task", "complete")
	if code == 0 {
		t.Fatalf("红判据未转绿时 task complete 应被拒，却通过了: %s", out)
	}
	if !strings.Contains(out+errs, "未闭合") {
		t.Fatalf("拒绝理由应含「未闭合」: %s %s", out, errs)
	}
}

// TestRedAttempts_GoTestNormalized 钉住 go test 的 -v 归一化：task start 会把带
// expected 的 `go test …` 改写成 `go test -v …`，而 --accept-red 原样记录；
// 不归一化就会在 complete 处永久死锁（review-rfe-1 P1-c 实证）。
func TestRedAttempts_GoTestNormalized(t *testing.T) {
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

	s := &tasktypes.TaskState{
		Acceptance:  []tasktypes.AcceptanceCriterion{{Run: "go test -v ./...", Passed: true}},
		HeadCommit:  head,
		RedAttempts: []tasktypes.RedAttempt{{Run: "go test ./...", Exit: 1, Head: head}},
	}
	if err := CheckRedFirst(dir, s); err != nil {
		t.Fatalf("go test 判据按 -v 归一化后应视为同一 run: %v", err)
	}
}

// TestReportEscapeHatch_ZeroCriteria 钉住逃生舱留痕在「零判据 + 零红记录」时仍披露。
func TestReportEscapeHatch_ZeroCriteria(t *testing.T) {
	s := &tasktypes.TaskState{AllowEmptyCriteria: true}
	rep, _ := buildTaskReport("/repo", s)
	if !strings.Contains(rep.RedFirstLine, "--allow-empty-criteria 留痕") {
		t.Fatalf("零判据任务的交付单必须披露留痕, got %q", rep.RedFirstLine)
	}
}

// TestRedAttempts_BackdatedCommits：红快照只能由 task start 当场拍下。若任务开工
// 之后混进了「提交时间早于开工时间」的提交，说明红快照是事后 checkout 回基线补拍的，
// 必须拒绝（review-rfe-1 P2-a）。
func TestRedAttempts_BackdatedCommits(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	start := gitHeadForTest(t, dir)
	started := time.Now().UTC()

	cmd := exec.Command("git", "commit", "--allow-empty", "-m", "backdated")
	cmd.Dir = dir
	// 提交时间早于开工时间 10 分钟：这是「先开工、再回补一个假红」的典型形态。
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE="+started.Add(-10*time.Minute).Format(time.RFC3339),
		"GIT_COMMITTER_DATE="+started.Add(-10*time.Minute).Format(time.RFC3339),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("backdated commit: %v %s", err, out)
	}

	s := &tasktypes.TaskState{
		Acceptance:  []tasktypes.AcceptanceCriterion{{Run: "true", Passed: true}},
		HeadCommit:  start,
		StartedAt:   started,
		RedAttempts: []tasktypes.RedAttempt{{Run: "true", Exit: 1, Head: start, At: started.Format(time.RFC3339)}},
	}
	err := CheckRedFirst(dir, s)
	if err == nil || !strings.Contains(err.Error(), "可疑") {
		t.Fatalf("开工后出现早于开工时间的提交必须报可疑, got %v", err)
	}
}
