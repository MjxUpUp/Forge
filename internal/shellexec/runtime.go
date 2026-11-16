package shellexec

import (
	"regexp"
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
