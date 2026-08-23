package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// This file pins the dsh advisory-promotion landing (2026-08-22 incident: the
// task-guard WARN reached the dsh model's inbox via agent.inject yet was ignored —
// its text self-describes as "allowed", and every downstream gate is task-scoped).
// dsh registers task-guard only (admission path (b) of hostcap PromoteAdvisory:
// channel delivers, enforcement needed), so the scope pins below assert bash-guard
// and assertion-check do NOT promote on dsh.
//
// 本文件钉住 dsh advisory 提升落地（2026-08-22 事件：task-guard 的 WARN 经
// agent.inject 到达了 dsh 模型 inbox 却被无视——其文案自述「allowed」，且所有
// 下游门禁都 task-scoped）。dsh 仅注册 task-guard（hostcap PromoteAdvisory 的
// 准入路径 (b)：通道送达，需要执法），故下方范围钉死 bash-guard 与
// assertion-check 在 dsh 上**不**提升。

// TestPromoteDshTaskGuardAdvisory covers the pure helper on dsh: the real no-task
// advisory promotes, the Auto-created success path must NOT (it just enabled the
// edit), and the promotion is task-guard-scoped — dsh carries no rule for the
// other two hooks kimi promotes (their consequence chains still work on dsh:
// file-sentinel quarantines Bash-written files; assertion-check advisory delivers).
//
// TestPromoteDshTaskGuardAdvisory 覆盖 dsh 上的纯函数：真无任务 advisory 提升；
// Auto-created 成功路径不得提升（它刚放行了该编辑）；且提升范围仅 task-guard
// ——dsh 不带 kimi 另两条 hook 的规则（它们的后果链在 dsh 上仍有效：
// file-sentinel 会 quarantine Bash 写的文件；assertion-check 的 advisory 送达）。
func TestPromoteDshTaskGuardAdvisory(t *testing.T) {
	cases := []struct {
		name   string
		hook   string
		passed bool
		detail string
		want   bool
	}{
		{"no-task advisory (promote)", "task-guard", true, "[task-guard] No active task. Source edit DENIED until one exists — run: forge task start --ref <ref> --branch.", true},
		{"legacy advisory wording (promote)", "task-guard", true, "[task-guard] No active task. Source changes are allowed but not tracked by a Forge task.", true},
		{"auto-create success path (must NOT)", "task-guard", true, "[task-guard] Auto-created task 'feat/x' from branch. Source changes tracked.", false},
		{"bare PASS (empty detail)", "task-guard", true, "", false},
		{"already blocked (no double-flip)", "task-guard", false, "[task-guard] No active task.", false},
		// Scope pins: dsh registers task-guard ONLY.
		{"bash-guard out of scope on dsh", "bash-guard", true, "[bash-guard] Bash write without active task. Changes are allowed but not tracked.", false},
		{"assertion-check out of scope on dsh", "assertion-check", true, "[assertion-check] Advisory: 疑似断言弱化。", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := promoteAdvisory("dsh", tt.hook, tt.passed, tt.detail)
			if got != tt.want {
				t.Errorf("promoteAdvisory(dsh, %q, passed=%v, detail=%q) = %v, want %v", tt.hook, tt.passed, tt.detail, got, tt.want)
			}
		})
	}
	// Unknown host stays advisory (the claude-compatible default).
	//
	// 未知宿主保持 advisory（Claude 兼容默认）。
	if promoteAdvisory("claude-code", "task-guard", true, "[task-guard] No active task.") {
		t.Error("claude-code must not promote (advisory channel suffices)")
	}
}

// TestAdvisoryPromotionEscapeHatches pins both hatches: the generic
// FORGE_ADVISORY_PROMOTION=soft (added with dsh — one knob for every promoted
// host) and the shipped FORGE_KIMI_ADVISORY=soft (back-compat, named in docs).
// Either must suppress promotion for every host.
//
// TestAdvisoryPromotionEscapeHatches 钉住两个逃生舱：泛化的
// FORGE_ADVISORY_PROMOTION=soft（随 dsh 加入——所有提升宿主一个开关）与已发布的
// FORGE_KIMI_ADVISORY=soft（向后兼容，文档具名）。任一设置都必须对所有宿主抑制提升。
func TestAdvisoryPromotionEscapeHatches(t *testing.T) {
	for _, host := range []string{"kimi", "dsh"} {
		t.Run("generic/"+host, func(t *testing.T) {
			t.Setenv("FORGE_ADVISORY_PROMOTION", "soft")
			if promoteAdvisory(host, "task-guard", true, "[task-guard] No active task.") {
				t.Errorf("FORGE_ADVISORY_PROMOTION=soft must suppress promotion on %s", host)
			}
		})
	}
	t.Run("kimi back-compat", func(t *testing.T) {
		t.Setenv("FORGE_KIMI_ADVISORY", "soft")
		if promoteAdvisory("kimi", "task-guard", true, "[task-guard] No active task.") {
			t.Error("FORGE_KIMI_ADVISORY=soft must keep suppressing kimi promotion")
		}
		// The legacy kimi hatch must NOT leak onto other hosts.
		//
		// kimi 旧开关不得泄漏到其他宿主。
		if !promoteAdvisory("dsh", "task-guard", true, "[task-guard] No active task.") {
			t.Error("FORGE_KIMI_ADVISORY=soft is kimi-scoped; dsh promotion must stay active")
		}
	})
}

// TestTaskGuardPromotionActive pins the env pre-configuration predicate runHook
// uses to set FORGE_TASKGUARD_PROMOTED: true exactly when the host carries a
// task-guard rule AND no hatch is open — a set env with suppressed promotion
// would resurrect the 139-WARN spam with no enforcement behind it (dogfood 3.1).
//
// TestTaskGuardPromotionActive 钉住 runHook 用来设 FORGE_TASKGUARD_PROMOTED 的
// env 预配置谓词：恰在宿主持有 task-guard 规则且逃生舱全关时为真——env 已设而
// 提升被抑制会复活 139 次 WARN 刷屏（dogfood 3.1）且背后无执法。
func TestTaskGuardPromotionActive(t *testing.T) {
	cases := []struct {
		agent string
		want  bool
	}{
		{"dsh", true},
		{"kimi", true},
		{"claude-code", false},
		{"", false},
		{"unknown-host", false},
	}
	for _, c := range cases {
		if got := taskGuardPromotionActive(c.agent); got != c.want {
			t.Errorf("taskGuardPromotionActive(%q) = %v, want %v", c.agent, got, c.want)
		}
	}
	t.Setenv("FORGE_ADVISORY_PROMOTION", "soft")
	if taskGuardPromotionActive("dsh") {
		t.Error("hatch open must disable the promotion env too (no de-noise drop without enforcement)")
	}
}

// runTaskGuardHookOnce invokes the real task-guard script through runHook in a
// fresh forge project (fixture shape of TestHookOutput_StructuredJSON) with the
// given agent declaration and session id, returning the captured output and
// error. The temp project is not a git repo, so the script's branch resolution
// yields "" and falls to the no-task warn/promote branch — the same path taken
// on main/master.
//
// runTaskGuardHookOnce 在 newTaskGuardProject 已 chdir 的项目里经 runHook 真跑一次
// task-guard 脚本，按给定 agent 声明与 session id，返回捕获的输出与错误。temp 项目
// 非 git 仓库，脚本的分支解析得 ""，落到无任务 warn/promote 分支——与 main/master
// 上走的同一条路径（项目根由 findProjectRoot 从 cwd 解析，不经参数传递）。
func runTaskGuardHookOnce(t *testing.T, agentDecl, sessionID string) (stdout, stderr string, err error) {
	t.Helper()
	payload := fmt.Sprintf(`{"hook_event_name":"PreToolUse","tool_name":"Write","session_id":%q,%s"tool_input":{"file_path":"src/main.go","content":"package main"}}`, sessionID, agentDecl)
	oldStdin := os.Stdin
	tmpStdin, ierr := os.CreateTemp("", "hook-stdin-*.json")
	if ierr != nil {
		t.Fatal(ierr)
	}
	if _, ierr = tmpStdin.WriteString(payload); ierr != nil {
		t.Fatal(ierr)
	}
	if _, ierr = tmpStdin.Seek(0, 0); ierr != nil {
		t.Fatal(ierr)
	}
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()
	return captureOutput(t, func() error {
		// Minimal cobra root: the Go-internal hook dispatch reads cmd.Root().Version
		// and would nil-panic on nil (same note as runHookWithStdin).
		//
		// 最小 cobra root：Go 内 hook 分派读 cmd.Root().Version，nil 会空指针
		// （同 runHookWithStdin 的注释）。
		return runHook(&cobra.Command{}, []string{"task-guard"})
	})
}

// newTaskGuardProject builds the isolated fixture both E2E tests below share:
// fresh forge project + isolated DataHome + cleared agent envs (attribution then
// rides only the payload under test).
//
// newTaskGuardProject 构建下面两个 E2E 共用的隔离 fixture：全新 forge 项目 +
// 隔离 DataHome + 清空的 agent env（归因只受被测 payload 驱动）。
func newTaskGuardProject(t *testing.T) {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Setenv("FORGE_HOOK_AGENT", "")
	t.Setenv("FORGE_AGENT", "")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".forge", "hooks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644); err != nil {
		t.Fatal(err)
	}
	originalWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
}

// TestHook_DshTaskGuardNoTaskBlocks is the incident regression test (2026-08-22:
// a dsh session edited 4 pulse files on main with no task — the WARN reached the
// model and was ignored). The exact production path must now deny: payload
// forge_agent:"dsh" → task-guard advisory promoted → emitClaudeOutput block →
// *HookBlockError (exit 2), with a directive reason. The SECOND identical edit
// must STILL deny — under promotion the once-per-session NOWARN marker is a
// bypass (blind retry passes silently), so the Go layer sets
// FORGE_TASKGUARD_PROMOTED and the script emits the WARN every time.
//
// TestHook_DshTaskGuardNoTaskBlocks 是事件回归测试（2026-08-22：一个 dsh 会话
// 在 main 无任务改了 4 个 pulse 文件——WARN 到达模型却被无视）。现在这条生产
// 路径必须拒绝：payload forge_agent:"dsh" → task-guard advisory 被提升 →
// emitClaudeOutput 阻断 → *HookBlockError（exit 2），reason 为指令式文案。
// 第二次相同编辑仍须拒绝——提升语义下每会话一次的 NOWARN 标记是旁路（盲重试
// 静默放行），故 Go 层设置 FORGE_TASKGUARD_PROMOTED、脚本每次输出 WARN。
func TestHook_DshTaskGuardNoTaskBlocks(t *testing.T) {
	newTaskGuardProject(t)
	// Unique session id per run: the NOWARN/source-touched markers survive in the
	// real temp dir across test runs — a reused id would make the control test
	// flaky. Each run leaves 3 zero-byte marker files behind: acceptable (CI temp
	// dirs are ephemeral); redirecting TMPDIR would feed Windows-style paths into
	// the MSYS bash script (os.TempDir vs $TMPDIR divergence this repo works
	// around elsewhere) — riskier than the litter is large.
	//
	// 每次运行用唯一 session id：NOWARN/source-touched 标记在真实 temp 目录跨测
	// 试运行存活——复用 id 会让对照测试抖动。每次运行遗留 3 个 0 字节标记文件：
	// 可接受（CI 临时目录一次性）；改道 TMPDIR 会把 Windows 形式路径喂进 MSYS
	// bash 脚本（os.TempDir 与 $TMPDIR 分叉，本仓其他地方为此绕行）——风险大于
	// 遗留量。
	sess := fmt.Sprintf("dsh-e2e-%d", time.Now().UnixNano())

	for i := 1; i <= 2; i++ {
		stdout, stderr, err := runTaskGuardHookOnce(t, `"forge_agent":"dsh",`, sess)
		var blockErr *HookBlockError
		if !errors.As(err, &blockErr) {
			t.Fatalf("edit #%d must be denied (*HookBlockError → exit 2), got %T %v (stdout=%q stderr=%q)", i, err, err, stdout, stderr)
		}
		if !strings.Contains(blockErr.Reason, "DENIED") || !strings.Contains(blockErr.Reason, "forge task start") {
			t.Errorf("edit #%d block reason must be directive (DENIED + forge task start), got %q", i, blockErr.Reason)
		}
		if !strings.Contains(stderr, "[task-guard]") {
			t.Errorf("edit #%d stderr must carry the reason, got %q", i, stderr)
		}
		// dsh's runner parses stdout JSON: decision:block on PreToolUse.
		//
		// dsh 的 runner 解析 stdout JSON：PreToolUse 上为 decision:block。
		if !strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("edit #%d stdout must carry decision:block for the dsh runner, got %q", i, stdout)
		}
	}
}

// TestHook_TaskGuardScrubPromotedEnvOnClaude pins the finding-1 fix from this
// landing's review: if the outer environment already carries
// FORGE_TASKGUARD_PROMOTED=1 (leaked from a promoted-host run or set by hand),
// a NON-promoted host must not inherit it — the script would print the DENIED
// directive while the Go layer still passes the edit (copy without enforcement,
// the exact shape this change exists to kill). runHook therefore appends an
// empty FORGE_TASKGUARD_PROMOTED to the child env on non-promoted hosts, and
// os/exec env dedup keeps the LAST occurrence — the empty value shadows the
// inherited one.
//
// TestHook_TaskGuardScrubPromotedEnvOnClaude 钉住本次落地审查发现 1 的修复：
// 若外部环境已带 FORGE_TASKGUARD_PROMOTED=1（从提升宿主运行泄漏或手工设置），
// 非提升宿主不得继承——否则脚本会打印 DENIED 指令文案而 Go 层照样放行（有文案
// 无执法，正是本次变更要消灭的形状）。runHook 因此在非提升宿主上向子进程 env
// 追加空值 FORGE_TASKGUARD_PROMOTED，os/exec 的 env 去重保留最后一次出现——
// 空值压掉继承值。
func TestHook_TaskGuardScrubPromotedEnvOnClaude(t *testing.T) {
	newTaskGuardProject(t)
	t.Setenv("FORGE_TASKGUARD_PROMOTED", "1")
	sess := fmt.Sprintf("claude-scrub-%d", time.Now().UnixNano())

	stdout, _, err := runTaskGuardHookOnce(t, ``, sess)
	if err != nil {
		t.Fatalf("claude-compatible host must stay allowed even with promoted env residue, got %v", err)
	}
	if strings.Contains(stdout, "DENIED") {
		t.Errorf("inherited FORGE_TASKGUARD_PROMOTED=1 must be scrubbed on non-promoted hosts (DENIED copy without enforcement), got stdout %q", stdout)
	}
	if !strings.Contains(stdout, "[task-guard]") {
		t.Errorf("first edit should surface the normal WARN advisory, got stdout %q", stdout)
	}
}

// TestHook_TaskGuardAdvisoryOncePerSessionOnClaude pins the counterfactual: on a
// host WITHOUT promotion (claude-compatible default) the no-task source edit
// stays an allow — first edit WARNs into additionalContext, the second is
// silently allowed by the NOWARN marker. If this breaks, the de-noise (dogfood
// 3.1) was dropped everywhere instead of only on promoted hosts.
//
// TestHook_TaskGuardAdvisoryOncePerSessionOnClaude 钉住反事实：无提升的宿主
// （Claude 兼容默认）上无任务源码编辑保持放行——首次编辑 WARN 进
// additionalContext，第二次被 NOWARN 标记静默放行。若此测试破，说明去噪
// （dogfood 3.1）被全局删掉而非仅限提升宿主。
func TestHook_TaskGuardAdvisoryOncePerSessionOnClaude(t *testing.T) {
	newTaskGuardProject(t)
	sess := fmt.Sprintf("claude-e2e-%d", time.Now().UnixNano())

	// First edit: allow with the WARN as additionalContext (advisory, not block).
	//
	// 首次编辑：放行，WARN 作 additionalContext（advisory 非阻断）。
	stdout, _, err := runTaskGuardHookOnce(t, ``, sess)
	if err != nil {
		var blockErr *HookBlockError
		if errors.As(err, &blockErr) {
			t.Fatalf("claude-compatible host must not block, got block reason %q", blockErr.Reason)
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "[task-guard]") {
		t.Errorf("first edit should surface the WARN advisory, got stdout %q", stdout)
	}

	// Second identical edit: NOWARN marker set → silent allow.
	//
	// 第二次相同编辑：NOWARN 标记已置 → 静默放行。
	stdout, _, err = runTaskGuardHookOnce(t, ``, sess)
	if err != nil {
		t.Fatalf("second edit must stay allowed, got %v", err)
	}
	if stdout != "" {
		t.Errorf("second edit must be silent (NOWARN de-noise), got stdout %q", stdout)
	}
}
