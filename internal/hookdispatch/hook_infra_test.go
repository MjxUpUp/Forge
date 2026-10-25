package hookdispatch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// hook-infra 收口测试（docs/plans/hook-silent-failure-visibility-2026-10.md）：
// 退出码映射、同会话去重、checklog 行（shown/suppressed）、逃生口审计行。

// isolateInfra 密闭去重状态（$TMPDIR）并复位本进程的会话记忆。
func isolateInfra(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	t.Setenv("FORGE_HOOK_INFRA_DEDUP", "")
	infraSession = ""
	t.Cleanup(func() { infraSession = "" })
}

func infraRows(t *testing.T, root string) (infra, escape []checklog.Entry) {
	t.Helper()
	all, err := checklog.LoadAll(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range all {
		switch e.Check {
		case checklog.CheckHookInfra:
			infra = append(infra, e)
		case checklog.CheckEscapeHatch:
			if checklog.EscapeGateOf(&e) == "hook-infra-dedup" {
				escape = append(escape, e)
			}
		}
	}
	return infra, escape
}

// TestHandleHookFailure_DedupAndAudit：同会话同因首次 exit 1、重复 exit 0；每次发生都落
// 一行 checklog（shown=true 一行、shown=false 其余），Meta 带 cause/subject。
func TestHandleHookFailure_DedupAndAudit(t *testing.T) {
	isolateInfra(t)
	root := newHookProject(t)
	rememberInfraSession("sess-audit")

	err := &UnknownHookError{Name: "doc-lint"}
	codes := []int{HandleHookFailure(err), HandleHookFailure(err), HandleHookFailure(err)}
	if codes[0] != 1 || codes[1] != 0 || codes[2] != 0 {
		t.Fatalf("退出码应为 1,0,0，got %v", codes)
	}
	rows, esc := infraRows(t, root)
	if len(rows) != 3 || len(esc) != 0 {
		t.Fatalf("应 3 行 hook-infra、0 行逃生行，got %d / %d", len(rows), len(esc))
	}
	shown := 0
	for _, r := range rows {
		if r.Meta["cause"] != "unknown-hook" || r.Meta["subject"] != "doc-lint" || r.Meta["dedup"] != "on" {
			t.Errorf("Meta 应带 cause/subject/dedup，got %v", r.Meta)
		}
		if r.Meta["shown"] == "true" {
			shown++
		}
		if r.SessionID != "sess-audit" {
			t.Errorf("行应带归一后的会话 id，got %q", r.SessionID)
		}
	}
	if shown != 1 {
		t.Errorf("每个 (会话, 原因) 恰好一行 shown=true，got %d", shown)
	}

	// 不同会话：同因重新提示。
	rememberInfraSession("sess-other")
	if got := HandleHookFailure(err); got != 1 {
		t.Errorf("新会话同因应重新 exit 1，got %d", got)
	}
}

// TestHandleHookFailure_EscapeHatch：FORGE_HOOK_INFRA_DEDUP=0 恢复逐次 exit 1，且每次
// 落 hook-infra 行（dedup=off）+ 逃生行（escape.gate=hook-infra-dedup）供审计。
func TestHandleHookFailure_EscapeHatch(t *testing.T) {
	isolateInfra(t)
	root := newHookProject(t)
	rememberInfraSession("sess-esc")
	t.Setenv("FORGE_HOOK_INFRA_DEDUP", "0")

	err := errors.New("boom")
	for i := 0; i < 3; i++ {
		if got := HandleHookFailure(err); got != 1 {
			t.Fatalf("去重关闭时每次应 exit 1，第 %d 次 got %d", i+1, got)
		}
	}
	rows, esc := infraRows(t, root)
	if len(rows) != 3 || len(esc) != 3 {
		t.Fatalf("关闭去重时每次应落 hook-infra 行与逃生行，got %d / %d", len(rows), len(esc))
	}
	for _, r := range rows {
		if r.Meta["dedup"] != "off" || r.Meta["shown"] != "true" {
			t.Errorf("逃生口态每次都 shown，且 dedup=off，got %v", r.Meta)
		}
	}
}

// TestHandleHookPanic_Dedup：panic 路径同样去重，且从不返回 2。
func TestHandleHookPanic_Dedup(t *testing.T) {
	isolateInfra(t)
	newHookProject(t)
	rememberInfraSession("sess-panic")
	if got := HandleHookPanic("index out of range"); got != 1 {
		t.Errorf("panic 首次应 exit 1，got %d", got)
	}
	if got := HandleHookPanic("index out of range"); got != 0 {
		t.Errorf("同因 panic 重复应 exit 0，got %d", got)
	}
	if got := HandleHookPanic("a different panic"); got != 1 {
		t.Errorf("不同 panic 应各提示一次，got %d", got)
	}
}

// TestRunHook_UnknownHookTypedAndSession：unknown hook 返回类型化错误，且从 stdin 的
// session_id 为去重定键（stdin 在拒绝前被尽力读取）。
func TestRunHook_UnknownHookTypedAndSession(t *testing.T) {
	isolateInfra(t)
	newHookProject(t)
	p := filepath.Join(t.TempDir(), "stdin.json")
	if err := os.WriteFile(p, []byte(`{"session_id":"abc.def","hook_event_name":"PostToolUse"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = old; f.Close() }()

	rerr := RunHook(nil, []string{"nonexistent-hook"})
	var unknown *UnknownHookError
	if !errors.As(rerr, &unknown) || unknown.Name != "nonexistent-hook" {
		t.Fatalf("应返回 *UnknownHookError{nonexistent-hook}，got %T %v", rerr, rerr)
	}
	if want := "abc_def"; infraSession != want {
		t.Errorf("session id 应经 SanitizeSessionID 归一后记下，want %q got %q", want, infraSession)
	}
	if got := fmt.Sprint(rerr); got != "unknown hook: nonexistent-hook" {
		t.Errorf("历史文本应保持，got %q", got)
	}
}
