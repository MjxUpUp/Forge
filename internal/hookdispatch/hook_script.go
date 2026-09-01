package hookdispatch

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

// projectTagFor 为给定 project root 返回稳定的 hex tag。通过对 canonical
// （绝对、clean 后的）路径做哈希，使 tag 在路径大小写、盘符格式、symlink 之间保持
// 不变——而 $PWD cksum 还依赖宿主的 cksum 格式（GNU vs BSD）。hook 通过
// FORGE_PROJECT_TAG env var 读取它来按 project 隔离状态。
func ProjectTagFor(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	h := fnv.New64a()
	h.Write([]byte(filepath.Clean(abs)))
	return strconv.FormatUint(h.Sum64(), 16)
}

// suggestTagFor 返回某目录的 init-suggest marker tag，按其 git root 作 key，
// 这样无论 agent 从哪个 subdir 执行 `forge suggest decline`，同一 project 只会被
// tag 一次。这守护 decline 契约：此前按 cwd 作 key，从 subdir decline 会写出与
// hook 在 project root 读到的不同的 tag，使 decline 静默 no-op。非 git 目录回退到
// ProjectTagFor(dir)（仍是稳定的 per-dir tag）。由 init-suggest hook
// （FORGE_CWD_TAG）和 `forge suggest` 共用——两者对同一 project 必须产出相同的
// tag。
func SuggestTagFor(dir string) string {
	if root := forgedata.FindGitRoot(dir); root != "" {
		return ProjectTagFor(root)
	}
	return ProjectTagFor(dir)
}

// maxEnvValueLen 是传给 bash 脚本的 env var value 的最大长度，
// 用于防止内存问题。
const maxEnvValueLen = 100000

// readsFilePath 返回本 session 的 reads log 绝对路径——PreToolUse
// read-before-edit hook（方案2 shift-left）grep 它来拦截 Edit-without-Read 的
// 磁盘 side-channel。Per-session（按 sanitized session id 作 key）、ephemeral
// （$TMPDIR）。落盘而非存于 context，是为了在 session 内 SURVIVES compaction：
// compact 之前的 Read 仍计入之后的 Edit，消除基于 context 检查的最大假阳性来源。
func readsFilePath(root, sessionID string) string {
	// ProjectTagFor(root) 把 reads log 按 project 分桶：$TMPDIR 跨项目共享，仅按 session id
	// 命名会在短/复用 session id（如测试 sid-*）下让 A 项目的 reads log 被 B 项目读到——
	// read-before-edit hook 会误判 Edit 已 Read 过（假阳性放行）。project tag 是 fnv hex
	// （文件名安全），与 FORGE_PROJECT_TAG 同源。
	return filepath.Join(os.TempDir(), "forge-session-reads-"+ProjectTagFor(root)+"-"+readsFileKey(sessionID)+".log")
}

// readsFileKey 把 session id 收敛为 filename-safe 的 token。SanitizeSessionID
// 保留可读性，但仍可能含某些平台上被文件系统特殊对待的字符；将 [A-Za-z0-9._-]
// 之外的字符一律折叠为 '_'，使临时文件名始终安全，且不把原始 id 泄漏到 $TMPDIR。
func readsFileKey(sessionID string) string {
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

// sanitizeForShell 把字符串净化为可安全用于 shell env var 的形式。防止
// user-controlled 内容经 env var 传入 bash 脚本时发生 shell injection。
//
// 策略：
//   - 截断到 maxEnvValueLen，防内存耗尽
//   - 替换 NULL 字节和控制字符（tab、newline、carriage return 除外）
//   - Unicode-safe 校验（拒绝非法 UTF-8）
//   - 不做引号或转义——调用方须自行用 export VAR=$value 并给 value 加双引号
//
// 注意：这是 defense-in-depth 措施。hook 脚本自身在使用前也应校验输入。
func sanitizeForShell(value string) string {
	if value == "" {
		return ""
	}

	// 截断以防内存问题
	if len(value) > maxEnvValueLen {
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
		// 兜底：10 字节窗口内含非法 UTF-8 时可能找不到 RuneStart，循环走完不
		// 截断，超长 value 会原样进 env。改为在限制处硬截断。
		if len(value) > maxEnvValueLen {
			value = value[:maxEnvValueLen]
		}
	}

	// 校验 UTF-8 并移除控制字符
	var result strings.Builder
	result.Grow(len(value))

	for _, r := range value {
		// 检查 UTF-8 合法性
		if r == utf8.RuneError {
			// 跳过非法 rune
			continue
		}

		// 移除 NULL 字节和大多数控制字符
		// 放行：tab (0x09)、newline (0x0A)、carriage return (0x0D)
		// 拦截：NULL (0x00) 及其他控制字符 (0x01-0x08、0x0B-0x0C、0x0E-0x1F)
		if r == 0 {
			// NULL 替换为空格
			result.WriteRune(' ')
			continue
		}
		if r < 0x20 && r != 0x09 && r != 0x0A && r != 0x0D {
			// 跳过其他控制字符
			continue
		}

		result.WriteRune(r)
	}

	return result.String()
}

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

// findBash 解析 hook 脚本的 bash 解释器。实现（含 Windows WSL 规避逻辑）在
// internal/shellexec，与 gate 路径（taskpipeline.runEmbeddedHook）共用——那里
// 曾用裸 PATH 查找解析到 WSL bash，导致 gate 的 auto-compile 全部报
// 'forge-gate-*.sh: No such file or directory'。
func findBash() (string, error) {
	return shellexec.FindBash()
}

// isHookInfraFailure 区分"bash 没能跑起脚本"与"脚本跑了并报告 FAIL"（spawn
// 错误或 bash exit 126/127 → fail-open，非门禁结论）。实现与 gate 路径共用在
// internal/shellexec。
func isHookInfraFailure(err error) bool {
	return shellexec.IsHookInfraFailure(err)
}
