package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/spf13/cobra"
)

// 本文件钉住 dsh advisory 提升落地（2026-08-22 事件：task-guard 的 WARN 经
// agent.inject 到达了 dsh 模型 inbox 却被无视——其文案自述「allowed」，且所有
// 下游门禁都 task-scoped）。dsh 仅注册 task-guard（hostcap PromoteAdvisory 的
// 准入路径 (b)：通道送达，需要执法），故下方范围钉死 bash-guard 与
// assertion-check 在 dsh 上**不**提升。

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
	// 未知宿主保持 advisory（Claude 兼容默认）。
	if promoteAdvisory("claude-code", "task-guard", true, "[task-guard] No active task.") {
		t.Error("claude-code must not promote (advisory channel suffices)")
	}
}

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
		// kimi 旧开关不得泄漏到其他宿主。
		if !promoteAdvisory("dsh", "task-guard", true, "[task-guard] No active task.") {
			t.Error("FORGE_KIMI_ADVISORY=soft is kimi-scoped; dsh promotion must stay active")
		}
	})
}

// TestTaskGuardPromotionActive 钉住 runHook 用来设 FORGE_TASKGUARD_PROMOTED 的
// env 预配置谓词：恰在宿主持有 task-guard 规则且逃生舱全关时为真——env 已设而
// 提升被抑制会复活 139 次 WARN 刷屏（dogfood 3.1）且背后无执法。
func TestTaskGuardPromotionActive(t *testing.T) {
	cases := []struct {
		agent string
		want  bool
	}{
		{"dsh", true},
		{"kimi", false}, // kimi's rules retired 2026-08-24 — advisories queue, never block
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
		// 最小 cobra root：Go 内 hook 分派读 cmd.Root().Version，nil 会空指针
		// （同 runHookWithStdin 的注释）。
		return runHook(&cobra.Command{}, []string{"task-guard"})
	})
}

// newTaskGuardProject 构建本文件 E2E 共用的隔离 fixture：全新 forge 项目 +
// 隔离 DataHome + 清空的 agent env（归因只受被测 payload 驱动）。返回项目根
// （测试期间的 cwd），供调用方推导 FORGE_DATA_DIR 解析出的路径。
func newTaskGuardProject(t *testing.T) string {
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
	return root
}

// TestHook_TaskGuardNowarnMarkerLivesInDataDir 钉住 marker 根目录迁移
// （2026-08-23）：NOWARN 标记必须落在 FORGE_DATA_DIR（Go 层注入——forge 自己
// 的可写 data home）下，而非 ${TMPDIR:-/tmp}。在 MSYS /tmp 只读的机器上
// （Git for Windows 默认装进 Program Files 即如此），`touch ... 2>/dev/null
// || true` 静默吞掉写错误，标记永不落盘，每会话一次的去噪退化成每次编辑都
// WARN——恰是 dogfood 3.1 要消灭的刷屏。AdvisoryOncePerSessionOnClaude 在这
// 类机器上红（CI 的 /tmp 可写故绿）；本测试不依赖机器 tmpdir 权限，独立钉住
// 根因修复。
func TestHook_TaskGuardNowarnMarkerLivesInDataDir(t *testing.T) {
	root := newTaskGuardProject(t)
	sess := fmt.Sprintf("claude-datadir-%d", time.Now().UnixNano())

	stdout, _, err := runTaskGuardHookOnce(t, ``, sess)
	if err != nil {
		var blockErr *HookBlockError
		if errors.As(err, &blockErr) {
			t.Fatalf("claude-compatible host must not block, got block reason %q", blockErr.Reason)
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "[task-guard]") {
		t.Fatalf("first edit should surface the WARN advisory, got stdout %q", stdout)
	}

	// 标记必须存在于 Go 层解析的 DataDir（经 FORGE_DATA_DIR 传入）的 markers/
	// 子目录下，按 sanitize 后的 session id 键控——用前缀查找而非精确名
	// （sanitizeForShell 可能改写 id）。
	markersDir := filepath.Join(forgedata.DataDirFor(root), "markers")
	entries, derr := os.ReadDir(markersDir)
	if derr != nil {
		t.Fatalf("read markers dir %s: %v", markersDir, derr)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "forge-taskguard-nowarn-") {
			found = true
		}
	}
	if !found {
		t.Errorf("NOWARN marker must live under FORGE_DATA_DIR/markers (%s) — on read-only-MSYS-/tmp machines the old TMPDIR root silently lost it and de-noise degraded to per-edit WARN spam", markersDir)
	}
}

// TestHook_DshTaskGuardNoTaskBlocks 是事件回归测试（2026-08-22：一个 dsh 会话
// 在 main 无任务改了 4 个 pulse 文件——WARN 到达模型却被无视）。现在这条生产
// 路径必须拒绝：payload forge_agent:"dsh" → task-guard advisory 被提升 →
// emitClaudeOutput 阻断 → *HookBlockError（exit 2），reason 为指令式文案。
// 第二次相同编辑仍须拒绝——提升语义下每会话一次的 NOWARN 标记是旁路（盲重试
// 静默放行），故 Go 层设置 FORGE_TASKGUARD_PROMOTED、脚本每次输出 WARN。
func TestHook_DshTaskGuardNoTaskBlocks(t *testing.T) {
	newTaskGuardProject(t)
	// 每次运行用唯一 session id：NOWARN/source-touched 标记跨测试运行存活——复用
	// id 会让对照测试抖动。2026-08-23 marker 根迁移后它们落在 fixture 隔离的
	// FORGE_DATA_HOME（t.TempDir，自动清理）里，每运行的遗留不复存在；唯一 id
	// 保留用于运行间隔离，且被测的去噪语义本就是按会话的。
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
		// dsh 的 runner 解析 stdout JSON：PreToolUse 上为 decision:block。
		if !strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("edit #%d stdout must carry decision:block for the dsh runner, got %q", i, stdout)
		}
	}
}

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

// TestHook_TaskGuardAdvisoryOncePerSessionOnClaude 钉住反事实：无提升的宿主
// （Claude 兼容默认）上无任务源码编辑保持放行——首次编辑 WARN 进
// additionalContext，第二次被 NOWARN 标记静默放行。若此测试破，说明去噪
// （dogfood 3.1）被全局删掉而非仅限提升宿主。
func TestHook_TaskGuardAdvisoryOncePerSessionOnClaude(t *testing.T) {
	newTaskGuardProject(t)
	sess := fmt.Sprintf("claude-e2e-%d", time.Now().UnixNano())

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

	// 第二次相同编辑：NOWARN 标记已置 → 静默放行。
	stdout, _, err = runTaskGuardHookOnce(t, ``, sess)
	if err != nil {
		t.Fatalf("second edit must stay allowed, got %v", err)
	}
	if stdout != "" {
		t.Errorf("second edit must be silent (NOWARN de-noise), got stdout %q", stdout)
	}
}
