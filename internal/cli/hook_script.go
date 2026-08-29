package cli

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/shellexec"
	"github.com/MjxUpUp/Forge/internal/util"
)

// projectTagFor returns a stable hex tag for a given project root. By hashing the canonical
// (absolute, cleaned) path, the tag stays invariant across path case, drive letter format, and symlinks —
// whereas $PWD cksum also depends on the host's cksum format (GNU vs BSD). The hook reads it via
// the FORGE_PROJECT_TAG env var to isolate state per project.
//
// projectTagFor 为给定 project root 返回稳定的 hex tag。通过对 canonical
// （绝对、clean 后的）路径做哈希，使 tag 在路径大小写、盘符格式、symlink 之间保持
// 不变——而 $PWD cksum 还依赖宿主的 cksum 格式（GNU vs BSD）。hook 通过
// FORGE_PROJECT_TAG env var 读取它来按 project 隔离状态。
func projectTagFor(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	h := fnv.New64a()
	h.Write([]byte(filepath.Clean(abs)))
	return strconv.FormatUint(h.Sum64(), 16)
}

// suggestTagFor returns the init-suggest marker tag for a directory, keyed by its git root,
// so no matter which subdir the agent runs `forge suggest decline` from, the same project is tagged only
// once. This guards the decline contract: previously keyed by cwd, declining from a subdir would write a different tag
// than the hook read at the project root, silently making decline a no-op. Non-git directories fall back to
// projectTagFor(dir) (still a stable per-dir tag). Shared by the init-suggest hook
// (FORGE_CWD_TAG) and `forge suggest` — both must produce the same tag for the same project.
//
// suggestTagFor 返回某目录的 init-suggest marker tag，按其 git root 作 key，
// 这样无论 agent 从哪个 subdir 执行 `forge suggest decline`，同一 project 只会被
// tag 一次。这守护 decline 契约：此前按 cwd 作 key，从 subdir decline 会写出与
// hook 在 project root 读到的不同的 tag，使 decline 静默 no-op。非 git 目录回退到
// projectTagFor(dir)（仍是稳定的 per-dir tag）。由 init-suggest hook
// （FORGE_CWD_TAG）和 `forge suggest` 共用——两者对同一 project 必须产出相同的
// tag。
func suggestTagFor(dir string) string {
	if root := forgedata.FindGitRoot(dir); root != "" {
		return projectTagFor(root)
	}
	return projectTagFor(dir)
}

// maxEnvValueLen is the maximum length of an env var value passed to the bash script,
// used to prevent memory issues.
//
// maxEnvValueLen 是传给 bash 脚本的 env var value 的最大长度，
// 用于防止内存问题。
const maxEnvValueLen = 100000

// readsFilePath returns the absolute path of this session's reads log — the PreToolUse
// read-before-edit hook (scheme 2 shift-left) greps it to intercept Edit-without-Read via this
// on-disk side channel. Per-session (keyed by sanitized session id), ephemeral
// ($TMPDIR). Persisted to disk rather than carried in context so it SURVIVES compaction within a session:
// a Read before compaction still counts toward an Edit after it, eliminating the biggest false-positive source of context-based checks.
//
// readsFilePath 返回本 session 的 reads log 绝对路径——PreToolUse
// read-before-edit hook（方案2 shift-left）grep 它来拦截 Edit-without-Read 的
// 磁盘 side-channel。Per-session（按 sanitized session id 作 key）、ephemeral
// （$TMPDIR）。落盘而非存于 context，是为了在 session 内 SURVIVES compaction：
// compact 之前的 Read 仍计入之后的 Edit，消除基于 context 检查的最大假阳性来源。
func readsFilePath(root, sessionID string) string {
	// projectTagFor(root) buckets the reads log by project: $TMPDIR is shared across projects, and naming by session id alone
	// would let project A's reads log be read by project B under short/reused session ids (e.g. test sid-*) —
	// the read-before-edit hook would then falsely conclude an Edit had been Read (false-positive pass). The project tag is fnv hex
	// (filename-safe), sourced identically with FORGE_PROJECT_TAG.
	//
	// projectTagFor(root) 把 reads log 按 project 分桶：$TMPDIR 跨项目共享，仅按 session id
	// 命名会在短/复用 session id（如测试 sid-*）下让 A 项目的 reads log 被 B 项目读到——
	// read-before-edit hook 会误判 Edit 已 Read 过（假阳性放行）。project tag 是 fnv hex
	// （文件名安全），与 FORGE_PROJECT_TAG 同源。
	return filepath.Join(os.TempDir(), "forge-session-reads-"+projectTagFor(root)+"-"+readsFileKey(sessionID)+".log")
}

// readsFileKey collapses a session id into a filename-safe token. SanitizeSessionID
// preserves readability but may still contain characters that file systems treat specially on some platforms; any character
// outside [A-Za-z0-9._-] is collapsed to '_' so the temp file name is always safe and the original id is not leaked into $TMPDIR.
//
// readsFileKey 把 session id 收敛为 filename-safe 的 token。SanitizeSessionID
// 保留可读性，但仍可能含某些平台上被文件系统特殊对待的字符；将 [A-Za-z0-9._-]
// 之外的字符一律折叠为 '_'，使临时文件名始终安全，且不把原始 id 泄漏到 $TMPDIR。
func readsFileKey(sessionID string) string {
	// Defensive: pin the empty-input token at the cli layer. util.SanitizeSessionID
	// is shared with other packages whose fallback semantics may evolve (it now
	// returns "session" for ""); the reads-log filename contract here stays
	// "default" for an empty session id regardless.
	//
	// 防御式：空输入 token 钉在 cli 层。util.SanitizeSessionID 与其他包共享，
	// 其兜底语义可能演进（现在 "" 返回 "session"）；本处 reads-log 文件名契约
	// 对空 session id 保持 "default"。
	if sessionID == "" {
		return "default"
	}
	s := util.SanitizeSessionID(sessionID)
	if s == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

// appendSessionRead appends a repo-relative Read path to the per-session reads log.
// Best-effort (advisory side channel): a write failure only means the read-before-edit hook
// will not see this Read — it must never cause the tool call to fail.
//
// appendSessionRead 把 repo-relative 的 Read 路径追加到 per-session reads log。
// Best-effort（advisory side-channel）：写入失败仅意味着 read-before-edit hook
// 看不到这次 Read——绝不能让 tool call 因此失败。
func appendSessionRead(path, relPath string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		// A dropped Read record later turns a legitimate Edit into an unexplained
		// read-before-edit false block — leave a trace so that is attributable.
		fmt.Fprintf(os.Stderr, "[forge] warning: session reads-log append failed (%s): %v\n", relPath, err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(relPath + "\n"); err != nil {
		fmt.Fprintf(os.Stderr, "[forge] warning: session reads-log append failed (%s): %v\n", relPath, err)
	}
}

// sanitizeForShell sanitizes a string into a form safe for use as a shell env var. Prevents
// shell injection when user-controlled content reaches a bash script via an env var.
//
// sanitizeForShell 把字符串净化为可安全用于 shell env var 的形式。防止
// user-controlled 内容经 env var 传入 bash 脚本时发生 shell injection。
//
// Strategy:
//   - Truncate to maxEnvValueLen to prevent memory exhaustion
//   - Replace NULL bytes and control characters (except tab, newline, carriage return)
//   - Unicode-safe validation (reject invalid UTF-8)
//   - No quoting or escaping — callers must use export VAR=$value themselves and double-quote the value
//
// 策略：
//   - 截断到 maxEnvValueLen，防内存耗尽
//   - 替换 NULL 字节和控制字符（tab、newline、carriage return 除外）
//   - Unicode-safe 校验（拒绝非法 UTF-8）
//   - 不做引号或转义——调用方须自行用 export VAR=$value 并给 value 加双引号
//
// Note: this is a defense-in-depth measure. The hook script itself should also validate input before use.
//
// 注意：这是 defense-in-depth 措施。hook 脚本自身在使用前也应校验输入。
func sanitizeForShell(value string) string {
	if value == "" {
		return ""
	}

	// Truncate to prevent memory issues.
	//
	// 截断以防内存问题
	if len(value) > maxEnvValueLen {
		// Truncate at a UTF-8 boundary.
		//
		// 在 UTF-8 边界处截断
		for offset := maxEnvValueLen - 10; offset < maxEnvValueLen; offset++ {
			if offset >= len(value) {
				break
			}
			if utf8.RuneStart(value[offset]) {
				value = value[:offset]
				break
			}
		}
		// Fallback: invalid UTF-8 in the 10-byte window can leave no RuneStart,
		// so the loop above finishes without truncating and the overlong value
		// would reach the env unchanged. Hard-truncate at the limit instead.
		//
		// 兜底：10 字节窗口内含非法 UTF-8 时可能找不到 RuneStart，循环走完不
		// 截断，超长 value 会原样进 env。改为在限制处硬截断。
		if len(value) > maxEnvValueLen {
			value = value[:maxEnvValueLen]
		}
	}

	// Validate UTF-8 and remove control characters.
	//
	// 校验 UTF-8 并移除控制字符
	var result strings.Builder
	result.Grow(len(value))

	for _, r := range value {
		// Check UTF-8 validity.
		//
		// 检查 UTF-8 合法性
		if r == utf8.RuneError {
			// Skip invalid runes.
			//
			// 跳过非法 rune
			continue
		}

		// Remove NULL bytes and most control characters.
		// Allow: tab (0x09), newline (0x0A), carriage return (0x0D).
		// Block: NULL (0x00) and other control characters (0x01-0x08, 0x0B-0x0C, 0x0E-0x1F).
		//
		// 移除 NULL 字节和大多数控制字符
		// 放行：tab (0x09)、newline (0x0A)、carriage return (0x0D)
		// 拦截：NULL (0x00) 及其他控制字符 (0x01-0x08、0x0B-0x0C、0x0E-0x1F)
		if r == 0 {
			// Replace NULL with a space.
			//
			// NULL 替换为空格
			result.WriteRune(' ')
			continue
		}
		if r < 0x20 && r != 0x09 && r != 0x0A && r != 0x0D {
			// Skip other control characters.
			//
			// 跳过其他控制字符
			continue
		}

		result.WriteRune(r)
	}

	return result.String()
}

// extractDetail parses output of PASS/WARN/FAIL with optional detail. Returns the
// detail part after the keyword; if the output does not start with a known prefix, returns the full output.
//
// extractDetail 解析 PASS/WARN/FAIL 加可选 detail 的输出。返回关键字之后的
// detail 部分；若不以已知前缀开头，则返回完整输出。
func extractDetail(stdout, prefix string) string {
	if stdout == "" {
		return ""
	}
	for _, p := range []string{prefix, "WARN"} {
		after, ok := strings.CutPrefix(stdout, p)
		if ok {
			return strings.TrimSpace(after)
		}
	}
	return stdout
}

// applyPatchFilePath extracts the first target path from a codex apply_patch payload.
// The patch headers are `*** Add File: <path>` / `*** Update File: <path>` /
// `*** Delete File: <path>`; the first header wins for multi-file patches (the common
// case is single-file). Returns "" when no header is present (malformed/unrelated
// command) — the hooks then see an empty path, same as today.
//
// applyPatchFilePath 从 codex apply_patch payload 抽取第一个目标路径。patch 头是
// `*** Add File: <path>` / `*** Update File: <path>` / `*** Delete File: <path>`；
// 多文件 patch 取第一个头（常见情形是单文件）。无头（畸形/无关命令）返回 ""——
// hook 于是看到空路径，与现状一致。
func applyPatchFilePath(patch string) string {
	for _, line := range strings.Split(patch, "\n") {
		body := strings.TrimPrefix(strings.TrimSpace(line), "*** ")
		for _, header := range []string{"Add File:", "Update File:", "Delete File:"} {
			if strings.HasPrefix(body, header) {
				if p := strings.TrimSpace(strings.TrimPrefix(body, header)); p != "" {
					return p
				}
			}
		}
	}
	return ""
}

// findBash resolves the bash interpreter for hook scripts. The implementation
// (including the Windows WSL-avoidance logic) lives in internal/shellexec and is
// shared with the gate path (taskpipeline.runEmbeddedHook) — a bare PATH lookup
// there resolved to WSL bash and failed every gate auto-compile with
// 'forge-gate-*.sh: No such file or directory'.
//
// findBash 解析 hook 脚本的 bash 解释器。实现（含 Windows WSL 规避逻辑）在
// internal/shellexec，与 gate 路径（taskpipeline.runEmbeddedHook）共用——那里
// 曾用裸 PATH 查找解析到 WSL bash，导致 gate 的 auto-compile 全部报
// 'forge-gate-*.sh: No such file or directory'。
func findBash() (string, error) {
	return shellexec.FindBash()
}

// isHookInfraFailure distinguishes "bash could not run the script" from "the
// script ran and reported FAIL" (spawn error or bash exit 126/127 → fail-open,
// not a gate verdict). Implementation shared with the gate path in
// internal/shellexec.
//
// isHookInfraFailure 区分"bash 没能跑起脚本"与"脚本跑了并报告 FAIL"（spawn
// 错误或 bash exit 126/127 → fail-open，非门禁结论）。实现与 gate 路径共用在
// internal/shellexec。
func isHookInfraFailure(err error) bool {
	return shellexec.IsHookInfraFailure(err)
}
