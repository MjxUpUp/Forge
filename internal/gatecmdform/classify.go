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
// gateRe 匹配 forge 门禁族子命令。计入的拼写口径（1.58 BLOCKED 前必须钉死——评审指出
// 引号/反引号包装是可发现绕过）：`forge` / `forge.exe` / `forge-dev` / `forge-dev.exe` /
// `./bin/forge-dev(.exe)`，前缀为行首、空白、`;&|(`、反引号或 `/`（绝对/相对路径调用）。
// 已知边界（有意取舍，包注释「计入清单」）：`ssh host "forge ..."` 的远端调用不计；
// 纯数据引用（commit message / echo）经 quoteBlanked 排除，但 `bash -c "..."` /
// `sh -c '...'` 的包装体是真实调用，quoteBlanked 保留其体内文本。
var gateRe = regexp.MustCompile("(?:^|[\\s;&|(`/\"'])(?:\\./bin/)?forge(?:-dev)?(?:\\.exe)?\\s+(?:task\\s+gate|task\\s+complete|task\\s+verify-acceptance|review\\s+pass|docs\\s+lint|task\\s+doc-review)\\b")

var (
	cdPrefixRe  = regexp.MustCompile(`^\s*cd\s+\S+\s*&&\s*`)
	pipeTruncRe = regexp.MustCompile(`\|\s*(?:tail|head|grep|cut|sed|awk|wc)\b`)
	grepMaskRe  = regexp.MustCompile(`\|\s*grep\b`)
	semicolonRe = regexp.MustCompile(`;\s*\S`)
	quotedRe    = regexp.MustCompile(`"[^"]*"|'[^']*'`)
	// shellWrapRe 识别 `bash -c "…"` / `sh -c '…'` 包装：包裹的是待执行命令而非数据——
	// 引号体保留参与匹配（cheat-detector：无差别置空会让包装调用从 C1–C3 静默消失，
	// 也是 1.58 执法的绕过面）。
	shellWrapRe = regexp.MustCompile(`(?:^|[\s;&|(]|` + "`" + `)(?:ba|z|da|k)?sh\s+-c\s+("[^"]*"|'[^']*')`)
	// heredocOpenRe 匹配 `<<[-]['"]?TAG['"]?`；正文到独占一行的 TAG 为止（RE2 无反向引用，
	// 配对由 stripHeredocs 过程式完成）。写给脚本/解释器的文本是数据（与
	// skilltrigger.sanitizeCommand、hazard-guard 数据上下文同哲学）。
	heredocOpenRe = regexp.MustCompile(`<<-?\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)
)

// stripHeredocs blanks heredoc bodies (text fed to an interpreter is data, not an invocation); the opening line (with any trailing `| tail`) is kept.
//
// stripHeredocs 置空 heredoc 正文（喂给解释器/脚本文件的文本是数据，不是调用）；保留
// 开头那一行（含 `<<TAG` 之后的 `| tail` 等连接符），只挖掉正文到结束 TAG 行。
func stripHeredocs(cmd string) string {
	lines := strings.Split(cmd, "\n")
	for i := 0; i < len(lines); i++ {
		m := heredocOpenRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		tag := m[1]
		end := -1
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == tag {
				end = j
				break
			}
		}
		if end < 0 {
			continue // 无结束标记：保守不动
		}
		for j := i + 1; j < end; j++ {
			lines[j] = ""
		}
		i = end
	}
	return strings.Join(lines, "\n")
}

// blankQuotes blanks quoted segments EXCEPT shell-wrapper bodies (bash -c "…" wraps an invocation, not data).
//
// blankQuotes 置空引号段，但 shell 包装体（bash -c "…"）除外——它包裹的是调用不是数据。
func blankQuotes(cmd string) string {
	preserve := map[string]string{}
	out := shellWrapRe.ReplaceAllStringFunc(cmd, func(m string) string {
		key := "\x00" + string(rune(len(preserve))) + "\x00"
		preserve[key] = m
		return key
	})
	out = quotedRe.ReplaceAllStringFunc(out, func(s string) string { return strings.Repeat(" ", len(s)) })
	for k, v := range preserve {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}

// Classify classifies one shell command line. Quoted segments are blanked before matching so a gate command mentioned inside a string (commit message, echo) is data, not an invocation.
//
// Classify 判定一条命令行。引号内容先置空再匹配——commit message / echo 里提及的门禁
// 命令是数据不是调用（与 hazard-guard 的数据上下文放行同哲学）。多行命令按换行视作
// 分号续行（与 hazard-guard 的段切分一致）。
func Classify(cmd string) Form {
	var f Form
	text := blankQuotes(stripHeredocs(cmd))
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
