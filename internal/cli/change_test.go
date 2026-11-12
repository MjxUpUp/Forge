package cli

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/change"
)

func TestChangeRoot_FallsBackToGitRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用")
	}
	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	got, err := changeRoot()
	if err != nil {
		t.Fatalf("changeRoot: %v", err)
	}
	if !samePath(got, root) {
		t.Errorf("changeRoot=%s，期望 git 根 %s", got, root)
	}
}

func TestChangeRoot_PlainDirUsesCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	got, err := changeRoot()
	if err != nil {
		t.Fatalf("changeRoot: %v", err)
	}
	if !samePath(got, dir) {
		t.Errorf("changeRoot=%s，期望 cwd %s", got, dir)
	}
}

func TestChangeFirstLine(t *testing.T) {
	cases := map[string]string{
		"go build ./...":             "go build ./...",
		"  go test ./internal/...  ": "go test ./internal/...",
		"cd sub\nmake test":          "cd sub …",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q)=%q，期望 %q", in, got, want)
		}
	}
}

func TestChangeDur(t *testing.T) {
	if got := dur(420); got != "420ms" {
		t.Errorf("dur(420)=%q", got)
	}
	if got := dur(2500); got != "2.5s" {
		t.Errorf("dur(2500)=%q", got)
	}
}

func TestChangeEvidenceCell(t *testing.T) {
	if got := evidenceCell(change.Change{}); got != "-" {
		t.Errorf("无证据应显示 -，got %q", got)
	}
	if got := evidenceCell(change.Change{EvidenceCount: 2, Sealed: true}); !strings.Contains(got, "2") || !strings.Contains(got, "✓") {
		t.Errorf("有封印证据应带计数与勾，got %q", got)
	}
	if got := evidenceCell(change.Change{EvidenceCount: 3}); got != "3" {
		t.Errorf("未封印证据只显示计数，got %q", got)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// samePath 比较两个路径，容忍 Windows 短路径（ADMINI~1）与大小写差异。
func samePath(a, b string) bool {
	aa, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	bb, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	if strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb)) {
		return true
	}
	ra, erra := filepath.EvalSymlinks(aa)
	rb, errb := filepath.EvalSymlinks(bb)
	return erra == nil && errb == nil && strings.EqualFold(ra, rb)
}

// contractRepo 建一个已声明契约的临时仓库，返回根目录。
func contractRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := change.Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestChangeStatus_PrintsNextSteps(t *testing.T) {
	root := contractRepo(t)
	if _, err := change.New(root, "demo", "示例"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	stdout, _, err := captureOutput(t, func() error {
		return runChangeStatus(changeStatusCmd, []string{"demo"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"下一步：", "forge:fill"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("状态输出缺少 %q：\n%s", want, stdout)
		}
	}
}

func TestChangeStatus_JSONHasNextSteps(t *testing.T) {
	root := contractRepo(t)
	if _, err := change.New(root, "demo", "示例"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if err := changeStatusCmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = changeStatusCmd.Flags().Set("json", "false") }()

	stdout, _, err := captureOutput(t, func() error {
		return runChangeStatus(changeStatusCmd, []string{"demo"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"next_steps"`) || !strings.Contains(stdout, `"verdict"`) {
		t.Errorf("JSON 输出缺字段：\n%s", stdout)
	}
}

func TestChangeGate_RequiresTaskFlag(t *testing.T) {
	root := contractRepo(t)
	if _, err := change.New(root, "demo", "示例"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	err := runChangeGate(changeGateCmd, []string{"demo"})
	if err == nil || !strings.Contains(err.Error(), "--task") {
		t.Fatalf("缺 --task 时应给出用法错误，got %v", err)
	}
}

func TestChangeNewFast_WritesTwoTemplates(t *testing.T) {
	root := contractRepo(t)
	t.Chdir(root)
	if err := changeNewCmd.Flags().Set("fast", "true"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = changeNewCmd.Flags().Set("fast", "false") }()

	_, _, err := captureOutput(t, func() error {
		return runChangeNew(changeNewCmd, []string{"tiny"})
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "spec", "changes", "tiny")
	for _, name := range []string{change.FileProposal, change.FileAcceptance, change.FastFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("fast change 缺 %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, change.FileDesign)); err == nil {
		t.Error("fast change 不该写 design.md")
	}
}

func TestChangeList_MarksFastChanges(t *testing.T) {
	root := contractRepo(t)
	if _, err := change.NewFast(root, "tiny", "小改"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	stdout, _, err := captureOutput(t, func() error {
		return runChangeList(changeListCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "fast") {
		t.Errorf("列表要能看出 fast change：\n%s", stdout)
	}
}

func TestChangeAgents_AppendsAndPreserves(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if _, err := change.Init(root, "mini"); err != nil {
		t.Fatalf("change.Init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# 团队约定\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runChangeAgents(nil, nil); err != nil {
		t.Fatalf("runChangeAgents: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "# 团队约定\n") || !strings.Contains(string(raw), "forge:begin") {
		t.Fatalf("AGENTS.md 结果不对：%q", raw)
	}

	// 再跑一次不报错，也不改文件。
	if err := runChangeAgents(nil, nil); err != nil {
		t.Fatalf("二次 runChangeAgents: %v", err)
	}
	again, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if string(again) != string(raw) {
		t.Fatal("二次同步改动了文件")
	}
}

func TestChangeAgents_RequiresContract(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := runChangeAgents(nil, nil); err == nil {
		t.Fatal("没有 spec/forge.yaml 时应报错并指路 change init")
	} else if !strings.Contains(err.Error(), "forge change init") {
		t.Fatalf("报错应指路 change init，got %v", err)
	}
}

func TestChangeCheck_NoChangesPasses(t *testing.T) {
	root := contractRepo(t)
	t.Chdir(root)
	stdout, _, err := captureOutput(t, func() error {
		return runChangeCheck(changeCheckCmd, nil)
	})
	if err != nil {
		t.Fatalf("没有 change 时不该失败：%v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "没有未归档的 change") {
		t.Errorf("应说明没有可检查的 change：\n%s", stdout)
	}
}

func TestChangeCheck_UnsealedFails(t *testing.T) {
	root := contractRepo(t)
	if _, err := change.New(root, "demo", "示例"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	stdout, _, err := captureOutput(t, func() error {
		return runChangeCheck(changeCheckCmd, nil)
	})
	if !errors.Is(err, errHardExit) {
		t.Fatalf("未封印的 change 应返回 errHardExit，got %v", err)
	}
	for _, want := range []string{"没有封印的证据", "forge change verify demo", "不是让你在循环里重试"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("输出缺少 %q：\n%s", want, stdout)
		}
	}
}

func TestChangeCheck_StagedCatchesCodeOnlyAndTouchedChange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用")
	}
	root := contractRepo(t)
	if _, err := change.New(root, "demo", "示例"); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "config", "user.email", "t@t.t")
	gitRun(t, root, "config", "user.name", "t")
	t.Chdir(root)
	if err := changeCheckCmd.Flags().Set("staged", "true"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = changeCheckCmd.Flags().Set("staged", "false") }()

	// 暂存区只碰了 change 目录之外的代码：封印证据可能早就不对应当前代码了，
	// 只查"被碰到的 change"会放行一个注定在 CI 变红的提交——必须退化为全量。
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "src/main.go")
	_, _, err := captureOutput(t, func() error {
		return runChangeCheck(changeCheckCmd, nil)
	})
	if !errors.Is(err, errHardExit) {
		t.Fatalf("只暂存代码时也必须检查全部 change（未封印的 demo 应拦下），got %v", err)
	}

	// 碰了 demo 自己的产物：只查它，未封印同样拦下。
	gitRun(t, root, "add", "spec/changes/demo/proposal.md")
	_, _, err = captureOutput(t, func() error {
		return runChangeCheck(changeCheckCmd, nil)
	})
	if !errors.Is(err, errHardExit) {
		t.Fatalf("--staged 碰到未封印 change 应拦下，got %v", err)
	}
}

func TestChangeCheck_JSONSkipsWithValidJSON(t *testing.T) {
	root := contractRepo(t)
	t.Chdir(root)
	if err := changeCheckCmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = changeCheckCmd.Flags().Set("json", "false") }()

	parse := func(t *testing.T, stdout string) (checked, problems []string, skipped string) {
		t.Helper()
		var payload struct {
			Checked  []string `json:"checked"`
			Problems []any    `json:"problems"`
			Skipped  string   `json:"skipped"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &payload); err != nil {
			t.Fatalf("输出不是合法 JSON：%v\n%s", err, stdout)
		}
		if payload.Checked == nil || payload.Problems == nil {
			t.Fatalf("空列表要写 [] 而不是 null：%s", stdout)
		}
		p := make([]string, 0, len(payload.Problems))
		return payload.Checked, p, payload.Skipped
	}

	// 有 spec/changes/ 但没有 change：合法 JSON，空数组。
	stdout, _, err := captureOutput(t, func() error { return runChangeCheck(changeCheckCmd, nil) })
	if err != nil {
		t.Fatalf("没有 change 不是失败：%v\n%s", err, stdout)
	}
	if _, _, skipped := parse(t, stdout); skipped != "" {
		t.Errorf("没有 change 时不该写 skipped：%s", stdout)
	}

	// 连 spec/changes/ 都没有：跳过路径也必须是同一份 JSON，并写清原因。
	if err := os.RemoveAll(filepath.Join(root, "spec", "changes")); err != nil {
		t.Fatal(err)
	}
	stdout, _, err = captureOutput(t, func() error { return runChangeCheck(changeCheckCmd, nil) })
	if err != nil {
		t.Fatalf("跳过不是失败：%v\n%s", err, stdout)
	}
	if _, _, skipped := parse(t, stdout); skipped == "" {
		t.Errorf("跳过时要写 skipped 原因：%s", stdout)
	}
}

func TestChangeCheck_JSONAlsoExitsHard(t *testing.T) {
	root := contractRepo(t)
	if _, err := change.New(root, "demo", "示例"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if err := changeCheckCmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = changeCheckCmd.Flags().Set("json", "false") }()

	stdout, _, err := captureOutput(t, func() error {
		return runChangeCheck(changeCheckCmd, nil)
	})
	if !errors.Is(err, errHardExit) {
		t.Fatalf("JSON 模式不合格时也应返回 errHardExit，got %v", err)
	}
	for _, want := range []string{`"checked"`, `"problems"`, `"demo"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("JSON 输出缺少 %q：\n%s", want, stdout)
		}
	}
}
