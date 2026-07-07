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
	if !containsString(FileSentinelHook, "forge data-dir") {
		t.Error("FileSentinelHook must resolve DataDir via 'forge data-dir' (refactor-data-home commit D)")
	}
	if !containsString(FileSentinelHook, "quarantine_base") {
		t.Error("FileSentinelHook missing quarantine_base path logic (refactor-data-home: DataDir/quarantine)")
	}
	if !containsString(FileSentinelHook, "Recover:") {
		t.Error("FileSentinelHook missing recovery instructions")
	}
	// refactor-data-home commit D: gates/tasks/specs/reviews 迁用户级 DataDir（git 不跟踪），
	// file-sentinel 基于 git diff 检测不到 DataDir 路径——A6（守 .forge/gates/status.json 不被
	// Bash 篡改）机制失效，缺口由 TestHook_FileSentinel_GateStatusBeyondGitDiff 钉死（负向）。
	// CFG_EXT 现只守项目级 .forge/hooks/（ConfigDir 配置层，git 可见）。gate verdict 防护
	// 暂缺——commit E 或后续补 forge 自身完整性校验（DataDir 不在 git，git diff 维度的
	// file-sentinel 管不到，不能用空话假装改由 forge 校验）。
	if !containsString(FileSentinelHook, ".forge/hooks/") {
		t.Error("FileSentinelHook CFG_EXT must include .forge/hooks/ (config-layer protection after DataDir migration)")
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
	if !containsString(TaskVerifyHook, "$_DATA_DIR/checklog.jsonl") {
		t.Error("TaskVerifyHook advisory must append to $_DATA_DIR/checklog.jsonl (refactor-data-home: DataDir)")
	}
	if !containsString(TaskVerifyHook, "forge data-dir") {
		t.Error("TaskVerifyHook must resolve DataDir via 'forge data-dir' (refactor-data-home commit D)")
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

// writeSettingsLocal 写 dir/.claude/settings.local.json（原样内容，供 StripForgeHooks 测试）。
// content 是 JSON 文本——用反引号 raw 传入，保留 ASCII 双引号不被 Windows 输入腐蚀。
func writeSettingsLocal(t *testing.T, dir, content string) {
	t.Helper()
	p := filepath.Join(dir, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

func settingsLocalExists(t *testing.T, dir string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, ".claude", "settings.local.json"))
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat settings: %v", err)
	return false
}

func settingsPath(dir string) string {
	return filepath.Join(dir, ".claude", "settings.local.json")
}

// TestStripForgeHooks_NoFile：无 settings.local.json 时 no-op（changed=false，不报错）。
func TestStripForgeHooks_NoFile(t *testing.T) {
	dir := t.TempDir()
	changed, err := StripForgeHooks(dir, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed {
		t.Error(`无 settings.local.json 应 changed=false`)
	}
}

// TestStripForgeHooks_ForgeOnly_DeletesFile：GenerateSettings 写纯 forge hooks,
// strip 后 settings 仅剩空 hooks → 手动语义（keepEmpty=false）删除整个文件。
// 自动路径（keepEmpty=true）见 TestStripForgeHooks_ForgeOnly_KeepsEmpty。
func TestStripForgeHooks_ForgeOnly_DeletesFile(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatal(err)
	}
	changed, err := StripForgeHooks(dir, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Error(`纯 forge hooks 应 changed=true`)
	}
	if settingsLocalExists(t, dir) {
		t.Error(`纯 forge hooks 移除后文件应删除（无残留空 hooks 对象）`)
	}
}

// TestStripForgeHooks_ForgeOnly_KeepsEmpty：钉死自动路径行为——GenerateSettings 写
// 纯 forge hooks,keepEmpty=true（init-suggest SessionStart / autoSync / init·sync 自动 dedupe）
// strip 后写空对象 {} 保留文件壳,绝不删。用户痛点:settings.local.json 是 gitignored 个人配置,
// 用户主动放置/正要编辑,forge 自动 dedupe 静默删整个文件 → 用户配置丢失。空 {} 对 Claude Code
// 无害（无 hooks/permissions）。手动 forge plugin dedupe 不传 --keep-empty,走 DeletesFile 删空。
func TestStripForgeHooks_ForgeOnly_KeepsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateSettings(dir); err != nil {
		t.Fatal(err)
	}
	changed, err := StripForgeHooks(dir, true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Error(`纯 forge hooks 应 changed=true`)
	}
	data, err := os.ReadFile(settingsPath(dir))
	if err != nil {
		t.Fatalf(`keepEmpty=true 应保留文件壳写 {},不删: %v`, err)
	}
	if got := string(data); got != "{}\n" {
		t.Errorf(`keepEmpty=true 应写 {} 后跟换行, got %q`, got)
	}
}

// TestStripForgeHooks_KeepEmpty_NoEffect_WithUserFields：钉死 keepEmpty 仅在纯 forge 文件
// （len(settings)==0,整文件只剩 forge 来源）时生效——有用户字段（用户 hooks 或顶层 permissions）
// 时落 MarshalIndent 分支保留用户内容,keepEmpty 不影响（绝不写 {}）。防未来重构误扩散 keepEmpty
// 语义到混合场景（用户配置被空对象覆盖）。
func TestStripForgeHooks_KeepEmpty_NoEffect_WithUserFields(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		wantKeep string
	}{
		{
			name:     "mixed_user_and_forge_hooks",
			content:  `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"forge hook bash-guard"},{"type":"command","command":"npx prettier"}]}]}}`,
			wantKeep: "npx prettier",
		},
		{
			name:     "forge_hooks_plus_permissions",
			content:  `{"permissions":{"allow":["Bash(go test:*)"]},"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"forge hook bash-guard"}]}]}}`,
			wantKeep: "permissions",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeSettingsLocal(t, dir, tc.content)
			changed, err := StripForgeHooks(dir, true)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if !changed {
				t.Error(`有 forge hook 应 changed=true`)
			}
			data, err := os.ReadFile(settingsPath(dir))
			if err != nil {
				t.Fatalf(`read: %v`, err)
			}
			if string(data) == "{}\n" {
				t.Error(`有用户字段时不应写纯 {}（keepEmpty 仅纯 forge 文件生效）`)
			}
			body := string(data)
			if !containsString(body, tc.wantKeep) {
				t.Errorf(`用户字段 %q 被误删`, tc.wantKeep)
			}
			if containsString(body, "forge hook") {
				t.Error(`forge hook 未移除`)
			}
		})
	}
}

// TestStripForgeHooks_PreservesUserHooks：同 matcher 内 forge hook + 用户 hook，
// 删 forge 保留用户 hook（文件保留）。
func TestStripForgeHooks_PreservesUserHooks(t *testing.T) {
	dir := t.TempDir()
	content := `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {"type": "command", "command": "forge hook bash-guard"},
          {"type": "command", "command": "npx prettier"}
        ]
      }
    ]
  }
}`
	writeSettingsLocal(t, dir, content)
	changed, err := StripForgeHooks(dir, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Error(`有 forge hook 应 changed=true`)
	}
	data, err := os.ReadFile(settingsPath(dir))
	if err != nil {
		t.Fatalf(`read: %v`, err)
	}
	body := string(data)
	if containsString(body, "forge hook") {
		t.Error(`forge hook 未被移除`)
	}
	if !containsString(body, "npx prettier") {
		t.Error(`用户自定义 hook 被误删`)
	}
}

// TestStripForgeHooks_NoForgeHooks_NoOp：纯用户 hooks（无 forge 来源）时 no-op。
func TestStripForgeHooks_NoForgeHooks_NoOp(t *testing.T) {
	dir := t.TempDir()
	content := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"npx prettier"}]}]}}`
	writeSettingsLocal(t, dir, content)
	changed, err := StripForgeHooks(dir, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed {
		t.Error(`无 forge hook 应 changed=false（no-op）`)
	}
	data, _ := os.ReadFile(settingsPath(dir))
	if !containsString(string(data), "npx prettier") {
		t.Error(`无 forge hook 时文件不应被改动`)
	}
}

// TestStripForgeHooks_PreservesTopLevelFields：forge hooks + 其他顶层字段（permissions）
// —— 删 forge hooks 后保留 permissions，空 hooks 键被删除。
func TestStripForgeHooks_PreservesTopLevelFields(t *testing.T) {
	dir := t.TempDir()
	content := `{
  "permissions": {"allow": ["Bash(go test:*)"]},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "forge hook bash-guard"}]}
    ]
  }
}`
	writeSettingsLocal(t, dir, content)
	changed, err := StripForgeHooks(dir, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Error(`应 changed=true`)
	}
	var cfg map[string]json.RawMessage
	data, err := os.ReadFile(settingsPath(dir))
	if err != nil {
		t.Fatalf(`read: %v`, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf(`parse: %v`, err)
	}
	if _, ok := cfg["permissions"]; !ok {
		t.Error(`顶层 permissions 字段被误删`)
	}
	if _, ok := cfg["hooks"]; ok {
		t.Error(`hooks（纯 forge 移除后空）应被删除`)
	}
}

// TestStripForgeHooks_PreservesUserHooksAndTopLevel：forge hook + 用户 hook（不同事件）
// + permissions —— 删 forge，保留用户 hook + permissions + 文件本身。
func TestStripForgeHooks_PreservesUserHooksAndTopLevel(t *testing.T) {
	dir := t.TempDir()
	content := `{
  "permissions": {"allow": ["Bash(go test:*)"]},
  "hooks": {
    "PostToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "forge hook file-sentinel"}]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "make lint"}]}
    ]
  }
}`
	writeSettingsLocal(t, dir, content)
	changed, err := StripForgeHooks(dir, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Error(`应 changed=true`)
	}
	data, _ := os.ReadFile(settingsPath(dir))
	body := string(data)
	if containsString(body, "forge hook") {
		t.Error(`forge hook 未移除`)
	}
	if !containsString(body, "make lint") {
		t.Error(`Stop 事件用户 hook 被误删`)
	}
	if !containsString(body, "permissions") {
		t.Error(`permissions 顶层字段丢失`)
	}
}

// TestStripForgeHooks_RemovesGateCommand：N4 守卫——ForgeHookSpec 含 forge gate 命令
// （非仅 forge hook），StripForgeHooks 必须同样移除 gate。此前断言只查 "forge hook" 子串，
// 若 StripForgeHooks 只删 hook 漏删 gate，现有测试抓不到（gate 命令残留 → plugin 已装时
// project-level gate 仍双跑）。
func TestStripForgeHooks_RemovesGateCommand(t *testing.T) {
	dir := t.TempDir()
	content := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"forge gate --current --silent"},{"type":"command","command":"make lint"}]}]}}`
	writeSettingsLocal(t, dir, content)
	changed, err := StripForgeHooks(dir, false)
	if err != nil {
		t.Fatalf(`err: %v`, err)
	}
	if !changed {
		t.Error(`应 changed=true（有 forge gate）`)
	}
	data, _ := os.ReadFile(settingsPath(dir))
	body := string(data)
	if containsString(body, "forge gate") {
		t.Error(`forge gate 未被移除（断言不能只查 forge hook 子串,N4）`)
	}
	if containsString(body, "forge hook") {
		t.Error(`forge hook 残留`)
	}
	if !containsString(body, "make lint") {
		t.Error(`用户 hook（make lint）被误删`)
	}
}

// TestGenerateSettings_PreservesUserTopLevelFields:钉死 GenerateSettings 合并式——
// 用户现有非 hooks 顶层字段(env/model/enabledPlugins)必须保留,只 hooks 段更新为
// ForgeHookSpec。1.2.0 回归:覆盖式写丢用户配置,plugin-dedupe 删 hooks 后文件被删、
// env/model 丢失(真实事故:Agentworld 项目 forge init 后 ollama/Qwen 配置全丢)。
// 注:hooks 段由 forge 管理(GenerateSettings 覆盖为 ForgeHookSpec),用户自定义 hook
// 放 hooks 段会被覆盖——当前 hotfix 聚焦非 hooks 顶层字段保留,hooks 段合并是后续优化。
func TestGenerateSettings_PreservesUserTopLevelFields(t *testing.T) {
	dir := t.TempDir()
	existing := `{"env":{"API_KEY":"secret"},"model":"my-model","hooks":{"Stop":[{"hooks":[{"type":"command","command":"make lint"}]}]}}`
	writeSettingsLocal(t, dir, existing)

	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings: %v", err)
	}

	data, err := os.ReadFile(settingsPath(dir))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	body := string(data)

	if !containsString(body, "secret") {
		t.Error(`用户 env.API_KEY=secret 被 GenerateSettings 删除(1.2.0 回归)`)
	}
	if !containsString(body, "my-model") {
		t.Error(`用户 model=my-model 被删除`)
	}
	if !containsString(body, "forge hook") {
		t.Error(`hooks 段未更新为 ForgeHookSpec`)
	}
}

// TestInitFlow_PluginInstalled_PreservesUserConfig:端到端钉死 1.2.0 事故场景——
// plugin 已装时 init 流程(GenerateSettings 写 hooks → StripForgeHooks 删 forge hooks)
// 必须保留用户 env/model,文件不删。1.2.0 GenerateSettings 覆盖式 → 用户配置丢 + 文件删
// (Agentworld 项目 forge init 后 ollama/Qwen 配置全丢)。1.2.1 修。
func TestInitFlow_PluginInstalled_PreservesUserConfig(t *testing.T) {
	dir := t.TempDir()
	writeSettingsLocal(t, dir, `{"env":{"API_KEY":"secret"},"model":"my-model"}`)

	// init 流程:GenerateSettings(合并写 hooks)→ StripForgeHooks(dedupe 删 forge hooks)。
	if err := GenerateSettings(dir); err != nil {
		t.Fatalf("GenerateSettings: %v", err)
	}
	if _, err := StripForgeHooks(dir, true); err != nil {
		t.Fatalf("StripForgeHooks: %v", err)
	}

	data, err := os.ReadFile(settingsPath(dir))
	if err != nil {
		t.Fatalf(`settings.local.json 被删(1.2.0 回归): %v`, err)
	}
	body := string(data)
	if !containsString(body, "secret") {
		t.Error(`用户 env.API_KEY 丢失`)
	}
	if !containsString(body, "my-model") {
		t.Error(`用户 model 丢失`)
	}
	if containsString(body, "forge hook") {
		t.Error(`dedupe 后应无 forge hooks`)
	}
}

// TestIsForgeHookCommand：钉死 forge 来源命令的识别（forge hook X / forge gate X /
// 裸 forge hook / forge gate）。非 forge 命令（含 forge plugin status 等其他子命令）
// 不被误判——避免 StripForgeHooks 误删用户的非 hook forge 调用。
func TestIsForgeHookCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"forge hook bash-guard", true},
		{"forge hook", true},
		{"forge gate task-verify", true},
		{"forge gate", true},
		{"forge hooks", false}, // forge hooks（复数）非 hook 命令
		{"forge plugin status", false},
		{"forge plugin dedupe", false},
		{"npx prettier", false},
		{"./scripts/lint.sh", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isForgeHookCommand(c.cmd); got != c.want {
			t.Errorf(`isForgeHookCommand(%q) = %v, want %v`, c.cmd, got, c.want)
		}
	}
}
