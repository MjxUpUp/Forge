package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
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

	rendered, err := runSkillTriggerCore(HookInput{HookEventName: "Stop"}, "", "v", "", true)
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

	rendered, err := runSkillTriggerCore(HookInput{HookEventName: "PostToolUse"}, "", "v", "", true)
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
		"", "v", "", true)
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
	}, "", "v", "", true)
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
	}, "", "v", "", true)
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
		"", "v", "", true)
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
	// allow-with-detail 现为裸 hookSpecificOutput——decision 必须为空
	// （decision:"approve" 会绕过 Claude 权限流程，codex 会判 hook failed）。
	if ho.Decision != "" {
		t.Errorf("decision 应为空（裸 hookSpecificOutput），got %q", ho.Decision)
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

	// 无命中 = 无 detail 的 allow → 各宿主均静默（exit 0、零 stdout），
	// 不再打 {"decision":"approve"} envelope。
	if strings.TrimSpace(out) != "" {
		t.Errorf("无命中应静默（无 stdout），got %q", out)
	}
}

// TestRunSkillTriggerCore_EmbedFallback: P0 回归——不设 FORGE_SKILLS_CANONICAL，强制 Resolve
// 走 embed fallback（ok=isExternal=false）。runSkillTriggerCore 曾因 !ok 误把 embed cache 判为
// "无 canonical 源"，导致生产所有事件静默 PASS、skill-trigger 框架完全失效（1.14.0）。
// 本测试重定向 home 避免污染用户 ~/.forge，验证 embed 路径正常扫描命中。
func TestRunSkillTriggerCore_EmbedFallback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("USERPROFILE", tmp)           // Windows: os.UserHomeDir 读 USERPROFILE
	t.Setenv("HOME", tmp)                  // Unix fallback
	t.Setenv("FORGE_SKILLS_CANONICAL", "") // 强制 embed fallback（非 env 覆盖）

	rendered, err := runSkillTriggerCore(
		HookInput{HookEventName: "UserPromptSubmit", Prompt: "帮我实现一个排序算法", SessionID: "embed-fb-test"},
		"", "v", "", true) // dryRun=true 用 InMemory noise，不落盘 marker
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

// TestRunSkillTriggerHook_KimiSuppressedOffUserPromptSubmit is the kimi delivery
// guard: on every event except UserPromptSubmit, kimi drops allow-path stdout
// from the model context (verified via wire.jsonl), so skill-trigger must NOT
// print there.
//
// TestRunSkillTriggerHook_KimiSuppressedOffUserPromptSubmit 是 kimi 投递守卫：除
// UserPromptSubmit 外的每个事件，kimi 丢弃 allow-path stdout（wire.jsonl 实测），故
// skill-trigger 不得在这些事件打印——但引擎仍运行、渲染出的注入入队待
// UserPromptSubmit 攒发，并把每条命中以 Delivered=false +
// Channel=kimi/advisory-queue 落 checklog（诚实可观测：看板事件流与 usage 漏斗看到
// 完整触发图景；漏斗只计 Delivered=true，故这些记录不可能复活虚假繁荣 bug，且队列
// 通道标签让「入队待投」与「永久丢失」可区分）。每个子测试建一个本会在该事件触发的
// skill，然后断言：无 stdout 且每条记录都落了未送达章。
func TestRunSkillTriggerHook_KimiSuppressedOffUserPromptSubmit(t *testing.T) {
	events := []string{"PreToolUse", "PostToolUse", "Stop", "SessionStart"}
	for _, ev := range events {
		t.Run(ev, func(t *testing.T) {
			dir := withCanonicalEnv(t)
			isolateSkillTriggerTmp(t)
			root := t.TempDir()
			// A skill that WOULD trigger on this event — the delivery guard applies regardless.
			writeSkill(t, dir, "probe-skill", fmt.Sprintf(`[{"event":%q,"when":"coding_intent"}]`, ev))

			out := captureStdout(t, func() {
				if err := runSkillTriggerHook(HookInput{
					HookEventName: ev,
					Prompt:        "帮我实现功能", // coding_intent true → would trigger
					SessionID:     "kimi-suppress-" + ev,
				}, root, "v", "kimi"); err != nil {
					t.Errorf("runSkillTriggerHook kimi %s: %v", ev, err)
				}
			})

			if out != "" {
				t.Errorf("kimi %s: must emit NO stdout (kimi drops it for this event), got %q", ev, out)
			}
			// 诚实记录断言：命中被记录（事件流不再有 kimi 盲区）但必须落
			// Delivered=false 章，usage 漏斗永不计为送达。
			entries, err := checklog.LoadAll(root)
			if err != nil {
				t.Fatalf("LoadAll checklog: %v", err)
			}
			var hits int
			for _, e := range entries {
				if e.Check != checklog.CheckSkillTrigger {
					continue
				}
				hits++
				if e.Delivered == nil || *e.Delivered {
					t.Errorf("kimi %s: recorded hit must be stamped Delivered=false (false-prosperity guard), got %+v", ev, e)
				}
				if e.Channel != "kimi/advisory-queue" {
					t.Errorf("kimi %s: Channel must be kimi/advisory-queue (queued for the UserPromptSubmit drain, not lost), got %q", ev, e.Channel)
				}
			}
			if hits == 0 {
				t.Errorf("kimi %s: hit must be RECORDED with Delivered=false (observability), got zero skill-trigger entries", ev)
			}
		})
	}
}

// TestRunSkillTriggerHook_KimiUserPromptSubmitStillInjects is the positive
// complement to the suppression test: UserPromptSubmit is the ONE event kimi
// reaches the model on, so skill-trigger must STILL run + inject + record a
// checklog there.
//
// TestRunSkillTriggerHook_KimiUserPromptSubmitStillInjects 是抑制测试的正向补集：UserPromptSubmit
// 是 kimi 唯一能到达模型的事件，故 skill-trigger 必须在此处照常运行 + 注入 + 记 checklog。
// 证明守卫是按事件的（非 kimi 一刀切抑制），且合法"已送达"触发信号得以保留。
func TestRunSkillTriggerHook_KimiUserPromptSubmitStillInjects(t *testing.T) {
	dir := withCanonicalEnv(t)
	isolateSkillTriggerTmp(t)
	root := t.TempDir()
	writeSkill(t, dir, "probe-skill", `[{"event":"UserPromptSubmit","when":"coding_intent"}]`)

	out := captureStdout(t, func() {
		if err := runSkillTriggerHook(HookInput{
			HookEventName: "UserPromptSubmit",
			Prompt:        "帮我实现功能",
			SessionID:     "kimi-ups-inject",
		}, root, "v", "kimi"); err != nil {
			t.Errorf("runSkillTriggerHook kimi UserPromptSubmit: %v", err)
		}
	})

	if !strings.Contains(out, "probe-skill") {
		t.Errorf("kimi UserPromptSubmit must STILL inject (only non-UPS events are suppressed), got %q", out)
	}
	// And it DID record a checklog entry — the legit delivered trigger.
	entries, err := checklog.LoadAll(root)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Check == checklog.CheckSkillTrigger {
			found = true
		}
	}
	if !found {
		t.Errorf("kimi UserPromptSubmit must record a skill-trigger checklog entry (legit delivered trigger), got %d entries", len(entries))
	}
}
