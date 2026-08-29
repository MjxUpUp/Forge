package skilltrigger

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/attribution"
	"github.com/MjxUpUp/Forge/internal/skillsqa"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// runGit 在 dir 跑 git 命令，失败即终止（fixture 建立，无降级语义）。
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestCondSourceChanged(t *testing.T) {
	// 空 ProjectRoot / 非 git dir → false（优雅降级）
	if condSourceChanged(Context{}) {
		t.Fatal("空 ProjectRoot 应 false")
	}
	if condSourceChanged(Context{ProjectRoot: t.TempDir()}) {
		t.Fatal("非 git dir 应 false")
	}

	// 真实 git 仓库
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.t")
	runGit(t, dir, "config", "user.name", "t")

	if condSourceChanged(Context{ProjectRoot: dir}) {
		t.Fatal("空 git 仓库应 false")
	}

	// 未跟踪 .go → true
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x"), 0644); err != nil {
		t.Fatal(err)
	}
	if !condSourceChanged(Context{ProjectRoot: dir}) {
		t.Fatal("未跟踪 .go 应 true")
	}

	// 跟踪 + commit 后无变更 → false
	runGit(t, dir, "add", "a.go")
	runGit(t, dir, "commit", "-m", "init")
	if condSourceChanged(Context{ProjectRoot: dir}) {
		t.Fatal("已 commit 无变更应 false")
	}

	// 修改已跟踪 .go → true
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package y // changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if !condSourceChanged(Context{ProjectRoot: dir}) {
		t.Fatal("已跟踪 .go 被修改应 true")
	}

	// 恢复 a.go 干净，排除源码变更干扰后再测非源码文件
	runGit(t, dir, "checkout", "--", "a.go")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# x"), 0644); err != nil {
		t.Fatal(err)
	}
	if condSourceChanged(Context{ProjectRoot: dir}) {
		t.Fatal(".md 变更应 false（非源码扩展名）")
	}
}

func TestCondTestCommandFailed(t *testing.T) {
	tests := []struct {
		name string
		ctx  Context
		want bool
	}{
		{
			"go test 失败",
			Context{ToolInput: map[string]any{"command": "go test ./..."}, ToolOutput: map[string]any{"exit_code": float64(1)}},
			true,
		},
		{
			"go test 通过",
			Context{ToolInput: map[string]any{"command": "go test ./..."}, ToolOutput: map[string]any{"exit_code": float64(0)}},
			false,
		},
		{
			"pytest 失败（int exit_code）",
			Context{ToolInput: map[string]any{"command": "pytest -x"}, ToolOutput: map[string]any{"exit_code": 2}},
			true,
		},
		{
			"非测试命令失败",
			Context{ToolInput: map[string]any{"command": "ls -la"}, ToolOutput: map[string]any{"exit_code": float64(1)}},
			false,
		},
		{
			"无 command",
			Context{ToolOutput: map[string]any{"exit_code": float64(1)}},
			false,
		},
		{
			"测试命令但缺 exit_code（无法判定）",
			Context{ToolInput: map[string]any{"command": "go test ./..."}, ToolOutput: map[string]any{}},
			false,
		},
		{
			"测试命令 interrupted（用户中断也算失败）",
			Context{ToolInput: map[string]any{"command": "go test ./..."}, ToolOutput: map[string]any{"interrupted": true}},
			true,
		},
		{
			"npm run test 失败（字符串 exit_code）",
			Context{ToolInput: map[string]any{"command": "npm run test"}, ToolOutput: map[string]any{"exit_code": "1"}},
			true,
		},
		{
			"lngo test（word-boundary：lngo 中 go 前 r 是单词字符，非 go test）",
			Context{ToolInput: map[string]any{"command": "lngo test"}, ToolOutput: map[string]any{"exit_code": float64(1)}},
			false,
		},
		{
			"go testbed（word-boundary：testbed 中 test 后 bed 是单词字符，非 test）",
			Context{ToolInput: map[string]any{"command": "go testbed"}, ToolOutput: map[string]any{"exit_code": float64(1)}},
			false,
		},
		{
			"缺 exit_code 但输出含 --- FAIL（kimi 宿主降级：失败签名）",
			Context{ToolInput: map[string]any{"command": "go test ./..."}, ToolOutput: map[string]any{"output": "=== RUN TestX\n--- FAIL: TestX (0.00s)\nFAIL\nexit status 1"}},
			true,
		},
		{
			"缺 exit_code 输出干净（ok 摘要，无失败签名）",
			Context{ToolInput: map[string]any{"command": "go test ./..."}, ToolOutput: map[string]any{"output": "ok  \tpkg\t0.01s"}},
			false,
		},
		{
			"缺 exit_code，fail 一词不在行首（防误报）",
			Context{ToolInput: map[string]any{"command": "go test ./..."}, ToolOutput: map[string]any{"output": "covering cases where tests fail gracefully: all pass"}},
			false,
		},
	}
	for _, tt := range tests {
		if got := condTestCommandFailed(tt.ctx); got != tt.want {
			t.Errorf("%s: got %v want %v", tt.name, got, tt.want)
		}
	}
}

func TestCondCodingIntent(t *testing.T) {
	tests := []struct {
		prompt string
		want   bool
	}{
		{"帮我实现一个排序算法", true},
		{"重构这个模块", true},
		{"修复这个 bug", true},
		{"新增一个接口", true},
		{"refactor the auth module", true},
		{"fix the login error", true},
		{"implement feature X", true},
		{"看看这段代码", false},
		{"", false},
		{"今天天气不错", false},
		{"git 是什么", false},
		{"what is the prefix of this string", false}, // F4: prefix 含 fix 但 \bfix\b 不匹配（pre 是单词字符）
		{"explain the suffix array", false},          // F4: suffix 含 fix 但 \bfix\b 不匹配（suf 是单词字符）
		{"building materials are expensive", false},  // F4: building 含 build 但 \bbuild\b 不匹配（ing 接尾）
		{"addition is commutative", false},           // F4: addition 含 add 但 \badd\b 不匹配（ition 接尾）
		{"build the feature now", true},              // \bbuild\b 正常命中
		{"add a unit test", true},                    // \badd\b 正常命中
	}
	for _, tt := range tests {
		if got := condCodingIntent(Context{Prompt: tt.prompt}); got != tt.want {
			t.Errorf("prompt %q: got %v want %v", tt.prompt, got, tt.want)
		}
	}
}

func TestCondSkillFileTouched(t *testing.T) {
	cases := []struct {
		name string
		ctx  Context
		want bool
	}{
		{"Write file_path 指向 SKILL.md", Context{ToolInput: map[string]any{"file_path": "skills/foo/SKILL.md"}}, true},
		{"file_path 大小写不敏感", Context{ToolInput: map[string]any{"file_path": "E:\\skills\\foo\\Skill.MD"}}, true},
		{"path 键（kimi 文件类工具）", Context{ToolInput: map[string]any{"path": "skills/foo/SKILL.md"}}, true},
		{"普通源码文件", Context{ToolInput: map[string]any{"file_path": "internal/cli/hook.go"}}, false},
		{"decisions.md 不算（只锚 SKILL.md 行为契约）", Context{ToolInput: map[string]any{"file_path": "skills/foo/decisions.md"}}, false},
		{"无路径输入", Context{ToolInput: map[string]any{}}, false},
		{"空 Context", Context{}, false},
	}
	for _, c := range cases {
		if got := condSkillFileTouched(c.ctx); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestCondTaskActiveNoReview_NoTask(t *testing.T) {
	// 空 ProjectRoot → false
	if condTaskActiveNoReview(Context{SessionID: "s1"}) {
		t.Fatal("空 ProjectRoot 应 false")
	}
	// 非 git dir 无 task → false（优雅降级）
	if condTaskActiveNoReview(Context{ProjectRoot: t.TempDir(), SessionID: "s1"}) {
		t.Fatal("无 active task 应 false")
	}
}

func TestCondTaskActiveNoReview_ActiveUnreviewed(t *testing.T) {
	dir := t.TempDir()
	state := &taskpipeline.TaskState{TaskRef: "feat/x"} // ReviewPassed 默认 false
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	if err := taskpipeline.SetActiveTaskRef(dir, "sess1", "feat/x"); err != nil {
		t.Fatalf("SetActiveTaskRef: %v", err)
	}
	if !condTaskActiveNoReview(Context{ProjectRoot: dir, SessionID: "sess1"}) {
		t.Fatal("active task 且 ReviewPassed=false 应 true")
	}
}

func TestCondTaskActiveNoReview_Reviewed(t *testing.T) {
	dir := t.TempDir()
	state := &taskpipeline.TaskState{TaskRef: "feat/y"}
	state.MarkReviewPassed("", "") // ReviewPassed=true（空基线跳过快照检查）
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	if err := taskpipeline.SetActiveTaskRef(dir, "sess1", "feat/y"); err != nil {
		t.Fatalf("SetActiveTaskRef: %v", err)
	}
	if condTaskActiveNoReview(Context{ProjectRoot: dir, SessionID: "sess1"}) {
		t.Fatal("ReviewPassed=true 应 false")
	}
}

func TestExitCodeOf(t *testing.T) {
	cases := []struct {
		out  map[string]any
		want int
	}{
		{map[string]any{"exit_code": float64(3)}, 3},
		{map[string]any{"exitCode": float64(2)}, 2},
		{map[string]any{"code": 1}, 1},
		{map[string]any{"status": "5"}, 5},
		{map[string]any{}, -1},
		{map[string]any{"exit_code": "bad"}, -1},
	}
	for _, c := range cases {
		if got := exitCodeOf(c.out); got != c.want {
			t.Errorf("exitCodeOf(%v)=%d want %d", c.out, got, c.want)
		}
	}
}

func TestIsSourcePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"foo.go", true},
		{"foo.GO", true}, // 大小写不敏感
		{"src/main.rs", true},
		{"a/b/c.ts", true},
		{"a/b/c.py", true},
		{"README.md", false},
		{"config.json", false},
		{"Makefile", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isSourcePath(c.path); got != c.want {
			t.Errorf("isSourcePath(%q)=%v want %v", c.path, got, c.want)
		}
	}
}

// TestValidConditions_MatchEngine: drift guard — skilltrigger.Conditions (engine) must stay in sync with skillsqa.ValidConditions (R12 validator).
//
// TestValidConditions_MatchEngine：drift 守卫——skilltrigger.Conditions（引擎）必须与
// skillsqa.ValidConditions（R12 校验器）同步。只改一边会静默破坏引擎 condition 派发或
// R12 校验。放在这里而非 skillsqa 测试，因 skillsqa 不能 import skilltrigger
// （taskpipeline→skillsdist→skillsqa 成环）；这个方向无环。
func TestValidConditions_MatchEngine(t *testing.T) {
	if len(Conditions) != len(skillsqa.ValidConditions) {
		t.Fatalf("skilltrigger.Conditions(%d) 与 skillsqa.ValidConditions(%d) 数量漂移——新增 condition 须同时改 conditions.go 与 rules.go",
			len(Conditions), len(skillsqa.ValidConditions))
	}
	for k := range Conditions {
		if !skillsqa.ValidConditions[k] {
			t.Errorf("skilltrigger.Conditions 含 %q 但 skillsqa.ValidConditions 无——新增 condition 须同时改 conditions.go 与 rules.go", k)
		}
	}
	for k := range skillsqa.ValidConditions {
		if _, ok := Conditions[k]; !ok {
			t.Errorf("skillsqa.ValidConditions 含 %q 但 skilltrigger.Conditions 无——新增 condition 须同时改 rules.go 与 conditions.go", k)
		}
	}
}

// TestCondSourceChanged_SessionAttribution pins the L3 attribution filter (multi-task-concurrency §6, T3): the Stop discipline fires only when THIS session's ledger-touched set intersects the changed source set — another window's WIP in the shared working tree no longer nags this window (the user's P1 pain).
//
// TestCondSourceChanged_SessionAttribution 钉住 L3 归属过滤（multi-task-concurrency
// §6，T3）：Stop 纪律只在「本会话台账触碰集 ∩ 变更源码集」非空时触发——共享工作树
// 里另一个窗口的 WIP 不再触发本窗口的提示（用户实测痛点 P1）。逃生舱
// FORGE_ATTRIBUTION=0 与无 sid 降级路径都回落全树行为。
func TestCondSourceChanged_SessionAttribution(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // 归属台账走 DataDir——隔离真实 ~/.forge
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.t")
	runGit(t, dir, "config", "user.name", "t")
	for _, f := range []string{"mine.go", "theirs.go"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("package x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	attribution.Record(dir, attribution.Event{Ts: time.Now(), Sid: "sess-a", Kind: attribution.KindWrite, Path: "mine.go"})

	// 窗口 B（另一任务）看同一棵工作树：A 的 WIP 不得触发 B 的纪律提示。
	if condSourceChanged(Context{ProjectRoot: dir, SessionID: "sess-b"}) {
		t.Fatal("sess-b 未触碰任何变更文件——另一窗口的 WIP 不应触发本窗口提示（P1 痛点）")
	}
	// 窗口 A 自己动过源码 → 正常触发。
	if !condSourceChanged(Context{ProjectRoot: dir, SessionID: "sess-a"}) {
		t.Fatal("sess-a 触碰过 mine.go 且仍在变更集——应触发")
	}
	// 无 sid（无身份宿主）→ 旧全树行为（advisory 宁多勿漏）。
	if !condSourceChanged(Context{ProjectRoot: dir}) {
		t.Fatal("无 sid 应回落全树行为为 true")
	}
	// 逃生舱：FORGE_ATTRIBUTION=0 一键回 L3 之前。
	t.Setenv("FORGE_ATTRIBUTION", "0")
	if !condSourceChanged(Context{ProjectRoot: dir, SessionID: "sess-b"}) {
		t.Fatal("FORGE_ATTRIBUTION=0 应回退全树行为为 true")
	}
}
