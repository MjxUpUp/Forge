package skilltrigger

import (
	"regexp"
	"strings"
)

// heredocStart 匹配 heredoc 起始标记 `<<`/`<<-` + 可选空白 + 可选引号 + 定界词
// （定界词须以字母/下划线开头，排除 `2<<` fd 重定向与 `$((x<<2))` 算术左移的数字形态）。
// RE2 不支持反向引用，开/闭引号各自独立可选——闭引号与开引号不一致的病态输入在真实
// shell 里本就非法，取到定界词即足够。
var heredocStart = regexp.MustCompile(`<<-?[ \t]*['"]?([A-Za-z_][A-Za-z0-9_]*)`)

// boundaryOK 校验 `<<` 匹配点的前一个字符：必须是行首/空白/`;`/`&`/`|`/`(`。
// 防止把 `x<<2`、`$((a<<b))` 这类 token 内部左移误判为 heredoc 起始，进而错误丢弃
// 后续行（那会造成关键词漏配的假阴性）。
func boundaryOK(line string, start int) bool {
	if start == 0 {
		return true
	}
	switch line[start-1] {
	case ' ', '\t', ';', '&', '|', '(':
		return true
	}
	return false
}

// sanitizeCommand 剥离 shell 命令中 heredoc 的 body（含定界符终止行）后返回，供关键词
// 匹配使用。动机：PreToolUse keywords 对 command 全文做子串匹配，分析/生成类脚本
// （`python - <<'EOF' ...`）的 body 里出现 "npm publish"、"git tag" 等字样会误触
// release-readiness 这类发布守卫 skill——body 是数据不是待执行命令行，不该参与匹配。
// 保留 marker 行本身（`goreleaser release --config <<EOF` 里的命令仍可命中）。
//
// 已知取舍一：嵌套引号内脚本（`bash -c 'cat <<X ... X'`）的 body 同样被剥离——该形态下
// body 确实会被内层 shell 执行，理论上属于应匹配内容；但 keyword trigger 是 advisory
// 注入而非安全门（真正的危险命令拦截在 hazard-guard hook），此处优先消除高频误触。
// 已知取舍二：同一行引号字符串里的 `<<`（`git commit -m "see a << b" && git push`）会被
// 误当 heredoc 起始、其后续行被剥——跨行引号状态跟踪复杂度不值得，偶发漏注入可接受。
func sanitizeCommand(cmd string) string {
	if !strings.Contains(cmd, "<<") {
		return cmd
	}
	var b strings.Builder
	inBody := false
	dash := false
	delim := ""
	for _, ln := range strings.Split(cmd, "\n") {
		if inBody {
			// 终止行按 bash 形状严格匹配：先兜掉 CRLF 行尾 \r；<<- 形态仅剥前导 tab
			// （bash 不剥空格）；再与定界词全等。行尾空格的 "EOF " 不算终止——旧的
			// TrimSpace 行为会把它误判为终止行、放走后续真实 body 行。
			l := strings.TrimSuffix(ln, "\r")
			if dash {
				l = strings.TrimLeft(l, "\t")
			}
			if l == delim {
				inBody = false
			}
			continue // 丢弃 body 行与终止行 / drop body and terminator lines
		}
		b.WriteString(ln)
		b.WriteByte('\n')
		if m := heredocStart.FindStringSubmatchIndex(ln); m != nil && boundaryOK(ln, m[0]) {
			delim = ln[m[2]:m[3]]
			dash = strings.HasPrefix(ln[m[0]:], "<<-")
			inBody = true
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}
