package hookdispatch

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// The WSL-launcher recognition and bash-resolution tests (TestIsWSLBash /
// TestFindBash_SkipsWSL / TestFindBash_GitDerived) moved to
// internal/shellexec/bash_test.go together with the implementation when the
// resolution logic was shared with the gate path (taskpipeline.runEmbeddedHook).
// What remains here pins the cli-side wrappers and the fail-open output contract.

// exitErrorOf runs a trivial command that exits with the given code, producing a real
// *exec.ExitError for isHookInfraFailure tests.
func exitErrorOf(t *testing.T, code int) error {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "exit", strconv.Itoa(code))
	} else {
		cmd = exec.Command("sh", "-c", "exit "+strconv.Itoa(code))
	}
	err := cmd.Run()
	if err == nil {
		t.Fatalf("command with exit %d returned nil error", code)
	}
	return err
}

// TestIsHookInfraFailure pins the fail-open boundary: spawn errors and bash 126/127 are
// infrastructure (fail open); script exit 1 (gate FAIL) and exit 2 (review-stop) remain
// gate verdicts.
func TestIsHookInfraFailure(t *testing.T) {
	if isHookInfraFailure(nil) {
		t.Error("nil error is not an infra failure")
	}
	if !isHookInfraFailure(exec.ErrNotFound) {
		t.Error("spawn error (non-ExitError) must be infra failure")
	}
	for _, code := range []int{126, 127} {
		if !isHookInfraFailure(exitErrorOf(t, code)) {
			t.Errorf("exit %d must be infra failure", code)
		}
	}
	for _, code := range []int{1, 2} {
		if isHookInfraFailure(exitErrorOf(t, code)) {
			t.Errorf("exit %d (script gate verdict) must NOT be infra failure", code)
		}
	}
}

// TestEmitInfraAllow pins the fail-open output contract: on kimi the warning
// rides the advisory queue — PreToolUse stays SILENT (any stdout there is read
// as a deny: the hook would fail open AND block the edit) and lands in
// advisories-pending.jsonl for the UserPromptSubmit drain; claude gets a BARE
// hookSpecificOutput (no decision — an allow
// hook must not grant permissions) whose hookEventName is present — without it Claude
// drops additionalContext and the warning would be silently lost.
func TestEmitInfraAllow(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()
	t.Setenv("TMPDIR", t.TempDir())

	// kimi PreToolUse：静默（该事件的 stdout = deny），警告入队。
	stdout, _, err := captureOutput(t, func() error {
		return emitInfraAllow("kimi", "PreToolUse", "infra-hook", root, "sess-infra", "[forge] hook x 基础设施失败，fail-open 放行")
	})
	if err != nil {
		t.Fatalf("kimi infra allow must return nil (exit 0), got %v", err)
	}
	if stdout != "" {
		t.Errorf("kimi PreToolUse infra warning must NOT print stdout (read as deny), got %q", stdout)
	}
	queued, qerr := os.ReadFile(kimiAdvisoryQueuePath(root))
	if qerr != nil || !strings.Contains(string(queued), "基础设施失败") {
		t.Errorf("kimi infra warning must be queued for the UserPromptSubmit drain, got %q err=%v", queued, qerr)
	}

	// kimi UserPromptSubmit：警告照常打印，且积压队列随之一并 drain。
	stdout, _, err = captureOutput(t, func() error {
		return emitInfraAllow("kimi", "UserPromptSubmit", "infra-hook", root, "sess-infra", "[forge] hook y 基础设施失败")
	})
	if err != nil {
		t.Fatalf("kimi UserPromptSubmit infra allow must return nil, got %v", err)
	}
	if !strings.Contains(stdout, "基础设施失败") {
		t.Errorf("kimi UserPromptSubmit must surface the warning + drained backlog, got %q", stdout)
	}

	stdout, _, err = captureOutput(t, func() error {
		return emitInfraAllow("", "PreToolUse", "infra-hook", root, "sess-infra", "warn")
	})
	if err != nil {
		t.Fatalf("claude infra allow must return nil, got %v", err)
	}
	var out HookOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &out); err != nil {
		t.Fatalf("claude path must emit valid HookOutput JSON: %v\n%s", err, stdout)
	}
	if out.Decision != "" {
		t.Errorf("decision = %q, want \"\" (bare hookSpecificOutput; allow must never emit approve)", out.Decision)
	}
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookSpecificOutput.hookEventName missing/wrong: %+v", out.HookSpecificOutput)
	}
	if out.HookSpecificOutput == nil || out.HookSpecificOutput.AdditionalContext != "warn" {
		t.Errorf("additionalContext missing: %+v", out.HookSpecificOutput)
	}
}

// TestRunHookCreateTempFailureRoutesInfraAllow pins the infra-exit unification
// (fix/cleanup-batch, 2026-08-29): the script-never-runs failures in runHook
// step 3/4 (temp-file create/write, findBash) are the same infrastructure class
// as the step-5 bash-spawn failure and must route through emitInfraAllow.
//
// TestRunHookCreateTempFailureRoutesInfraAllow 钉住 infra 出口统一
// （fix/cleanup-batch，2026-08-29）：runHook step 3/4 的「脚本永远跑不起来」失败
// （临时文件创建/写入、findBash）与 step 5 的 bash 起不来同属基础设施类，必须改走
// emitInfraAllow——可见的、按宿主路由的警告 + exit 0（fail-open）——而非返回裸
// error（Execute 打印、exit 1，任何宿主上下文通道都看不到）。临时文件失败以
// 「temp 目录 env 指向不存在路径」诱发；claude 形态的发射以裸 hookSpecificOutput
// 落在 stdout。
func TestRunHookCreateTempFailureRoutesInfraAllow(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	// 把所有 temp 目录 env 指向不存在的路径使 os.CreateTemp 失败——即 step-3
	// infra 失败（Windows 查 TMP/TEMP、Unix 查 TMPDIR；三个都设即全覆盖）。
	badTmp := filepath.Join(t.TempDir(), "no-such-dir")
	t.Setenv("TMP", badTmp)
	t.Setenv("TEMP", badTmp)
	t.Setenv("TMPDIR", badTmp)

	// 最小 forge 项目，让项目级分发走到 step 3。
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".forge", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644); err != nil {
		t.Fatal(err)
	}
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	oldStdin := os.Stdin
	// 显式目录：本测试刻意毒化 os.TempDir()（那正是要诱发的失败），stdin 夹具
	// 必须落在真实存在的目录里。
	tmpStdin, err := os.CreateTemp(t.TempDir(), "hook-stdin-*.json")
	if err != nil {
		t.Fatal(err)
	}
	tmpStdin.WriteString(`{"hook_event_name":"PreToolUse","tool_name":"Edit","tool_input":{"file_path":"src/main.go","old_string":"a","new_string":"b"}}`)
	tmpStdin.Seek(0, 0)
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()

	stdout, _, err := captureOutput(t, func() error {
		return RunHook(nil, []string{"task-guard"})
	})
	// fail-open 契约：nil error（exit 0），绝不再返回裸 error。
	if err != nil {
		t.Fatalf("temp-file infra failure must fail open (nil, exit 0) via emitInfraAllow, got %v", err)
	}
	// 警告在 claude 上下文通道可见（裸 hookSpecificOutput JSON），点名 infra
	// 失败与 fail-open。
	if !strings.Contains(stdout, "基础设施失败") || !strings.Contains(stdout, "fail-open") {
		t.Fatalf("infra warning must surface on the host channel, got stdout %q", stdout)
	}
	if strings.Contains(stdout, `"decision"`) {
		t.Fatalf("allow path must not carry a decision field, got %q", stdout)
	}
}
