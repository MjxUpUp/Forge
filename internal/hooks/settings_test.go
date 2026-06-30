package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateSettingsCreatesFile(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings returned error: %v", err)
	}

	expected := filepath.Join(dir, ".claude", "settings.local.json")
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Fatalf("settings file not created at %s", expected)
	}
}

func TestGenerateSettingsJSONStructure(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	hooks, ok := parsed["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("missing top-level 'hooks' key or wrong type")
	}

	for _, key := range []string{"PreToolUse", "PostToolUse", "Stop"} {
		if _, exists := hooks[key]; !exists {
			t.Errorf("hooks.%s not found", key)
		}
	}
}

func TestGenerateSettingsHookEntries(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}

	var parsed struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher,omitempty"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	for _, hookGroup := range parsed.Hooks {
		for _, matcher := range hookGroup {
			for _, entry := range matcher.Hooks {
				if entry.Type != "command" {
					t.Errorf("hook entry has type=%q, want \"command\"", entry.Type)
				}
				if entry.Command == "" {
					t.Error("hook entry has empty command")
				}
				// Hook commands must NOT use bash with relative .forge/ paths.
				if strings.Contains(entry.Command, "bash .forge/") {
					t.Errorf("hook command uses relative path %q — must use 'forge hook <name>'", entry.Command)
				}
			}
		}
	}
}

func TestGenerateSettingsUsesForgeHook(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}
	content := string(data)

	// All hook invocations should route through "forge hook <name>"
	for _, name := range []string{"auto-compile", "assertion-check", "task-verify", "task-guard", "bash-guard", "file-sentinel", "skill-scan", "workflow-test-guard"} {
		expected := "forge hook " + name
		if !strings.Contains(content, expected) {
			t.Errorf("settings missing %q command", expected)
		}
	}
}

func TestEmbeddedContent(t *testing.T) {
	// Known hooks return content and true
	for _, name := range []string{"auto-compile", "assertion-check", "task-verify", "bash-guard", "file-sentinel", "task-guard", "skill-scan", "workflow-test-guard"} {
		content, ok := EmbeddedContent(name)
		if !ok {
			t.Errorf("EmbeddedContent(%q) returned false", name)
		}
		if len(content) == 0 {
			t.Errorf("EmbeddedContent(%q) returned empty content", name)
		}
	}

	// Unknown hook returns false
	_, ok := EmbeddedContent("nonexistent")
	if ok {
		t.Error("EmbeddedContent should return false for unknown hook")
	}
}

func TestWriteHookTemplatesCreatesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := WriteHookTemplates(dir); err != nil {
		t.Fatalf("WriteHookTemplates returned error: %v", err)
	}

	hooksDir := filepath.Join(dir, "hooks")
	expected := []string{"auto-compile.sh", "assertion-check.sh", "task-verify.sh", "task-guard.sh", "bash-guard.sh", "file-sentinel.sh", "skill-scan.sh", "workflow-test-guard.sh"}
	for _, name := range expected {
		path := filepath.Join(hooksDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("hook file not created: %s", path)
		}
	}
}

func TestWriteHookTemplatesContentMatches(t *testing.T) {
	dir := t.TempDir()
	if err := WriteHookTemplates(dir); err != nil {
		t.Fatalf("WriteHookTemplates returned error: %v", err)
	}

	hooksDir := filepath.Join(dir, "hooks")
	cases := []struct {
		filename string
		needle   string
	}{
		// v0.25 advisory: auto-compile 不再跑编译器，只提醒 agent 自检；
		// assertion-check 检测到弱化只提醒不阻塞。
		{"auto-compile.sh", "advisory"},
		{"auto-compile.sh", "编译命令确认编译通过"},
		{"assertion-check.sh", "advisory"},
		{"assertion-check.sh", "疑似断言弱化"},
	}

	for _, tc := range cases {
		data, err := os.ReadFile(filepath.Join(hooksDir, tc.filename))
		if err != nil {
			t.Fatalf("failed to read %s: %v", tc.filename, err)
		}
		content := string(data)
		if !containsString(content, tc.needle) {
			t.Errorf("%s: expected to contain %q", tc.filename, tc.needle)
		}
	}
}

func TestStopHooksIncludeTaskVerify(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}

	var parsed struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	stopHooks := parsed.Hooks["Stop"]
	found := false
	for _, group := range stopHooks {
		for _, h := range group.Hooks {
			if strings.Contains(h.Command, "forge hook task-verify") {
				found = true
			}
		}
	}
	if !found {
		t.Error("Stop hooks missing 'forge hook task-verify' command")
	}
}

func TestTaskVerifyHookContainsMasterCheck(t *testing.T) {
	if !containsString(TaskVerifyHook, "Code changes on") {
		t.Error("TaskVerifyHook missing 'Code changes on' master branch detection")
	}
	if !containsString(TaskVerifyHook, "without active task") {
		t.Error("TaskVerifyHook missing 'without active task' warning")
	}
	if !containsString(TaskVerifyHook, "forge task start") {
		t.Error("TaskVerifyHook missing 'forge task start' hint in warning")
	}
	// BSD-safe: the master-check source-extension filter must use is_code_file
	// case-glob, NOT grep -E '\.(go|rs|...)$' — BSD/macOS aborts on ERE
	// alternation with "Unmatched ( or \(", silently disabling the check.
	if !containsString(TaskVerifyHook, "is_code_file") {
		t.Error("TaskVerifyHook master-check must use is_code_file (BSD-safe case-glob), not grep -E alternation")
	}
}

// TestTaskVerifyHookCounterIsProjectScoped guards against the global-counter
// regression: a bare /tmp/forge-verify-fail-count (no per-project tag) leaked
// failure counts across concurrent projects and parallel e2e tests, making
// TestMasterBranchReminder flaky (an unrelated project's first failure hit the
// 3-strike threshold and force-allowed, masking its real warning).
func TestTaskGuardHookContainsKeyChecks(t *testing.T) {
	if !containsString(TaskGuardHook, "FORGE_TASK_REF") {
		t.Error("TaskGuardHook missing FORGE_TASK_REF check")
	}
	if !containsString(TaskGuardHook, "FORGE_TASK_GATE") {
		t.Error("TaskGuardHook missing FORGE_TASK_GATE check")
	}
	if !containsString(TaskGuardHook, "WARN [task-guard]") {
		t.Error("TaskGuardHook missing WARN for no-task scenario")
	}
	if !containsString(TaskGuardHook, "auto-create") {
		t.Error("TaskGuardHook contains auto-create task path")
	}
	if !containsString(TaskGuardHook, "WARN") {
		t.Error("TaskGuardHook missing WARN for pre-design state")
	}
}

func TestTaskGuardHookPassesNonCodeFiles(t *testing.T) {
	if !containsString(TaskGuardHook, ".(go|rs|ts|tsx|js|jsx|py|java|rb|zig|nim)") {
		t.Error("TaskGuardHook missing code file extension filter")
	}
}

func TestBashGuardHookContainsKeyChecks(t *testing.T) {
	if !containsString(BashGuardHook, "FORGE_COMMAND") {
		t.Error("BashGuardHook missing FORGE_COMMAND check")
	}
	if !containsString(BashGuardHook, "writeFile") {
		t.Error("BashGuardHook missing writeFile pattern detection")
	}
	if !containsString(BashGuardHook, "WARN [bash-guard]") {
		t.Error("BashGuardHook missing WARN for no-task scenario")
	}
	if !containsString(BashGuardHook, "bash-guard") {
		t.Error("BashGuardHook missing [bash-guard] prefix")
	}
	// P0 fix: bash-guard must record whether THIS command is a write command
	// (forge-write-<session> flag file) so file-sentinel's secondary gate can
	// distinguish read-only commands (ls/cat/git diff) from write commands and
	// never quarantine under a read-only command.
	if !containsString(BashGuardHook, "forge-write") {
		t.Error("BashGuardHook missing write-flag file (forge-write-<session>) for file-sentinel secondary gate")
	}
}

func TestFileSentinelHookContainsKeyChecks(t *testing.T) {
	if !containsString(FileSentinelHook, "SNAPSHOT_FILE") {
		t.Error("FileSentinelHook missing SNAPSHOT_FILE reference")
	}
	if !containsString(FileSentinelHook, "file-sentinel") {
		t.Error("FileSentinelHook missing [file-sentinel] prefix")
	}
	if !containsString(FileSentinelHook, "git checkout") {
		t.Error("FileSentinelHook missing git checkout restore logic")
	}
	if !containsString(FileSentinelHook, "quarantine_files") {
		t.Error("FileSentinelHook missing quarantine_files function")
	}
	if !containsString(FileSentinelHook, ".forge/quarantine") {
		t.Error("FileSentinelHook missing quarantine directory path")
	}
	if !containsString(FileSentinelHook, "Recover:") {
		t.Error("FileSentinelHook missing recovery instructions")
	}
	// A6: CFG_EXT must cover .forge/gates/ so a Bash-written
	// gates/<id>/status.json (the gate's truth — all_gates_passed) is quarantined
	// as config, not silently flipped to unblock a failed gate.
	if !containsString(FileSentinelHook, "gates") {
		t.Error("FileSentinelHook CFG_EXT must include gates/ (A6: protect gate verdict files)")
	}
	// P0 fix: file-sentinel must FAIL-OPEN when the PreToolUse snapshot is
	// empty/unreliable (BEFORE_ALL empty while working tree has changes) — it
	// must never treat the whole working tree as new violations and quarantine
	// + git-checkout away the user's existing uncommitted source. And a
	// read-only Bash command must never trigger quarantine (secondary gate).
	if !containsString(FileSentinelHook, "IS_WRITE_CMD") {
		t.Error("FileSentinelHook missing IS_WRITE_CMD secondary gate (read-only command must not quarantine)")
	}
	if !containsString(FileSentinelHook, "WRITE_FLAG_FILE") {
		t.Error("FileSentinelHook missing WRITE_FLAG_FILE read of bash-guard's write flag")
	}
	if containsString(FileSentinelHook, `rm -f "$f"`) {
		t.Error("FileSentinelHook should NOT use rm -f on user files — use quarantine instead")
	}
}

// TestBashGuardHookWritePatterns guards E3: has_write_pattern must detect in-place
// editors and apply-style writers (perl -i, git apply, patch, printf >) that
// mutate files without a shell redirect.
func TestBashGuardHookWritePatterns(t *testing.T) {
	for _, pat := range []string{"perl", "git apply", "patch", "printf"} {
		if !containsString(BashGuardHook, pat) {
			t.Errorf("BashGuardHook has_write_pattern missing write pattern %q (E3)", pat)
		}
	}
}

// TestTaskVerifyHookIsAdvisory guards the advisory rewrite: task-verify must
// NEVER block (no exit 1, no failure counter), yet still persist detected
// issues to checklog so they stay traceable via 'forge trace'. This carries
// the E4 spirit — bypass/issues queryable — via the advisory checklog entry
// instead of the removed force-pass-after-3-failures counter.
func TestTaskVerifyHookIsAdvisory(t *testing.T) {
	if containsString(TaskVerifyHook, "exit 1") {
		t.Error("TaskVerifyHook must not block (advisory) — found 'exit 1'")
	}
	if containsString(TaskVerifyHook, "VERIFY_COUNTER") {
		t.Error("TaskVerifyHook must not carry a failure counter (advisory, never blocks)")
	}
	if !containsString(TaskVerifyHook, `"check":"task-verify"`) {
		t.Error("TaskVerifyHook must record an advisory checklog entry for detected issues")
	}
	if !containsString(TaskVerifyHook, ".forge/checklog.jsonl") {
		t.Error("TaskVerifyHook advisory must append to .forge/checklog.jsonl")
	}
}

// TestReviewStopHookPassIsSilent 守护 Stop 死循环修复（2026-06-27）：review-stop
// 在 gate exit 0（PASS/ADVISORY）时必须静默 exit 0，分支体内不得有任何 echo。
// hook.go runHook 把脚本 stdout（extractDetail 去 "PASS " 前缀）当 AdditionalContext
// 注入 Claude Code——PASS 时若 echo "PASS 无未提交变更..."，harness 就把它当 feedback
// 激活 agent 再响应一轮，造成 Stop→feedback→响应→Stop 死循环（"无未提交变更，无需审查"
// 反复刷屏即此症）。FAIL 分支（exit 2）才允许 echo 指引。
func TestReviewStopHookPassIsSilent(t *testing.T) {
	idx := strings.Index(ReviewStopHook, "[ \"$CODE\" -eq 0 ]; then")
	if idx < 0 {
		t.Fatal("ReviewStopHook missing 'CODE -eq 0' PASS/ADVISORY branch")
	}
	body := ReviewStopHook[idx:]
	end := strings.Index(body, "\nfi")
	if end < 0 {
		t.Fatal("ReviewStopHook CODE=0 branch not terminated by '\\nfi'")
	}
	body = body[:end]
	if strings.Contains(body, "echo") {
		t.Errorf("ReviewStopHook CODE=0 (PASS/ADVISORY) branch must be silent (no echo) — got branch body:\n%s", body)
	}
	if !strings.Contains(body, "exit 0") {
		t.Errorf("ReviewStopHook CODE=0 branch must 'exit 0' to allow Stop — got:\n%s", body)
	}
}

// TestTaskVerifyHookSkipVerifyAudited guards A4: the FORGE_SKIP_VERIFY=1 escape
// hatch is kept (it frees a permanently-trapped session), but its use must be
// audited to checklog — otherwise a stop-retry loop silently bypasses the gate
// forever with no trace.
func TestTaskVerifyHookSkipVerifyAudited(t *testing.T) {
	if !containsString(TaskVerifyHook, "FORGE_SKIP_VERIFY") {
		t.Error("TaskVerifyHook missing FORGE_SKIP_VERIFY escape hatch")
	}
	// The audit must write an escape-hatch checklog entry, not just echo PASS.
	if !containsString(TaskVerifyHook, `"check":"escape-hatch"`) {
		t.Error("TaskVerifyHook must record an escape-hatch checklog entry when FORGE_SKIP_VERIFY=1 (A4)")
	}
	if !containsString(TaskVerifyHook, "FORGE_SKIP_VERIFY=1") {
		t.Error("TaskVerifyHook escape-hatch detail must name FORGE_SKIP_VERIFY=1 (A4)")
	}
}

func TestTaskGuardHookSelfProtection(t *testing.T) {
	if !containsString(TaskGuardHook, ".forge/*") {
		t.Error("TaskGuardHook missing .forge/ self-protection")
	}
	if !containsString(TaskGuardHook, ".claude/settings") {
		t.Error("TaskGuardHook missing .claude/settings self-protection")
	}
	if !containsString(TaskGuardHook, "protocol.yml") {
		t.Error("TaskGuardHook should whitelist protocol.yml/pipeline.yml as user-editable config")
	}
	if !containsString(TaskGuardHook, "Forge-managed") {
		t.Error("TaskGuardHook missing self-protection error message")
	}
}

func TestPreToolUseHasBashGuard(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}

	var parsed struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher,omitempty"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	found := false
	for _, matcher := range parsed.Hooks["PreToolUse"] {
		if matcher.Matcher == "Bash" {
			for _, h := range matcher.Hooks {
				if h.Command == "forge hook bash-guard" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("PreToolUse missing Bash matcher with 'forge hook bash-guard'")
	}
}

func TestPreToolUseHasHazardGuard(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}

	var parsed struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher,omitempty"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	found := false
	for _, matcher := range parsed.Hooks["PreToolUse"] {
		if matcher.Matcher == "Bash" {
			for _, h := range matcher.Hooks {
				if h.Command == "forge hook hazard-guard" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("PreToolUse missing Bash matcher with 'forge hook hazard-guard'")
	}
}

func TestPostToolUseHasFileSentinel(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}

	var parsed struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher,omitempty"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	found := false
	for _, matcher := range parsed.Hooks["PostToolUse"] {
		if matcher.Matcher == "Bash" {
			for _, h := range matcher.Hooks {
				if h.Command == "forge hook file-sentinel" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("PostToolUse missing Bash matcher with 'forge hook file-sentinel'")
	}
}

// TestWriteHookTemplatesRemovesStaleHooks guards the cleanup path: hooks removed
// from the embedded set (e.g. sunk to skill text, or deleted no-ops) must not
// linger on disk. .forge/hooks/ is Forge-managed, so WriteHookTemplates prunes
// any .sh not in the current set — otherwise removed hooks accumulate forever.
func TestWriteHookTemplatesRemovesStaleHooks(t *testing.T) {
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed a stale hook from a prior version.
	stale := filepath.Join(hooksDir, "read-check.sh")
	if err := os.WriteFile(stale, []byte("# stale\n"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := WriteHookTemplates(dir); err != nil {
		t.Fatalf("WriteHookTemplates: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("WriteHookTemplates did not remove stale hook read-check.sh")
	}
	// A current hook must still be present.
	if _, err := os.Stat(filepath.Join(hooksDir, "auto-compile.sh")); err != nil {
		t.Error("WriteHookTemplates removed a current hook (auto-compile.sh)")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestSessionStartHasSkillScan guards that the global skill-scan hook is
// registered on the SessionStart event. It scans ~/.claude/skills for risks at
// session start (advisory), covering skills that entered outside the install
// gate (manual cp/clone, git pull, external junctions like lark-*). Without
// SessionStart registration the hook never fires.
func TestSessionStartHasSkillScan(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var parsed struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	groups, ok := parsed.Hooks["SessionStart"]
	if !ok {
		t.Fatal("SessionStart event not registered in settings")
	}
	found := false
	for _, g := range groups {
		for _, h := range g.Hooks {
			if h.Command == "forge hook skill-scan" {
				found = true
			}
		}
	}
	if !found {
		t.Error("SessionStart missing 'forge hook skill-scan' command")
	}
}

// TestSkillScanHookContainsKeyChecks guards the SessionStart advisory skill
// scanner content: it must scan the global skill dir via 'forge skills audit
// scan', be advisory (PASS, never exit 1 / block), and surface ✗ risk skills.
func TestSkillScanHookContainsKeyChecks(t *testing.T) {
	if !containsString(SkillScanHook, "forge skills audit scan") {
		t.Error("SkillScanHook must invoke 'forge skills audit scan'")
	}
	if !containsString(SkillScanHook, "$HOME/.claude/skills") {
		t.Error("SkillScanHook must scan $HOME/.claude/skills (the global skill dir)")
	}
	if containsString(SkillScanHook, "exit 1") {
		t.Error("SkillScanHook must be advisory (no 'exit 1' block)")
	}
	if !containsString(SkillScanHook, "PASS") {
		t.Error("SkillScanHook must PASS (advisory, non-blocking)")
	}
	if !containsString(SkillScanHook, "advisory") {
		t.Error("SkillScanHook must document its advisory nature")
	}
	// 诚实信号（fix 审查报告 fix#1）：用 --gate exit code 区分 scan 成功(0/4)/崩溃，
	// scan 失败时报"未完成"而非假 "all SAFE"。
	if !containsString(SkillScanHook, "--gate") {
		t.Error("SkillScanHook must use --gate (exit code encodes scan outcome)")
	}
	if !containsString(SkillScanHook, "CODE=$?") {
		t.Error("SkillScanHook must capture audit scan exit code to distinguish success vs crash")
	}
	if !containsString(SkillScanHook, "扫描未完成") {
		t.Error("SkillScanHook must report 'scan incomplete' on failure (honest signal, not fake 'all SAFE')")
	}
}

// TestWorkflowTestGuardHookContainsKeyChecks 守护 CI 防绕过的实时反馈 hook 内容。
// 关键差异：这个 hook 必须 exit 1 block（非 advisory）——用户明确要"保证捕获并反馈
// 到真实修改"，advisory 会被 agent 忽略，只有 block 才闭合"沙盒检测→异常反馈"的环。
func TestWorkflowTestGuardHookContainsKeyChecks(t *testing.T) {
	// 必须用 FORGE_FILE_PATH 判断改的文件（PostToolUse Write|Edit 的 tool_input）
	if !containsString(WorkflowTestGuardHook, "FORGE_FILE_PATH") {
		t.Error("WorkflowTestGuardHook missing FORGE_FILE_PATH check")
	}
	// 必须跑 internal/ci 守护测试（整个 hook 的核心动作）
	if !containsString(WorkflowTestGuardHook, "go test ./internal/ci/") {
		t.Error("WorkflowTestGuardHook must run 'go test ./internal/ci/' (the guard tests)")
	}
	// 必须有 [workflow-test-guard] 前缀
	if !containsString(WorkflowTestGuardHook, "[workflow-test-guard]") {
		t.Error("WorkflowTestGuardHook missing [workflow-test-guard] prefix")
	}
	// 必须 BSD-safe case-glob 判断 .github/workflows/*.yml（不用 grep -E 交替，参其他 hook）
	if !containsString(WorkflowTestGuardHook, ".github/workflows/*.yml") {
		t.Error("WorkflowTestGuardHook must case-glob .github/workflows/*.yml (BSD-safe, no grep -E)")
	}
	// 必须 exit 1 block on FAIL——这是"保证反馈"的关键，advisory 会被忽略
	if !containsString(WorkflowTestGuardHook, "exit 1") {
		t.Error("WorkflowTestGuardHook must exit 1 (block) on test failure — advisory won't guarantee feedback")
	}
	// 必须 fail-open：internal/ci 不存在时静默 PASS（老项目/未启用 CI 配置守护）
	if !containsString(WorkflowTestGuardHook, "internal/ci") {
		t.Error("WorkflowTestGuardHook must fail-open (PASS) when internal/ci absent")
	}
}

// TestPostToolUseHasWorkflowTestGuard 守护 hook 注册到 PostToolUse Write|Edit——
// 未注册则 Claude Code 永远不会在改 workflow yaml 时触发它，"实时反馈"落空。
func TestPostToolUseHasWorkflowTestGuard(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var parsed struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher,omitempty"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	found := false
	for _, matcher := range parsed.Hooks["PostToolUse"] {
		if matcher.Matcher == "Write|Edit" {
			for _, h := range matcher.Hooks {
				if h.Command == "forge hook workflow-test-guard" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("PostToolUse Write|Edit missing 'forge hook workflow-test-guard'")
	}
}
