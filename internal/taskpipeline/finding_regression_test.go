package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskcontext"
)

// finding_regression_test.go — L4 校验行为钉（配对 finding_regression.go）。

// TestRequireFindingRegression_RevalidatesBinding: a recorded file binding is
// re-validated at consumption — deleting the test file invalidates the GREEN
// half (review P2-2: the binding must not be a ghost).
//
// TestRequireFindingRegression_RevalidatesBinding：文件绑定在消费侧复验——
// 删掉测试文件后「绿」半边失效（审查 P2-2：绑定不得是幽灵）。
func TestRequireFindingRegression_RevalidatesBinding(t *testing.T) {
	dir, state := questioningFixture(t, false)
	if err := os.WriteFile(filepath.Join(dir, "solver_test.go"),
		[]byte("package q\n\nimport \"testing\"\n\nfunc TestSolve(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MutateTaskState(dir, state.TaskRef, func(s *TaskState) error {
		s.AddFinding(Finding{ID: "f-re", Content: "越界"})
		if s.RegressionTests == nil {
			s.RegressionTests = map[string]string{}
		}
		s.RegressionTests["f-re"] = "solver_test.go"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// 消费侧读盘上状态（MutateTaskState 写的是盘；内存副本是陈旧的）。
	disk, err := LoadTaskState(dir, state.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := RequireFindingRegression(dir, disk, "f-re"); err != nil {
		t.Fatalf(`绑定有效时应放行: %v`, err)
	}
	if err := os.Remove(filepath.Join(dir, "solver_test.go")); err != nil {
		t.Fatal(err)
	}
	if err := RequireFindingRegression(dir, disk, "f-re"); err == nil || !strings.Contains(err.Error(), "失效") {
		t.Fatalf(`测试文件删除后绑定应失效: %v`, err)
	}
}

// TestTaskChangedFilesStrict：门禁侧要能区分「真的零改动」与「git 读不出来」。
// 非 git 目录（大量单元夹具）沿用旧行为返回空集不报错；在真仓库里返回工作树改动。
func TestTaskChangedFilesStrict(t *testing.T) {
	plain := t.TempDir()
	if files, err := TaskChangedFilesStrict(plain, &TaskState{}); err != nil || len(files) != 0 {
		t.Fatalf("非 git 目录应返回空集且无错误: files=%v err=%v", files, err)
	}

	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@example.com")
	runGit(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := TaskChangedFilesStrict(dir, &TaskState{HeadCommit: base})
	if err != nil {
		t.Fatalf("git 仓库读改动不应报错: %v", err)
	}
	found := false
	for _, f := range files {
		if filepath.ToSlash(f) == "b.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("严格版应看到工作树新增 b.go，实得 %v", files)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

// TestIsGoTestFileWithTests: the three-way validation (suffix / task window /
// real test funcs) rejects each failure mode.
//
// TestIsGoTestFileWithTests：三重校验（后缀/任务窗口/真有测试函数）逐失败面
// 拒绝。
func TestIsGoTestFileWithTests(t *testing.T) {
	dir, state := questioningFixture(t, false)
	if err := os.WriteFile(filepath.Join(dir, "solver_test.go"), []byte("package q\n\nimport \"testing\"\n\nfunc TestSolve(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plain.go"), []byte("package q\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := TaskChangedFiles(dir, state)
	if !IsGoTestFileWithTests(dir, "solver_test.go", changed) {
		t.Error(`任务窗口内含 Test 函数的 _test.go 应通过`)
	}
	if IsGoTestFileWithTests(dir, "calc.go", changed) {
		t.Error(`非 _test.go 应拒绝`)
	}
	if IsGoTestFileWithTests(dir, "calc_test.go", []string{}) {
		t.Error(`不在任务窗口的文件应拒绝（拿旧测试冒充回归）`)
	}
}

// TestRequireFindingRegression_IntegrityBroken（复审 P1-2 测试缺口）：手改盘上
// state 注入绑定 → 验签失败 → resolve 前置拒绝（手改的 gate-satisfying 字段
// 不被采信，后续重签名洗不白）。
func TestRequireFindingRegression_IntegrityBroken(t *testing.T) {
	withTestIdentity(t)
	dir := t.TempDir()
	state := &TaskState{TaskRef: "tampered", Branch: "feat/x"}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	sPath := filepath.Join(dataHome(dir), "tasks", taskcontext.SanitizeRef(state.TaskRef)+".json")
	body, err := os.ReadFile(sPath)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(body), "{", `{"regression_tests":{"f-1":"none:hand-edited"},`, 1)
	if err := os.WriteFile(sPath, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadTaskState(dir, state.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.IntegrityBroken() {
		t.Fatal(`前置：手改后的 state 应带 IntegrityBroken 标记（withTestIdentity 已启用签名）`)
	}
	got := RequireFindingRegression(dir, reloaded, "f-1")
	if got == nil || !strings.Contains(got.Error(), "完整性校验失败") {
		t.Fatalf(`手改 state 注入的绑定应被验签拒绝: %v`, got)
	}
}
