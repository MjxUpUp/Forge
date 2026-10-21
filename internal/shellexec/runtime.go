package shellexec

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// runtimeWarningLine matches a warning the bash interpreter prints about its own
// runtime environment, as opposed to a line the script produced. The anchor is
// "<interpreter>: warning: " with NO "line N:" segment in between — bash prefixes
// every warning raised while executing script text with "line N:", so script
// output can never match. Covered: MSYS/Git-for-Windows "bash.exe: warning: could
// not find /tmp, please create!" and glibc's "bash: warning: setlocale: ...".
//
// runtimeWarningLine 匹配 bash 解释器关于自身运行环境的警告行（而非脚本产生的
// 行）。锚点是「<解释器>: warning: 」且中间**没有**「line N:」段——bash 对执行
// 脚本文本时触发的告警一律带「line N:」前缀，脚本输出不可能命中。覆盖 MSYS /
// Git for Windows 的「bash.exe: warning: could not find /tmp, please create!」
// 与 glibc 的「bash: warning: setlocale: ...」。
//
// Scope: startup-time warnings only. The same /tmp warning raised mid-script
// (here-doc, mktemp) carries "line N:" and is kept — PinTempDir is what keeps
// scripts off /tmp in the first place.
//
// 范围：只剔启动期告警。脚本执行中（here-doc、mktemp）触发的同一 /tmp 告警带
// 「line N:」会被保留——让脚本根本不走 /tmp 的是 PinTempDir。
var runtimeWarningLine = regexp.MustCompile(`^(?:[A-Za-z]:)?(?:[^:]*[/\\])?(?:ba)?sh(?:\.exe)?: warning: `)

// StripRuntimeNoise removes bash runtime-environment warning lines from a hook
// script's output. Such lines depend on the host (Windows runners print the MSYS
// /tmp warning intermittently), not on the script's verdict: left in, they become
// the checklog detail (cmp.Or prefers stderr) and its dedupe fingerprint, so two
// deliveries of the same blocked event fingerprint differently and are both
// recorded. Every other line is kept verbatim, in order.
//
// StripRuntimeNoise 从 hook 脚本输出里剔除 bash 运行环境告警行。这类行取决于宿主
// （Windows runner 间歇打印 MSYS /tmp 告警），与脚本结论无关：留着它会成为
// checklog detail（cmp.Or 优先 stderr）及其去重指纹，同一被阻断事件的两次投递
// 指纹不同、被记两条。其余行原样按序保留。
func StripRuntimeNoise(out string) string {
	if !strings.Contains(out, ": warning: ") {
		return out
	}
	lines := strings.Split(out, "\n")
	kept := lines[:0]
	for _, l := range lines {
		if runtimeWarningLine.MatchString(strings.TrimRight(l, "\r")) {
			continue
		}
		kept = append(kept, l)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// PinTempDir returns env with TMPDIR set to the Go-resolved temp dir when env
// carries no non-empty TMPDIR. Hook scripts default to ${TMPDIR:-/tmp} (bash-guard
// snapshots, file-sentinel pairing, bash's own here-doc files); on Windows /tmp is
// an MSYS mount that Git Bash intermittently fails to resolve on CI runners
// ("could not find /tmp, please create!"), and every redirect into it then fails
// silently behind `2>/dev/null || true` — file-sentinel finds no snapshot and
// fails open. os.TempDir() is the same directory the usertemp mount points at,
// so this removes the mount dependency without moving any file. On Unix the
// pinned value is $TMPDIR-or-/tmp, i.e. unchanged behavior. An explicit TMPDIR
// is never overridden.
//
// PinTempDir 在 env 不含非空 TMPDIR 时返回补上 Go 侧解析的临时目录的 env。hook
// 脚本默认 ${TMPDIR:-/tmp}（bash-guard 快照、file-sentinel 配对、bash 自身的
// here-doc 临时文件）；Windows 上 /tmp 是 MSYS 挂载，CI runner 上 Git Bash 间歇
// 解析不到（「could not find /tmp, please create!」），此时所有写入都被
// `2>/dev/null || true` 静默吞掉——file-sentinel 找不到快照、fail-open 放行。
// os.TempDir() 正是 usertemp 挂载指向的同一目录，故只去掉挂载依赖、不搬任何
// 文件；Unix 上取值即 $TMPDIR 或 /tmp，行为不变。显式 TMPDIR 永不覆盖。
func PinTempDir(env []string) []string {
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		// Windows env keys are case-insensitive (os/exec dedups them that way), so
		// an inherited "TmpDir=X" is an explicit TMPDIR there.
		//
		// Windows 的 env 键不区分大小写（os/exec 也按此去重），继承来的「TmpDir=X」
		// 在那里就是显式 TMPDIR。
		if (k == "TMPDIR" || runtime.GOOS == "windows" && strings.EqualFold(k, "TMPDIR")) && v != "" {
			return env
		}
	}
	return append(env, "TMPDIR="+filepath.ToSlash(os.TempDir()))
}
