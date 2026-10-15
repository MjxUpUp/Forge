package skilltrigger

import (
	"regexp"
	"strings"
)

// harnessEnvelopeTags lists the host-injected envelope blocks that are not user-authored text: subagent completion notices, system reminders, local command output, and session/CI event notes. Blocks that carry user intent (command-name, command-args, bash-input) are deliberately absent.
//
// harnessEnvelopeTags 是宿主注入的信封块标签：子代理完成通知、system-reminder、本地命令
// 输出、会话/CI 事件通知——这些块不是用户写的字。宿主把它们作为用户回合投递时（UserPromptSubmit
// 的 prompt 字段原样携带），块内的「红队」「设计文档」「交叉验证」等词会误触不相关 skill，
// 提示还标「来自你的输入」（2026-10-15 research-workflow 复盘会话实录 6 次）。表达用户意图的
// 块（command-name / command-args / bash-input）刻意不在表内——用户敲的 slash 命令与终端命令
// 就是信号。
var harnessEnvelopeTags = []string{
	"task-notification",
	"system-reminder",
	"local-command-stdout",
	"local-command-stderr",
	"local-command-caveat",
	"bash-stdout",
	"bash-stderr",
	"side-sessions-event",
	"ci-monitor-event",
}

// envelopeBlockRes holds one closed-block regexp and one unclosed-opener regexp per tag (RE2 has no backreferences, so each tag gets its own pair).
//
// envelopeBlockRes 每个标签一对正则：闭合块与未闭合开标签（RE2 无反向引用，逐标签各建一对）。
// 未闭合开标签只在行首（可带缩进）时剥到文本末尾——宿主投递的信封总是独占行起头，被截断的
// 通知仍是通知；用户正文里提到字面标签（「修 <system-reminder> 的解析」）不在行首，不会吞掉
// 其后的正文。
var envelopeBlockRes = func() []*regexp.Regexp {
	res := make([]*regexp.Regexp, 0, 2*len(harnessEnvelopeTags))
	for _, tag := range harnessEnvelopeTags {
		q := regexp.QuoteMeta(tag)
		res = append(res,
			regexp.MustCompile(`(?is)<`+q+`(?:\s[^>]*)?>.*?</`+q+`\s*>`),
			regexp.MustCompile(`(?ism)^[ \t]*<`+q+`(?:\s[^>]*)?>.*\z`),
		)
	}
	return res
}()

// SanitizePrompt strips host envelope blocks from a UserPromptSubmit prompt before keyword and coding-intent matching. When a block is removed the result is trimmed; a prompt without any envelope opener is returned byte-for-byte. It is idempotent.
//
// SanitizePrompt 在关键词与 coding_intent 匹配前剥离 prompt 中的宿主信封块。剥离发生时
// 结果首尾去空白；不含任何信封开标签的 prompt 原样逐字节返回（快速路径，零正则开销）。幂等。
func SanitizePrompt(prompt string) string {
	if !hasEnvelopeOpener(prompt) {
		return prompt
	}
	out := prompt
	for _, re := range envelopeBlockRes {
		out = re.ReplaceAllString(out, " ")
	}
	return strings.TrimSpace(out)
}

// hasEnvelopeOpener reports whether the prompt contains any envelope opening tag (case-insensitive) — the cheap pre-check that keeps the 18 regexps off ordinary prompts.
//
// hasEnvelopeOpener 判断 prompt 是否含任一信封开标签（大小写不敏感）——让普通 prompt 不跑
// 18 条正则的廉价预检。
func hasEnvelopeOpener(prompt string) bool {
	if !strings.Contains(prompt, "<") {
		return false
	}
	lower := strings.ToLower(prompt)
	for _, tag := range harnessEnvelopeTags {
		if strings.Contains(lower, "<"+tag) {
			return true
		}
	}
	return false
}
