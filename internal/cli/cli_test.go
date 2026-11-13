package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/hostcap"
)

var forgeExe string

func TestMain(m *testing.M) {
	exeName := "forge"
	if runtime.GOOS == "windows" {
		exeName = "forge.exe"
	}
	tmpDir, err := os.MkdirTemp("", "forge-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	// W0.3 密闭性：档位默认 standard，防 shell 导出的 FORGE_PROFILE 泄漏假红。
	os.Setenv("FORGE_PROFILE", "standard")
	// ADR-0002 阶段 B：hook 执法默认 audit-only。本包 freeze-guard/task-guard 行为
	// 测试断言「执法时」的 block/allow，需执法前提——与 hookdispatch TestMain 同款
	// 置 FORGE_HOOKS=on（宿主层 opt-in）。默认安静策略由 hookdispatch contract_gate
	// 专测以 t.Setenv("FORGE_HOOKS","") 隔离钉住，不在此覆盖。
	os.Setenv("FORGE_HOOKS", "on")
	// 会话身份密闭性：宿主 shell 注入的会话 env（如 Claude Code 的
	// CLAUDE_CODE_SESSION_ID）与 FORGE_SESSION_ID 一律清空——本机复现 CI 的「无会话
	// 身份」条件，runForge 拉起的子进程同样继承；需要会话的测试显式 t.Setenv。
	// env 名从 hostcap 注册表派生（同 isolateSessionIdentity）。
	for _, h := range hostcap.Hosts {
		if h.ShellSessionEnv != "" {
			os.Unsetenv(h.ShellSessionEnv)
		}
	}
	os.Unsetenv("FORGE_SESSION_ID")
	forgeExe = filepath.Join(tmpDir, exeName)

	cmd := exec.Command("go", "build", "-o", forgeExe, "../../cmd/forge")
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build forge binary: %v\n%s\n", err, output)
		os.Exit(1)
	}

	// 把全局状态根重定向到 tmpDir（FORGE_DATA_HOME），避免 init/dashboard 测试污染真实
	// ~/.forge（registry projects.json + DataDir projects/<key>/）。子进程（runForge 跑
	// forge 二进制）继承此 env。refactor-data-home commit E：registry 统一 FORGE_DATA_HOME。
	os.Setenv("FORGE_DATA_HOME", tmpDir)

	// 隔离 Claude plugin 检测：强制 IsClaudePluginInstalled()=false（空 CLAUDE_CONFIG_DIR 下
	// 无 plugins/installed_plugins.json）。cli 测试跑 init/sync（含 dedupeProjectLevelIfPlugin）
	// 时不依赖本机是否装了 forge plugin——否则本地装了 plugin 会让 init 的 dedupe 删掉
	// settings.local.json,断言"settings 存在"的测试在本地失败、CI（未装）通过的飘忽。
	// 子进程（runForge 跑 forge 二进制）继承此 env。
	os.Setenv("CLAUDE_CONFIG_DIR", tmpDir)

	// 为子进程隔离用户 HOME：user-level-assets 之后 init/sync 会写用户级资产
	// （~/.codex/AGENTS.md、~/.cursor/hooks.json、~/.codeium/...、
	// ~/.config/opencode/...），DetectAgents 会扫用户级安装目录——不隔离 HOME，
	// 测试会写入并检测到开发者的真实 home。Windows 用 USERPROFILE，unix 用 HOME；
	// CODEX_HOME 是 codex 自己的覆盖。
	homeDir := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(homeDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create isolated home: %v\n", err)
		os.Exit(1)
	}
	os.Setenv("HOME", homeDir)
	os.Setenv("USERPROFILE", homeDir)
	os.Setenv("CODEX_HOME", filepath.Join(homeDir, ".codex"))

	code := m.Run()
	os.RemoveAll(tmpDir)
	os.Exit(code)
}

// buildForge returns the path to the pre-built forge binary.
func buildForge(t *testing.T) string {
	t.Helper()
	return forgeExe
}

// runForge executes the forge CLI in the given working directory.
func runForge(t *testing.T, dir string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	exe := buildForge(t)
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, "", exitErr.ExitCode()
		}
		return output, err.Error(), -1
	}
	return output, "", 0
}

// initProject 在 dir 跑 forge init（带给定 flags），非零退出即测试失败——所有
// init 系测试共享的样板。
func initProject(t *testing.T, dir string, flags ...string) {
	t.Helper()
	if out, _, code := runForge(t, dir, append([]string{"init"}, flags...)...); code != 0 {
		t.Fatalf("forge init %v failed: %s", flags, out)
	}
}

// --------------- Test 1: TestInitCreatesRepoProducts ---------------

// TestInitCreatesRepoProducts：init 后仓库里有契约产物，用户级 hook 模板不再由
// init 落盘（Phase C：宿主接线是 forge integrations enable 的职责，见 init_test.go）。
func TestInitCreatesRepoProducts(t *testing.T) {
	tmpDir := t.TempDir()
	initProject(t, tmpDir)

	for _, p := range []string{"spec/forge.yaml", "spec/changes/archive/.gitkeep", "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(tmpDir, filepath.FromSlash(p))); err != nil {
			t.Fatalf("%s not created: %v", p, err)
		}
	}

	// 项目级 .forge/ 与宿主目录一律不写（零项目写入 + Phase C）。
	for _, name := range []string{".forge", ".claude", ".cursor", ".codex"} {
		if _, err := os.Stat(filepath.Join(tmpDir, name)); !os.IsNotExist(err) {
			t.Errorf("init must not write project-level %s", name)
		}
	}
}

// --------------- Test 5-6 / idempotent / agents: post-init smoke table ---------------

// TestPostInitSmokeCommands 合并逐命令 exit-0 smoke：init 后各命令变体必须干净运行
// （exit 0）并满足各自行输出断言。行语义逐一保留自被吸收测试（status smoke、
// status --json、status --agents、二次 init 幂等）。项目级管道删除后 status 不再渲染
// "pending"；无任务输出为空属正常，故保持 exit-0 smoke。
func TestPostInitSmokeCommands(t *testing.T) {
	tests := []struct {
		name          string
		initFlags     []string
		args          []string // command run after init
		wantIn        string   // required substring in output ("" = none)
		wantJSONField string   // required top-level JSON key ("" = none)
	}{
		{name: "status", initFlags: nil, args: []string{"status"}},
		{name: "status json", initFlags: nil, args: []string{"status", "--json"}, wantJSONField: "tasks"},
		// 二次 init 必须都成功（幂等）。
		{name: "init idempotent", initFlags: nil, args: []string{"init"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			initProject(t, tmpDir, tt.initFlags...)

			stdout, _, code := runForge(t, tmpDir, tt.args...)
			if code != 0 {
				t.Fatalf("forge %v exit code %d, output: %s", tt.args, code, stdout)
			}
			if tt.wantIn != "" && !strings.Contains(stdout, tt.wantIn) {
				t.Errorf("output missing %q:\n%s", tt.wantIn, stdout)
			}
			if tt.wantJSONField != "" {
				var result map[string]json.RawMessage
				if err := json.Unmarshal([]byte(stdout), &result); err != nil {
					t.Fatalf("failed to parse JSON: %v\noutput: %s", err, stdout)
				}
				if _, ok := result[tt.wantJSONField]; !ok {
					t.Errorf("JSON output missing %q field", tt.wantJSONField)
				}
			}
		})
	}
}

// --------------- Test: TestStatusShowsHealthSignal ---------------

// TestStatusShowsHealthSignal 钉住 status 接入项目级质量信号：有完成任务结论时，status
// 主入口必须亮出证据盲区率/复发低分维度——防 deterministic 信号在 forge health 算了但
// status（"项目在哪"主入口）看不到的可见性缺口。
func TestStatusShowsHealthSignal(t *testing.T) {
	tmpDir, p := forgedatatest.RealProject(t)
	initProject(t, tmpDir, "--mode", "medium")

	// 种 2 个结论：1 Strong + 1 Unverified → 盲区率 50%（触发系统性告警）；都带 scope 低分。
	seed := []act.Conclusion{
		{TaskRef: `feat/a`, Grade: `A`, Strength: `Strong`, Score: 95, LowDimensions: []string{`scope`}, CompletedAt: time.Now()},
		{TaskRef: `feat/b`, Grade: `D`, Strength: `Unverified`, Score: 60, LowDimensions: []string{`scope`}, RetrospectiveNudge: true, CompletedAt: time.Now()},
	}
	for i := range seed {
		if err := act.Append(p, &seed[i]); err != nil {
			t.Fatalf("seed conclusion: %v", err)
		}
	}

	// pretty：质量信号块 + 系统性盲区告警 + scope 复发都必须出现。
	stdout, _, code := runForge(t, tmpDir, "status")
	if code != 0 {
		t.Fatalf("forge status exit %d: %s", code, stdout)
	}
	for _, want := range []string{`质量信号`, `证据盲区率`, `系统性盲区`, `scope`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("status 缺 %q（质量信号块未渲染）\noutput: %s", want, stdout)
		}
	}

	// json：health 字段在，blind_spot_rate=0.5, total_tasks=2。
	stdout, _, code = runForge(t, tmpDir, "status", "--json")
	if code != 0 {
		t.Fatalf("forge status --json exit %d: %s", code, stdout)
	}
	var result struct {
		Health *struct {
			BlindSpotRate float64 `json:"blind_spot_rate"`
			TotalTasks    int     `json:"total_tasks"`
		} `json:"health"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse status JSON: %v\n%s", err, stdout)
	}
	if result.Health == nil {
		t.Fatalf("status --json 缺 health 字段（有结论时应含）\n%s", stdout)
	}
	if result.Health.TotalTasks != 2 || result.Health.BlindSpotRate != 0.5 {
		t.Errorf("health=%+v want TotalTasks=2 BlindSpotRate=0.5", result.Health)
	}
}

// --------------- Test 8: TestHelperFunctions ---------------

func TestHelperFunctions(t *testing.T) {
	t.Run("truncate", func(t *testing.T) {
		tests := []struct {
			input string
			max   int
			want  string
		}{
			{"hello", 10, "hello"},
			{"hello world", 8, "hello..."},
			{"short", 5, "short"},
			{"abcdef", 5, "ab..."},
			{"abc", 3, "abc"},
			{"中文测试内容", 4, "中..."},
		}
		for _, tc := range tests {
			got := truncate(tc.input, tc.max)
			if got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.max, got, tc.want)
			}
		}
	})

	t.Run("findProjectRoot", func(t *testing.T) {
		tmpDir := t.TempDir()
		projectDir := filepath.Join(tmpDir, "myproject")
		subDir := filepath.Join(projectDir, "subdir")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create .forge/ at the project root
		if err := os.MkdirAll(filepath.Join(projectDir, ".forge"), 0755); err != nil {
			t.Fatal(err)
		}

		originalDir, _ := os.Getwd()
		if err := os.Chdir(subDir); err != nil {
			t.Fatalf("failed to chdir: %v", err)
		}
		defer os.Chdir(originalDir)

		root, err := findProjectRoot()
		if err != nil {
			t.Fatalf("findProjectRoot failed: %v", err)
		}
		// Resolve symlinks for comparison (macOS /var → /private/var)
		resolvedRoot, _ := filepath.EvalSymlinks(root)
		resolvedWant, _ := filepath.EvalSymlinks(projectDir)
		if resolvedRoot != resolvedWant {
			t.Errorf("findProjectRoot returned %q (resolved: %q), want %q (resolved: %q)", root, resolvedRoot, projectDir, resolvedWant)
		}
	})

}

// --------------- Test: System status health check ---------------

func TestSystemStatusRequiresForge(t *testing.T) {
	tmpDir := t.TempDir()

	// forge status --system 对 FORGE_DATA_HOME 指向的全局根跑系统级健康检查
	// （TestMain 已把全局根重定向到共享 tmpDir，这里再指到一个不存在的子路径，
	// 钉住「根缺失必须显式失败并点名缺失目录」——输出为解析后的绝对路径）。
	t.Setenv("FORGE_DATA_HOME", filepath.Join(tmpDir, "no-forge-root"))
	out, _, code := runForge(t, tmpDir, "status", "--system")
	if code == 0 {
		t.Fatalf("expected non-zero exit when forge root is missing, got 0\noutput:\n%s", out)
	}
	if !strings.Contains(out, "no-forge-root") {
		t.Errorf("output should name the missing forge root:\n%s", out)
	}
}

// --------------- Test: Status without init ---------------

func TestStatusWithoutInit(t *testing.T) {
	// 密封"非 forge 项目"前提：包级 TestMain 的注册表是共享的，会被各 init
	// 测试累积条目——本测试需要空注册表，故用自己的 FORGE_DATA_HOME。
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	tmpDir := t.TempDir()

	out, _, code := runForge(t, tmpDir, "status")
	if code == 0 {
		// 失败时带上完整输出定位解析到了哪个“项目”（CI 偶发，本地不复现）
		t.Fatalf("expected non-zero exit when status called without init\ncode=%d\noutput:\n%s", code, out)
	}
}

// --------------- Test: Init idempotent (run twice) ---------------

// --------------- Test: First-run experience ---------------
// A user who has never seen forge should understand what it does
// and what to do next within the first 30 seconds.

func TestFirstRunExperience(t *testing.T) {
	tmpDir := t.TempDir()

	// Running forge with no arguments must provide actionable guidance
	stdout, _, code := runForge(t, tmpDir)

	// Must exit 0 (help output, not an error)
	if code != 0 {
		t.Fatalf("forge with no args returned exit %d, expected 0", code)
	}
	// Must state what the tool does
	if !strings.Contains(stdout, "门禁") {
		t.Fatal("first-run output missing tool purpose (门禁)")
	}
	// Must tell user what to do next
	if !strings.Contains(stdout, "forge init") {
		t.Fatal("first-run output missing 'forge init' quick start")
	}
	// Must link to documentation
	if !strings.Contains(stdout, "github.com") {
		t.Fatal("first-run output missing documentation link")
	}
}

// --------------- Test: init no longer writes a user-level protocol.yml ---------------

// TestInitDoesNotWriteProtocolYML：Phase C 后协议默认值来自 spec 契约，init 不再落
// 用户级 protocol.yml（那是宿主接线的产物）。
func TestInitDoesNotWriteProtocolYML(t *testing.T) {
	tmpDir := t.TempDir()
	initProject(t, tmpDir)

	if _, err := os.Stat(filepath.Join(forgedata.DataDirFor(tmpDir), "protocol.yml")); !os.IsNotExist(err) {
		t.Fatal("init 不应再写 DataDir/protocol.yml（宿主接线才写）")
	}
}

// --------------- Test: Task scoring workflow ---------------

func TestTaskScoreWorkflow(t *testing.T) {
	// Disable gate timing for test (gates are passed rapidly in sequence)
	origInterval := os.Getenv("FORGE_GATE_MIN_INTERVAL")
	os.Setenv("FORGE_GATE_MIN_INTERVAL", "0s")
	defer os.Setenv("FORGE_GATE_MIN_INTERVAL", origInterval)
	origWorkActivity := os.Getenv("FORGE_WORK_ACTIVITY")
	os.Setenv("FORGE_WORK_ACTIVITY", "disable")
	defer os.Setenv("FORGE_WORK_ACTIVITY", origWorkActivity)

	tmpDir := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "test@test.com")
	runGit(t, tmpDir, "config", "user.name", "Test")

	initProject(t, tmpDir, "--mode", "medium")

	// Create and commit initial files
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "initial")

	// Create a feature branch
	runGit(t, tmpDir, "checkout", "-b", "feature/test-scoring")

	// Start a task
	stdout, _, code := runForge(t, tmpDir, "task", "start")
	if code != 0 {
		t.Fatalf("forge task start failed: %s", stdout)
	}

	// task-implement's hasCodeChanges check requires real code changes since task
	// start (working-tree diff or feature-branch commits beyond base). This test
	// repo has no .gitignore, so the OLD autoSync's mutation of a tracked
	// .forge/state.json once supplied a spurious working-tree change that let a
	// pre-code task-implement pass; that signal is gone (state.json removed,
	// .sync-version is untracked). task-implement is now exercised only after the
	// real code change below — matching its actual contract.
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hello\") }\n"), 0644)
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "implement feature")

	// oracle-pipeline L1：complete 登记门硬前置至少一条验收——登记快验收并实跑
	//（freshness 消费实跑快照）。
	if stdout, _, code = runForge(t, tmpDir, "task", "accept", "go version :: go version"); code != 0 {
		t.Fatalf("forge task accept failed: %s", stdout)
	}
	if stdout, _, code = runForge(t, tmpDir, "task", "verify-acceptance"); code != 0 {
		t.Fatalf("forge task verify-acceptance failed: %s", stdout)
	}

	// Pass gates (task-implement now has real code changes; task-verify follows)
	for _, g := range []string{"task-implement", "task-verify"} {
		stdout, _, code = runForge(t, tmpDir, "task", "gate", g)
		if code != 0 {
			t.Fatalf("forge task gate %s failed: %s", g, stdout)
		}
	}
	// task-complete 的 ReviewPassed 硬前置：先 review pass 满足之。
	if stdout, _, code = runForge(t, tmpDir, "review", "pass"); code != 0 {
		t.Fatalf("forge review pass failed: %s", stdout)
	}
	stdout, _, code = runForge(t, tmpDir, "task", "gate", "task-complete")
	if code != 0 {
		t.Fatalf("forge task gate task-complete failed: %s", stdout)
	}

	// Complete the task
	stdout, _, code = runForge(t, tmpDir, "task", "complete")
	if code != 0 {
		t.Fatalf("forge task complete failed: %s", stdout)
	}

	// Score should be present
	if !strings.Contains(stdout, "Score:") {
		t.Fatalf("expected score in complete output, got: %s", stdout)
	}

	// Query score explicitly
	stdout, _, code = runForge(t, tmpDir, "task", "score")
	if code != 0 {
		t.Fatalf("forge task score failed: %s", stdout)
	}
	if !strings.Contains(stdout, "Overall:") {
		t.Fatalf("expected Overall in score output, got: %s", stdout)
	}

	// Score JSON output
	stdout, _, code = runForge(t, tmpDir, "task", "score", "--json")
	if code != 0 {
		t.Fatalf("forge task score --json failed: %s", stdout)
	}
	var scoreResult map[string]any
	if err := json.Unmarshal([]byte(stdout), &scoreResult); err != nil {
		t.Fatalf("score JSON parse error: %v, output: %s", err, stdout)
	}
	if _, ok := scoreResult["overall"]; !ok {
		t.Fatal("score JSON missing 'overall' field")
	}
	if _, ok := scoreResult["grade"]; !ok {
		t.Fatal("score JSON missing 'grade' field")
	}
	if _, ok := scoreResult["dimensions"]; !ok {
		t.Fatal("score JSON missing 'dimensions' field")
	}
}

// --------------- Test: --agents 已废弃（宿主接线移到 integrations） ---------------

// runGit is a test helper to run git commands.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, string(out), err)
	}
}
func TestInitWithAgentsIsNoOp(t *testing.T) {
	tmpDir := t.TempDir()
	out, _, code := runForge(t, tmpDir, "init", "--agents", "cursor,copilot")
	if code == 0 {
		t.Fatalf("--agents 随宿主层删除，应报 unknown flag：%s", out)
	}
	if !strings.Contains(out, "unknown flag: --agents") {
		t.Errorf("错误信息应点名 --agents：%s", out)
	}
	for _, name := range []string{".cursor", ".github"} {
		if _, err := os.Stat(filepath.Join(tmpDir, name)); !os.IsNotExist(err) {
			t.Errorf("init --agents must not write %s into the project dir", name)
		}
	}
}
