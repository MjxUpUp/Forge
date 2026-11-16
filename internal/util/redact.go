package util

import (
	"os"
	"os/user"
	"regexp"
	"strings"
)

// redact.go — 证据归档与项目 bundle 的脱敏层。
//
// 两类产物会把「本机身份」带出仓库边界：forge change verify 封存的
// stdout/stderr 随 git 走，forge project export/sync 打包的日志推远端。两者
// 都可能含本机绝对路径、用户名与 hostname——脱敏点放在写盘/打包的同一处，
// 而不是要求调用方记得先擦一遍。

const (
	// RedactedToken 替换命中的 secret 形态。保持可读（说明被脱敏的类别）而不泄漏长度信息。
	RedactedToken = "[REDACTED]"
	// PlaceholderHome 替换本机用户主目录前缀，保留其后的相对结构。
	PlaceholderHome = "<home>"
	// PlaceholderHost 替换本机 hostname。
	PlaceholderHost = "<host>"
	// PlaceholderUser 替换裸用户名（DOMAIN\name 的 name 部分）。
	PlaceholderUser = "<user>"
)

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
	regexp.MustCompile(`(?i)(api[_-]?key|apikey|token|secret|password|passwd|pwd)\s*[=:]\s*["']?[^\s"',;&]{6,}`),
	// JSON 键值形式："api_key": "hunter2secret"——摘录来源（stdout/prompt）
	// 里用户粘贴的配置最常见形态，键名后的引号会挡掉上面的赋值正则。
	regexp.MustCompile(`(?i)"(api[_-]?key|apikey|token|secret|password|passwd|pwd)"\s*:\s*"[^"]{6,}"`),
	// XML/YAML 键值形式：<password>hunter2</password>。
	regexp.MustCompile(`(?i)<(api[_-]?key|apikey|token|secret|password|passwd|pwd)[^>]*>[^<]{6,}</(api[_-]?key|apikey|token|secret|password|passwd|pwd)>`),
}

// 本机路径形态。Windows 与 MSYS/Unix 三种写法都要覆盖：同一段输出里
// `C:\Users\me`、`C:/Users/me`、`/c/Users/me` 可能同时出现（测试与 CI 工具混用）。
// 用户名的字符类排除分隔符与引号——路径在 JSON/日志里常被引号包裹。
var (
	winUserDir  = regexp.MustCompile(`(?i)\b[A-Za-z]:[\\/]+Users[\\/]+[^\\/\s"'<>:]+`)
	msysUserDir = regexp.MustCompile(`(?i)/[a-z]/Users/[^/\s"'<>]+`)
	unixUserDir = regexp.MustCompile(`/(?:home|Users)/[^/\s"'<>]+`)
)

// ScrubPaths 把本机主目录、用户名与 hostname 替换为占位符。
//
// 只抹身份前缀，保留其后的相对结构（`<home>\AppData\Local\Temp\x` 比整段抹掉
// 更有助于复盘）。home 与 username 先按实际值精确替换，再用形态正则兜住换机器
// 后重放的输出（例如在另一台机器上归档的历史证据）。
func ScrubPaths(s string) string {
	if s == `` {
		return s
	}
	if home, err := os.UserHomeDir(); err == nil && home != `` {
		for _, v := range homeVariants(home) {
			if v != `` {
				s = strings.ReplaceAll(s, v, PlaceholderHome)
			}
		}
	}
	if u, err := user.Current(); err == nil {
		name := u.Username
		if i := strings.LastIndexAny(name, `\/`); i >= 0 {
			name = name[i+1:]
		}
		if len(name) >= 3 {
			s = strings.ReplaceAll(s, name, PlaceholderUser)
		}
	}
	if host, err := os.Hostname(); err == nil && len(host) >= 3 {
		s = strings.ReplaceAll(s, host, PlaceholderHost)
	}
	s = winUserDir.ReplaceAllString(s, PlaceholderHome)
	s = msysUserDir.ReplaceAllString(s, PlaceholderHome)
	s = unixUserDir.ReplaceAllString(s, PlaceholderHome)
	return s
}

// homeVariants 返回主目录的常见书写形态：原生、正斜杠、MSYS 盘符形式。
func homeVariants(home string) []string {
	slash := strings.ReplaceAll(home, `\`, `/`)
	out := []string{home, slash}
	// C:/Users/x → /c/Users/x（MSYS bash 里的 pwd/echo 形态）。
	if len(slash) >= 3 && slash[1] == ':' && slash[2] == '/' {
		out = append(out, `/`+strings.ToLower(slash[:1])+slash[2:])
	}
	return out
}

// RedactSecrets 把已知 secret 形态替换为占位符。
func RedactSecrets(s string) string {
	if s == `` {
		return s
	}
	for _, re := range redactPatterns {
		s = re.ReplaceAllString(s, RedactedToken)
	}
	return s
}

// Redact 是证据与 bundle 的统一脱敏入口：先抹 secret，再抹本机身份。
func Redact(s string) string {
	return ScrubPaths(RedactSecrets(s))
}
