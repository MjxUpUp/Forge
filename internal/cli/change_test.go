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
	if err := changeNewCmd.Flags().Set("by-human", "true"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = changeNewCmd.Flags().Set("fast", "false")
		_ = changeNewCmd.Flags().Set("by-human", "false")
	}()

	_, _, err := captureOutput(t, func() error {
		return runChangeNew(changeNewCmd, []string{"tiny"})
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "spec", "changes", "tiny")
	for _, name := range []string{change.FileGoal, change.FileProposal, change.FileAcceptance, change.FastFile} {
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

	if err := runChangeAgents(changeAgentsCmd, nil); err != nil {
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
	if err := runChangeAgents(changeAgentsCmd, nil); err != nil {
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
	if err := runChangeAgents(changeAgentsCmd, nil); err == nil {
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
	stdout, _, err := captureOutput(t, func() error {
		return runChangeCheck(changeCheckCmd, nil)
	})
	if !errors.Is(err, errHardExit) {
		t.Fatalf("只暂存代码时也必须检查全部 change（未封印的 demo 应拦下），got %v", err)
	}
	if !strings.Contains(stdout, "改为检查全部 change") {
		t.Errorf("退化检查应说明原因：\n%s", stdout)
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
		t.Errorf("没有 change 时 skipped 该是空串：%s", stdout)
	}
	// 契约是「所有路径同一份形状」：skipped 即使为空也要出现。
	if !strings.Contains(stdout, `"skipped"`) {
		t.Errorf("正常路径也应有 skipped 键：%s", stdout)
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

func TestChangeCheck_StagedNonGitFallsBackWithNote(t *testing.T) {
	root := contractRepo(t) // 不 git init
	if _, err := change.New(root, "demo", "示例"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if err := changeCheckCmd.Flags().Set("staged", "true"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = changeCheckCmd.Flags().Set("staged", "false") }()
	if err := changeCheckCmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = changeCheckCmd.Flags().Set("json", "false") }()

	stdout, _, err := captureOutput(t, func() error {
		return runChangeCheck(changeCheckCmd, nil)
	})
	if !errors.Is(err, errHardExit) {
		t.Fatalf("非 git 也要检查全部 change，未封印的 demo 应拦下，got %v", err)
	}
	// 退化是 note，不是 skipped：skipped 只表示"什么都没查"，两者不该同时有。
	var payload struct {
		Checked  []string `json:"checked"`
		Problems []any    `json:"problems"`
		Skipped  string   `json:"skipped"`
		Note     string   `json:"note"`
	}
	if uerr := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &payload); uerr != nil {
		t.Fatalf("输出不是合法 JSON：%v\n%s", uerr, stdout)
	}
	if payload.Skipped != "" || payload.Note == "" || len(payload.Checked) != 1 || len(payload.Problems) != 1 {
		t.Fatalf("退化应有 note、无 skipped，且 checked/problems 非空：%s", stdout)
	}
}

func TestChangeStatus_ShowsPriorAttempts(t *testing.T) {
	root := contractRepo(t)
	if _, err := change.New(root, "demo", "示例"); err != nil {
		t.Fatal(err)
	}
	ev := filepath.Join(root, "spec", "changes", "demo", "evidence", "20260101T000000Z")
	if err := os.MkdirAll(ev, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(change.Summary{
		Change: "demo", Passed: false,
		Checks: []change.Check{{Index: 1, Command: "go test ./x", Exit: 1, Shell: "bash", StderrFile: "stderr.txt"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ev, "summary.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	stdout, _, err := captureOutput(t, func() error {
		return runChangeStatus(changeStatusCmd, []string{"demo"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"此前失败的尝试",
		"20260101T000000Z",
		"go test ./x（exit 1，bash）",
		"evidence/20260101T000000Z/stderr.txt",
		"别重试已证伪的路",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("状态输出缺 %q：\n%s", want, stdout)
		}
	}
}

func TestChangeAgents_PointersFlag(t *testing.T) {
	root := contractRepo(t)
	t.Chdir(root)

	reset := func() { _ = changeAgentsCmd.Flags().Set("pointers", "") }
	reset()
	defer reset()

	// 默认：没有入口文件就不建，输出说明怎么启用。
	stdout, _, err := captureOutput(t, func() error {
		return runChangeAgents(changeAgentsCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "CLAUDE.md：不存在，未创建") || !strings.Contains(stdout, "--pointers claude") {
		t.Errorf("默认应跳过并给启用提示：\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(root, change.ClaudeFile)); !os.IsNotExist(err) {
		t.Fatal("默认不该创建 CLAUDE.md")
	}

	// 点名 all：两个都创建，再跑一次不变。
	if err := changeAgentsCmd.Flags().Set("pointers", "all"); err != nil {
		t.Fatal(err)
	}
	stdout, _, err = captureOutput(t, func() error {
		return runChangeAgents(changeAgentsCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CLAUDE.md：已新建指针", ".cursor/rules/forge.mdc：已新建指针"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("缺 %q：\n%s", want, stdout)
		}
	}
	stdout, _, err = captureOutput(t, func() error {
		return runChangeAgents(changeAgentsCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "CLAUDE.md：指针已是最新") {
		t.Errorf("第二次应报告已是最新：\n%s", stdout)
	}

	// 未知取值要报错，而不是静默忽略。
	if err := changeAgentsCmd.Flags().Set("pointers", "bogus"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := captureOutput(t, func() error {
		return runChangeAgents(changeAgentsCmd, nil)
	}); err == nil {
		t.Fatal("未知 --pointers 取值应报错")
	}
}

// init 时仓库若已经在用 Claude Code，顺手把 CLAUDE.md 指到 AGENTS.md。
func TestChangeInit_PointsExistingClaudeFile(t *testing.T) {
	root := t.TempDir()
	skipIfAncestorHasContract(t, root)
	human := "# 我的约定\n\n- 先跑 make check\n"
	if err := os.WriteFile(filepath.Join(root, change.ClaudeFile), []byte(human), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	stdout, _, err := captureOutput(t, func() error {
		return runChangeInit(changeInitCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "CLAUDE.md（指针 → AGENTS.md）") {
		t.Errorf("init 应报告指针已就位：\n%s", stdout)
	}
	got, err := os.ReadFile(filepath.Join(root, change.ClaudeFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), human) || !strings.Contains(string(got), "@AGENTS.md") {
		t.Fatalf("CLAUDE.md 应保留人工内容并追加导入：\n%s", got)
	}
}

// skipIfAncestorHasContract 跳过受宿主环境影响的用例：changeRoot 的成员资格
// 解析会沿目录向上找 spec/forge.yaml，temp 目录的祖先里恰好有契约时（开发机的
// 沙盒残留、共享 /tmp），本用例会在那里 init，断言必然错位。
func skipIfAncestorHasContract(t *testing.T, root string) {
	t.Helper()
	for dir := filepath.Dir(root); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "spec", "forge.yaml")); err == nil {
			t.Skipf("祖先目录 %s 已有 spec/forge.yaml，跳过依赖根解析的用例", dir)
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
}

func TestParsePointerNames_TrimsNoneAndRejectsMixed(t *testing.T) {
	for _, raw := range []string{"", "  ", "none", " none "} {
		got, _, err := parsePointerNames(raw)
		if err != nil || len(got) != 0 {
			t.Fatalf("%q 应表示不动指针，got %v err %v", raw, got, err)
		}
	}
	if _, _, err := parsePointerNames("all,bogus"); err == nil {
		t.Fatal("all 后面的非法取值不能被静默吞掉")
	}
	if _, _, err := parsePointerNames("none,claude"); err == nil {
		t.Fatal("none 不能和别的取值混写")
	}
	got, _, err := parsePointerNames(" cursor , claude ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "claude" || got[1] != "cursor" {
		t.Fatalf("解析结果应固定顺序 claude,cursor，got %v", got)
	}
}

func TestChangeAgents_NoneWithSpacesLeavesPointersAlone(t *testing.T) {
	root := contractRepo(t)
	t.Chdir(root)
	human := "# 手写 CLAUDE.md\n"
	if err := os.WriteFile(filepath.Join(root, change.ClaudeFile), []byte(human), 0o644); err != nil {
		t.Fatal(err)
	}
	reset := func() { _ = changeAgentsCmd.Flags().Set("pointers", "") }
	reset()
	defer reset()
	if err := changeAgentsCmd.Flags().Set("pointers", " none "); err != nil {
		t.Fatal(err)
	}
	if _, _, err := captureOutput(t, func() error {
		return runChangeAgents(changeAgentsCmd, nil)
	}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, change.ClaudeFile)); string(got) != human {
		t.Fatalf("--pointers \" none \" 不该碰指针文件，got %q", got)
	}
}

func TestChangeStatus_StderrPathIsRepoRelativeOnce(t *testing.T) {
	root := contractRepo(t)
	if _, err := change.New(root, "demo", "示例"); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"goal.md":       "# 示例：目标契约\n\n## Objective\n\n跑一次失败判据。\n\n## Why\n\n验证证据路径。\n\n## Constraints\n\n不改实现。\n\n## Out of Scope\n\n不做修复。\n\n## Check\n\nacceptance #1。\n",
		"proposal.md":   "# 提案\n\ndemo\n",
		"design.md":     "# 设计\n\ndemo\n",
		"tasks.md":      "# 任务\n\n- [ ] 1. 跑\n\n判据：acceptance #1\n",
		"acceptance.md": "# 验收\n\n```bash\nls /definitely/not/here\n```\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, "spec", "changes", "demo", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	if _, _, err := captureOutput(t, func() error {
		return runChangeVerify(changeVerifyCmd, []string{"demo"})
	}); err == nil {
		t.Fatal("判据失败应报错")
	}
	stdout, _, err := captureOutput(t, func() error {
		return runChangeStatus(changeStatusCmd, []string{"demo"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "spec/changes/demo/evidence/") {
		t.Fatalf("证据路径应按仓库根打印：\n%s", stdout)
	}
	if strings.Contains(stdout, ".stderr/") || strings.Contains(stdout, ".stdout/") {
		t.Fatalf("文件名不该重复拼接：\n%s", stdout)
	}
}

func TestChangeAgents_NoneWithTrailingCommaLeavesPointersAlone(t *testing.T) {
	for _, raw := range []string{"none,", "none ,", " none,"} {
		t.Run(raw, func(t *testing.T) {
			root := contractRepo(t)
			t.Chdir(root)
			human := "# 手写 CLAUDE.md\n"
			if err := os.WriteFile(filepath.Join(root, change.ClaudeFile), []byte(human), 0o644); err != nil {
				t.Fatal(err)
			}
			reset := func() { _ = changeAgentsCmd.Flags().Set("pointers", "") }
			reset()
			defer reset()
			if err := changeAgentsCmd.Flags().Set("pointers", raw); err != nil {
				t.Fatal(err)
			}
			if _, _, err := captureOutput(t, func() error {
				return runChangeAgents(changeAgentsCmd, nil)
			}); err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(filepath.Join(root, change.ClaudeFile)); string(got) != human {
				t.Fatalf("--pointers %q 不该碰指针文件，got %q", raw, got)
			}
		})
	}
}

func TestChangeNew_WritesGoalContract(t *testing.T) {
	root := contractRepo(t)
	t.Chdir(root)
	if _, _, err := captureOutput(t, func() error {
		return runChangeNew(changeNewCmd, []string{"with-goal"})
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "spec", "changes", "with-goal", change.FileGoal)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("change new 应生成 goal.md: %v", err)
	}
	if _, perr := change.ParseGoal(string(raw)); perr == nil {
		t.Fatal("新生成的 goal.md 是骨架，ParseGoal 必须报未完成")
	}
}

func TestChangeNewFast_RequiresHumanApproval(t *testing.T) {
	root := contractRepo(t)
	t.Chdir(root)
	if err := changeNewCmd.Flags().Set("fast", "true"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = changeNewCmd.Flags().Set("fast", "false") }()

	_, _, err := captureOutput(t, func() error {
		return runChangeNew(changeNewCmd, []string{"sneaky"})
	})
	if err == nil || !strings.Contains(err.Error(), "--by-human") {
		t.Fatalf("缺 --by-human 应拒绝 fast，got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "spec", "changes", "sneaky")); statErr == nil {
		t.Fatal("被拒绝的 fast 不应留下目录")
	}
}

func TestChangeNewFast_WithHumanApproval(t *testing.T) {
	root := contractRepo(t)
	t.Chdir(root)
	for flag, val := range map[string]string{"fast": "true", "by-human": "true"} {
		if err := changeNewCmd.Flags().Set(flag, val); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_ = changeNewCmd.Flags().Set("fast", "false")
		_ = changeNewCmd.Flags().Set("by-human", "false")
	}()

	_, _, err := captureOutput(t, func() error {
		return runChangeNew(changeNewCmd, []string{"approved"})
	})
	if err != nil {
		t.Fatalf("带 --by-human 应通过: %v", err)
	}
	dir := filepath.Join(root, "spec", "changes", "approved")
	for _, name := range []string{change.FileGoal, change.FileProposal, change.FileAcceptance} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("approved fast change 缺 %s: %v", name, err)
		}
	}
}
