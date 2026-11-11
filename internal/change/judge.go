package change

import (
	"fmt"
	"strings"
)

// word 是拆出来的一个 shell 词：值已去引号，start 是它在原串里的起点
// （用来判断它是不是处在命令位置）。
type word struct {
	val string
	// cmd 为真表示这个词处在命令位置（行首或 ; | && 之后、管道之后）。
	// 判 `eval` 这类关键词要它：`git log --grep eval` 里的 eval 只是参数。
	cmd bool
}

// shellWords 按 shell 的规则把命令拆成词：引号去掉、反斜杠转义还原、注释丢掉。
//
// 关键词检查必须看"去引号之后"的写法：`bash "-c"`、`bash \-c`、`"eval"` 在
// shell 里就是 `bash -c`、`bash -c`、`eval`，只看原文会被引号骗过去。
func shellWords(s string) []word {
	var out []word
	var cur strings.Builder
	inWord := false
	var quote byte
	atCmd := true
	flush := func() {
		if inWord || cur.Len() > 0 {
			out = append(out, word{val: cur.String(), cmd: atCmd})
			atCmd = false
		}
		cur.Reset()
		inWord = false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
				continue
			}
			cur.WriteByte(c)
			inWord = true
		case quote == '"':
			if c == '\\' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
				continue
			}
			if c == '"' {
				quote = 0
				continue
			}
			cur.WriteByte(c)
			inWord = true
		case c == '\'' || c == '"':
			quote = c
			inWord = true
		case c == '\\' && i+1 < len(s) && s[i+1] != '\n':
			i++
			cur.WriteByte(s[i])
			inWord = true
		case c == ' ' || c == '\t':
			flush()
		case c == '\n' || c == ';' || c == '|' || c == '&' || c == '(' || c == ')':
			flush()
			atCmd = true
		case c == '#' && !inWord:
			for i < len(s) && s[i] != '\n' {
				i++
			}
			flush()
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	flush()
	return out
}

var shellBases = map[string]bool{
	"bash": true, "sh": true, "zsh": true, "dash": true,
	"ksh": true, "busybox": true, "pwsh": true, "powershell": true,
}

// shellBase 取词的解释器名：去路径、去 .exe、统一小写。
func shellBase(w string) string {
	b := strings.ToLower(w)
	b = strings.ReplaceAll(b, `\`, "/")
	if i := strings.LastIndexByte(b, '/'); i >= 0 {
		b = b[i+1:]
	}
	return strings.TrimSuffix(b, ".exe")
}

// nestedShell 判断判据里有没有再调用一层 shell。
//
// 内层 shell 不继承外层 prefix 里的 errexit/pipefail：`bash -lc 'false | true'`、
// `/bin/sh -c ...`、`bash.exe -ec ...`、`bash "-c" ...`、`bash \-c ...`、
// `$SHELL -c ...` 都能把失败变成 0，而外层只看到内层退出码 0。只要解释器后面
// 跟着带 c 的选项就拒绝；`${IFS}` 在拆词前已还原成空格。
func nestedShell(words []word) bool {
	for i, w := range words {
		base := shellBase(w.val)
		isShell := shellBases[base] || w.val == "$SHELL" || w.val == "${SHELL}"
		if !isShell {
			continue
		}
		for _, n := range words[i+1:] {
			if !strings.HasPrefix(n.val, "-") {
				break
			}
			if n.val == "--command" {
				return true
			}
			if !strings.HasPrefix(n.val, "--") && strings.ContainsRune(n.val, 'c') {
				return true
			}
		}
	}
	return false
}

// hasEval 判断命令位置上有没有 eval：`eval 'false | true'` 的退出码同样由
// 最后一条命令决定。`git log --grep eval` 里的 eval 是参数，不算。
func hasEval(words []word) bool {
	for _, w := range words {
		if w.val == "eval" && w.cmd {
			return true
		}
	}
	return false
}

// disablesProtection 判断有没有用它自己的写法关掉保护：`set +e`、`set +o errexit`、
// `set "+o"` 都算（引号在拆词时已去掉）。
func disablesProtection(words []word) bool {
	for i, w := range words {
		if w.val != "set" {
			continue
		}
		for _, a := range words[i+1:] {
			if strings.HasPrefix(a.val, "+") {
				return true
			}
			if !strings.HasPrefix(a.val, "-") {
				break
			}
		}
	}
	return false
}

// lintJudge 拒绝那些能把失败变成 0 的写法。
//
// 文档里的"禁止"必须由机器执行：只写在模板里，agent 换个写法照样封印。
// 操作符检查看"引号外的代码"（`echo "a||b"` 是字面量）；关键词检查看"去引号
// 之后"的词（`bash "-c"` 是调用内层 shell）——两种视图各管各的，都不能省。
//
// 这套检查拦的是常见写法，不是沙箱：判据里刻意做间接调用（脚本里再开 shell、
// 变量拼命令）仍然可能骗过它。判据越直白，证据越可信。
func lintJudge(cmd string) error {
	s := stripQuoted(cmd)
	// ${IFS} 是空格的字面写法，`bash${IFS}-c` 在 shell 里就是 `bash -c`。
	normalized := strings.ReplaceAll(strings.ReplaceAll(cmd, "${IFS}", " "), "$IFS", " ")
	s = strings.ReplaceAll(s, "${IFS}", " ")
	s = strings.ReplaceAll(s, "$IFS", " ")
	words := shellWords(normalized)
	switch {
	case strings.Contains(s, "||"):
		return fmt.Errorf("判据里不能用 `||`（它会把前一条的失败咽掉）；用 `&&` 串联，或把逻辑写进脚本")
	case strings.Contains(s, "|"):
		return fmt.Errorf("判据里不要用管道 `|`——需要看输出就写进文件，或把逻辑写进脚本（sh 环境没有 pipefail，管道失败会变成通过）")
	case hasSingleAmpersand(s):
		return fmt.Errorf("判据里不能用后台 `&`（后台任务的失败不会被等待）；要并行请写进脚本并 wait")
	case disablesProtection(words):
		return fmt.Errorf("判据里不能关掉保护（`set +e` / `set +o errexit` 等于允许失败）")
	case hasEval(words):
		return fmt.Errorf("判据里不能用 `eval`（它会把失败吞成最后一条命令的退出码）")
	case nestedShell(words):
		return fmt.Errorf("判据里不能再调用一层 shell（`bash -c`、`bash -lc`、`/bin/sh -c` 都算：内层 shell 不受 pipefail 约束）")
	}
	return nil
}
