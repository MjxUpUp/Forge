package hookdispatch

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/shellexec"
	"github.com/MjxUpUp/Forge/internal/util"
)

// ProjectTagFor 为给定 project root 返回稳定的 hex tag。通过对 canonical
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

// SuggestTagFor 返回某目录的 init-suggest marker tag，按其 git root 作 key，
// 这样无论 agent 从哪个 subdir 执行 `forge off`，同一 project 只会被
// tag 一次。这守护 decline 契约：此前按 cwd 作 key，从 subdir decline 会写出与
// hook 在 project root 读到的不同的 tag，使 decline 静默 no-op。非 git 目录回退到
// ProjectTagFor(dir)（仍是稳定的 per-dir tag）。由 init-suggest hook
// （FORGE_CWD_TAG）和 off/on 的 marker 助手共用——两者对同一 project 必须产出相同的
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

// recordSessionRead credits path as read in the reads log of the project the hook
// runs in (cwd root) and, when the file lives under a different project root — a git
// worktree, or another checkout the session visited — also in that root's own bucket
// under that root's relative path. The reads log is bucketed by project root and holds
// root-relative paths, so a Read made while the session cwd sat in the main checkout
// was invisible to the Edit made later from inside the worktree (same session, same
// absolute path, different bucket and different relative path): read-before-edit
// blocked a file the agent had read.
//
// recordSessionRead 把 path 记为已读：记入 hook 所在项目（cwd 根）的读记录；当文件在
// 另一个项目根之下（git worktree、或会话去过的另一个仓库）时，再按该根自己的相对路径
// 记入该根的桶。读记录按项目根分桶、内容是根相对路径，所以会话 cwd 在主仓库时做的
// Read，对之后在 worktree 里做的 Edit 不可见（同会话、同绝对路径，桶与相对路径都不同）
// ——read-before-edit 因此拦下 agent 明明读过的文件。
func recordSessionRead(root, sessionID, path string) {
	if rel := toRelPath(root, path); rel != "" && rel != "." {
		appendSessionRead(readsFilePath(root, sessionID), rel)
	}
	fileRoot := fileProjectRoot(path)
	if fileRoot == "" || sameProjectRoot(fileRoot, root) {
		return
	}
	// The relative path must stay inside the file's own root: a "../" result would
	// mean the root resolution disagreed with the path and the credit would be bogus.
	// 相对路径必须落在文件自己的根内：出现 "../" 说明根解析与路径不一致，记账会失真。
	if rel := toRelPath(fileRoot, path); rel != "" && rel != "." && !strings.HasPrefix(rel, "../") {
		appendSessionRead(readsFilePath(fileRoot, sessionID), rel)
	}
}

// fileProjectRoot returns the forge project root that contains the absolute path, or
// "" when the path is relative or not inside any forge project.
//
// fileProjectRoot 返回包含该绝对路径的 forge 项目根；路径非绝对或不在任何 forge 项目内
// 时返回空串。
func fileProjectRoot(path string) string {
	if !filepath.IsAbs(path) {
		return ""
	}
	// Read-only resolution: classifying a file must not write the global registry or
	// enrol a project the user never asked forge to manage (review F1).
	// 只读解析：给文件归类不得写全局注册表，也不得登记用户没打算让 forge 接管的项目
	// （评审 F1）。
	root, err := projectroot.FindFromReadOnly(filepath.Dir(path))
	if err != nil {
		return ""
	}
	return root
}

// sameProjectRoot reports whether two project roots are the same directory once
// symlinks are resolved.
//
// sameProjectRoot 报告两个项目根在解析 symlink 之后是否是同一目录。
func sameProjectRoot(a, b string) bool {
	return filepath.Clean(resolveSymlinks(a)) == filepath.Clean(resolveSymlinks(b))
}

// bashReadPath returns the absolute path of the one file a Bash command merely shows
// the agent — cat <f>, head/tail [-n N | -N] <f>, sed -n <N[,M]p> <f> — or "" for
// anything else. The shape is deliberately strict: any shell metacharacter, a second
// operand, a flag outside those forms, a missing file or a non-regular file yields ""
// (a missed credit is a false block the agent can resolve with a Read; a wrong credit
// would let an unread file through). grep is excluded: a match snippet is not a read.
//
// bashReadPath 返回 Bash 命令"只是把内容给 agent 看"的那个文件的绝对路径——cat <f>、
// head/tail [-n N | -N] <f>、sed -n <N[,M]p> <f>——其余一律返回空串。形态刻意从严：
// 任何 shell 元字符、第二个操作数、这些形态之外的 flag、不存在或非常规文件都返回空
// （漏记只是 agent 补一次 Read 就能解决的误拦；误记会让没读过的文件放行）。grep 不计：
// 匹配片段不等于读过。
func bashReadPath(command string) string {
	if strings.ContainsAny(command, "|;&<>$`(){}[]*?!\\\n\r") {
		return ""
	}
	// ASCII only: strings.Fields splits on Unicode whitespace (a no-break space) where
	// bash does not, so a non-ASCII command could be parsed into a different file than
	// the one bash opens (review F3).
	// 只认 ASCII：strings.Fields 按 Unicode 空白（如不换行空格）切词而 bash 不会，非 ASCII
	// 命令可能被解析成与 bash 实际打开的不同的文件（评审 F3）。
	// Control bytes other than tab too: Fields also splits on form feed and vertical
	// tab, which bash's IFS does not.
	// 除 tab 外的控制字节同样拒绝：Fields 也按换页与垂直制表切词，而 bash 的 IFS 不会。
	for _, r := range command {
		if r > unicode.MaxASCII || (r < 0x20 && r != '\t') || r == 0x7f {
			return ""
		}
	}
	words := strings.Fields(command)
	if len(words) < 2 {
		return ""
	}
	// A word starting with # is a comment in bash (the rest of the line is not an
	// operand); a leading ~ is expanded to HOME by bash but would be taken literally.
	// 词首 # 在 bash 里是注释（其后不是操作数）；词首 ~ 会被 bash 展开成 HOME，这里却会
	// 当字面目录。
	for _, w := range words {
		if strings.HasPrefix(w, "#") || strings.HasPrefix(w, "~") {
			return ""
		}
	}
	var operands []string
	switch words[0] {
	case "cat":
		operands = words[1:]
	case "head", "tail":
		rest := words[1:]
		switch {
		case len(rest) >= 2 && rest[0] == "-n" && isAllDigits(rest[1]):
			rest = rest[2:]
		case len(rest) >= 1 && strings.HasPrefix(rest[0], "-") && isAllDigits(rest[0][1:]):
			rest = rest[1:]
		}
		operands = rest
	case "sed":
		if len(words) != 4 || words[1] != "-n" || !isSedPrintRange(trimQuotes(words[2])) {
			return ""
		}
		operands = words[3:]
	default:
		return ""
	}
	if len(operands) != 1 {
		return ""
	}
	p := trimQuotes(operands[0])
	if p == "" || strings.HasPrefix(p, "-") || strings.ContainsAny(p, "'\"") {
		return ""
	}
	// A ".." segment is resolved by bash through symlinks but by filepath.Clean
	// lexically, so the two can name different files (review F2): refuse it.
	// ".." 段在 bash 里经符号链接解析，而 filepath.Clean 是词法折叠，两者可能指向不同
	// 文件（评审 F2）：直接拒绝。
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == ".." {
			return ""
		}
	}
	if !filepath.IsAbs(p) {
		wd, err := os.Getwd()
		if err != nil {
			return ""
		}
		p = filepath.Join(wd, p)
	}
	p = filepath.Clean(p)
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		return ""
	}
	return p
}

// isAllDigits reports whether s is a non-empty run of ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isSedPrintRange reports whether s is a sed print script of the form N,Mp or Np.
func isSedPrintRange(s string) bool {
	if !strings.HasSuffix(s, "p") {
		return false
	}
	for _, part := range strings.Split(strings.TrimSuffix(s, "p"), ",") {
		if !isAllDigits(part) {
			return false
		}
	}
	return len(strings.Split(strings.TrimSuffix(s, "p"), ",")) <= 2
}

// trimQuotes strips one pair of matching single or double quotes around s.
func trimQuotes(s string) string {
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
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
