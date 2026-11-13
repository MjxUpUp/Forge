// Package e2e contains multi-perspective end-to-end tests for Forge.
// These tests verify different user scenarios: fresh install, version upgrades,
// master branch warnings — all via subprocess invocations
// of the compiled forge binary.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

var forgeBin string

// forgeBuildDir 是 TestMain 持有的构建目录（defer os.RemoveAll）：
// buildVersionedForge（kimi_stale_test.go）的按版本二进制必须放这里，不能放进任何
// 测试自己的 t.TempDir()——缓存路径比创建它的测试活得久，后续缓存调用方会拿到悬空
// 路径（2026-08-15 踩坑：第二个 kimi stale 测试 fork/exec 失败于第一个测试已删除的
// temp 目录）。
var forgeBuildDir string

func TestMain(m *testing.M) {
	// Build forge binary once for all tests.
	tmpDir, err := os.MkdirTemp("", "forge-e2e-build")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: failed to create temp build dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)
	forgeBuildDir = tmpDir

	binPath := filepath.Join(tmpDir, "forge")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/forge/")
	cmd.Dir = repoRoot()
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: failed to build forge binary: %v\n%s\n", err, output)
		os.Exit(1)
	}

	forgeBin = binPath

	// W0.3 密闭性 + ADR-0002 阶段 B：档位默认 standard、hook 执法 opt-in。e2e 的
	// hook 拦截测试经子进程 `forge hook` 走真实执法路径，断言「执法时」的 block/allow，
	// 需执法前提——置 FORGE_HOOKS=on（宿主层 opt-in，子进程经 os.Environ() 继承）。
	// 默认安静策略由 hookdispatch contract_gate 专测隔离钉住。
	os.Setenv("FORGE_PROFILE", "standard")
	os.Setenv("FORGE_HOOKS", "on")

	// 隔离 Claude plugin 检测：强制 IsClaudePluginInstalled()=false（空 CLAUDE_CONFIG_DIR 下
	// 无 plugins/installed_plugins.json）。e2e 跑 forge binary 子进程（init/sync 含
	// dedupeProjectLevelIfPlugin），不隔离会让本机装了 forge plugin 时 dedupe 改写/删
	// project-level 文件（settings.local.json / .mcp.json）,干扰 e2e 子进程行为一致性。
	// 当前断言只查 settings.local.json 存在（fileExists）,但保留隔离为未来加内容断言留
	// 确定性,避免本地（装 plugin）与 CI（未装）飘忽。
	os.Setenv("CLAUDE_CONFIG_DIR", tmpDir)

	// 为子进程隔离用户 HOME：user-level-assets 之后 init/sync 会写用户级资产
	// （~/.codex/AGENTS.md、~/.cursor/hooks.json、~/.codeium/...、
	// ~/.config/opencode/...），DetectAgents 会扫用户级安装目录——不隔离 HOME，
	// e2e 会写入并检测到开发者的真实 home。Windows 用 USERPROFILE，unix 用 HOME；
	// CODEX_HOME 是 codex 自己的覆盖。
	homeDir := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(homeDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: failed to create isolated home: %v\n", err)
		os.Exit(1)
	}
	os.Setenv("HOME", homeDir)
	os.Setenv("USERPROFILE", homeDir)
	os.Setenv("CODEX_HOME", filepath.Join(homeDir, ".codex"))

	os.Exit(m.Run())
}

// repoRoot returns the forge repository root (the directory containing go.mod).
func repoRoot() string {
	// Walk up from current file to find go.mod.
	dir := "."
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			abs, _ := filepath.Abs(dir)
			return abs
		}
		dir = filepath.Join(dir, "..")
	}
	panic("cannot find repo root (go.mod)")
}

// forge runs a forge command in the given working directory.
func forge(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(forgeBin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("forge %s failed: %v\noutput: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// forgeErr runs a forge command and returns (stdout+stderr, error).
// It does NOT fatal on non-zero exit — the caller decides.
func forgeErr(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(forgeBin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// git runs a git command in the given working directory.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\noutput: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// initGoProject creates a minimal Go project in dir.
func initGoProject(t *testing.T, dir string) {
	t.Helper()
	goMod := `module example.com/test

go 1.24
`
	writeFile(t, dir, "go.mod", goMod)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
}

// writeFile writes a file with content inside dir.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// fileExists checks if a file (or directory) exists.
func fileExists(t *testing.T, dir, name string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// readFile reads a file inside dir.
func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("failed to read %s: %v", name, err)
	}
	return string(data)
}

// freshProject creates a temp dir with git init + go project + forge init.
// Returns the project directory.
func freshProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// 把用户级 data home 重定向到 per-test temp dir：forge 子进程（init/hazard/hooks）
	// 与本测试进程的 store 读取都按 FORGE_DATA_HOME 解析 DataDir，二者一致且不污染真实 ~/.forge。
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	// 钉死初始分支名：裸 `git init` 继承机器的 init.defaultBranch（macOS 系统
	// gitconfig 在 /Library/Developer/CommandLineTools 里设了 main），而建在这套
	// 夹具上的测试——TestMasterBranchReminder——之后要 `git checkout master`；在
	// main 默认的机器上该 checkout 因环境原因必挂，非代码原因。-b 需
	// git >= 2.28（2020 年）；所有 CI runner 与开发机都远高于此。
	git(t, dir, "init", "-b", "master")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	initGoProject(t, dir)
	forge(t, dir, "init")
	enableHooksForFixture(t, dir)
	// Phase C：init 只写仓库产物，宿主接线（DataDir 的 protocol/hook 副本、用户级
	// settings/CLAUDE.md/skill）改由 integrations enable 显式完成。夹具跑的是执法
	// 路径，需要这些资产在位，故在 init 之后接线 claude-code（TestMain 已隔离
	// CLAUDE_CONFIG_DIR / HOME / FORGE_DATA_HOME）。
	forge(t, dir, "registry", "add", ".")
	return dir
}

// enableHooksForFixture 把夹具契约调成执法档。Phase C 起 `forge init` 只写仓库内
// 产物且契约缺省 `profile: mini`、`hooks: off`（audit-only）；e2e 夹具验证的是
// standard 执法路径，必须显式 opt-in，否则 hook 全部静默/被按档滤掉、断言全落空。
func enableHooksForFixture(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "spec", "forge.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture contract: %v", err)
	}
	// 按行改写（不能用子串替换）：模板注释行里也有「hooks: off 表示……」的字样，
	// 子串替换会先命中注释、把真取值留在 off 上，契约门于是静默放行；profile 的
	// 取值还带引号，裸子串同样匹配不上。
	lines := strings.Split(string(b), "\n")
	flippedHooks, flippedProfile := false, false
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "hooks:"):
			lines[i] = "hooks: on"
			flippedHooks = true
		case strings.HasPrefix(line, "profile:"):
			lines[i] = `profile: "standard"`
			flippedProfile = true
		}
	}
	if !flippedHooks || !flippedProfile {
		t.Fatalf("fixture contract missing hooks/profile values:\n%s", b)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("write fixture contract: %v", err)
	}
}

// ---------- Test Scenarios ----------

// TestFreshInstall verifies the Phase C init contract: `forge init` writes ONLY
// repository products (no DataDir protocol/hooks, no user-level agent config).
// Host wiring is opt-in via `forge integrations enable` (covered by
// TestIntegrations_EnableAndDisable in internal/cli).
func TestFreshInstall(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	// 用户级断言要求本用例独占宿主目录：TestMain 的 CLAUDE_CONFIG_DIR 是
	// 全包共享的，别的用例 integrations enable 会先写进 settings.json。
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	git(t, dir, "init", "-b", "master")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	initGoProject(t, dir)
	forge(t, dir, "init")

	// 仓库内产物必须在位，且缺省是 audit-only（hooks: off / mini）。
	contract := readFile(t, dir, filepath.Join("spec", "forge.yaml"))
	for _, want := range []string{"hooks: off", `profile: "mini"`} {
		if !strings.Contains(contract, want) {
			t.Errorf("spec/forge.yaml missing %q:\n%s", want, contract)
		}
	}
	if !fileExists(t, dir, filepath.Join("spec", "changes", "archive", ".gitkeep")) {
		t.Error("spec/changes/archive/.gitkeep missing after forge init")
	}
	if !fileExists(t, dir, "AGENTS.md") {
		t.Error("AGENTS.md missing after forge init")
	}

	// init 不得写项目内的宿主资产。
	for _, path := range []string{".forge", ".claude", ".cursor"} {
		if fileExists(t, dir, path) {
			t.Errorf("forge init must not write %s into the project", path)
		}
	}

	// init 不得写用户级/DataDir 资产（阶段 C 判据）。
	dataDir := forgedata.DataDirFor(dir)
	for _, path := range []string{"protocol.yml", "hooks", ".sync-version"} {
		if fileExists(t, dataDir, path) {
			t.Errorf("forge init must not write DataDir/%s (host wiring is integrations enable)", path)
		}
	}
	claudeHome := os.Getenv("CLAUDE_CONFIG_DIR")
	if fileExists(t, claudeHome, "settings.json") {
		t.Error("forge init must not write user-level settings.json (integrations enable does that)")
	}
	if fileExists(t, claudeHome, "CLAUDE.md") {
		t.Error("forge init must not write user-level CLAUDE.md (integrations enable does that)")
	}

	// forge status should succeed.
	out := forge(t, dir, "status")
	if !strings.Contains(out, "Project:") {
		t.Errorf("forge status output should contain 'Project:', got: %s", out)
	}

}

// TestMasterBranchReminder verifies the task-verify hook warns about
// code changes on master without an active task.

// ---------- Helpers ----------

// passAllGates passes all 3 task gates (v0.17: reduced from 5) for the given task ref.
func passAllGates(t *testing.T, dir, ref string) {
	t.Helper()

	// Disable gate timing for E2E tests (gates pass in rapid sequence)
	os.Setenv("FORGE_GATE_MIN_INTERVAL", "0s")
	defer os.Unsetenv("FORGE_GATE_MIN_INTERVAL")
	os.Setenv("FORGE_WORK_ACTIVITY", "disable")
	defer os.Unsetenv("FORGE_WORK_ACTIVITY")

	// 制造一次真实的文件变更并 commit——task-implement 的代码变更检查比对
	// 内容（git diff HeadCommit..HEAD），空 commit 无法满足。
	scratch := filepath.Join(dir, "e2e-scratch.txt")
	f, err := os.OpenFile(scratch, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("create scratch file: %v", err)
	}
	if _, err := fmt.Fprintf(f, "change for %s\n", ref); err != nil {
		f.Close()
		t.Fatalf("write scratch file: %v", err)
	}
	f.Close()
	git(t, dir, "add", "e2e-scratch.txt")
	git(t, dir, "commit", "-m", "e2e: code change for task-implement")

	// Pass gates in order: task-implement, task-verify, then task-complete.
	// task-complete 有 ReviewPassed 硬前置——先 forge review pass 标记（task 模式
	// 写 TaskState.ReviewPassed），否则 task-complete 被拦。
	for _, g := range []string{"task-implement", "task-verify"} {
		out, err := forgeErr(t, dir, "task", "gate", g, "--ref", ref)
		if err != nil {
			t.Fatalf("forge task gate %s failed: %v\noutput: %s", g, err, out)
		}
	}
	if out, err := forgeErr(t, dir, "review", "pass"); err != nil {
		t.Fatalf("forge review pass failed: %v\noutput: %s", err, out)
	}
	out, err := forgeErr(t, dir, "task", "gate", "task-complete", "--ref", ref)
	if err != nil {
		t.Fatalf("forge task gate task-complete failed: %v\noutput: %s", err, out)
	}
}
