package hooks_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hookdispatch"
)

// withStdin 把 payload 写进临时文件并换载 os.Stdin（普通文件——不是终端，
// peekInfraSession/RunHook 的 ReadAll 都不会阻塞）。
func withStdin(t *testing.T, payload string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "stdin.json")
	if err := os.WriteFile(p, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = old
		f.Close()
	})
}

// TestAdvisoryHookNeverBlocks 钉死 INV-1：咨询类 hook 仍不阻断、退出码语义不变——
// 基础设施失败路径（unknown hook、panic、一般 hook 错误）的退出码只会是 0 或 1，
// 从不是 2（阻断码）；进程内咨询 hook 面对垃圾 stdin 也只会放行或报非阻断错误；
// 去重开/关两态都成立；同会话重复由 1 降为 0（更宽松，从不更严）。
func TestAdvisoryHookNeverBlocks(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FORGE_DATA_HOME", tmp)
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	t.Setenv("FORGE_PROFILE", "standard")
	t.Setenv("FORGE_HOOK_INFRA_DEDUP", "")
	// 非 forge 目录：project-scoped hook 提前静默放行，不碰任何真实状态。
	wd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	// 1. unknown hook：类型化错误、非阻断；同会话 1 → 0。
	withStdin(t, `{"session_id":"adv-never-blocks","hook_event_name":"PostToolUse"}`)
	err := hookdispatch.RunHook(hookdispatch.HookCmd, []string{"hook-from-the-future"})
	var unknown *hookdispatch.UnknownHookError
	if !errors.As(err, &unknown) {
		t.Fatalf("unknown hook 应返回 *UnknownHookError，got %T %v", err, err)
	}
	var block *hookdispatch.HookBlockError
	if errors.As(err, &block) {
		t.Fatal("unknown hook 不得是阻断裁决")
	}
	if got := hookdispatch.HandleHookFailure(err); got != 1 {
		t.Errorf("同会话首次应 exit 1（带提示、不阻断），got %d", got)
	}
	for i := 0; i < 3; i++ {
		if got := hookdispatch.HandleHookFailure(err); got != 0 {
			t.Errorf("同会话重复应静默 exit 0，got %d", got)
		}
	}

	// 2. panic 与一般 hook 错误：同样只会是 0/1。
	for name, run := range map[string]func() int{
		"panic":      func() int { return hookdispatch.HandleHookPanic("boom") },
		"hook-error": func() int { return hookdispatch.HandleHookFailure(fmt.Errorf("batch stdin 读取失败: boom")) },
	} {
		for i := 0; i < 2; i++ {
			if got := run(); got != 0 && got != 1 {
				t.Errorf("%s 第 %d 次退出码 %d：基础设施失败只会是 0 或 1，从不是 2", name, i+1, got)
			}
		}
	}

	// 3. 逃生口态（去重关闭）：恢复逐次 exit 1，仍从不是 2。
	t.Setenv("FORGE_HOOK_INFRA_DEDUP", "0")
	for i := 0; i < 3; i++ {
		if got := hookdispatch.HandleHookFailure(err); got != 1 {
			t.Errorf("去重关闭时应逐次 exit 1，第 %d 次 got %d", i+1, got)
		}
	}
	t.Setenv("FORGE_HOOK_INFRA_DEDUP", "")

	// 4. 阻断裁决不被收口吞掉：HookBlockError 仍映射 exit 2（裁决 ≠ 基础设施）。
	if got := hookdispatch.HandleHookFailure(&hookdispatch.HookBlockError{}); got != 2 {
		t.Errorf("门禁裁决应保持 exit 2，got %d", got)
	}

	// 5. 进程内咨询 hook × 垃圾 stdin：只放行或报非阻断错误。
	for _, name := range []string{"doc-lint", "test-nudge", "failure-track", "subagent-track", "conventions-context", "conventions-write"} {
		withStdin(t, `{garbage`)
		herr := hookdispatch.RunHook(hookdispatch.HookCmd, []string{name})
		if herr == nil {
			continue
		}
		if errors.As(herr, &block) {
			t.Errorf("咨询 hook %s 面对垃圾 stdin 不得阻断：%v", name, herr)
		}
		if got := hookdispatch.HandleHookFailure(herr); got > 1 {
			t.Errorf("咨询 hook %s 失败的退出码 %d：只会是 0 或 1", name, got)
		}
	}
}
