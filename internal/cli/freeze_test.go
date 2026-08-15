package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupFreezeProject creates a minimal non-git forge project (legacy .forge/ dir
// is enough for projectroot.FindProject; FORGE_DATA_HOME is TestMain-isolated).
//
// setupFreezeProject 建最小非 git forge 项目（遗留 .forge/ 目录即可被
// projectroot.FindProject 识别；FORGE_DATA_HOME 已被 TestMain 隔离）。
func setupFreezeProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".forge"), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFreezeActivateStatusOff(t *testing.T) {
	dir := setupFreezeProject(t)

	// 激活前 --status：未激活
	stdout, _, code := runForge(t, dir, "freeze", "--status")
	if code != 0 {
		t.Fatalf("freeze --status exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "未激活") {
		t.Errorf("status before activate = %q, want 未激活", stdout)
	}

	// 激活（相对路径）
	stdout, _, code = runForge(t, dir, "freeze", "src")
	if code != 0 {
		t.Fatalf("freeze src exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "已 freeze") {
		t.Errorf("activate output = %q, want 已 freeze", stdout)
	}

	// 激活后 --status：列出路径（canonical 绝对路径，含 src）
	stdout, _, code = runForge(t, dir, "freeze", "--status")
	if code != 0 {
		t.Fatalf("freeze --status exit = %d", code)
	}
	if !strings.Contains(stdout, "激活中") || !strings.Contains(stdout, "src") {
		t.Errorf("status after activate = %q, want 激活中 + src path", stdout)
	}

	// 解除
	stdout, _, code = runForge(t, dir, "freeze", "--off")
	if code != 0 || !strings.Contains(stdout, "已解除") {
		t.Errorf("--off = %q (exit %d), want 已解除", stdout, code)
	}
	stdout, _, _ = runForge(t, dir, "freeze", "--status")
	if !strings.Contains(stdout, "未激活") {
		t.Errorf("status after --off = %q, want 未激活", stdout)
	}
}

func TestFreezeNoArgsIsError(t *testing.T) {
	dir := setupFreezeProject(t)
	if _, _, code := runForge(t, dir, "freeze"); code == 0 {
		t.Error("bare `forge freeze` should fail (needs paths / --off / --status)")
	}
}

func TestFreezeCheckExitCodes(t *testing.T) {
	dir := setupFreezeProject(t)

	// 无 freeze：一律放行（exit 0，静默）
	stdout, _, code := runForge(t, dir, "freeze", "check", "--path", "anywhere/x.go")
	if code != 0 || stdout != "" {
		t.Fatalf("check without freeze = exit %d, stdout %q; want 0, silent", code, stdout)
	}

	// 多路径激活后：冻结路径内放行、外阻断
	if _, _, code := runForge(t, dir, "freeze", "src", "docs"); code != 0 {
		t.Fatal("activate failed")
	}
	for _, allow := range []string{"src/a.go", "docs/b.md"} {
		if _, _, code := runForge(t, dir, "freeze", "check", "--path", allow); code != 0 {
			t.Errorf("check --path %s exit = %d, want 0 (inside frozen scope)", allow, code)
		}
	}
	stdout, _, code = runForge(t, dir, "freeze", "check", "--path", "scripts/c.sh")
	if code != 1 {
		t.Fatalf("check outside frozen scope exit = %d, want 1", code)
	}
	if !strings.Contains(stdout, "目录已 freeze") {
		t.Errorf("blocked reason = %q, want 目录已 freeze", stdout)
	}
	if !strings.Contains(stdout, "forge freeze --off") {
		t.Errorf("blocked reason should point at --off: %q", stdout)
	}

	// 解除后恢复放行
	if _, _, code := runForge(t, dir, "freeze", "--off"); code != 0 {
		t.Fatal("--off failed")
	}
	if _, _, code := runForge(t, dir, "freeze", "check", "--path", "scripts/c.sh"); code != 0 {
		t.Error("check after --off should allow")
	}
}

// runFreezeGuardHook runs the freeze-guard hook in-process with a Write payload
// and parses the Claude-Code-shape JSON verdict. The hook's bash script shells
// out to `forge freeze check`, so the built forge binary's dir is prepended to
// PATH (TestMain built it; FORGE_DATA_HOME isolation is inherited).
//
// runFreezeGuardHook 在进程内跑 freeze-guard hook（Write payload）并解析
// Claude-Code 形 JSON 结论。hook 的 bash 脚本会 fork `forge freeze check`，
// 故把已构建 forge 二进制所在目录前置进 PATH（TestMain 已构建；
// FORGE_DATA_HOME 隔离随之继承）。
func runFreezeGuardHook(t *testing.T, dir, filePath string) HookOutput {
	t.Helper()
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", filepath.Dir(buildForge(t))+string(os.PathListSeparator)+origPath)
	defer os.Setenv("PATH", origPath)

	origWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	oldStdin := os.Stdin
	tmpStdin, err := os.CreateTemp("", "hook-stdin-*.json")
	if err != nil {
		t.Fatal(err)
	}
	payloadBytes, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Write",
		"tool_input":      map[string]string{"file_path": filePath},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmpStdin.Write(payloadBytes); err != nil {
		t.Fatal(err)
	}
	tmpStdin.Seek(0, 0)
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = runHook(nil, []string{"freeze-guard"})

	w.Close()
	os.Stdout = oldStdout

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	output := strings.TrimSpace(string(buf[:n]))
	// Silent allow (exit 0, no stdout) is the legal allow shape since Wave 1 —
	// returns a zero HookOutput whose Decision is "" (never "block").
	var result HookOutput
	if output != "" {
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("output is not valid JSON: %q, err: %v", output, err)
		}
	}
	return result
}

func TestHook_FreezeGuardBlocksOutsideFrozenPath(t *testing.T) {
	dir := setupFreezeProject(t)
	if _, _, code := runForge(t, dir, "freeze", "src"); code != 0 {
		t.Fatal("activate failed")
	}

	inside := filepath.Join(dir, "src", "main.go")
	out := runFreezeGuardHook(t, dir, inside)
	if out.Decision == "block" {
		t.Errorf("write inside frozen path must allow (silent or bare), got decision = %q", out.Decision)
	}

	outside := filepath.Join(dir, "docs", "readme.md")
	out = runFreezeGuardHook(t, dir, outside)
	if out.Decision != "block" {
		t.Fatalf("write outside frozen path: decision = %q, want block", out.Decision)
	}
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "目录已 freeze") {
		t.Errorf("block additionalContext = %+v, want 目录已 freeze 提示", out.HookSpecificOutput)
	}
}

func TestHook_FreezeGuardInactiveAllows(t *testing.T) {
	dir := setupFreezeProject(t)
	out := runFreezeGuardHook(t, dir, filepath.Join(dir, "anywhere", "x.go"))
	if out.Decision == "block" {
		t.Errorf("no freeze active: must allow (silent or bare), got decision = %q", out.Decision)
	}
}
