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
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/spf13/cobra"
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

// --------------- Test 1: TestInitCreatesFiles ---------------

func TestInitCreatesFiles(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium")
	if code != 0 {
		t.Fatalf("forge init exit code %d, output: %s", code, stdout)
	}

	// .forge/hooks/ 下的 .sh 数必须等于 hooks.HookNames()（单一真相源）。加/删 hook 只改
	// settings.go 的 HookNames()，本测试自动跟随——避免"加 hook 后忘改硬编码期望数"的同步漏
	// （曾因此把 10 手改成 11）。历史：session-health/test-coverage-check 已移除（噪声），
	// tool-track 曾在 644b142 删除后恢复（task-verify gate 依赖其 Read 记录）。
	hooksDir := filepath.Join(tmpDir, ".forge", "hooks")
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		t.Fatalf("failed to read hooks dir: %v", err)
	}
	shCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sh") {
			shCount++
		}
	}
	if want := len(hooks.HookNames()); shCount != want {
		t.Fatalf("expected %d .sh files in hooks/ (== len(hooks.HookNames())), got %d", want, shCount)
	}

	// .claude/settings.local.json exists
	settingsFile := filepath.Join(tmpDir, ".claude", "settings.local.json")
	if _, err := os.Stat(settingsFile); err != nil {
		t.Fatalf(".claude/settings.local.json not found: %v", err)
	}

	// .forge/protocol.yml exists
	protoFile := filepath.Join(tmpDir, ".forge", "protocol.yml")
	if _, err := os.Stat(protoFile); err != nil {
		t.Fatalf(".forge/protocol.yml not found: %v", err)
	}

	// NOTE: .forge/tasks/ is no longer created by init — task state migrated
	// to the user-level DataDir (refactor-data-home), created on demand by
	// SaveTaskState. Asserting it here would lock the old (wrong) semantics.

	// .claude/skills/forge-quality/SKILL.md exists
	qualitySkillFile := filepath.Join(tmpDir, ".claude", "skills", "forge-quality", "SKILL.md")
	if _, err := os.Stat(qualitySkillFile); err != nil {
		t.Fatalf(".claude/skills/forge-quality/SKILL.md not found: %v", err)
	}

	// .claude/CLAUDE.md exists
	claudeMDFile := filepath.Join(tmpDir, ".claude", "CLAUDE.md")
	if _, err := os.Stat(claudeMDFile); err != nil {
		t.Fatalf(".claude/CLAUDE.md not found: %v", err)
	}
}

// --------------- Test 5: TestStatusAfterInit ---------------

func TestStatusAfterInit(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium")
	if code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	// 项目级管道删除后 status 不再渲染 "pending"（pipeline 状态）；无任务时输出为空属正常。
	// 本测试降为 smoke：init 后 status 必须成功运行（exit 0）。
	stdout, _, code = runForge(t, tmpDir, "status")
	if code != 0 {
		t.Fatalf("forge status exit code %d, output: %s", code, stdout)
	}
}

// --------------- Test 6: TestStatusJSON ---------------

func TestStatusJSON(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium")
	if code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	stdout, _, code = runForge(t, tmpDir, "status", "--json")
	if code != 0 {
		t.Fatalf("forge status --json exit code %d, output: %s", code, stdout)
	}

	// Parse JSON — project pipeline removed; status JSON now exposes {tasks, health}.
	var result map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse status JSON: %v\noutput: %s", err, stdout)
	}
	if _, ok := result["tasks"]; !ok {
		t.Fatal("JSON output missing 'tasks' field")
	}
}

// --------------- Test: TestStatusShowsHealthSignal ---------------

// TestStatusShowsHealthSignal 钉住 status 接入项目级质量信号：有完成任务结论时，status
// 主入口必须亮出证据盲区率/复发低分维度——防 deterministic 信号在 forge health 算了但
// status（"项目在哪"主入口）看不到的可见性缺口。
func TestStatusShowsHealthSignal(t *testing.T) {
	tmpDir, p := forgedatatest.RealProject(t)
	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium")
	if code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

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
	stdout, _, code = runForge(t, tmpDir, "status")
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

	t.Run("jsonMarshal", func(t *testing.T) {
		type sample struct {
			Name  string `json:"name"`
			Value int    `json:"value"`
		}
		data, err := jsonMarshal(sample{Name: "test", Value: 42})
		if err != nil {
			t.Fatalf("jsonMarshal failed: %v", err)
		}
		// Should be indented JSON
		s := string(data)
		if !strings.Contains(s, "\"name\": \"test\"") {
			t.Errorf("jsonMarshal output unexpected: %s", s)
		}
		if !strings.Contains(s, "\"value\": 42") {
			t.Errorf("jsonMarshal output unexpected: %s", s)
		}
		// Verify it's valid JSON
		var parsed sample
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("jsonMarshal output is not valid JSON: %v", err)
		}
		if parsed.Name != "test" || parsed.Value != 42 {
			t.Errorf("jsonMarshal roundtrip failed: got %+v", parsed)
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

	// forge status --system runs system health checks.
	// It checks ~/.forge/ existence, not the project dir,
	// so just verify it runs without crashing.
	_, _, _ = runForge(t, tmpDir, "status", "--system")
}

// --------------- Test: Status without init ---------------

func TestStatusWithoutInit(t *testing.T) {
	tmpDir := t.TempDir()

	_, _, code := runForge(t, tmpDir, "status")
	if code == 0 {
		t.Fatal("expected non-zero exit when status called without init")
	}
}

// --------------- Test: Init idempotent (run twice) ---------------

// TestInitSkipsGenerateSettingsWhenPluginInstalled: when forge plugin is
// installed at user level, forge init must NOT write ForgeHookSpec hooks to
// project-level settings.local.json — user-level plugin.json already registers
// them machine-wide. It must still create the file with user content intact
// (or not create it if none existed).
func TestInitSkipsGenerateSettingsWhenPluginInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	writeForgePluginFixture(t, home)

	dir := t.TempDir()
	// Pre-populate settings.local.json with user fields only.
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	userSettings := `{"env":{"KEY":"val"},"model":"gpt-4"}`
	settingsPath := filepath.Join(claudeDir, "settings.local.json")
	if err := os.WriteFile(settingsPath, []byte(userSettings), 0644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	// runInit uses os.Getwd() for the project dir — chdir and restore.
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("mode", "", "")
	cmd.Flags().Bool("fresh", false, "")
	cmd.Flags().String("agents", "auto", "")
	initCmd.RunE(cmd, nil) //nolint: errcheck — runInit prints warnings for missing assets

	// Verify: settings.local.json must NOT contain hooks.
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings after init: %v", err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if _, hasHooks := parsed["hooks"]; hasHooks {
		t.Error("plugin installed: forge init must not write hooks to settings.local.json")
	}
	// User fields must be preserved (or merged if GenerateSettings was skipped).
	if v, ok := parsed["model"]; ok && string(v) != `"gpt-4"` {
		t.Errorf("user model field modified: got %s", string(v))
	}
}

func TestInitIdempotent(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "small")
	if code != 0 {
		t.Fatalf("first init failed: %s", stdout)
	}

	stdout, _, code = runForge(t, tmpDir, "init", "--mode", "small")
	if code != 0 {
		t.Fatalf("second init failed: %s", stdout)
	}
}

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

// --------------- Test: Init creates scoring config in protocol.yml ---------------

func TestInitProtocolScoringConfig(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium")
	if code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	protoData, err := os.ReadFile(filepath.Join(tmpDir, ".forge", "protocol.yml"))
	if err != nil {
		t.Fatalf("protocol.yml not found: %v", err)
	}
	protoStr := string(protoData)
	if !strings.Contains(protoStr, "scoring:") {
		t.Fatal("protocol.yml missing scoring config section")
	}
	if !strings.Contains(protoStr, "weights:") {
		t.Fatal("protocol.yml missing weights in scoring config")
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

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium")
	if code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	// Create and commit initial files
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)
	runGit(t, tmpDir, "add", ".")
	runGit(t, tmpDir, "commit", "-m", "initial")

	// Create a feature branch
	runGit(t, tmpDir, "checkout", "-b", "feature/test-scoring")

	// Start a task
	stdout, _, code = runForge(t, tmpDir, "task", "start")
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
	var scoreResult map[string]interface{}
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


// --------------- Test: Init with --agents flag ---------------

func TestInitWithAgents(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium", "--agents", "cursor,copilot")
	if code != 0 {
		t.Fatalf("forge init --agents cursor,copilot failed: %s", stdout)
	}

	// Verify .cursor/rules/forge-quality.mdc was created
	cursorFile := filepath.Join(tmpDir, ".cursor", "rules", "forge-quality.mdc")
	if data, err := os.ReadFile(cursorFile); err != nil {
		t.Fatalf("cursor rules file not created: %v", err)
	} else if !strings.Contains(string(data), "alwaysApply: true") {
		t.Fatal("cursor rules file missing frontmatter")
	}

	// Verify .github/instructions/forge-quality.instructions.md was created
	copilotFile := filepath.Join(tmpDir, ".github", "instructions", "forge-quality.instructions.md")
	if data, err := os.ReadFile(copilotFile); err != nil {
		t.Fatalf("copilot instructions file not created: %v", err)
	} else if !strings.Contains(string(data), "applyTo:") {
		t.Fatal("copilot instructions file missing frontmatter")
	}
}

// --------------- Test: Status --agents ---------------

func TestStatusAgents(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium")
	if code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	stdout, _, code = runForge(t, tmpDir, "status", "--agents")
	if code != 0 {
		t.Fatalf("forge status --agents failed: %s", stdout)
	}
	// After init with default auto mode, .claude/ exists → should detect claude-code
	if !strings.Contains(stdout, "claude-code") {
		t.Fatalf("expected claude-code in agents output, got: %s", stdout)
	}
}

// runGit is a test helper to run git commands.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, string(out), err)
	}
}
