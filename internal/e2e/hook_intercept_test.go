package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hazard"
)

// 2026-08-30 测试瘦身：此处移除 e2e hazard-guard 分类 battery——每个场景都有
// 更快的脚本级孪生（internal/hooks/hazard_script_test.go：TmpdirEnvExempt /
// EnvBypassRemoved / MktempSelfCleanupExempt / MultilineQuotedDangerIsData /
// GitBranchDScope / BlockGuidanceCopy / InterpDeleteBypass）。本文件保留：走
// 真实 forge 二进制的三个接线冒烟（BlocksHazardousCommand / ConfirmReleases /
// FingerprintReleases）、events.jsonl 落盘对、以及脚本级【未】覆盖的误报回归
// （RmFPathNotFlag / TruncatePathNotBlocked / CommentNotBlocked /
// DataContextNotBlocked / ExecWrappedStillBlocked / RmSubstringInWord /
// RmFlagWithOtherFlags / ForceWithLeaseAllowed）.

// forgeHook runs `forge hook <name>` as a subprocess, feeding the given stdin
// JSON — exactly what Claude Code does when it invokes a configured hook. This
// lets E2E tests exercise the real intercept path (runHook → embedded bash
// script → structured decision JSON) without a live Claude Code. The session_id
// is carried inside stdinJSON (see hookStdin) and scopes the hook's per-session
// state (active-task lookup, snapshot files); keep it unique per test.
//
// Returns (stdout, stderr, exitErr). It does NOT fatal — the caller decides
// whether a non-zero exit is expected (block) or a test failure.
func forgeHook(t *testing.T, dir, hookName, stdinJSON string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(forgeBin, "hook", hookName)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdinJSON)
	// Isolate TMPDIR so bash-guard/file-sentinel snapshot files don't collide
	// across tests or leak into the host /tmp.
	tmp := t.TempDir()
	binDir := filepath.Dir(forgeBin)
	cmd.Env = append(os.Environ(),
		"TMPDIR="+tmp,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// hookStdin builds the Claude Code HookInput JSON for a hook invocation.
// toolInput is marshalled into the tool_input field (file_path/content/command).
func hookStdin(t *testing.T, sessionID, eventName, toolName string, toolInput map[string]any) string {
	t.Helper()
	ti, _ := json.Marshal(toolInput)
	in := map[string]any{
		"session_id":      sessionID,
		"hook_event_name": eventName,
		"tool_name":       toolName,
		"tool_input":      json.RawMessage(ti),
	}
	b, _ := json.Marshal(in)
	return string(b)
}

// assertAllowOutput 钉死 Wave-1 默认（Claude Code）输出协议的放行契约：放行的
// hook 以退出码 0 结束（调用方已断言），stdout 为空或
// {"hookSpecificOutput":{...,"additionalContext":...}} 上下文对象——绝不能是
// "decision":"approve"（放行 hook 不得授予权限）或任何 block 标记。旧的
// `{"decision":"approve"}` 信封在按 agent 分发输出协议的波次中刻意移除；这些
// 断言钉住新契约，并非弱化旧断言。
func assertAllowOutput(t *testing.T, stdout string) {
	t.Helper()
	if strings.Contains(stdout, `"decision":"block"`) {
		t.Errorf("hook blocked (decision:block) where allow was required:\n%s", stdout)
	}
	if strings.Contains(stdout, `"decision":"approve"`) {
		t.Errorf("allow output must not carry decision:approve (Wave-1 contract: an allow hook must not grant permissions):\n%s", stdout)
	}
}

// 注：state.json 随项目级管道删除已不再生成，此处仅作 .forge/* 受管路径的代表例——
// task-guard 按路径模式拦截，不依赖该文件是否存在。
func TestHook_TaskGuard_BlocksForgeManagedFile(t *testing.T) {
	dir := freshProject(t)

	in := hookStdin(t, "sess-selfprotect", "PreToolUse", "Write", map[string]any{
		"file_path": filepath.Join(dir, ".forge", "state.json"),
		"content":   `{"hacked":true}`,
	})

	stdout, _, err := forgeHook(t, dir, "task-guard", in)

	// task-guard FAILs the managed-file write → runHook returns error → non-zero exit.
	if err == nil {
		t.Fatal("task-guard should block write to .forge/state.json, got exit 0")
	}
	// And emit the structured block decision Claude Code acts on.
	if !strings.Contains(stdout, `"decision":"block"`) {
		t.Errorf("task-guard stdout missing decision=block:\n%s", stdout)
	}
	// The block reason must identify the guard so the agent knows what tripped.
	if !strings.Contains(stdout, "task-guard") {
		t.Errorf("task-guard stdout missing guard identifier in additionalContext:\n%s", stdout)
	}
}

// TestHook_Cline_ProjectResolvedFromWorkspaceRoots 钉死 Wave 3b diff 上代码审查
// 暴露的 runHook 时序：normalizeAgentStdin 必须先于 adoptPayloadCwd/
// findProjectRoot 执行，否则 cline 的 workspaceRoots[0]→Cwd 映射是死代码
// （cline payload 没有 cwd 字段——该映射是项目目录的唯一来源）。本测试模拟
// 未文档化的最坏情形——cline 在 workspace 之外拉起 wrapper 脚本（进程 cwd 在
// 项目外）——并断言项目仍被解析：task-guard 经 cline 阻断协议（cancel:true +
// exit 2）拦下 .forge 写入。旧时序下 findProjectRoot 按进程 cwd 解析、失败、
// hook 静默放行——整个 cline 门禁层零症状空转。
func TestHook_Cline_ProjectResolvedFromWorkspaceRoots(t *testing.T) {
	dir := freshProject(t)
	outside := t.TempDir() // process cwd: outside any forge project

	clineIn, err := json.Marshal(map[string]any{
		"clineVersion":   "3.36.0",
		"hookName":       "PreToolUse",
		"taskId":         "sess-cline-cwd",
		"workspaceRoots": []string{dir},
		"tool":           "write_to_file",
		"parameters": map[string]any{
			"path":    filepath.Join(dir, ".forge", "state.json"),
			"content": `{"hacked":true}`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(forgeBin, "hook", "task-guard", "--agent", "cline")
	cmd.Dir = outside
	cmd.Stdin = strings.NewReader(string(clineIn))
	cmd.Env = append(os.Environ(),
		"TMPDIR="+t.TempDir(),
		"PATH="+filepath.Dir(forgeBin)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err == nil {
		t.Fatalf("task-guard must resolve the project from cline workspaceRoots and BLOCK the .forge write even when spawned outside the workspace; exit 0 = silent allow (the normalize-before-adoptPayloadCwd ordering regressed). stdout:\n%s", out.String())
	}
	stdout := out.String()
	if !strings.Contains(stdout, `"cancel":true`) {
		t.Errorf("cline block protocol must emit cancel:true, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "task-guard") {
		t.Errorf("stdout missing task-guard identifier in the block reason:\n%s", stdout)
	}
}

// TestHook_HazardGuard_BlocksHazardousCommand verifies the on-demand-guards
// auto-tier: hazard-guard must BLOCK destructive commands (rm -rf / git push
// --force / DROP TABLE / kubectl delete / DELETE without WHERE) and emit the
// HITL guidance pointing at `forge hazard confirm` as the escape hatch. bash-guard
// only watches for write-via-shell patterns and is blind to these — hazard-guard
// closes that gap.
func TestHook_HazardGuard_BlocksHazardousCommand(t *testing.T) {
	dir := freshProject(t)
	const hazardous = "rm -rf ./important-data"

	in := hookStdin(t, "sess-hazard-block", "PreToolUse", "Bash", map[string]any{
		"command": hazardous,
	})

	stdout, _, err := forgeHook(t, dir, "hazard-guard", in)

	if err == nil {
		t.Fatal("hazard-guard should block 'rm -rf', got exit 0")
	}
	if !strings.Contains(stdout, `"decision":"block"`) {
		t.Errorf("expected decision=block, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "hazard-guard") {
		t.Errorf("stdout missing hazard-guard identifier:\n%s", stdout)
	}
	// HITL guidance must point the agent at the escape hatch — without this the
	// agent has no way to proceed on a legitimately-needed destructive op.
	if !strings.Contains(stdout, "forge hazard confirm") {
		t.Errorf("stdout missing HITL confirm guidance:\n%s", stdout)
	}
	// Fingerprint-drift warning (ce9b2410 lesson): agents retry with && echo / && ls
	// verification suffixes, which rewrites the command string → new hash → re-blocked
	// despite confirm. The block hint must warn "逐字重试" (verbatim retry). Anchored on
	// "逐字" not "&& echo" because stdout is JSON-encoded (the & literal is escaped),
	// and a plain keyword won't false-fail on rewording.
	if !strings.Contains(stdout, "逐字") {
		t.Errorf("stdout missing fingerprint-drift warning (verbatim retry hint):\n%s", stdout)
	}
}

// TestHook_HazardGuard_ConfirmReleases verifies the human-in-the-loop loop
// closes: a blocked command is unblocked after `forge hazard confirm` registers
// the 5-min mark. This is the "confirm → retry → pass" path that makes the gate
// HITL rather than a hard wall.
func TestHook_HazardGuard_ConfirmReleases(t *testing.T) {
	dir := freshProject(t)
	const hazardous = "git push --force origin main"

	in := hookStdin(t, "sess-hazard-confirm", "PreToolUse", "Bash", map[string]any{
		"command": hazardous,
	})

	// 1. Pre-confirm: blocked.
	if stdout, _, err := forgeHook(t, dir, "hazard-guard", in); err == nil {
		t.Fatalf("hazard-guard should block 'git push --force' pre-confirm, got exit 0\n%s", stdout)
	}

	// 2. Register the HITL confirmation (the escape hatch the guidance points at).
	confirm := exec.Command(forgeBin, "hazard", "confirm", hazardous)
	confirm.Dir = dir
	if out, err := confirm.CombinedOutput(); err != nil {
		t.Fatalf("forge hazard confirm failed: %v\n%s", err, out)
	}

	// 3. Retry the same command: now passes within the 5-min window.
	stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
	if err != nil {
		t.Fatalf("hazard-guard should pass post-confirm, got error. stdout:\n%s", stdout)
	}
	assertAllowOutput(t, stdout)
}

// TestHook_HazardGuard_FingerprintReleases verifies the --fingerprint path the hook
// guidance now points at: hook echoes a hex fingerprint, the agent confirms by
// fingerprint (not command string) and retries. This is the robust path for commands
// containing quotes (e.g. SQL `mysql -e 'DROP TABLE t'`) — a command-string confirm
// would have its quotes eaten by the agent's shell re-parsing, diverging from the
// hook's fingerprint and leaving the command blocked.
func TestHook_HazardGuard_FingerprintReleases(t *testing.T) {
	dir := freshProject(t)
	const hazardous = "mysql -e 'DROP TABLE users'" // contains single quotes

	in := hookStdin(t, "sess-hazard-fp", "PreToolUse", "Bash", map[string]any{
		"command": hazardous,
	})

	// 1. Pre-confirm: blocked; guidance must point at --fingerprint (hex, no quote loss).
	stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
	if err == nil {
		t.Fatalf("hazard-guard should block SQL DROP pre-confirm, got exit 0\n%s", stdout)
	}
	if !strings.Contains(stdout, "forge hazard confirm --fingerprint") {
		t.Fatalf("guidance must point at --fingerprint, got:\n%s", stdout)
	}

	// 2. Compute the same fingerprint the hook uses (forge hazard fingerprint <cmd>).
	fpOut, err := exec.Command(forgeBin, "hazard", "fingerprint", hazardous).Output()
	if err != nil {
		t.Fatalf("forge hazard fingerprint: %v", err)
	}
	fp := strings.TrimSpace(string(fpOut))

	// 3. Agent confirms by fingerprint — exactly what the hook told it to do.
	confirm := exec.Command(forgeBin, "hazard", "confirm", "--fingerprint", fp)
	confirm.Dir = dir
	if out, err := confirm.CombinedOutput(); err != nil {
		t.Fatalf("forge hazard confirm --fingerprint failed: %v\n%s", err, out)
	}

	// 4. Retry: passes within the 5-min window.
	stdout, _, err = forgeHook(t, dir, "hazard-guard", in)
	if err != nil {
		t.Fatalf("hazard-guard should pass post-confirm, got error. stdout:\n%s", stdout)
	}
	assertAllowOutput(t, stdout)
}

// TestHook_HazardGuard_RmFPathNotFlag regressions the 2026-06 .lark-report.xml false
// positive: rm -f <path containing an 'r'> must NOT be misread as rm -rf. The old
// is_hazardous used bare grep '-r'/'-f' substrings, so the -r inside ".lark-report"
// was treated as rm's -r flag and, combined with -f, misclassified as rm -rf. rm -f
// of a single file is not destructive anyway — it must pass.
func TestHook_HazardGuard_RmFPathNotFlag(t *testing.T) {
	dir := freshProject(t)
	const safe = `rm -f .lark-report.xml`

	in := hookStdin(t, "sess-hazard-rmf", "PreToolUse", "Bash", map[string]any{
		"command": safe,
	})

	stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
	if err != nil {
		t.Fatalf("hazard-guard must pass 'rm -f <path-with-r>' (not rm -rf), got block. stdout:\n%s", stdout)
	}
	assertAllowOutput(t, stdout)
}

// TestHook_HazardGuard_ForceWithLeaseAllowed：--force-with-lease 是 git 推荐的 --force 的安全
// 替代（远端前进时拒绝），所以不应像裸 --force 那样被硬拦。裸 --force 仍拦（回归保护）。
func TestHook_HazardGuard_ForceWithLeaseAllowed(t *testing.T) {
	dir := freshProject(t)

	// lease 放行
	inLease := hookStdin(t, "sess-hazard-lease", "PreToolUse", "Bash", map[string]any{
		"command": "git push --force-with-lease origin main",
	})
	stdout, _, err := forgeHook(t, dir, "hazard-guard", inLease)
	if err != nil {
		t.Fatalf("hazard-guard should allow --force-with-lease, got block. stdout:\n%s", stdout)
	}
	assertAllowOutput(t, stdout)

	// 带值变体 --force-with-lease=<ref>:<expect>（CI 最常用形态）同样放行
	inLeaseVal := hookStdin(t, "sess-hazard-lease-val", "PreToolUse", "Bash", map[string]any{
		"command": "git push --force-with-lease=main:abc123 origin main",
	})
	stdout, _, err = forgeHook(t, dir, "hazard-guard", inLeaseVal)
	if err != nil {
		t.Fatalf("hazard-guard should allow --force-with-lease=<ref>:<expect>, got block. stdout:\n%s", stdout)
	}
	assertAllowOutput(t, stdout)

	// 裸 --force 仍拦（回归保护：lease 放行不能导致裸 force 漏拦）
	inForce := hookStdin(t, "sess-hazard-force", "PreToolUse", "Bash", map[string]any{
		"command": "git push --force origin main",
	})
	stdout, _, err = forgeHook(t, dir, "hazard-guard", inForce)
	if err == nil {
		t.Fatalf("hazard-guard must still block bare --force, got exit 0. stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"decision":"block"`) {
		t.Errorf("expected decision=block for bare --force, got:\n%s", stdout)
	}
}

// TestHook_HazardGuard_RmFlagWithOtherFlags regressions 审查 S1：rm 前置其他 flag
// （-i / --one-file-system / -v）再接 -rf 必须仍被拦。这些是合法 rm 写法，「rm 紧跟单簇」
// 锚定会漏检它们（真高危漏放）。
func TestHook_HazardGuard_RmFlagWithOtherFlags(t *testing.T) {
	dir := freshProject(t)
	for _, cmd := range []string{
		"rm -i -rf ./important-data",
		"rm --one-file-system -rf ./important-data",
		"rm -v -rf ./vault",
	} {
		in := hookStdin(t, "sess-hazard-flagorder", "PreToolUse", "Bash", map[string]any{
			"command": cmd,
		})
		stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
		if err == nil {
			t.Fatalf("hazard-guard must block %q (rm with extra flags + -rf), got exit 0. stdout:\n%s", cmd, stdout)
		}
		if !strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("expected decision=block for %q, got:\n%s", cmd, stdout)
		}
	}
}

// TestHook_HazardGuard_DataContextNotBlocked: 危险串仅在引号内（数据）不拦——context
// classification。grep"rm -rf"/ git commit -m"fix rm -rf bug"/ echo"DROP TABLE"都
// 是把危险串当数据传递，不是执行。根治 2026-06 类别级误判（.lark-report 是单点表现）。
// 第二条 git commit -m 正是会话最初 commit title 含 rm -f 被拦的真实案例。
func TestHook_HazardGuard_DataContextNotBlocked(t *testing.T) {
	dir := freshProject(t)
	cases := []string{
		`grep "rm -rf" file.go`,
		`git commit -m "fix: handle rm -rf path bug"`,
		`echo "DROP TABLE users"`,
		`printf '%s' "git push --force"`,
		`echo "rm -rf" | xargs cat`,
	}
	for _, cmd := range cases {
		in := hookStdin(t, "sess-hazard-data", "PreToolUse", "Bash", map[string]any{
			"command": cmd,
		})
		stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
		if err != nil {
			t.Fatalf("hazard-guard must pass data-context %q (danger only in quotes), got block. stdout:\n%s", cmd, stdout)
		}
	}
}

// TestHook_HazardGuard_ExecWrappedStillBlocked: 引号内但被 bash -c / sh -c / eval 包裹的是
// 真执行，context classification 不能放行——strip_quotes 会剥离引号内内容，若无此兜底会
// 漏检真高危（bash -c"rm -rf"是 agent 真删数据）。
func TestHook_HazardGuard_ExecWrappedStillBlocked(t *testing.T) {
	dir := freshProject(t)
	cases := []string{
		`bash -c "rm -rf ./vault"`,
		`sh -c "rm -rf ./data"`,
		`eval "git push --force"`,
		`mysql -e 'DROP TABLE users'`,
		`python3 -c "import os; os.system('rm -rf ./.git')"`,
	}
	for _, cmd := range cases {
		in := hookStdin(t, "sess-hazard-exec", "PreToolUse", "Bash", map[string]any{
			"command": cmd,
		})
		stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
		if err == nil {
			t.Fatalf("hazard-guard must block exec-wrapped %q, got exit 0. stdout:\n%s", cmd, stdout)
		}
		if !strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("expected decision=block for exec-wrapped %q, got:\n%s", cmd, stdout)
		}
	}
}

// TestHook_HazardGuard_LogsBlockEvent: block 事件落盘 events.jsonl，可结构化追溯——
// 补全「被拦命令无独立记录」痛点（2026-06 hazards 审计 19 条 FAIL 只能扒 checklog）。
func TestHook_HazardGuard_LogsBlockEvent(t *testing.T) {
	dir := freshProject(t)
	in := hookStdin(t, "sess-hazard-logblk", "PreToolUse", "Bash", map[string]any{
		"command": "rm -rf ./important-data",
	})
	forgeHook(t, dir, "hazard-guard", in) // 触发 block → 落盘 block 事件

	p, err := forgedata.ProjectFor(dir)
	if err != nil {
		t.Fatalf("ProjectFor: %v", err)
	}
	events, err := hazard.LoadEvents(p)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	var found bool
	for _, e := range events {
		if e.Type == hazard.EventBlock && strings.Contains(e.Command, "important-data") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("block event not logged in events.jsonl. got %d events: %+v", len(events), events)
	}
}

// TestHook_HazardGuard_LogsReleaseEvent: 完整 HITL 事件流 block → confirm → release
// 均落盘。confirm 登记（Confirmation）+ release 放行事件双记录，可追溯「被拦后是否被确认」。
func TestHook_HazardGuard_LogsReleaseEvent(t *testing.T) {
	dir := freshProject(t)
	const cmd = "git push --force origin main"
	in := hookStdin(t, "sess-hazard-logrel", "PreToolUse", "Bash", map[string]any{
		"command": cmd,
	})

	forgeHook(t, dir, "hazard-guard", in) // block → 记 block 事件
	confirm := exec.Command(forgeBin, "hazard", "confirm", cmd)
	confirm.Dir = dir
	if out, err := confirm.CombinedOutput(); err != nil {
		t.Fatalf("forge hazard confirm: %v\n%s", err, out)
	}
	forgeHook(t, dir, "hazard-guard", in) // release → 记 release 事件

	p, err := forgedata.ProjectFor(dir)
	if err != nil {
		t.Fatalf("ProjectFor: %v", err)
	}
	events, err := hazard.LoadEvents(p)
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	var foundBlock, foundRelease bool
	for _, e := range events {
		if !strings.Contains(e.Command, "push --force") {
			continue
		}
		if e.Type == hazard.EventBlock {
			foundBlock = true
		}
		if e.Type == hazard.EventRelease {
			foundRelease = true
		}
	}
	if !foundBlock {
		t.Error("block event missing from events.jsonl")
	}
	if !foundRelease {
		t.Error("release event missing from events.jsonl")
	}
}

// TestHook_HazardGuard_RmSubstringInWordNotHazardous regressions 2026-07 误伤：裸
// 'rm ' 子串匹配会误中 confirm/perform/transform 等词内的 rm（confirm = confi+rm），
// 叠加 --fingerprint 含 -f...r 的 flag 检测，把 'go run . hazard confirm --fingerprint'
// 误判 rm -rf——非 'forge hazard' 开头的调用（go run / cd && forge hazard）不命中豁免、
// 走到 is_hazardous 被反复拦截。fix：rm 检测要求 rm 是独立 token（前导行首或非小写字母）。
// 这些命令必须放行，且真 rm -rf 仍必须 block（词边界收紧不能放过真高危）。
func TestHook_HazardGuard_RmSubstringInWordNotHazardous(t *testing.T) {
	dir := freshProject(t)
	// 都不以 'forge hazard' 开头（不命中豁免），含 rm 子串（confirm 内）+ -f...r flag
	// （--fingerprint）——修复前误判 rm -rf，修复后必须放行。
	pass := []string{
		`go run . hazard confirm --fingerprint abc123`,
		`cd /e/x && forge hazard confirm --fingerprint deadbeef`,
		`sudo forge hazard confirm --fingerprint feedface`,
	}
	for _, cmd := range pass {
		in := hookStdin(t, "sess-rmsubstr", "PreToolUse", "Bash", map[string]any{
			"command": cmd,
		})
		stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
		if err != nil {
			t.Fatalf("hazard-guard must pass %q (rm only inside 'confirm', not a rm token), got block. stdout:\n%s", cmd, stdout)
		}
	}

	// 回归：真 rm -rf（rm 是独立 token）仍必须 block——词边界收紧不能放过真高危。
	// 覆盖词边界两分支：行首 rm（^rm ）与 rm 前空格（[^a-z]rm ，如 sudo rm -rf）。
	block := []string{
		`rm -rf ./important-data`,
		`sudo rm -rf ./data`,
	}
	for _, cmd := range block {
		in := hookStdin(t, "sess-rmsubstr-real", "PreToolUse", "Bash", map[string]any{
			"command": cmd,
		})
		stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
		if err == nil {
			t.Fatalf("hazard-guard must still block real %q (rm is an independent token), got exit 0. stdout:\n%s", cmd, stdout)
		}
	}
}

// TestHook_HazardGuard_CommentNotBlocked 钉死 dogfood 3.2a：危险串在 # 注释行（非引号内、
// 词边界处）是数据不执行，应放行。electron-builder"# Clean up"含 rm 的注释被当执行误拦
// （AgentWorld 误报之一）。strip_quotes 增加 # 注释剥离：make build # rm -rf build/ 中
// rm -rf 在注释里 → 剥离后 is_hazardous 不命中 → 数据上下文放行。
// 回归保护：注释后真执行的危险串（如 # note ; rm -rf x 的分号续接）本测试不覆盖，留给
// code-review-gate——hook 只对纯注释行（# 到行尾无分号续接）放行。
func TestHook_HazardGuard_CommentNotBlocked(t *testing.T) {
	dir := freshProject(t)
	cases := []string{
		`make build # rm -rf build/ then rebuild`,
		`npm run dev # todo: drop table users after migrate`,
		`./deploy.sh # git push --force if rollback needed`,
	}
	for _, cmd := range cases {
		in := hookStdin(t, "sess-hazard-comment", "PreToolUse", "Bash", map[string]any{
			"command": cmd,
		})
		stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
		if err != nil {
			t.Fatalf("hazard-guard must pass %q (danger only in # comment), got block. stdout:\n%s", cmd, stdout)
		}
	}
}

// TestHook_HazardGuard_TruncatePathNotBlocked 钉死 dogfood 3.2b：裸"truncate"子串匹配
// 误伤路径片段（cd truncate-output/ / --no-truncate flag）。收窄到 SQL DDL 语境后这些
// 必须放行，且真 TRUNCATE TABLE 仍必须 block（收窄不能放过真 DDL）。
func TestHook_HazardGuard_TruncatePathNotBlocked(t *testing.T) {
	dir := freshProject(t)
	// 路径/flag 片段含 truncate 子串——非 SQL DDL，放行
	pass := []string{
		`cd truncate-output/`,
		`pytest --no-truncate`,
		`cat ./logs/truncate-event.log`,
	}
	for _, cmd := range pass {
		in := hookStdin(t, "sess-hazard-truncpath", "PreToolUse", "Bash", map[string]any{
			"command": cmd,
		})
		stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
		if err != nil {
			t.Fatalf("hazard-guard must pass %q (truncate is a path/flag fragment, not SQL DDL), got block. stdout:\n%s", cmd, stdout)
		}
	}
	// 回归：真 SQL TRUNCATE TABLE 仍必须 block——收窄不能放过破坏性 DDL
	// 含裸 TRUNCATE（MySQL/PG 的 TABLE 关键字可选，TRUNCATE users ≡ TRUNCATE TABLE users）。
	block := []string{
		`mysql -e 'TRUNCATE TABLE users'`,
		`psql -c 'truncate table audit_log'`,
		`mysql -e 'TRUNCATE users'`,
	}
	for _, cmd := range block {
		in := hookStdin(t, "sess-hazard-truncsql", "PreToolUse", "Bash", map[string]any{
			"command": cmd,
		})
		stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
		if err == nil {
			t.Fatalf("hazard-guard must still block %q (real TRUNCATE TABLE DDL), got exit 0. stdout:\n%s", cmd, stdout)
		}
		if !strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("expected decision=block for %q, got:\n%s", cmd, stdout)
		}
	}
}

// forgeHookShared runs `forge hook <name>` like forgeHook, but pins a SHARED
// temp dir across calls — read-before-edit (方案2) records Reads to a per-session
// disk log (tool-track append) and greps it at Edit time, so the Read and Edit
// invocations must resolve the same reads-file path. TMP/TEMP (Windows
// os.TempDir) and TMPDIR (bash) are all pinned so the Go dispatcher's
// readsFilePath() agrees across the two subprocesses on every platform.
func forgeHookShared(t *testing.T, dir, tmp, hookName, stdinJSON string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(forgeBin, "hook", hookName)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdinJSON)
	binDir := filepath.Dir(forgeBin)
	cmd.Env = append(os.Environ(),
		"TMPDIR="+tmp,
		"TMP="+tmp,
		"TEMP="+tmp,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// TestHook_ReadBeforeEdit table-drives the read-before-edit (方案2) gate matrix
// (the 7 former single-scenario tests, merged 2026-08-30 slim-down). Row
// rationale, from the former tests:
//   - unread-source: the core contract — within an active task, editing an
//     existing source file never Read this session hard-blocks at PreToolUse
//     (the downstream catch for 'blind edit from memory, old_string hits' —
//     caught at Edit time, not deferred to task-verify; no tool-track Read
//     record → reads-log miss → grep -qxF fails).
//   - after-read: the positive path — a tool-track (PostToolUse Read) record
//     into the per-session reads-log, then Edit of the same file passes;
//     proves the reads-log side-channel works end-to-end.
//   - no-task: with no active task the hook silently passes — quick non-task
//     edits are outside Forge's quality domain.
//   - new-file: the new-file exemption — Writing a file not on disk passes
//     ([ -f ] false); a new file cannot have been Read, and this is creation
//     not blind edit.
//   - after-write: the 2026-08-24 fix — a Write lands in the per-session
//     reads-log (via the auto-compile PostToolUse dispatch), so the Edit right
//     after a file-creating Write passes; 4 production sessions replayed the
//     old script (Write → immediate Edit → FAIL → ceremonial Read).
//   - double-fire: the 2026-08-24 record dedupe — a host double-firing ONE
//     Edit event (kimi, 98ms apart, consecutive checklog seq) must still block
//     BOTH deliveries but record exactly ONE checklog entry.
//   - override-escape: the per-task leak-prevention path — `forge task
//     override --work-activity disable` makes the Go dispatcher inject
//     FORGE_WORK_ACTIVITY=disable so unread edits pass; contrast the
//     unread-source row (same scenario without override must block).
//
// TestHook_ReadBeforeEdit 表驱动 read-before-edit（方案2）门禁矩阵（2026-08-30
// 瘦身合并原 7 个单场景测试）。逐行缘由：
//   - unread-source：核心契约——活跃任务内编辑本会话从未 Read 过的现存源文件，
//     PreToolUse 硬阻断（「凭记忆盲改、old_string 撞中」的下沉拦截，不拖到
//     task-verify；无 tool-track Read 记录 → reads-log 无该路径 → grep -qxF 失败）。
//   - after-read：正向路径——tool-track（PostToolUse Read）先记路径，再 Edit 同一
//     文件放行；证明 reads-log side-channel 端到端打通。
//   - no-task：无活跃任务时静默放行——非任务的快速编辑不在 Forge 质量域内。
//   - new-file：新建豁免——Write 一个不在盘上的文件放行（[ -f ] 为假）；新建
//     无法被 Read 过，且是创作非盲改。
//   - after-write：2026-08-24 修复——Write 经 auto-compile PostToolUse 分发计入
//     reads-log，建文件后紧随的 Edit 放行；4 个生产 session 复发过旧剧本
//     （Write → 紧接 Edit → FAIL → 被迫补形式 Read）。
//   - double-fire：2026-08-24 记录去重——宿主双发同一 Edit 事件（kimi 98ms 内
//     两次、checklog seq 连号）两次投递都必须仍阻断，但 checklog 只记一条。
//   - override-escape：per-task 防泄漏路径——`forge task override --work-activity
//     disable` 让 Go dispatcher 注入 FORGE_WORK_ACTIVITY=disable 放行未读编辑；
//     对照 unread-source 行（同场景无 override 必 block）。
func TestHook_ReadBeforeEdit(t *testing.T) {
	editTarget := func(dir string) map[string]any {
		return map[string]any{
			"file_path":  filepath.Join(dir, "target.go"),
			"old_string": "func old() {}",
			"new_string": "func new() {}",
		}
	}
	for _, tc := range []struct {
		name        string
		startTask   bool
		override    bool
		prep        func(t *testing.T, dir, tmp, sid string)
		seedFile    string // relative path; written when seedContent is non-empty
		seedContent string
		tool        string // PreToolUse tool under test (Edit | Write)
		toolInput   func(dir string) map[string]any
		fires       int  // deliveries of the same event (kimi double-fire)
		wantBlock   bool // expected verdict per delivery
		wantStdout  []string
		wantLogN    int // read-before-edit checklog entries; -1 = skip
	}{
		{
			name: "unread-source", startTask: true,
			seedFile: "target.go", seedContent: "package main\n\nfunc old() {}\n",
			tool: "Edit", toolInput: editTarget, fires: 1, wantBlock: true,
			wantStdout: []string{"read-before-edit"}, // block 原因须点名守卫
			wantLogN:   -1,
		},
		{
			name: "after-read", startTask: true,
			// 先经 tool-track（PostToolUse Read）把该路径记进 per-session reads-log。
			prep: func(t *testing.T, dir, tmp, sid string) {
				readIn := hookStdin(t, sid, "PostToolUse", "Read", map[string]any{
					"file_path": filepath.Join(dir, "target.go"),
				})
				if _, _, err := forgeHookShared(t, dir, tmp, "tool-track", readIn); err != nil {
					t.Fatalf("tool-track Read record step failed: %v", err)
				}
			},
			seedFile: "target.go", seedContent: "package main\n\nfunc old() {}\n",
			tool: "Edit", toolInput: editTarget, fires: 1, wantBlock: false, wantLogN: -1,
		},
		{
			name:     "no-task", // 故意不启动任务
			seedFile: "target.go", seedContent: "package main\n",
			tool: "Edit", fires: 1, wantBlock: false, wantLogN: -1,
			toolInput: func(dir string) map[string]any {
				return map[string]any{
					"file_path":  filepath.Join(dir, "target.go"),
					"old_string": "x",
					"new_string": "y",
				}
			},
		},
		{
			name: "new-file", startTask: true,
			// brand_new.go 不在盘上（[ -f ] 为假 → 新建分支）。
			tool: "Write", fires: 1, wantBlock: false, wantLogN: -1,
			toolInput: func(dir string) map[string]any {
				return map[string]any{
					"file_path": filepath.Join(dir, "brand_new.go"),
					"content":   "package main\n",
				}
			},
		},
		{
			name: "after-write", startTask: true,
			// 1. Write 建文件（新建豁免放行 PreToolUse）→ 2. 写入落盘 →
			// 3. PostToolUse auto-compile 把路径记进会话 reads-log。
			prep: func(t *testing.T, dir, tmp, sid string) {
				writeIn := hookStdin(t, sid, "PreToolUse", "Write", map[string]any{
					"file_path": filepath.Join(dir, "created.go"),
					"content":   "package main\n\nfunc made() {}\n",
				})
				if stdout, _, err := forgeHookShared(t, dir, tmp, "read-before-edit", writeIn); err != nil {
					t.Fatalf("Write of a new file must pass read-before-edit, got block. stdout:\n%s", stdout)
				}
				writeFile(t, dir, "created.go", "package main\n\nfunc made() {}\n")
				postIn := hookStdin(t, sid, "PostToolUse", "Write", map[string]any{
					"file_path": filepath.Join(dir, "created.go"),
					"content":   "package main\n\nfunc made() {}\n",
				})
				if _, _, err := forgeHookShared(t, dir, tmp, "auto-compile", postIn); err != nil {
					t.Fatalf("auto-compile PostToolUse Write step failed: %v", err)
				}
			},
			tool: "Edit", fires: 1, wantBlock: false, wantLogN: -1,
			toolInput: func(dir string) map[string]any {
				return map[string]any{
					"file_path":  filepath.Join(dir, "created.go"),
					"old_string": "func made() {}",
					"new_string": "func made() { println(1) }",
				}
			},
		},
		{
			name: "double-fire", startTask: true,
			// 现存源文件、本会话未 Read → 两次投递都阻断。
			seedFile: "target.go", seedContent: "package main\n\nfunc old() {}\n",
			tool: "Edit", toolInput: editTarget, fires: 2, wantBlock: true,
			// 双发的单个 Edit 事件必须恰好记一条 checklog。
			wantLogN: 1,
		},
		{
			name: "override-escape", startTask: true, override: true,
			seedFile: "target.go", seedContent: "package main\n\nfunc old() {}\n",
			tool: "Edit", toolInput: editTarget, fires: 1, wantBlock: false, wantLogN: -1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := freshProject(t)
			tmp := t.TempDir()
			sid := "sess-rbe-" + tc.name
			if tc.startTask {
				forge(t, dir, "task", "start", "--ref", "feat/rbe-"+tc.name, "--title", tc.name)
			}
			if tc.override {
				forge(t, dir, "task", "override", "--work-activity", "disable")
			}
			if tc.seedContent != "" {
				writeFile(t, dir, tc.seedFile, tc.seedContent)
			}
			if tc.prep != nil {
				tc.prep(t, dir, tmp, sid)
			}

			in := hookStdin(t, sid, "PreToolUse", tc.tool, tc.toolInput(dir))
			for i := 1; i <= tc.fires; i++ {
				stdout, _, err := forgeHookShared(t, dir, tmp, "read-before-edit", in)
				if tc.wantBlock {
					if err == nil {
						t.Fatalf("delivery #%d: read-before-edit must BLOCK, got exit 0. stdout:\n%s", i, stdout)
					}
					if !strings.Contains(stdout, `"decision":"block"`) {
						t.Errorf("delivery #%d: expected decision=block, got:\n%s", i, stdout)
					}
				} else {
					if err != nil {
						t.Fatalf("read-before-edit must ALLOW, got block. stdout:\n%s", stdout)
					}
					assertAllowOutput(t, stdout)
				}
				for _, w := range tc.wantStdout {
					if !strings.Contains(stdout, w) {
						t.Errorf("stdout missing %q:\n%s", w, stdout)
					}
				}
			}

			if tc.wantLogN >= 0 {
				data, err := os.ReadFile(filepath.Join(forgedata.DataDirFor(dir), "checklog.jsonl"))
				if err != nil {
					t.Fatalf("read checklog: %v", err)
				}
				if got := strings.Count(string(data), `"check":"read-before-edit"`); got != tc.wantLogN {
					t.Errorf("double-fired single Edit event must produce exactly ONE checklog entry, got %d:\n%s", got, data)
				}
			}
		})
	}
}
