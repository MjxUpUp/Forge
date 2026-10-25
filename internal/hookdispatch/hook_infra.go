package hookdispatch

// hook_infra.go — hook 基础设施失败的收口：把「门禁裁决」与「基础设施失败」分开
// （docs/plans/hook-silent-failure-visibility-2026-10.md）。
//
// 裁决（*HookBlockError）由 Execute 先行映射 exit 2，不经本文件。其余一切——
// unknown hook、panic、hook 返回的非阻断错误——都是基础设施失败：宿主只会把它们
// 当「非阻塞错误」逐次刷屏。本文件让它们：每次落一行 checklog（shown / suppressed
// 可审计）；同一会话同一原因第一次输出带修复办法的提示并 exit 1（宿主把 stderr
// 展示给用户），之后同因静默 exit 0。退出码只会是 0 或 1，从不是 2——咨询类 hook
// 永远 fail-open（INV-1）。FORGE_HOOK_INFRA_DEDUP=0 恢复逐次 exit 1。
//
// hook_infra.go — funnel for hook infrastructure failures, separating gate
// verdicts from infra failures. Verdicts (*HookBlockError) are mapped to exit 2
// by Execute before reaching here; everything else — unknown hook, panic, a
// non-block hook error — is infra, which hosts surface only as per-call
// "non-blocking errors". Each occurrence lands a checklog row (shown vs
// suppressed, auditable); the first per (session, cause) prints a fix-bearing
// notice and exits 1 (the host shows stderr to the user), repeats stay silent
// with exit 0. The exit code is only ever 0 or 1, never 2 — advisory hooks
// always fail open (INV-1). FORGE_HOOK_INFRA_DEDUP=0 restores per-call exit 1.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/util"
)

// UnknownHookError is returned when the wired hook name is not known to this
// binary (settings written by a different forge version). Typed so the infra
// funnel classifies it without string matching.
//
// UnknownHookError 在接线的 hook 名本二进制不认识时返回（settings 由另一个 forge
// 版本写入）。类型化，让基础设施收口不必靠字符串匹配分类。
type UnknownHookError struct{ Name string }

// Error keeps the historical "unknown hook: <name>" text.
//
// Error 保持历史文本 "unknown hook: <name>"。
func (e *UnknownHookError) Error() string { return "unknown hook: " + e.Name }

// infraSession is this hook process's sanitized session id, remembered once the
// payload is parsed so the Execute-level funnel (which runs after RunHook has
// returned or panicked) can key the once-per-session dedup. Empty = unknown; the
// dedup bucket then falls back to the parent process.
//
// infraSession 是本 hook 进程已归一的 session id：payload 解析后记下，使
// Execute 层的收口（在 RunHook 返回或 panic 之后运行）能给同会话去重定键。空 =
// 未知；去重桶随后回落到父进程。
var infraSession string

// rememberInfraSession records the session id for the infra funnel.
//
// rememberInfraSession 为基础设施收口记下 session id。
func rememberInfraSession(sessionID string) {
	if sessionID != "" {
		infraSession = util.SanitizeSessionID(sessionID)
	}
}

// peekInfraSession best-effort reads the hook payload's session id on the
// unknown-hook path, which rejects before RunHook's normal stdin parse. A
// terminal stdin (manual run) is skipped — reading it would block.
//
// peekInfraSession 在 unknown-hook 路径上尽力读 hook payload 的 session id——该
// 路径在 RunHook 的常规 stdin 解析之前就拒绝了。终端 stdin（手工运行）跳过——读它
// 会阻塞。
func peekInfraSession() {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice != 0 {
		return
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil || len(data) == 0 {
		return
	}
	var p struct {
		SessionID      string `json:"session_id"`
		ConversationID string `json:"conversation_id"`
	}
	if json.Unmarshal(data, &p) != nil {
		return
	}
	if p.SessionID == "" {
		p.SessionID = p.ConversationID
	}
	rememberInfraSession(p.SessionID)
}

// HandleHookFailure is Execute's funnel for a non-nil error from a `forge hook`
// invocation and returns the process exit code. A block verdict maps to 2
// untouched (defensive — Execute already routes those); every other error is an
// infra failure: 1 on its first occurrence in the session (notice on stderr), 0
// on repeats.
//
// HandleHookFailure 是 Execute 对 `forge hook` 调用返回非 nil error 的收口，返回
// 进程退出码。阻断裁决原样映射 2（防御性——Execute 已先行分流）；其余错误都是基础
// 设施失败：本会话首次出现 → 1（stderr 提示），重复 → 0。
func HandleHookFailure(err error) int {
	var blockErr *HookBlockError
	if errors.As(err, &blockErr) {
		return 2
	}
	var unknown *UnknownHookError
	if errors.As(err, &unknown) {
		return handleInfraFailure(hooks.InfraFailure{Cause: hooks.InfraUnknownHook, Subject: unknown.Name, Detail: err.Error()})
	}
	return handleInfraFailure(hooks.InfraFailure{Cause: hooks.InfraHookError, Subject: firstLine(err.Error()), Detail: err.Error()})
}

// HandleHookPanic is Execute's panic funnel for a `forge hook` invocation:
// same dedup semantics, exit code 1 first / 0 repeats (never 2 — an internal
// crash must read as infrastructure, not as a gate verdict).
//
// HandleHookPanic 是 Execute 对 `forge hook` 调用 panic 的收口：去重语义相同，
// 首次退出码 1、重复 0（绝不是 2——内部崩溃必须读作基础设施而非门禁裁决）。
func HandleHookPanic(r any) int {
	msg := fmt.Sprint(r)
	return handleInfraFailure(hooks.InfraFailure{Cause: hooks.InfraPanic, Subject: firstLine(msg), Detail: "forge: internal panic: " + msg})
}

// handleInfraFailure applies dedup, records the checklog row, prints the notice
// on first occurrence, and returns the exit code.
//
// handleInfraFailure 施加去重、落 checklog 行、首次出现时打印提示，并返回退出码。
func handleInfraFailure(f hooks.InfraFailure) (code int) {
	// 收口自身出错（状态目录、checklog……）绝不能升级成 exit 2：未捕获的 panic 在 Go 里
	// 退出码就是 2，等于硬拦。兜底返回 1（非阻断）。
	//
	// A failure inside the funnel itself (state dir, checklog, ...) must never
	// escalate to exit 2: an uncaught panic exits 2 in Go, which would hard-block.
	// Fall back to 1 (non-blocking).
	defer func() {
		if r := recover(); r != nil {
			code = 1
		}
	}()
	dedup := hooks.InfraDedupEnabled()
	first := true
	if dedup {
		first = hooks.ClaimInfraNotice(infraSession, f)
	}
	recordInfraFailure(f, first, dedup)
	if !first {
		return 0
	}
	if dedup {
		fmt.Fprintln(os.Stderr, hooks.FormatInfraNotice(f))
	} else {
		// 逃生口态：保持修复前的逐次原始报错。
		fmt.Fprintln(os.Stderr, f.Detail)
	}
	return 1
}

// recordInfraFailure lands the audit row (best-effort — a checklog failure must
// not change the hook outcome). No project root = not a forge project: skip.
//
// recordInfraFailure 落审计行（尽力而为——checklog 失败不得改变 hook 结果）。无项目
// 根 = 非 forge 项目：跳过。
func recordInfraFailure(f hooks.InfraFailure, shown, dedup bool) {
	root, err := projectroot.Find()
	if err != nil || root == "" {
		return
	}
	dedupState := "on"
	if !dedup {
		dedupState = "off"
	}
	entry := &checklog.Entry{
		Check:     checklog.CheckHookInfra,
		Passed:    false,
		Checked:   true,
		Level:     checklog.LevelWarn,
		SessionID: infraSession,
		Detail:    util.TruncateRunes(fmt.Sprintf("hook-infra: cause=%s subject=%s shown=%t dedup=%s", f.Cause, f.Subject, shown, dedupState), 400),
		Source:    checklog.EvidenceDeterministic,
		Meta: map[string]string{
			"cause":   string(f.Cause),
			"subject": util.TruncateRunes(f.Subject, 120),
			"shown":   fmt.Sprintf("%t", shown),
			"dedup":   dedupState,
		},
	}
	_ = checklog.Record(root, entry)
	if !dedup {
		// 逃生口必须落审计：FORGE_HOOK_INFRA_DEDUP=0 逐次放大报错，与其他逃生口同纪律。
		esc := checklog.EscapeHatchEntry("hook-infra-dedup", checklog.EscapeReasonEnv, "env",
			"escape-hatch: hook 基础设施失败去重已关闭(env "+hooks.InfraDedupEnvName()+"=0)，恢复逐次报错")
		esc.SessionID = infraSession
		esc.Source = checklog.EvidenceDeterministic
		_ = checklog.Record(root, esc)
	}
}

// firstLine returns the trimmed first line of s, the stable part of an error
// for use as a dedup subject.
//
// firstLine 返回 s 去空白后的首行，即报错里可作去重 subject 的稳定部分。
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
