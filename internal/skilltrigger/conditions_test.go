package skilltrigger

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

// TestValidConditions_MatchEngine: drift guard — skilltrigger.Conditions (engine)
// must stay in sync with skillsqa.ValidConditions (R12 validator). Updating only one
// side silently breaks either the engine's condition dispatch or R12 validation.
// Lives here (not in skillsqa tests) because skillsqa cannot import skilltrigger
// (taskpipeline→skillsdist→skillsqa cycle); this direction is acyclic.
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
