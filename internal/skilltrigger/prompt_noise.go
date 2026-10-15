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
// 未闭合开标签剥到文本末尾——被截断的通知仍是通知，宁可漏注入也不把信封尾巴当用户输入。
var envelopeBlockRes = func() []*regexp.Regexp {
	res := make([]*regexp.Regexp, 0, 2*len(harnessEnvelopeTags))
	for _, tag := range harnessEnvelopeTags {
		q := regexp.QuoteMeta(tag)
		res = append(res,
			regexp.MustCompile(`(?is)<`+q+`(?:\s[^>]*)?>.*?</`+q+`\s*>`),
			regexp.MustCompile(`(?is)<`+q+`(?:\s[^>]*)?>.*$`),
		)
	}
	return res
}()

// SanitizePrompt strips host envelope blocks from a UserPromptSubmit prompt before keyword and coding-intent matching; text outside the blocks is returned unchanged (trimmed). It is idempotent and cheap when the prompt contains no '<'.
//
// SanitizePrompt 在关键词与 coding_intent 匹配前剥离 prompt 中的宿主信封块；块外的用户原文
// 原样保留（首尾去空白）。幂等；prompt 不含 '<' 时零成本直返。
func SanitizePrompt(prompt string) string {
	if !strings.Contains(prompt, "<") {
		return prompt
	}
	out := prompt
	for _, re := range envelopeBlockRes {
		out = re.ReplaceAllString(out, " ")
	}
	return strings.TrimSpace(out)
}
