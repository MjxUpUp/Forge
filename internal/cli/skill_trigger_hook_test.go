package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkill 在 canonical dir 下建一个带 triggers 的 skill（裸 JSON——frontmatter.go
// nestedRe 不剥嵌套 metadata 引号）。
func writeSkill(t *testing.T, canonicalDir, name, triggersJSON string) {
	t.Helper()
	dir := filepath.Join(canonicalDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\nmetadata:\n  triggers: " + triggersJSON + "\n---\nbody"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// withCanonicalEnv 把 FORGE_SKILLS_CANONICAL 指向一个临时 canonical dir 并返回它。
func withCanonicalEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FORGE_SKILLS_CANONICAL", dir)
	return dir
}

// isolateSkillTriggerTmp redirects the noise-controller marker dir into a
// per-test temp dir. The production (non-dry-run) path writes per-session
// cooldown markers under os.TempDir()/skill-trigger; with the fixed session
// ids these tests use, a marker left by a previous run (same machine, shared
// $TMPDIR) suppresses injection and the test fails on rerun. Redirecting
// TMPDIR/TMP/TEMP (Unix/Windows respectively) makes each run hermetic.
//
// isolateSkillTriggerTmp 把 noise-controller 的 marker 目录重定向到 per-test
// 临时目录。生产（非 dry-run）路径在 os.TempDir()/skill-trigger 下写
// per-session cooldown marker；这些测试用固定 session id，上一次运行遗留的
// marker（同机共享 $TMPDIR）会抑制注入导致重跑失败。重定向 TMPDIR/TMP/TEMP
// （分别对应 Unix/Windows）让每次运行自包含。
func isolateSkillTriggerTmp(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
}

func TestBuildTriggerContext_FieldsMapped(t *testing.T) {
	hi := HookInput{
		HookEventName: "PostToolUse",
		ToolName:      "Bash",
		Prompt:        "帮我实现",
		SessionID:     "abc-123",
		ToolInput:     json.RawMessage(`{"command":"go test ./..."}`),
		ToolOutput:    json.RawMessage(`{"exit_code":1}`),
	}
	ctx := buildTriggerContext(hi, "/repo")
	if ctx.Event != "PostToolUse" || ctx.ToolName != "Bash" || ctx.Prompt != "帮我实现" {
		t.Errorf("字段映射错: %+v", ctx)
	}
	if ctx.ProjectRoot != "/repo" {
		t.Errorf("ProjectRoot 应 /repo，got %q", ctx.ProjectRoot)
	}
	cmd, _ := ctx.ToolInput["command"].(string)
	if cmd != "go test ./..." {
		t.Errorf("ToolInput.command 未解析，got %v", ctx.ToolInput["command"])
	}
	code, _ := ctx.ToolOutput["exit_code"].(float64)
	if code != 1 {
		t.Errorf("ToolOutput.exit_code 未解析，got %v", ctx.ToolOutput["exit_code"])
	}
	if ctx.Now.IsZero() {
		t.Error("Now 应非零")
	}
}

func TestBuildTriggerContext_EmptyRawMessage(t *testing.T) {
	ctx := buildTriggerContext(HookInput{HookEventName: "Stop"}, "")
	if ctx.ToolInput != nil || ctx.ToolOutput != nil {
		t.Errorf("空 RawMessage 应保持 nil map")
	}
}

func TestRunSkillTriggerCore_NoTriggers(t *testing.T) {
	dir := withCanonicalEnv(t)
	writeSkill(t, dir, "empty-skill", "") // 无 triggers 元数据
	os.MkdirAll(filepath.Join(dir, "no-meta"), 0755)
	os.WriteFile(filepath.Join(dir, "no-meta", "SKILL.md"), []byte("---\nname: no-meta\n---\n"), 0644)

	rendered, err := runSkillTriggerCore(HookInput{HookEventName: "Stop"}, "", "v", true)
	if err != nil {
		t.Fatal(err)
	}
	if rendered != "" {
		t.Errorf("无 triggers 声明应返空，got %q", rendered)
	}
}

func TestRunSkillTriggerCore_EventMismatch(t *testing.T) {
	dir := withCanonicalEnv(t)
	writeSkill(t, dir, "td", `[{"event":"Stop"}]`)

	rendered, err := runSkillTriggerCore(HookInput{HookEventName: "PostToolUse"}, "", "v", true)
	if err != nil {
		t.Fatal(err)
	}
	if rendered != "" {
		t.Errorf("事件不匹配应返空，got %q", rendered)
	}
}

func TestRunSkillTriggerCore_HitCodingIntent(t *testing.T) {
	dir := withCanonicalEnv(t)
	writeSkill(t, dir, "implementation-discipline", `[{"event":"UserPromptSubmit","when":"coding_intent"}]`)

	rendered, err := runSkillTriggerCore(
		HookInput{HookEventName: "UserPromptSubmit", Prompt: "帮我实现一个排序算法", SessionID: "s1"},
		"", "v", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "implementation-discipline") {
		t.Errorf("应命中 implementation-discipline，got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "UserPromptSubmit") {
		t.Errorf("渲染应含事件名")
	}
}

func TestRunSkillTriggerCore_HitTestCommandFailed(t *testing.T) {
	dir := withCanonicalEnv(t)
	writeSkill(t, dir, "test-discipline", `[{"event":"PostToolUse","match":"Bash","when":"test_command_failed"}]`)

	rendered, err := runSkillTriggerCore(HookInput{
		HookEventName: "PostToolUse",
		ToolName:      "Bash",
		ToolInput:     json.RawMessage(`{"command":"go test ./..."}`),
		ToolOutput:    json.RawMessage(`{"exit_code":1}`),
		SessionID:     "s1",
	}, "", "v", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "test-discipline") {
		t.Errorf("测试失败应命中 test-discipline，got:\n%s", rendered)
	}
}

func TestRunSkillTriggerCore_TestCommandPassed_NoHit(t *testing.T) {
	dir := withCanonicalEnv(t)
	writeSkill(t, dir, "test-discipline", `[{"event":"PostToolUse","match":"Bash","when":"test_command_failed"}]`)

	rendered, err := runSkillTriggerCore(HookInput{
		HookEventName: "PostToolUse",
		ToolName:      "Bash",
		ToolInput:     json.RawMessage(`{"command":"go test ./..."}`),
		ToolOutput:    json.RawMessage(`{"exit_code":0}`),
	}, "", "v", true)
	if err != nil {
		t.Fatal(err)
	}
	if rendered != "" {
		t.Errorf("测试通过不应命中，got %q", rendered)
	}
}

func TestRunSkillTriggerCore_DryRunStderr(t *testing.T) {
	dir := withCanonicalEnv(t)
	writeSkill(t, dir, "td", `[{"event":"UserPromptSubmit","when":"coding_intent"}]`)

	// 捕获 stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	_, err := runSkillTriggerCore(
		HookInput{HookEventName: "UserPromptSubmit", Prompt: "实现", SessionID: "s1"},
		"", "v", true)
	w.Close()
	os.Stderr = old
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(r)
	s := string(out)
	if !strings.Contains(s, "[skill-trigger]") {
		t.Errorf("dry-run 应在 stderr 打扫描详情，got:\n%s", s)
	}
	if !strings.Contains(s, "命中") {
		t.Errorf("dry-run 应报告命中，got:\n%s", s)
	}
}

func TestRunSkillTriggerHook_HitOutput(t *testing.T) {
	dir := withCanonicalEnv(t)
	isolateSkillTriggerTmp(t)
	writeSkill(t, dir, "impl-disc", `[{"event":"UserPromptSubmit","when":"coding_intent"}]`)

	out := captureStdout(t, func() {
		if err := runSkillTriggerHook(HookInput{
			HookEventName: "UserPromptSubmit",
			Prompt:        "帮我实现功能",
			SessionID:     "cli-test-hook-hit",
		}, "", "v", ""); err != nil {
			t.Errorf("runSkillTriggerHook: %v", err)
		}
	})

	var ho HookOutput
	if err := json.Unmarshal([]byte(out), &ho); err != nil {
		t.Fatalf("输出非合法 HookOutput JSON: %v\n%s", err, out)
	}
	if ho.Decision != "approve" {
		t.Errorf("decision 应 approve，got %s", ho.Decision)
	}
	if ho.HookSpecificOutput == nil {
		t.Fatal("命中时应含 hookSpecificOutput")
	}
	if ho.HookSpecificOutput.HookEventName != "UserPromptSubmit" {
		t.Errorf("hookEventName 应 UserPromptSubmit，got %s", ho.HookSpecificOutput.HookEventName)
	}
	if !strings.Contains(ho.HookSpecificOutput.AdditionalContext, "impl-disc") {
		t.Errorf("additionalContext 应含 impl-disc，got:\n%s", ho.HookSpecificOutput.AdditionalContext)
	}
}

func TestRunSkillTriggerHook_NoHitOutput(t *testing.T) {
	dir := withCanonicalEnv(t)
	isolateSkillTriggerTmp(t)
	writeSkill(t, dir, "td", `[{"event":"Stop"}]`)

	out := captureStdout(t, func() {
		if err := runSkillTriggerHook(HookInput{
			HookEventName: "PostToolUse", // 不匹配 Stop
			SessionID:     "cli-test-hook-nohit",
		}, "", "v", ""); err != nil {
			t.Errorf("runSkillTriggerHook: %v", err)
		}
	})

	var ho HookOutput
	if err := json.Unmarshal([]byte(out), &ho); err != nil {
		t.Fatalf("输出非合法 JSON: %v\n%s", err, out)
	}
	if ho.Decision != "approve" {
		t.Errorf("无命中也应 approve，got %s", ho.Decision)
	}
	if ho.HookSpecificOutput != nil {
		t.Errorf("无命中不应有 additionalContext，got %+v", ho.HookSpecificOutput)
	}
}

// TestRunSkillTriggerCore_EmbedFallback: P0 回归——不设 FORGE_SKILLS_CANONICAL，强制 Resolve
// 走 embed fallback（ok=isExternal=false）。runSkillTriggerCore 曾因 !ok 误把 embed cache 判为
// "无 canonical 源"，导致生产所有事件静默 PASS、skill-trigger 框架完全失效（1.14.0）。
// 本测试重定向 home 避免污染用户 ~/.forge，验证 embed 路径正常扫描命中。
func TestRunSkillTriggerCore_EmbedFallback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("USERPROFILE", tmp)          // Windows: os.UserHomeDir 读 USERPROFILE
	t.Setenv("HOME", tmp)                 // Unix fallback
	t.Setenv("FORGE_SKILLS_CANONICAL", "") // 强制 embed fallback（非 env 覆盖）

	rendered, err := runSkillTriggerCore(
		HookInput{HookEventName: "UserPromptSubmit", Prompt: "帮我实现一个排序算法", SessionID: "embed-fb-test"},
		"", "v", true) // dryRun=true 用 InMemory noise，不落盘 marker
	if err != nil {
		t.Fatalf("runSkillTriggerCore: %v", err)
	}
	if !strings.Contains(rendered, "implementation-discipline") {
		t.Errorf("embed fallback（ok=false）应正常命中 implementation-discipline，不应误判无源，got:\n%s", rendered)
	}
}

func TestRunSkillTriggerHook_DeniedSkillSkipped(t *testing.T) {
	dir := withCanonicalEnv(t)
	isolateSkillTriggerTmp(t)
	// code-review-gate 在 DeniedSkills——即便声明 triggers 也不注入
	writeSkill(t, dir, "code-review-gate", `[{"event":"UserPromptSubmit","when":"coding_intent"}]`)
	writeSkill(t, dir, "normal", `[{"event":"UserPromptSubmit","when":"coding_intent"}]`)

	out := captureStdout(t, func() {
		runSkillTriggerHook(HookInput{
			HookEventName: "UserPromptSubmit",
			Prompt:        "实现",
			SessionID:     "cli-test-deny",
		}, "", "v", "")
	})
	if strings.Contains(out, "code-review-gate") {
		t.Errorf("denied skill 不应注入，got:\n%s", out)
	}
	if !strings.Contains(out, "normal") {
		t.Errorf("normal skill 应注入，got:\n%s", out)
	}
}
