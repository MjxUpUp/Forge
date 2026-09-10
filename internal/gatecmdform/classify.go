// Package gatecmdform classifies how a forge gate command is embedded in a shell command line (standalone / && chain / ; continuation / pipe truncation / multi-gate rush) — the shared judgment core of the gate-cmd-form hook (design C) and the harness-audit C1–C3 metrics (design M).
//
// Package gatecmdform 判定 forge 门禁命令在一条 shell 命令里的嵌入形态（standalone /
// && 链 / 分号续行 / 管道截断 / 多门禁连刷）——gate-cmd-form hook（设计 C）与
// harness-audit 的 C1–C3 度量（设计 M）共用的判定核。两机审计实录：standalone 0–1%，
// 管道截断 84%/27%，分号+grep 掩蔽 37%/13%——BLOCKED 前缀与退出码契约被链式/截断形态
// 系统性削弱（docs/design/harness-fixes-a-g-2026-09.md C、M）。纯函数、无 IO。
package gatecmdform

import (
	"regexp"
	"strings"
)

// Form is the classification of one shell command line.
//
// Form 是一条命令行的形态分类。Gates=0 表示命令里没有门禁子命令（不在本判定范围）；
// Compliant 仅对 Gates≥1 的命令有意义：形态限于 standalone / `cd X &&` 前缀 / `&&` 链，
// 且非多门禁、无 `;`、`|`、`||`。
type Form struct {
	Gates         int  // 门禁子命令出现次数
	Standalone    bool // 去掉 cd 前缀后无任何 && ; | || 连接
	CdPrefix      bool // 以 `cd <path> &&` 开头（允许的定位前缀）
	AndChain      bool // 含 &&（cd 前缀之外）
	OrChain       bool // 含 ||
	Semicolon     bool // 含分号续行——前一门禁 BLOCKED 后链条照走，退出码契约失效
	PipeTruncated bool // 含 `| tail/head/grep/cut/sed/awk/wc`——BLOCKED 文本面可能被吞
	GrepMasked    bool // 含 `| grep`——BLOCKED 行被过滤掉的最强掩蔽形态
	MultiGate     bool // 同一命令行 ≥2 个门禁子命令（B2「多门禁连刷」）
	Compliant     bool // Gates≥1 且形态合规（见类型注释）
}

// gateRe matches forge gate-family subcommands, including the dev-binary spelling.
//
// gateRe 匹配 forge 门禁族子命令（含 ./bin/forge-dev(.exe) 拼写）：task gate / task
// complete / task verify-acceptance / review pass / docs lint / task doc-review。
var gateRe = regexp.MustCompile(`(?:^|[\s;&|(])(?:\./bin/)?forge(?:-dev(?:\.exe)?)?\s+(?:task\s+gate|task\s+complete|task\s+verify-acceptance|review\s+pass|docs\s+lint|task\s+doc-review)\b`)

var (
	cdPrefixRe  = regexp.MustCompile(`^\s*cd\s+\S+\s*&&\s*`)
	pipeTruncRe = regexp.MustCompile(`\|\s*(?:tail|head|grep|cut|sed|awk|wc)\b`)
	grepMaskRe  = regexp.MustCompile(`\|\s*grep\b`)
	semicolonRe = regexp.MustCompile(`;\s*\S`)
	quotedRe    = regexp.MustCompile(`"[^"]*"|'[^']*'`)
)

// Classify classifies one shell command line. Quoted segments are blanked before matching so a gate command mentioned inside a string (commit message, echo) is data, not an invocation.
//
// Classify 判定一条命令行。引号内容先置空再匹配——commit message / echo 里提及的门禁
// 命令是数据不是调用（与 hazard-guard 的数据上下文放行同哲学）。多行命令按换行视作
// 分号续行（与 hazard-guard 的段切分一致）。
func Classify(cmd string) Form {
	var f Form
	text := quotedRe.ReplaceAllStringFunc(cmd, func(s string) string { return strings.Repeat(" ", len(s)) })
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", " ; ")
	f.Gates = len(gateRe.FindAllStringIndex(text, -1))
	if f.Gates == 0 {
		return f
	}
	rest := text
	if loc := cdPrefixRe.FindStringIndex(rest); loc != nil {
		f.CdPrefix = true
		rest = rest[loc[1]:]
	}
	f.MultiGate = f.Gates >= 2
	f.OrChain = strings.Contains(rest, "||")
	// `&&` 判定要排除 `||` 里的字符——先去掉 || 再看 &&。
	f.AndChain = strings.Contains(strings.ReplaceAll(rest, "||", ""), "&&")
	f.Semicolon = semicolonRe.MatchString(rest)
	f.PipeTruncated = pipeTruncRe.MatchString(rest)
	f.GrepMasked = grepMaskRe.MatchString(rest)
	// 单竖线管道（非 ||）也是连接符：`gate | cat` 虽不截断也非 standalone。
	singlePipe := strings.Contains(strings.ReplaceAll(rest, "||", ""), "|")
	f.Standalone = !f.AndChain && !f.OrChain && !f.Semicolon && !singlePipe
	f.Compliant = (f.Standalone || f.AndChain) && !f.OrChain && !f.Semicolon && !singlePipe && !f.MultiGate
	return f
}
