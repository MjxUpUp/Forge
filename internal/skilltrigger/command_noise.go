package skilltrigger

import (
	"regexp"
	"strings"
)

// heredocStart 匹配 heredoc 起始标记 `<<`/`<<-` + 可选空白 + 可选引号 + 定界词
// （定界词须以字母/下划线开头，排除 `2<<` fd 重定向与 `$((x<<2))` 算术左移的数字形态）。
// RE2 不支持反向引用，开/闭引号各自独立可选——闭引号与开引号不一致的病态输入在真实
// shell 里本就非法，取到定界词即足够。
//
// heredocStart matches a heredoc start marker `<<`/`<<-` + optional whitespace + optional
// quote + delimiter word (the word must start with a letter/underscore, ruling out the
// numeric forms of fd redirection `2<<` and arithmetic shift `$((x<<2))`).
// RE2 has no backreferences, so the opening/closing quotes are independently optional —
// a mismatched pair is invalid shell anyway; capturing the word is all we need.
var heredocStart = regexp.MustCompile(`<<-?[ \t]*['"]?([A-Za-z_][A-Za-z0-9_]*)`)

// boundaryOK 校验 `<<` 匹配点的前一个字符：必须是行首/空白/`;`/`&`/`|`/`(`。
// 防止把 `x<<2`、`$((a<<b))` 这类 token 内部左移误判为 heredoc 起始，进而错误丢弃
// 后续行（那会造成关键词漏配的假阴性）。
//
// boundaryOK validates the character preceding a `<<` match: it must be line start,
// whitespace, `;`, `&`, `|`, or `(`. Prevents misreading token-internal shifts like
// `x<<2` or `$((a<<b))` as heredoc starts, which would wrongly drop subsequent lines
// (turning into keyword false negatives).
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
// 已知取舍：嵌套引号内脚本（`bash -c 'cat <<X ... X'`）的 body 同样被剥离——该形态下
// body 确实会被内层 shell 执行，理论上属于应匹配内容；但 keyword trigger 是 advisory
// 注入而非安全门（真正的危险命令拦截在 hazard-guard hook），此处优先消除高频误触。
//
// sanitizeCommand strips heredoc bodies (including the delimiter terminator lines)
// from a shell command before keyword matching. Rationale: PreToolUse keywords match
// the command by substring over the full text; analysis/generation scripts
// (`python - <<'EOF' ...`) whose body merely mentions "npm publish" / "git tag"
// false-positive release-readiness-style guard skills — the body is data, not the
// command line being executed. The marker line itself is kept (a real
// `goreleaser release --config <<EOF` still matches).
//
// Known trade-off: bodies of quoted nested scripts (`bash -c 'cat <<X ... X'`) are
// stripped too — those DO get executed by the inner shell and would ideally match.
// But keyword triggers are advisory injections, not security gates (dangerous-command
// interception lives in the hazard-guard hook), so eliminating the frequent false
// positive wins.
func sanitizeCommand(cmd string) string {
	if !strings.Contains(cmd, "<<") {
		return cmd
	}
	var b strings.Builder
	inBody := false
	delim := ""
	for _, ln := range strings.Split(cmd, "\n") {
		if inBody {
			// <<- 允许终止行带前导 tab；TrimSpace 同时兜掉行尾空白。
			// <<- allows leading tabs on the terminator; TrimSpace also trims trailing space.
			if strings.TrimSpace(ln) == delim {
				inBody = false
			}
			continue // 丢弃 body 行与终止行 / drop body and terminator lines
		}
		b.WriteString(ln)
		b.WriteByte('\n')
		if m := heredocStart.FindStringSubmatchIndex(ln); m != nil && boundaryOK(ln, m[0]) {
			delim = ln[m[2]:m[3]]
			inBody = true
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}
