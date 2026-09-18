package cli

// selfcheck_test.go —— discipline-first-gates P2 的 CLI 守卫：无任务拒绝、配对
// 发现项输出+落痕+exit 语义、干净路径输出确认。计算正确性由 taskpipeline 侧
// TestSelfcheckPairingUntracked/TestSelfcheckScopeDrift 钉住，此处只钉命令层。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// selfcheckGitProject 建临时 git 仓库、在全局注册表登记为 managed 项目（projectroot
// 的成员判定走注册表）、隔离 FORGE_DATA_HOME（checklog 落盘走用户级 DataDir），并
// Chdir 进去（findProjectRoot 按 cwd 解析）。
func selfcheckGitProject(t *testing.T) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	for _, a := range [][]string{
		{"init"}, {"config", "user.email", "t@t.com"}, {"config", "user.name", "T"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, a...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	if err := registry.SetStatus(dir, registry.StatusManaged, "selfcheck-test"); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	return dir
}

// bindSelfcheckTask 把 session 绑定到未完成任务并落 state。
func bindSelfcheckTask(t *testing.T, root, sessionID, ref string) {
	t.Helper()
	t.Setenv("FORGE_SESSION_ID", sessionID)
	if err := taskpipeline.SetActiveTaskRef(root, sessionID, ref); err != nil {
		t.Fatal(err)
	}
	st := &taskpipeline.TaskState{TaskRef: ref, SessionID: sessionID, Branch: ref}
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		t.Fatal(err)
	}
}

func TestSelfcheckPairingNoActiveTask(t *testing.T) {
	selfcheckGitProject(t)
	t.Setenv("FORGE_SESSION_ID", "sess-sc-none")

	err := runSelfcheckPairing(selfcheckPairingCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "无活跃任务") {
		t.Fatalf("no active task must refuse with 无活跃任务, got: %v", err)
	}
}

func TestSelfcheckPairingFindsUnpairedAndRecords(t *testing.T) {
	root := selfcheckGitProject(t)
	bindSelfcheckTask(t, root, "sess-sc-1", "feat/sc-cli")

	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte("package p\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSelfcheckPairing(selfcheckPairingCmd, nil)
	})
	if runErr == nil {
		t.Fatal("unpaired source must exit non-nil (fact for scripts, not a block)")
	}
	if !strings.Contains(out, "x.go") || !strings.Contains(out, "1/1") {
		t.Errorf("output must name the unpaired file with counts, got: %q", out)
	}
	entries, err := checklog.LoadForTask(root, "feat/sc-cli")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Check == checklog.CheckSelfcheckPairing {
			found = true
			if e.Passed {
				t.Error("entry Passed must be false (1 unpaired file)")
			}
			if e.Meta["missing_files"] != "1" || e.Meta["missing_list"] != "x.go" {
				t.Errorf("entry meta = %v, want missing_files=1 missing_list=x.go", e.Meta)
			}
		}
	}
	if !found {
		t.Fatal("selfcheck-pairing entry must be recorded for the task")
	}
}

func TestSelfcheckPairingCleanPasses(t *testing.T) {
	root := selfcheckGitProject(t)
	bindSelfcheckTask(t, root, "sess-sc-2", "feat/sc-clean")

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSelfcheckPairing(selfcheckPairingCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("clean tree must pass, got: %v", runErr)
	}
	if !strings.Contains(out, "配对干净") {
		t.Errorf("clean output must confirm, got: %q", out)
	}
}

func TestSelfcheckScopeWithoutDeclarationIsNoop(t *testing.T) {
	root := selfcheckGitProject(t)
	bindSelfcheckTask(t, root, "sess-sc-3", "feat/sc-scope-cli")

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSelfcheckScope(selfcheckScopeCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("undeclared PlanScope is a no-op probe, got: %v", runErr)
	}
	if !strings.Contains(out, "PlanScope") {
		t.Errorf("output must explain the undeclared scope, got: %q", out)
	}
}

// TestSelfcheckPairingEscapeActive pins the mirror-honesty contract (guard
// supervision P2-3): under an active escape the GATE waives the check while
// selfcheck still reports facts — the command must label that divergence AND
// keep listing the files, or it teaches "escape = all clean" backwards. The
// entry is still recorded (Passed=false, real missing_list).
//
// TestSelfcheckPairingEscapeActive 钉住镜像诚实契约（守护监督 P2-3）：逃生激活下
// **门禁**豁免本检查而 selfcheck 照报事实——命令必须标注口径差**且**继续列文件，
// 否则反向教出「逃生=全干净」。条目照落（Passed=false、真实 missing_list）。
func TestSelfcheckPairingEscapeActive(t *testing.T) {
	root := selfcheckGitProject(t)
	bindSelfcheckTask(t, root, "sess-sc-esc", "feat/sc-escape")
	// 激活 per-task 逃生（与 override --test-coverage disable 同一字段语义）。
	st, err := taskpipeline.LoadTaskState(root, "feat/sc-escape")
	if err != nil {
		t.Fatal(err)
	}
	st.Overrides.TestCoverage = "disable"
	if err := taskpipeline.SaveTaskState(root, st); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte("package p\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSelfcheckPairing(selfcheckPairingCmd, nil)
	})
	if !strings.Contains(out, "escape active") {
		t.Errorf("escape must be labeled, got: %q", out)
	}
	if !strings.Contains(out, "x.go") {
		t.Errorf("facts must still be reported (file listed) under escape, got: %q", out)
	}
	if runErr == nil {
		t.Error("unpaired files under escape still exit non-nil (fact, not gate)")
	}
	entries, err := checklog.LoadForTask(root, "feat/sc-escape")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Check == checklog.CheckSelfcheckPairing {
			found = true
			if e.Passed || e.Meta["missing_list"] != "x.go" {
				t.Errorf("entry must record the real fact (Passed=false, missing_list=x.go), got %+v", e.Meta)
			}
		}
	}
	if !found {
		t.Fatal("selfcheck-pairing entry must be recorded under escape")
	}
}
