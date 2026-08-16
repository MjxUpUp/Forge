package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/userassets"
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

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	hooks, ok := parsed["hooks"].(map[string]any)
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

// TestFreezeGuardRegisteredFirst pins the freeze-priority contract: freeze-guard
// must be registered and must be the FIRST PreToolUse Write|Edit entry so an
// active freeze reports its block reason before task-guard warnings.
//
// TestFreezeGuardRegisteredFirst 钉住 freeze 优先判定契约：freeze-guard 必须注册
// 且排在 PreToolUse Write|Edit 首位——freeze 激活时先报 freeze 阻断原因而非
// task-guard 告警。
func TestFreezeGuardRegisteredFirst(t *testing.T) {
	if _, ok := embeddedHooks["freeze-guard"]; !ok {
		t.Fatal("freeze-guard missing from embeddedHooks")
	}
	spec := ForgeHookSpec()
	found := false
	for event, matchers := range spec {
		if event != "PreToolUse" {
			continue
		}
		for _, m := range matchers {
			if m.Matcher != "Write|Edit" {
				continue
			}
			found = true
			if len(m.Hooks) == 0 || !strings.Contains(m.Hooks[0].Command, "freeze-guard") {
				t.Errorf("PreToolUse Write|Edit first hook = %+v, want freeze-guard first", m.Hooks)
			}
		}
	}
	if !found {
		t.Fatal("no PreToolUse Write|Edit matcher in ForgeHookSpec")
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
	for _, name := range []string{"auto-compile", "assertion-check", "task-verify", "task-guard", "read-before-edit", "bash-guard", "file-sentinel", "skill-scan", "workflow-test-guard"} {
		expected := "forge hook " + name
		if !strings.Contains(content, expected) {
			t.Errorf("settings missing %q command", expected)
		}
	}
}

func TestEmbeddedContent(t *testing.T) {
	// Known hooks return content and true
	for _, name := range []string{"auto-compile", "assertion-check", "task-verify", "bash-guard", "file-sentinel", "task-guard", "read-before-edit", "skill-scan", "task-resume", "compact-resume", "resume-reinject", "workflow-test-guard"} {
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
	expected := []string{"auto-compile.sh", "assertion-check.sh", "task-verify.sh", "task-guard.sh", "read-before-edit.sh", "bash-guard.sh", "file-sentinel.sh", "skill-scan.sh", "workflow-test-guard.sh"}
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
		// v0.25 advisory: auto-compile no longer runs the compiler, only reminds the agent to self-check;
		// assertion-check only reminds (does not block) when weakening is detected.
		//
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
		if !strings.Contains(content, tc.needle) {
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
	if !strings.Contains(TaskVerifyHook, "Code changes on") {
		t.Error("TaskVerifyHook missing 'Code changes on' master branch detection")
	}
	if !strings.Contains(TaskVerifyHook, "without active task") {
		t.Error("TaskVerifyHook missing 'without active task' warning")
	}
	if !strings.Contains(TaskVerifyHook, "forge task start") {
		t.Error("TaskVerifyHook missing 'forge task start' hint in warning")
	}
	// BSD-safe: the master-check source-extension filter must use is_code_file
	// case-glob, NOT grep -E '\.(go|rs|...)$' — BSD/macOS aborts on ERE
	// alternation with "Unmatched ( or \(", silently disabling the check.
	if !strings.Contains(TaskVerifyHook, "is_code_file") {
		t.Error("TaskVerifyHook master-check must use is_code_file (BSD-safe case-glob), not grep -E alternation")
	}
}

// TestTaskVerifyHookCounterIsProjectScoped guards against the global-counter
// regression: a bare /tmp/forge-verify-fail-count (no per-project tag) leaked
// failure counts across concurrent projects and parallel e2e tests, making
// TestMasterBranchReminder flaky (an unrelated project's first failure hit the
// 3-strike threshold and force-allowed, masking its real warning).
func TestTaskGuardHookContainsKeyChecks(t *testing.T) {
	if !strings.Contains(TaskGuardHook, "FORGE_TASK_REF") {
		t.Error("TaskGuardHook missing FORGE_TASK_REF check")
	}
	if !strings.Contains(TaskGuardHook, "FORGE_TASK_GATE") {
		t.Error("TaskGuardHook missing FORGE_TASK_GATE check")
	}
	if !strings.Contains(TaskGuardHook, "WARN [task-guard]") {
		t.Error("TaskGuardHook missing WARN for no-task scenario")
	}
	if !strings.Contains(TaskGuardHook, "auto-create") {
		t.Error("TaskGuardHook contains auto-create task path")
	}
	if !strings.Contains(TaskGuardHook, "WARN") {
		t.Error("TaskGuardHook missing WARN for pre-design state")
	}
}

func TestTaskGuardHookPassesNonCodeFiles(t *testing.T) {
	if !strings.Contains(TaskGuardHook, ".(go|rs|ts|tsx|js|jsx|py|java|rb|zig|nim)") {
		t.Error("TaskGuardHook missing code file extension filter")
	}
}

func TestBashGuardHookContainsKeyChecks(t *testing.T) {
	if !strings.Contains(BashGuardHook, "FORGE_COMMAND") {
		t.Error("BashGuardHook missing FORGE_COMMAND check")
	}
	if !strings.Contains(BashGuardHook, "writeFile") {
		t.Error("BashGuardHook missing writeFile pattern detection")
	}
	if !strings.Contains(BashGuardHook, "WARN [bash-guard]") {
		t.Error("BashGuardHook missing WARN for no-task scenario")
	}
	if !strings.Contains(BashGuardHook, "bash-guard") {
		t.Error("BashGuardHook missing [bash-guard] prefix")
	}
	// P0 fix: bash-guard must record whether THIS command is a write command
	// (forge-write-<session> flag file) so file-sentinel's secondary gate can
	// distinguish read-only commands (ls/cat/git diff) from write commands and
	// never quarantine under a read-only command.
	if !strings.Contains(BashGuardHook, "forge-write") {
		t.Error("BashGuardHook missing write-flag file (forge-write-<session>) for file-sentinel secondary gate")
	}
}

func TestFileSentinelHookContainsKeyChecks(t *testing.T) {
	if !strings.Contains(FileSentinelHook, "SNAPSHOT_FILE") {
		t.Error("FileSentinelHook missing SNAPSHOT_FILE reference")
	}
	if !strings.Contains(FileSentinelHook, "file-sentinel") {
		t.Error("FileSentinelHook missing [file-sentinel] prefix")
	}
	if !strings.Contains(FileSentinelHook, "git checkout") {
		t.Error("FileSentinelHook missing git checkout restore logic")
	}
	if !strings.Contains(FileSentinelHook, "quarantine_files") {
		t.Error("FileSentinelHook missing quarantine_files function")
	}
	if !strings.Contains(FileSentinelHook, "forge data-dir") {
		t.Error("FileSentinelHook must resolve DataDir via 'forge data-dir' (refactor-data-home commit D)")
	}
	if !strings.Contains(FileSentinelHook, "quarantine_base") {
		t.Error("FileSentinelHook missing quarantine_base path logic (refactor-data-home: DataDir/quarantine)")
	}
	if !strings.Contains(FileSentinelHook, "Recover:") {
		t.Error("FileSentinelHook missing recovery instructions")
	}
	// refactor-data-home commit D: gates/tasks/specs/reviews migrated to user-level DataDir (git untracked),
	// file-sentinel relies on git diff and cannot detect DataDir paths — A6 (guarding .forge/gates/status.json from
	// Bash tampering) mechanism is void, the gap is pinned by TestHook_FileSentinel_GateStatusBeyondGitDiff (negative).
	// CFG_EXT now only guards project-level .forge/hooks/ (ConfigDir config layer, git-visible). gate verdict protection
	// is currently missing — commit E or follow-up adds forge self-integrity check (DataDir is outside git, git diff based
	// file-sentinel cannot reach it, do not pretend with empty words that forge now validates it).
	//
	// refactor-data-home commit D: gates/tasks/specs/reviews 迁用户级 DataDir（git 不跟踪），
	// file-sentinel 基于 git diff 检测不到 DataDir 路径——A6（守 .forge/gates/status.json 不被
	// Bash 篡改）机制失效，缺口由 TestHook_FileSentinel_GateStatusBeyondGitDiff 钉死（负向）。
	// CFG_EXT 现只守项目级 .forge/hooks/（ConfigDir 配置层，git 可见）。gate verdict 防护
	// 暂缺——commit E 或后续补 forge 自身完整性校验（DataDir 不在 git，git diff 维度的
	// file-sentinel 管不到，不能用空话假装改由 forge 校验）。
	if !strings.Contains(FileSentinelHook, ".forge/hooks/") {
		t.Error("FileSentinelHook CFG_EXT must include .forge/hooks/ (config-layer protection after DataDir migration)")
	}
	// P0 fix: file-sentinel must FAIL-OPEN when the PreToolUse snapshot is
	// empty/unreliable (BEFORE_ALL empty while working tree has changes) — it
	// must never treat the whole working tree as new violations and quarantine
	// + git-checkout away the user's existing uncommitted source. And a
	// read-only Bash command must never trigger quarantine (secondary gate).
	if !strings.Contains(FileSentinelHook, "IS_WRITE_CMD") {
		t.Error("FileSentinelHook missing IS_WRITE_CMD secondary gate (read-only command must not quarantine)")
	}
	if !strings.Contains(FileSentinelHook, "WRITE_FLAG_FILE") {
		t.Error("FileSentinelHook missing WRITE_FLAG_FILE read of bash-guard's write flag")
	}
	if strings.Contains(FileSentinelHook, `rm -f "$f"`) {
		t.Error("FileSentinelHook should NOT use rm -f on user files — use quarantine instead")
	}
}

// TestBashGuardHookWritePatterns guards E3: has_write_pattern must detect in-place
// editors and apply-style writers (perl -i, git apply, patch, printf >) that
// mutate files without a shell redirect.
func TestBashGuardHookWritePatterns(t *testing.T) {
	for _, pat := range []string{"perl", "git apply", "patch", "printf"} {
		if !strings.Contains(BashGuardHook, pat) {
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
	if strings.Contains(TaskVerifyHook, "exit 1") {
		t.Error("TaskVerifyHook must not block (advisory) — found 'exit 1'")
	}
	if strings.Contains(TaskVerifyHook, "VERIFY_COUNTER") {
		t.Error("TaskVerifyHook must not carry a failure counter (advisory, never blocks)")
	}
	if !strings.Contains(TaskVerifyHook, `"check":"task-verify"`) {
		t.Error("TaskVerifyHook must record an advisory checklog entry for detected issues")
	}
	if !strings.Contains(TaskVerifyHook, "$_DATA_DIR/checklog.jsonl") {
		t.Error("TaskVerifyHook advisory must append to $_DATA_DIR/checklog.jsonl (refactor-data-home: DataDir)")
	}
	if !strings.Contains(TaskVerifyHook, "forge data-dir") {
		t.Error("TaskVerifyHook must resolve DataDir via 'forge data-dir' (refactor-data-home commit D)")
	}
}

// TestReviewStopHookPassIsSilent guards the Stop infinite-loop fix (2026-06-27): review-stop
// must silently exit 0 when gate exits 0 (PASS/ADVISORY), and the branch body must contain no echo.
// hook.go runHook treats the script stdout (extractDetail strips the `PASS ` prefix) as AdditionalContext
// and injects it into Claude Code — on PASS, echoing `PASS 无未提交变更...` makes the harness treat it as feedback,
// reactivating the agent for another round, forming a Stop→feedback→response→Stop loop (the symptom of
// `无未提交变更，无需审查` repeatedly flooding the screen). Only the FAIL branch (exit 2) may echo guidance.
//
// TestReviewStopHookPassIsSilent 守护 Stop 死循环修复（2026-06-27）：review-stop
// 在 gate exit 0（PASS/ADVISORY）时必须静默 exit 0，分支体内不得有任何 echo。
// hook.go runHook 把脚本 stdout（extractDetail 去"PASS "前缀）当 AdditionalContext
// 注入 Claude Code——PASS 时若 echo「PASS 无未提交变更...」，harness 就把它当 feedback
// 激活 agent 再响应一轮，造成 Stop→feedback→响应→Stop 死循环（「无未提交变更，无需审查」
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

func TestTaskGuardHookSelfProtection(t *testing.T) {
	if !strings.Contains(TaskGuardHook, ".forge/*") {
		t.Error("TaskGuardHook missing .forge/ self-protection")
	}
	if !strings.Contains(TaskGuardHook, ".claude/settings") {
		t.Error("TaskGuardHook missing .claude/settings self-protection")
	}
	if !strings.Contains(TaskGuardHook, "protocol.yml") {
		t.Error("TaskGuardHook should whitelist protocol.yml/pipeline.yml as user-editable config")
	}
	if !strings.Contains(TaskGuardHook, "Forge-managed") {
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

// TestSessionStartHasTaskResume guards that the project-scoped task-resume hook
// is registered on SessionStart. It auto-injects the active task's continuity
// context (forge task resume --hook) at session start, so a fresh session lands
// on an in-progress task already knowing its goal/plan/decisions/blockers and
// is attached to it. Without SessionStart registration the hook never fires.
func TestSessionStartHasTaskResume(t *testing.T) {
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
			if h.Command == "forge hook task-resume" {
				found = true
			}
		}
	}
	if !found {
		t.Error("SessionStart missing 'forge hook task-resume' command")
	}
}

// TestPostCompactHasCompactResume guards that the claude-code-only PostCompact
// lifecycle hook is registered for compact-resume. gap#2 root-cause layer:
// PostCompact sets ResumeStale=true so the next UserPromptSubmit (resume-reinject)
// re-injects the full handoff after mid-session compaction. Without PostCompact
// registration the flag is never set and re-injection never fires — gap#2
// silently no-ops while every other test stays green.
func TestPostCompactHasCompactResume(t *testing.T) {
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
	groups, ok := parsed.Hooks[`PostCompact`]
	if !ok {
		t.Fatal(`PostCompact event not registered in settings`)
	}
	found := false
	for _, g := range groups {
		for _, h := range g.Hooks {
			if h.Command == `forge hook compact-resume` {
				found = true
			}
		}
	}
	if !found {
		t.Error(`PostCompact missing 'forge hook compact-resume' command`)
	}
}

// TestUserPromptSubmitHasResumeReinject guards that the claude-code-only
// UserPromptSubmit lifecycle hook is registered for resume-reinject. Companion
// to PostCompact: UserPromptSubmit checks ResumeStale and, if set, re-injects
// the full handoff then clears the flag (one-shot). Without UserPromptSubmit
// registration the PostCompact-set flag is never consumed — compaction leaves
// ResumeStale=true forever but nothing re-injects, same silent no-op.
func TestUserPromptSubmitHasResumeReinject(t *testing.T) {
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
	groups, ok := parsed.Hooks[`UserPromptSubmit`]
	if !ok {
		t.Fatal(`UserPromptSubmit event not registered in settings`)
	}
	found := false
	for _, g := range groups {
		for _, h := range g.Hooks {
			if h.Command == `forge hook resume-reinject` {
				found = true
			}
		}
	}
	if !found {
		t.Error(`UserPromptSubmit missing 'forge hook resume-reinject' command`)
	}
}

// TestSkillScanHookContainsKeyChecks guards the SessionStart advisory skill
// scanner content: it must scan the global skill dir via 'forge skills audit
// scan', be advisory (PASS, never exit 1 / block), and surface ✗ risk skills.
func TestSkillScanHookContainsKeyChecks(t *testing.T) {
	if !strings.Contains(SkillScanHook, "forge skills audit scan") {
		t.Error("SkillScanHook must invoke 'forge skills audit scan'")
	}
	if !strings.Contains(SkillScanHook, "$HOME/.claude/skills") {
		t.Error("SkillScanHook must scan $HOME/.claude/skills (the global skill dir)")
	}
	if strings.Contains(SkillScanHook, "exit 1") {
		t.Error("SkillScanHook must be advisory (no 'exit 1' block)")
	}
	if !strings.Contains(SkillScanHook, "PASS") {
		t.Error("SkillScanHook must PASS (advisory, non-blocking)")
	}
	if !strings.Contains(SkillScanHook, "advisory") {
		t.Error("SkillScanHook must document its advisory nature")
	}
	// Honest signal (fix review report fix#1): use --gate exit code to distinguish scan success (0/4) vs crash,
	// and on scan failure report `未完成` instead of a fake `all SAFE`.
	//
	// 诚实信号（fix 审查报告 fix#1）：用 --gate exit code 区分 scan 成功(0/4)/崩溃，
	// scan 失败时报「未完成」而非假"all SAFE"。
	if !strings.Contains(SkillScanHook, "--gate") {
		t.Error("SkillScanHook must use --gate (exit code encodes scan outcome)")
	}
	if !strings.Contains(SkillScanHook, "CODE=$?") {
		t.Error("SkillScanHook must capture audit scan exit code to distinguish success vs crash")
	}
	if !strings.Contains(SkillScanHook, "扫描未完成") {
		t.Error("SkillScanHook must report 'scan incomplete' on failure (honest signal, not fake 'all SAFE')")
	}
}

// TestWorkflowTestGuardHookContainsKeyChecks guards the real-time feedback hook content that prevents CI bypass.
// Key difference: this hook must exit 1 block (not advisory) — the user explicitly wants to `guarantee capture and feedback
// to real edits`, advisory will be ignored by the agent, only block closes the `sandbox detection → anomaly feedback` loop.
//
// TestWorkflowTestGuardHookContainsKeyChecks 守护 CI 防绕过的实时反馈 hook 内容。
// 关键差异：这个 hook 必须 exit 1 block（非 advisory）——用户明确要「保证捕获并反馈
// 到真实修改」，advisory 会被 agent 忽略，只有 block 才闭合「沙盒检测→异常反馈」的环。
func TestWorkflowTestGuardHookContainsKeyChecks(t *testing.T) {
	// Must use FORGE_FILE_PATH to determine the changed file (PostToolUse Write|Edit tool_input)
	//
	// 必须用 FORGE_FILE_PATH 判断改的文件（PostToolUse Write|Edit 的 tool_input）
	if !strings.Contains(WorkflowTestGuardHook, "FORGE_FILE_PATH") {
		t.Error("WorkflowTestGuardHook missing FORGE_FILE_PATH check")
	}
	// Must run the internal/ci guard tests (the core action of the entire hook)
	//
	// 必须跑 internal/ci 守护测试（整个 hook 的核心动作）
	if !strings.Contains(WorkflowTestGuardHook, "go test ./internal/ci/") {
		t.Error("WorkflowTestGuardHook must run 'go test ./internal/ci/' (the guard tests)")
	}
	// Must carry the [workflow-test-guard] prefix
	//
	// 必须有 [workflow-test-guard] 前缀
	if !strings.Contains(WorkflowTestGuardHook, "[workflow-test-guard]") {
		t.Error("WorkflowTestGuardHook missing [workflow-test-guard] prefix")
	}
	// Must use BSD-safe case-glob for .github/workflows/*.yml (no grep -E alternation, see other hooks)
	//
	// 必须 BSD-safe case-glob 判断 .github/workflows/*.yml（不用 grep -E 交替，参其他 hook）
	if !strings.Contains(WorkflowTestGuardHook, ".github/workflows/*.yml") {
		t.Error("WorkflowTestGuardHook must case-glob .github/workflows/*.yml (BSD-safe, no grep -E)")
	}
	// Must exit 1 (block) on FAIL — this is the key to `guaranteeing feedback`, advisory will be ignored
	//
	// 必须 exit 1 block on FAIL——这是「保证反馈」的关键，advisory 会被忽略
	if !strings.Contains(WorkflowTestGuardHook, "exit 1") {
		t.Error("WorkflowTestGuardHook must exit 1 (block) on test failure — advisory won't guarantee feedback")
	}
	// Must fail-open: silently PASS when internal/ci is absent (legacy projects / CI config guard not enabled)
	//
	// 必须 fail-open：internal/ci 不存在时静默 PASS（老项目/未启用 CI 配置守护）
	if !strings.Contains(WorkflowTestGuardHook, "internal/ci") {
		t.Error("WorkflowTestGuardHook must fail-open (PASS) when internal/ci absent")
	}
}

// TestPostToolUseHasWorkflowTestGuard guards that the hook is registered to PostToolUse Write|Edit —
// without registration Claude Code will never trigger it on workflow yaml edits, and `real-time feedback` falls through.
//
// TestPostToolUseHasWorkflowTestGuard 守护 hook 注册到 PostToolUse Write|Edit——
// 未注册则 Claude Code 永远不会在改 workflow yaml 时触发它，「实时反馈」落空。
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

// writeSettingsLocal writes dir/.claude/settings.local.json (content as-is, for StripForgeHooks testing).
// content is JSON text — passed via backtick raw string to preserve ASCII double-quotes from Windows input corruption.
//
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

// TestStripForgeHooksUserLevel: pins user-level dedupe — StripForgeHooksUserLevel locates
// ClaudeHome()/settings.local.json (CLAUDE_CONFIG_DIR first, fallback ~/.claude), removing forge
// hooks. plugin.json already registers all ForgeHookSpec at user-level → forge hooks here must be duplicates.
// Always keepEmpty=true: when the entire file is forge-sourced write {} to keep the shell, never delete user global config.
//
// TestStripForgeHooksUserLevel：钉死 user-level 去重——StripForgeHooksUserLevel 定位
// ClaudeHome()/settings.local.json（CLAUDE_CONFIG_DIR 优先,fallback ~/.claude），移除 forge
// hooks。plugin.json 已在 user-level 注册全部 ForgeHookSpec → 此处 forge hook 必重复。
// 始终 keepEmpty=true：整文件只剩 forge 来源时写 {} 保留壳，绝不删用户全局配置。
func TestStripForgeHooksUserLevel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)

	// Pure forge hooks → after strip writes {} to keep the shell (no delete).
	//
	// 纯 forge hooks → strip 后写 {} 保留壳（不删）。
	p := filepath.Join(home, "settings.local.json")
	forgeOnly := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"forge hook bash-guard"}]}]}}`
	if err := os.WriteFile(p, []byte(forgeOnly), 0644); err != nil {
		t.Fatalf("write user-level settings: %v", err)
	}
	changed, err := StripForgeHooksUserLevel()
	if err != nil {
		t.Fatalf("StripForgeHooksUserLevel: %v", err)
	}
	if !changed {
		t.Error(`有 forge hook 应 changed=true`)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf(`应保留文件壳写 {},不删: %v`, err)
	}
	if got := strings.TrimSpace(string(data)); got != "{}" {
		t.Errorf(`应写 {} 保留壳, got %q`, got)
	}
}

// TestStripForgeHooksUserLevel_PreservesUserHooks: when user-level has forge + user hooks,
// delete forge and keep user hooks + file (never write {} to overwrite user global config).
//
// TestStripForgeHooksUserLevel_PreservesUserHooks：user-level 有 forge + 用户 hook 时，
// 删 forge 保留用户 hook + 文件（绝不写 {} 覆盖用户全局配置）。
func TestStripForgeHooksUserLevel_PreservesUserHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	p := filepath.Join(home, "settings.local.json")
	mixed := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"forge hook bash-guard"},{"type":"command","command":"npx prettier"}]}]}}`
	if err := os.WriteFile(p, []byte(mixed), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	changed, err := StripForgeHooksUserLevel()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Error(`有 forge hook 应 changed=true`)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf(`read: %v`, err)
	}
	body := string(data)
	if !strings.Contains(body, "npx prettier") {
		t.Error(`用户 hook 应保留`)
	}
	if strings.Contains(body, "forge hook") {
		t.Error(`forge hook 应移除`)
	}
}

// TestStripForgeHooksUserLevel_NoFile: no-op when user-level settings.local.json is absent.
//
// TestStripForgeHooksUserLevel_NoFile：无 user-level settings.local.json 时 no-op。
func TestStripForgeHooksUserLevel_NoFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	changed, err := StripForgeHooksUserLevel()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed {
		t.Error(`无文件应 changed=false`)
	}
}

// TestStripForgeHooks_NoFile: no-op when settings.local.json is absent (changed=false, no error).
//
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

// TestStripForgeHooks_ForgeOnly_DeletesFile: GenerateSettings writes pure forge hooks,
// after strip the settings has only an empty hooks → manual semantics (keepEmpty=false) deletes the whole file.
// The automatic path (keepEmpty=true) is in TestStripForgeHooks_ForgeOnly_KeepsEmpty.
//
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

// TestStripForgeHooks_ForgeOnly_KeepsEmpty: pins the automatic path behavior — GenerateSettings writes
// pure forge hooks, keepEmpty=true (init-suggest SessionStart / autoSync / init·sync automatic dedupe)
// after strip writes empty object {} to keep the file shell, never delete. User pain point: settings.local.json is gitignored personal config,
// user actively placed / about to edit, forge automatic dedupe silently deletes the whole file → user config lost. Empty {} is harmless to Claude Code
// (no hooks/permissions). Manual forge plugin dedupe without --keep-empty goes through DeletesFile to delete empty.
//
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

// TestStripForgeHooks_KeepEmpty_NoEffect_WithUserFields: pins that keepEmpty only takes effect on pure forge files
// (len(settings)==0, the whole file is forge-sourced) — when there are user fields (user hooks or top-level permissions)
// it falls into the MarshalIndent branch preserving user content, keepEmpty has no effect (never writes {}). Prevents future refactors from mistakenly spreading keepEmpty
// semantics to mixed scenarios (user config overwritten by empty object).
//
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
			if !strings.Contains(body, tc.wantKeep) {
				t.Errorf(`用户字段 %q 被误删`, tc.wantKeep)
			}
			if strings.Contains(body, "forge hook") {
				t.Error(`forge hook 未移除`)
			}
		})
	}
}

// TestStripForgeHooks_PreservesUserHooks: with forge hook + user hook in the same matcher,
// delete forge and keep user hook (file retained).
//
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
	if strings.Contains(body, "forge hook") {
		t.Error(`forge hook 未被移除`)
	}
	if !strings.Contains(body, "npx prettier") {
		t.Error(`用户自定义 hook 被误删`)
	}
}

// TestStripForgeHooks_NoForgeHooks_NoOp: no-op for pure user hooks (no forge source).
//
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
	if !strings.Contains(string(data), "npx prettier") {
		t.Error(`无 forge hook 时文件不应被改动`)
	}
}

// TestStripForgeHooks_PreservesTopLevelFields: forge hooks + other top-level fields (permissions)
// — after deleting forge hooks, permissions is retained, and the empty hooks key is removed.
//
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

// TestStripForgeHooks_PreservesUserHooksAndTopLevel: forge hook + user hook (different events)
// + permissions — delete forge, keep user hook + permissions + the file itself.
//
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
	if strings.Contains(body, "forge hook") {
		t.Error(`forge hook 未移除`)
	}
	if !strings.Contains(body, "make lint") {
		t.Error(`Stop 事件用户 hook 被误删`)
	}
	if !strings.Contains(body, "permissions") {
		t.Error(`permissions 顶层字段丢失`)
	}
}

// TestStripForgeHooks_RemovesGateCommand: N4 guard — ForgeHookSpec contains forge gate commands
// (not only forge hook), StripForgeHooks must also remove gate. The previous assertion only checked the `forge hook` substring,
// if StripForgeHooks only deletes hook and misses gate, existing tests cannot catch it (gate command residue → when plugin is installed
// project-level gate still double-runs).
//
// TestStripForgeHooks_RemovesGateCommand：N4 守卫——ForgeHookSpec 含 forge gate 命令
// （非仅 forge hook），StripForgeHooks 必须同样移除 gate。此前断言只查"forge hook"子串，
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
	if strings.Contains(body, "forge gate") {
		t.Error(`forge gate 未被移除（断言不能只查 forge hook 子串,N4）`)
	}
	if strings.Contains(body, "forge hook") {
		t.Error(`forge hook 残留`)
	}
	if !strings.Contains(body, "make lint") {
		t.Error(`用户 hook（make lint）被误删`)
	}
}

// TestGenerateSettings_PreservesUserTopLevelFields: pins that GenerateSettings is merge-style —
// user's existing non-hooks top-level fields (env/model/enabledPlugins) must be retained, only the hooks section is updated to
// ForgeHookSpec. 1.2.0 regression: overwrite-style writes lost user config, plugin-dedupe deleted hooks then file got deleted,
// env/model lost (real incident: Agentworld project lost all ollama/Qwen config after forge init).
// Note: hooks section is forge-managed (GenerateSettings overwrites as ForgeHookSpec), user-defined hooks
// placed in hooks section will be overwritten — the current hotfix focuses on non-hooks top-level field retention, hooks section merging is a follow-up optimization.
//
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

	if !strings.Contains(body, "secret") {
		t.Error(`用户 env.API_KEY=secret 被 GenerateSettings 删除(1.2.0 回归)`)
	}
	if !strings.Contains(body, "my-model") {
		t.Error(`用户 model=my-model 被删除`)
	}
	if !strings.Contains(body, "forge hook") {
		t.Error(`hooks 段未更新为 ForgeHookSpec`)
	}
}

// TestInitFlow_PluginInstalled_PreservesUserConfig: end-to-end pin of the 1.2.0 incident —
// when plugin is installed, the init flow (GenerateSettings writes hooks → StripForgeHooks deletes forge hooks)
// must preserve user env/model, and the file must not be deleted. 1.2.0 GenerateSettings overwrite-style → user config lost + file deleted
// (Agentworld project lost all ollama/Qwen config after forge init). Fixed in 1.2.1.
//
// TestInitFlow_PluginInstalled_PreservesUserConfig:端到端钉死 1.2.0 事故场景——
// plugin 已装时 init 流程(GenerateSettings 写 hooks → StripForgeHooks 删 forge hooks)
// 必须保留用户 env/model,文件不删。1.2.0 GenerateSettings 覆盖式 → 用户配置丢 + 文件删
// (Agentworld 项目 forge init 后 ollama/Qwen 配置全丢)。1.2.1 修。
func TestInitFlow_PluginInstalled_PreservesUserConfig(t *testing.T) {
	dir := t.TempDir()
	writeSettingsLocal(t, dir, `{"env":{"API_KEY":"secret"},"model":"my-model"}`)

	// init flow: GenerateSettings (merge-write hooks) → StripForgeHooks (dedupe deletes forge hooks).
	//
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
	if !strings.Contains(body, "secret") {
		t.Error(`用户 env.API_KEY 丢失`)
	}
	if !strings.Contains(body, "my-model") {
		t.Error(`用户 model 丢失`)
	}
	if strings.Contains(body, "forge hook") {
		t.Error(`dedupe 后应无 forge hooks`)
	}
}

// TestIsForgeHookCommand: pins identification of forge-sourced commands (forge hook X / forge gate X /
// bare forge hook / forge gate). Non-forge commands (including other subcommands like forge plugin status)
// are not misidentified — prevents StripForgeHooks from mistakenly deleting user's non-hook forge invocations.
//
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

// TestPostToolUseToolTrackMatchesReadSkillAgent pins solution C: the tool-track hook's PostToolUse
// matcher must cover Read|Skill|Agent, so the toollog audit records the quality skills loaded by the agent and the spawned sub-agent
// types (the root cause of quality skill 0-triggering under advisory context is traceable). A Read-only matcher would leave Skill/Agent calls
// unrecorded — whether quality skills are driven cannot be audited.
//
// TestPostToolUseToolTrackMatchesReadSkillAgent 钉死方案 C：tool-track hook 的 PostToolUse
// matcher 必须覆盖 Read|Skill|Agent，让 toollog 审计记录 agent 加载的质量技能与派生的子 agent
// 类型（advisory 语境下质量 skill 0 触发的根因可追溯）。仅 Read matcher 会让 Skill/Agent 调用
// 不被记录——质量 skill 是否被驱动无从审计。
func TestPostToolUseToolTrackMatchesReadSkillAgent(t *testing.T) {
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
		if matcher.Matcher == "Read|Skill|Agent" {
			for _, h := range matcher.Hooks {
				if h.Command == "forge hook tool-track" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("PostToolUse 缺 Read|Skill|Agent matcher 的 forge hook tool-track（方案 C：Skill/Agent 调用不被审计）")
	}
}

// TestForgeHookSpec_SkillTriggerMounted 守护通用 skill-trigger hook 的挂载矩阵：
// skill-trigger 必须挂在 5 个事件（UserPromptSubmit / PreToolUse / PostToolUse / Stop / SessionStart）
// 的 7 个 matcher 末尾（PreToolUse/PostToolUse 各 2 个 + Stop/UserPromptSubmit/SessionStart 各 1 个），
// 且不得挂在 PostCompact（plan 边界：PostCompact 不支持 additionalContext 注入）与 PostToolUse
// Read|Skill|Agent matcher（无质量 skill 场景，徒增噪声）。直接断言 ForgeHookSpec() 结构，防回归
// （mirror 守卫 TestPluginPack_HooksMirrorSettings 发现不了单边删除：删 ForgeHookSpec 的同时
// plugin.json 同步消失，mirror 仍过）。
func TestForgeHookSpec_SkillTriggerMounted(t *testing.T) {
	spec := ForgeHookSpec()
	// 预期挂载 skill-trigger 的 event|matcher 组合。
	wantMounted := map[string]bool{
		"PreToolUse|Write|Edit":  true,
		"PreToolUse|Bash":        true,
		"PostToolUse|Write|Edit": true,
		"PostToolUse|Bash":       true,
		"Stop|":                  true,
		"UserPromptSubmit|":      true,
		"SessionStart|":          true,
	}
	gotMounted := map[string]bool{}
	for event, matchers := range spec {
		for _, m := range matchers {
			for _, h := range m.Hooks {
				if h.Command == "forge hook skill-trigger" {
					gotMounted[event+"|"+m.Matcher] = true
				}
			}
		}
	}
	for key := range wantMounted {
		if !gotMounted[key] {
			t.Errorf("skill-trigger 应挂在 %s 末尾，未找到", key)
		}
	}
	// 不应挂载的位置（plan 边界）。
	for event, matchers := range spec {
		for _, m := range matchers {
			for _, h := range m.Hooks {
				if h.Command != "forge hook skill-trigger" {
					continue
				}
				if event == "PostCompact" {
					t.Errorf("skill-trigger 不应挂 PostCompact（不支持 additionalContext 注入）")
				}
				if event == "PostToolUse" && m.Matcher == "Read|Skill|Agent" {
					t.Errorf("skill-trigger 不应挂 PostToolUse Read|Skill|Agent（无质量 skill 场景）")
				}
			}
		}
	}
}

// TestGenerateUserSettings_CreatesFile: with CLAUDE_CONFIG_DIR pointing at a fresh
// dir, GenerateUserSettings creates <home>/settings.json (the user-level machine-wide
// settings file — NOT settings.local.json) carrying the full ForgeHookSpec.
//
// TestGenerateUserSettings_CreatesFile：CLAUDE_CONFIG_DIR 指向全新目录时，
// GenerateUserSettings 创建 <home>/settings.json（user-level 全机器 settings 文件
// ——不是 settings.local.json），携带完整 ForgeHookSpec。
func TestGenerateUserSettings_CreatesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	if err := GenerateUserSettings(); err != nil {
		t.Fatalf("GenerateUserSettings: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not created: %v", err)
	}
	body := string(data)
	for _, want := range []string{`"hooks"`, "forge hook task-guard", "forge hook skill-scan"} {
		if !strings.Contains(body, want) {
			t.Errorf("user-level settings.json missing %q", want)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "settings.local.json")); !os.IsNotExist(err) {
		t.Errorf("user-level generation must write settings.json, not settings.local.json (err=%v)", err)
	}
}

// TestGenerateUserSettings_PreservesUserTopLevelFields: merge semantics identical to
// GenerateSettings — user-defined top-level fields survive, only the hooks section is
// replaced, and a second run is byte-identical (idempotent).
//
// TestGenerateUserSettings_PreservesUserTopLevelFields：merge 语义与 GenerateSettings
// 完全一致——用户自定义顶层字段保留，只替换 hooks 段，二次运行逐字节一致（幂等）。
func TestGenerateUserSettings_PreservesUserTopLevelFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	p := filepath.Join(home, "settings.json")
	existing := `{"env":{"KEY":"val"},"model":"claude-opus-4","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"forge hook stale-hook"}]}]}}`
	if err := os.WriteFile(p, []byte(existing), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := GenerateUserSettings(); err != nil {
		t.Fatalf("GenerateUserSettings: %v", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	// MarshalIndent re-indents the preserved RawMessage values, so assert the env
	// field semantically (unmarshal) rather than byte-for-byte.
	//
	// MarshalIndent 会重排被保留 RawMessage 值的缩进，故 env 字段按语义（反序列化）
	// 断言而非逐字节。
	var env map[string]string
	if err := json.Unmarshal(parsed["env"], &env); err != nil {
		t.Fatalf("parse preserved env field: %v", err)
	}
	if env["KEY"] != "val" {
		t.Errorf("user env field was modified: got %s", string(parsed["env"]))
	}
	if string(parsed["model"]) != `"claude-opus-4"` {
		t.Errorf("user model field was modified: got %s", string(parsed["model"]))
	}
	if strings.Contains(string(data), "stale-hook") {
		t.Error("stale forge hook not replaced")
	}

	// Idempotent: second run must be byte-identical.
	if err := GenerateUserSettings(); err != nil {
		t.Fatalf("second GenerateUserSettings: %v", err)
	}
	data2, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read after second run: %v", err)
	}
	if string(data) != string(data2) {
		t.Error("second GenerateUserSettings not idempotent")
	}
}

// ---- GenerateUserSettings merge + backup (user-level-assets fix) ----

// setupUserSettingsEnv isolates the Claude config home and the forge backup root
// into temp dirs — GenerateUserSettings must never touch the real home in tests.
//
// setupUserSettingsEnv 把 Claude config home 与 forge 备份根隔离进 temp dir——
// GenerateUserSettings 在测试中绝不碰真实 home。
func setupUserSettingsEnv(t *testing.T) (claudeHome string) {
	t.Helper()
	claudeHome = t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	return claudeHome
}

// TestGenerateUserSettings_MergePreservesUserHooks pins the blocker fix: merging
// ForgeHookSpec into ~/.claude/settings.json must NOT destroy the user's own
// hooks. User entries stay (unknown fields intact), stale forge entries are
// replaced, current forge entries appear exactly once, and within one event user
// entries come before forge entries.
//
// TestGenerateUserSettings_MergePreservesUserHooks 钉死 blocker 修复：把
// ForgeHookSpec 合并进 ~/.claude/settings.json 不得销毁用户自己的 hooks。用户
// 条目保留（未知字段不丢）、stale forge 条目被替换、当前 forge 条目恰好出现
// 一次、同事件下用户条目在 forge 条目之前。
func TestGenerateUserSettings_MergePreservesUserHooks(t *testing.T) {
	home := setupUserSettingsEnv(t)
	path := filepath.Join(home, "settings.json")
	existing := `{
  "model": "opus",
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [
        {"type": "command", "command": "./scripts/lint.sh", "timeout": 30},
        {"type": "command", "command": "forge hook stale-removed-hook"}
      ]}
    ],
    "Notification": [
      {"hooks": [{"type": "command", "command": "notify-send done"}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := GenerateUserSettings(); err != nil {
		t.Fatalf("GenerateUserSettings: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// User top-level field preserved.
	if !strings.Contains(content, `"model"`) || !strings.Contains(content, `"opus"`) {
		t.Error("user top-level field (model) not preserved")
	}
	// User hook entry preserved — including its unknown field (timeout), which a
	// typed round-trip would have dropped.
	if !strings.Contains(content, "./scripts/lint.sh") {
		t.Error("user hook entry not preserved")
	}
	if !strings.Contains(content, `"timeout"`) {
		t.Error("user hook entry's unknown field (timeout) dropped by merge")
	}
	// User hook on an event forge does not generate survives.
	if !strings.Contains(content, "notify-send done") {
		t.Error("user hook on non-forge event (Notification) not preserved")
	}
	// Stale forge entry replaced.
	if strings.Contains(content, "stale-removed-hook") {
		t.Error("stale forge hook entry not replaced")
	}
	// Forge wiring present exactly once per command.
	if n := strings.Count(content, `"forge hook task-guard"`); n != 1 {
		t.Errorf("forge hook task-guard appears %d times, want 1", n)
	}

	// Within PreToolUse, the user entry must precede the forge entries.
	var parsed struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	var order []string
	for _, m := range parsed.Hooks["PreToolUse"] {
		for _, h := range m.Hooks {
			order = append(order, h.Command)
		}
	}
	ui, fi := -1, -1
	for i, c := range order {
		if c == "./scripts/lint.sh" && ui == -1 {
			ui = i
		}
		if c == "forge hook task-guard" && fi == -1 {
			fi = i
		}
	}
	if ui == -1 || fi == -1 {
		t.Fatalf("expected both user and forge entries under PreToolUse, got %v", order)
	}
	if ui > fi {
		t.Errorf("user entry must precede forge entries within one event; order: %v", order)
	}
}

// TestGenerateUserSettings_Idempotent pins that a second run is byte-identical
// (strip-then-append must not duplicate forge entries).
//
// TestGenerateUserSettings_Idempotent 钉死第二次运行逐字节一致（先剥后追加不得
// 重复 forge 条目）。
func TestGenerateUserSettings_Idempotent(t *testing.T) {
	home := setupUserSettingsEnv(t)
	path := filepath.Join(home, "settings.json")

	if err := GenerateUserSettings(); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := GenerateUserSettings(); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("second GenerateUserSettings not idempotent")
	}
}

// TestGenerateUserSettings_BacksUpBeforeFirstWrite pins the rollback-anchor
// contract: the original settings.json is backed up before forge's first write,
// and RestoreOriginal rolls the file back to the user's bytes.
//
// TestGenerateUserSettings_BacksUpBeforeFirstWrite 钉死回滚锚点契约：forge 首次
// 写入前备份原 settings.json，RestoreOriginal 能回滚到用户原始字节。
func TestGenerateUserSettings_BacksUpBeforeFirstWrite(t *testing.T) {
	home := setupUserSettingsEnv(t)
	path := filepath.Join(home, "settings.json")
	original := `{"model": "opus", "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "./scripts/lint.sh"}]}]}}`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	if err := GenerateUserSettings(); err != nil {
		t.Fatal(err)
	}

	restored, err := userassets.RestoreOriginal(path)
	if err != nil {
		t.Fatalf("RestoreOriginal: %v", err)
	}
	if !restored {
		t.Fatal("no backup recorded — GenerateUserSettings must back up before first write")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Errorf("restored content mismatch:\n got: %s\nwant: %s", data, original)
	}
}

// TestForgeHookSpec_Gap2ReinjectChain 守护 gap#2 重注入链的 spec 结构（settings.go
// PostCompact/UserPromptSubmit 段注释描述的主机覆盖契约）：PostCompact 必须只挂
// compact-resume；UserPromptSubmit 必须挂 resume-reinject + skill-trigger
// （顺序敏感——先重注入上下文再触发 skill）。codex 两个事件都接、cursor 只接
// UserPromptSubmit 的宿主映射在 agentbridge 侧断言，此测试钉 spec 真相源本身。
//
// TestForgeHookSpec_Gap2ReinjectChain guards the spec structure of the gap#2
// re-injection chain (the host-coverage contract documented at the PostCompact/
// UserPromptSubmit section in settings.go): PostCompact carries only
// compact-resume; UserPromptSubmit carries resume-reinject + skill-trigger in
// that order (context first, skill trigger second). Host mapping (codex takes
// both, cursor only UserPromptSubmit) is asserted in agentbridge — this pins
// the spec source of truth itself.
func TestForgeHookSpec_Gap2ReinjectChain(t *testing.T) {
	spec := ForgeHookSpec()

	var postCompact []string
	for _, m := range spec["PostCompact"] {
		for _, h := range m.Hooks {
			postCompact = append(postCompact, h.Command)
		}
	}
	if len(postCompact) != 1 || postCompact[0] != "forge hook compact-resume" {
		t.Errorf("PostCompact hooks = %v, want [forge hook compact-resume]", postCompact)
	}

	var ups []string
	for _, m := range spec["UserPromptSubmit"] {
		for _, h := range m.Hooks {
			ups = append(ups, h.Command)
		}
	}
	want := []string{"forge hook resume-reinject", "forge hook skill-trigger"}
	if len(ups) != len(want) {
		t.Fatalf("UserPromptSubmit hooks = %v, want %v", ups, want)
	}
	for i := range want {
		if ups[i] != want[i] {
			t.Errorf("UserPromptSubmit hooks[%d] = %q, want %q（顺序：先重注入再触发）", i, ups[i], want[i])
		}
	}
}

// GenerateSettings is the test-local stand-in for the removed production writer (the
// project-level settings path was superseded by the plugin-pack takeover; production
// writes user-level via GenerateUserSettings). It preserves the exact old semantics —
// create .claude/, merge forge hooks into settings.local.json — so the merge-behavior
// tests above keep guarding mergeForgeHooksIntoSettings, which stays alive in
// production via GenerateUserSettings.
//
// GenerateSettings 是已删除的生产 writer（项目级 settings 路径已被 plugin-pack 接管
// 取代；生产经 GenerateUserSettings 写用户级）的测试本地替身。保持与旧实现完全相同
// 的语义——创建 .claude/、把 forge hooks 合并进 settings.local.json——让上面的
// merge 行为测试继续守护 mergeForgeHooksIntoSettings（该逻辑在生产中经
// GenerateUserSettings 存活）。
func GenerateSettings(projectDir string) error {
	if err := os.MkdirAll(filepath.Join(projectDir, ".claude"), 0755); err != nil {
		return err
	}
	return mergeForgeHooksIntoSettings(filepath.Join(projectDir, ".claude", "settings.local.json"))
}
