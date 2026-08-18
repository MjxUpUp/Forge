package util

import "regexp"

// redact.go — 触发审计摘录的 secret 脱敏（skill-trigger v2）。
//
// 设计边界（对抗辩论 R4/R7 的落地）：摘录只服务误报 triage 与 golden case 挖矿，
// 默认不采集（FORGE_TRIGGER_EXCERPT=1 才开）；即便开启也要先过本脱敏——组织安全
// 政策禁止凭证/PII 落日志。本函数是尽力而为的机械层：按已知 token 形态 + 赋值
// 形式匹配，不承诺覆盖全部敏感形态——所以摘录还有长度上限、且挖矿产物在进
// golden 前仍需人工改写（mine 的 --sanitize 只是把"约定"升为"机械步骤"，不是
// 人工审核的替代）。
//
// redact.go — secret redaction for trigger-audit excerpts (skill-trigger v2).
//
// Design boundary (landing of debate R4/R7): excerpts exist only for false-positive
// triage and golden-case mining, are OFF by default (FORGE_TRIGGER_EXCERPT=1), and
// even when on must pass this redaction first — the org security policy forbids
// credentials/PII in logs. This is a best-effort mechanical layer: known token shapes
// plus assignment forms; it does not promise full sensitive-shape coverage — hence the
// excerpt length cap, and mined drafts still require human rewrite before entering
// golden (mine's --sanitize promotes the convention to a mechanical step, not a
// replacement for human review).

// redactPatterns 已知 secret 形态：OpenAI/Anthropic 风格 key、GitHub token、AWS AKIA、
// JWT、Slack token、赋值形式（api_key=/token:/secret=/password= 后跟非空白串，允许可选
// 引号包裹——API_KEY="xxx" 是 shell/env 常态）。赋值形式大小写不敏感；命名 token 前缀
// 保持大小写敏感（现实形态全部如此，宽松化只会增加误脱敏噪声）。
var redactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(sk|rk|pk)-[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,}`),
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
	// shell/env 赋值形式：API_KEY=xxx / token: xxx（允许可选引号）。
	// shell/env assignment form: API_KEY=xxx / token: xxx (optional quotes).
	regexp.MustCompile(`(?i)(api[_-]?key|apikey|token|secret|password|passwd|pwd)\s*[=:]\s*["']?[^\s"',;&]{6,}`),
	// JSON 键值形式（review M3）："api_key": "hunter2secret"——摘录来源（stdout/prompt）
	// 里用户粘贴的配置最常见形态，键名后的引号会挡掉上面的赋值正则。
	// JSON key-value form (review M3): "api_key": "hunter2secret" — the most common
	// pasted-config shape in excerpts (stdout/prompt); the quote after the key would
	// block the assignment pattern above.
	regexp.MustCompile(`(?i)"(api[_-]?key|apikey|token|secret|password|passwd|pwd)"\s*:\s*"[^"]{6,}"`),
	// XML/YAML 键值形式（review M3）：<password>hunter2</password>。
	// XML/YAML tag form (review M3): <password>hunter2</password>.
	regexp.MustCompile(`(?i)<(api[_-]?key|apikey|token|secret|password|passwd|pwd)[^>]*>[^<]{6,}</(api[_-]?key|apikey|token|secret|password|passwd|pwd)>`),
}

// redactedToken 替换占位符。保持可读（说明被脱敏的类别）而不泄漏长度信息。
const redactedToken = "[REDACTED]"

// RedactSecrets 把文本中已知形态的 secret 替换为 [REDACTED]。纯函数、无 IO；
// 摘录采集（cli/skill_trigger.go）与挖矿草稿（skillseval/mine.go）双侧共用。
//
// RedactSecrets replaces known secret shapes in the text with [REDACTED]. Pure, no IO;
// shared by excerpt capture (cli/skill_trigger.go) and mining drafts (skillseval/mine.go).
func RedactSecrets(s string) string {
	for _, re := range redactPatterns {
		s = re.ReplaceAllString(s, redactedToken)
	}
	return s
}
