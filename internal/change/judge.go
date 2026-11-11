package change

import (
	"fmt"
	"strconv"
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
// `bash >/dev/null -c ...`、`$SHELL -c ...` 都能把失败变成 0，而外层只看到内层
// 退出码 0。解释器必须处在命令位置（`echo bash -c` 只是打印字面量，不该拦），
// 它后面任何位置出现带 c 的短选项或 --command 都算。
func nestedShell(words []word) bool {
	for _, i := range cmdIndexes(words) {
		w := words[i]
		base := shellBase(w.val)
		if !shellBases[base] && w.val != "$SHELL" && w.val != "${SHELL}" {
			continue
		}
		for _, n := range words[i+1:] {
			if n.cmd && !isAssignment(n.val) {
				break // 已经进入下一条命令
			}
			if n.val == "--command" {
				return true
			}
			if strings.HasPrefix(n.val, "-") && !strings.HasPrefix(n.val, "--") &&
				strings.ContainsRune(n.val, 'c') {
				return true
			}
		}
	}
	return false
}

// wrapperNames 是"自己不做判据、只是把后面那条命令包一层"的命令。
//
// `env bash -c ...`、`timeout 5 bash -c ...`、`sudo bash -c ...` 里的 bash 在
// 参数位置，但它执行的仍是内层 shell——按命令位置只认第一个词会漏掉它们。
// 包装词之后的词（跳过赋值、选项、时长）同样算命令位置。
var wrapperNames = map[string]bool{
	"env": true, "time": true, "timeout": true, "sudo": true, "doas": true,
	"nohup": true, "nice": true, "ionice": true, "stdbuf": true,
	"xargs": true, "watch": true, "setsid": true, "chronic": true,
}

// isDuration 判断 timeout/nice 这类包装命令的数值参数（5、2.5、10s、1m）。
func isDuration(w string) bool {
	if w == "" {
		return false
	}
	end := len(w)
	if strings.ContainsRune("smhd", rune(w[end-1])) {
		end--
	}
	if end == 0 {
		return false
	}
	for i := 0; i < end; i++ {
		if (w[i] < '0' || w[i] > '9') && w[i] != '.' {
			return false
		}
	}
	return true
}

// wrapperOptArg 记录包装命令里"会吃掉下一个词"的选项：`sudo -u root bash -c`、
// `env -u HOME bash -c`、`timeout -s KILL 5 bash -c` 里的 bash 都还在参数位，
// 但它执行的仍是内层 shell，不跳过这些参数就会漏。
var wrapperOptArg = map[string]map[string]bool{
	"sudo":    {"-u": true, "-g": true, "-p": true, "-C": true, "-U": true, "-r": true, "-t": true, "-h": true},
	"doas":    {"-u": true, "-C": true},
	"env":     {"-u": true, "-C": true, "-S": true},
	"nice":    {"-n": true},
	"ionice":  {"-c": true, "-n": true, "-p": true},
	"stdbuf":  {"-i": true, "-o": true, "-e": true},
	"xargs":   {"-n": true, "-I": true, "-P": true, "-L": true, "-s": true, "-E": true, "-d": true},
	"timeout": {"-s": true, "-k": true},
}

// cmdIndexes 给出每条命令的命令词下标：跳过 `FOO=1 bash -c` 这种前置赋值，
// 以及 `env`/`timeout`/`sudo` 这些包装命令自身的参数。
func cmdIndexes(words []word) []int {
	var out []int
	for i := range words {
		if !words[i].cmd {
			continue
		}
		j := i
		for j < len(words) {
			for j < len(words) && isAssignment(words[j].val) {
				j++
			}
			if j >= len(words) {
				break
			}
			out = append(out, j)
			if !wrapperNames[words[j].val] {
				break
			}
			// 包装命令：跳过它自己的选项（含吃参数的选项）与数值参数，
			// 继续找真正被执行的那条。
			opts := wrapperOptArg[words[j].val]
			j++
			for j < len(words) {
				v := words[j].val
				if isAssignment(v) || isDuration(v) {
					j++
					continue
				}
				if !strings.HasPrefix(v, "-") {
					break
				}
				j++
				if opts[v] && j < len(words) {
					j++
				}
			}
		}
	}
	// find 的 -exec/-execdir 后面的词也是被执行命令：`find . -exec bash -c ...`
	// 里的 bash 不在句首，但照样能开一层不受管的内层 shell。
	for k := 0; k+1 < len(words); k++ {
		if words[k].val == "-exec" || words[k].val == "-execdir" {
			out = append(out, k+1)
		}
	}
	return out
}

// isAssignment 判断一个词是不是 `FOO=bar` 形式的前置环境赋值。
func isAssignment(w string) bool {
	i := strings.IndexByte(w, '=')
	if i <= 0 {
		return false
	}
	return !strings.ContainsAny(w[:i], "/.-$`")
}

// hasEval 判断命令位置上有没有 eval：`eval 'false | true'` 的退出码同样由
// 最后一条命令决定。`git log --grep eval` 里的 eval 是参数，不算。
func hasEval(words []word) bool {
	for _, i := range cmdIndexes(words) {
		if words[i].val == "eval" {
			return true
		}
	}
	return false
}

// disablesProtection 判断有没有用它自己的写法关掉保护：`set +e`、`set +o errexit`、
// `set "+o"` 都算（引号在拆词时已去掉）。
// splittingEnv 判断有没有用 `env -S` 把一段字符串再拆成命令：那等于又开了一层，
// 拆出来的 `bash -c '...'` 不在命令位置上，识别不到。
func splittingEnv(words []word) bool {
	for i, w := range words {
		if w.val != "-S" && !strings.HasPrefix(w.val, "-S=") {
			continue
		}
		if i == 0 {
			return false
		}
		if strings.HasSuffix(shellBase(words[i-1].val), "env") {
			return true
		}
	}
	return false
}

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

// trivialJudge 识别"零工作量"判据：整条命令就是 true / : / exit 0。
//
// 机器判断不了判据有没有检验到真东西（那要靠 review），但至少能拒绝一行
// 必然成功的占位——否则"全部通过"只是自欺。
func trivialJudge(words []word) bool {
	var vals []string
	for _, w := range words {
		vals = append(vals, w.val)
	}
	body := strings.Join(vals, " ")
	switch body {
	case "true", ":", "exit 0", "/bin/true", "true 0":
		return true
	}
	if len(vals) == 0 {
		return false
	}
	// 绝对路径与多余参数都是同一个必然成功的命令：/usr/bin/true、true true。
	first := vals[0]
	if i := strings.LastIndexAny(first, "/\\"); i >= 0 {
		first = first[i+1:]
	}
	switch first {
	case "true", ":":
		return true
	// `exit` 不带参数返回上一条命令的状态（这里是 0），`exit 0` 是同一回事；
	// `exit 1` 不是零工作量——它会失败，不需要拦。
	case "exit":
		return len(vals) == 1 || vals[1] == "0"
	}
	return false
}

// compoundKeywords 是能把失败藏起来的构造。判据只该是"跑一条命令、看退出码"：
// 一旦出现条件、循环、分组、取反、包装命令，失败就可能落在 .、fi、变量赋值上，
// 外层看到的 0 已经代表不了判据本身（`test -f x && echo ok` 后接一行在 set -e
// 下照样退出 0；`if ! test -f x; then :; fi` 同理）。
var compoundKeywords = map[string]string{
	"if":       "`if` 即使条件失败也返回 0（走 else 分支）",
	"then":     "`if` 这类条件语句",
	"else":     "`if` 这类条件语句",
	"fi":       "`if` 这类条件语句",
	"elif":     "`if` 这类条件语句",
	"for":      "`for` 循环：失败不会让循环整体非零",
	"while":    "`while` 循环：失败不会让循环整体非零",
	"until":    "`until` 循环：失败不会让循环整体非零",
	"do":       "循环体",
	"done":     "循环体",
	"case":     "`case` 分支",
	"esac":     "`case` 分支",
	"select":   "`select` 循环",
	"!":        "`!` 会把失败翻成成功",
	"command":  "`command` 包装会绕过关键词检查",
	"builtin":  "`builtin` 包装会绕过关键词检查",
	"exec":     "`exec` 会把后续命令顶替当前进程，退出码不再是判据的",
	"source":   "`source` 进来的脚本吞掉失败，外层无从判断",
	".":        "`.` 引入的脚本吞掉失败，外层无从判断",
	"trap":     "`trap` 会改掉退出码传递",
	"function": "函数定义不是一条判据",
	"{":        "`{ ... }` 分组把多条命令揉成一条，失败位置看不出来",
	"}":        "`{ ... }` 分组把多条命令揉成一条，失败位置看不出来",
}

// compound 拒绝"一条判据里塞程序"的写法：串联、条件、循环、取反、命令替换。
//
// 关键取舍：`&&` 也拒。`set -e` 不会因为 `a && b` 里 a 失败而退出（AND-list 是
// 条件上下文），后面再跟一行就把整段跑成 0——模板早期建议用 `&&` 串联，正是
// review 抓到的假绿入口。要串多步就写成多行，每行一条命令。
func compound(code string, words []word) error {
	if strings.Contains(code, "&&") {
		return fmt.Errorf("判据里不要用 `&&` 串联（`&&` 的失败不触发 set -e，后面还有命令时整段会跑成 0）；每行写一条命令")
	}
	if strings.ContainsRune(code, '`') {
		return fmt.Errorf("判据里不要用反引号命令替换（替换失败不会让命令本身非零）")
	}
	if strings.Contains(code, "$(") {
		return fmt.Errorf("判据里不要用 `$( )` 命令替换（替换失败不会让命令本身非零）；需要列表就逐个写出来")
	}
	for _, line := range strings.Split(code, "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \t"), "\\") {
			return fmt.Errorf("判据里不要用行尾反斜杠续行（续行拼接后的失败位置没人看得清）；一行写一条命令")
		}
	}
	if trivialJudge(words) {
		return fmt.Errorf("这条判据什么都没检验（`true`、`:`、`exit 0` 必然成功）——判据要能因为代码坏了而失败")
	}
	for _, i := range cmdIndexes(words) {
		// `command -v go` 是查工具在不在，不是把命令包一层——放行这两个查询选项。
		if words[i].val == "command" && i+1 < len(words) &&
			(words[i+1].val == "-v" || words[i+1].val == "-V") {
			continue
		}
		if why, ok := compoundKeywords[words[i].val]; ok {
			return fmt.Errorf("判据里不能用 %s（%s）；判据只写一条命令，条件逻辑放进被测程序自己的测试", words[i].val, why)
		}
	}
	return nil
}

// lintJudge 拒绝那些能把失败变成 0 的写法。
//
// 文档里的"禁止"必须由机器执行：只写在模板里，agent 换个写法照样封印。
// 操作符检查看"引号外的代码"（`echo "a||b"` 是字面量）；关键词检查看"去引号
// 之后"的词（`bash "-c"` 是调用内层 shell）——两种视图各管各的，都不能省。
//
// 这套检查拦的是常见写法，不是沙箱：判据里刻意做间接调用（脚本里再开 shell、
// 变量拼命令）仍然可能骗过它。判据越直白，证据越可信。
// lexJudge 按 shell 的词法规则把判据拆成两份视图，顺便报出有没有执行命令的构造。
//
// 先 cook 再 scan：cook 去掉注释与行尾续行（shell 在词法阶段就把「反斜杠+换行」
// 删掉，`"$\<换行>(x)"` 实际就是 `"$(x)"`），scan 在剩下的代码上认引号、算术
// 与命令替换。两件事分开做，就不会出现「续行把注释行拼进代码」这种错位。
//
//   - code：引号内内容与算术展开抹成空格，转义字符换成 \x01 占位。操作符检查
//     （||、管道、后台 &）和 compound 看这份：`echo "a||b"`、`echo a\|b` 都是字面量。
//   - cooked：只去过注释与续行，引号与转义原样保留，交给 shellWords 拆词做关键词检查。
//
// cmdSub 报告"会执行命令且失败看不见"的构造：`$( )`、反引号、进程替换 `<( )`/`>( )`。
// 算术展开 `$(( ))` 不算，但内部套的 `$( )` 算。`$` 号按 bash 语义成对消耗：`$$`
// 是 PID，后跟的括号是字面量；`$$$(` 里前两个是 PID、第三个开替换，所以按 i 处
// 连续 `$` 的个数判奇偶——奇数才是命令替换。
func lexJudge(s string) (code, cooked string, cmdSub bool) {
	cooked = cookJudge(s)
	var b strings.Builder
	var quote byte
	arith := 0
	dollars := 0 // 连续未转义 $ 的个数：bash 成对消耗，奇数才开替换
	for i := 0; i < len(cooked); i++ {
		c := cooked[i]
		if arith > 0 {
			// 算术表达式按 bash 的规则像双引号一样处理：里面的 '、"都不起引号作用，
			// 所以 `$(( '$(touch x)' ))` 里的替换照样执行——单引号不能当豁免。
			if c == '`' {
				cmdSub = true
			}
			if c == '$' && i+1 < len(cooked) && cooked[i+1] == '\'' {
				// 算术里 $'\x24(...)' 解码后就是 $( )，会真执行。
				v, end := ansiC(cooked, i)
				if strings.Contains(v, "$(") || strings.Contains(v, "`") {
					cmdSub = true
				}
				b.WriteString(strings.Repeat(" ", end-i+1))
				i = end
				continue
			}
			if c == '$' && i+1 < len(cooked) && cooked[i+1] == '(' {
				if i+2 < len(cooked) && cooked[i+2] == '(' {
					arith += 2
					b.WriteString("   ")
					i += 2
					continue
				}
				cmdSub = true
			}
			if c == '(' {
				arith++
			} else if c == ')' {
				arith--
			}
			b.WriteByte(' ')
			continue
		}
		if c == '$' && i+1 < len(cooked) && cooked[i+1] == '\'' {
			// 外层 $'...' 是 ANSI-C 引号：转义解码后是字面量，不做替换——整段跳过，
			// 但必须按 ANSI-C 规则走到收尾引号，否则收尾的 ' 会被当成开引号，把
			// 后面的真 $( ) 一起抹掉。
			_, end := ansiC(cooked, i)
			b.WriteString(strings.Repeat(" ", end-i+1))
			i = end
			continue
		}
		if quote == '\'' {
			b.WriteByte(' ')
			if c == '\'' {
				quote = 0
			}
			dollars = 0
			continue
		}
		if quote == '"' {
			if c == '\\' && i+1 < len(cooked) {
				// 转义的 $ 是字面量，不参与配对。
				b.WriteString("  ")
				i++
				dollars = 0
				continue
			}
			if c == '"' {
				quote = 0
				b.WriteByte(' ')
				dollars = 0
				continue
			}
			if c == '`' {
				cmdSub = true
			}
			if c == '$' {
				dollars++
			} else {
				dollars = 0
			}
			if c == '$' && i+1 < len(cooked) && cooked[i+1] == '(' {
				if i+2 < len(cooked) && cooked[i+2] == '(' {
					arith += 2
					b.WriteString("   ")
					i += 2
					dollars = 0
					continue
				}
				if dollars%2 == 1 {
					cmdSub = true
				}
			}
			b.WriteByte(' ')
			continue
		}
		// 引号外
		if c == '\\' && i+1 < len(cooked) {
			b.WriteString("\x01 ")
			i++
			dollars = 0
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			b.WriteByte(' ')
			dollars = 0
			continue
		}
		if c == '`' {
			cmdSub = true
		}
		if c == '$' {
			dollars++
		} else {
			dollars = 0
		}
		if c == '$' && i+1 < len(cooked) && cooked[i+1] == '(' {
			if i+2 < len(cooked) && cooked[i+2] == '(' {
				arith += 2
				b.WriteString("   ")
				i += 2
				dollars = 0
				continue
			}
			if dollars%2 == 1 {
				cmdSub = true
			}
		}
		if arith == 0 && (c == '<' || c == '>') && i+1 < len(cooked) && cooked[i+1] == '(' {
			cmdSub = true
		}
		b.WriteByte(c)
	}
	return b.String(), cooked, cmdSub
}

// ansiC 解 $'...' 并返回内容与收尾引号位置。只关心会影响替换识别的转义：
// \xHH、\NNN、\uHHHH 解成对应字符，\' 与 \\ 按字面量处理，其余单字符转义原样保留。
func ansiC(s string, i int) (string, int) {
	var out []byte
	for j := i + 2; j < len(s); j++ {
		c := s[j]
		if c == '\'' {
			return string(out), j
		}
		if c != '\\' || j+1 >= len(s) {
			out = append(out, c)
			continue
		}
		j++
		switch s[j] {
		case 'x', 'u', 'U':
			width, base := 2, 16
			if s[j] == 'u' {
				width = 4
			} else if s[j] == 'U' {
				width = 8
			}
			var buf []byte
			for k := 0; k < width && j+1 < len(s) && isBase(s[j+1], base); k++ {
				j++
				buf = append(buf, s[j])
			}
			if v, err := strconv.ParseUint(string(buf), 16, 32); err == nil && v > 0 && v < 0x110000 {
				out = append(out, []byte(string(rune(v)))...)
			}
		case '0', '1', '2', '3', '4', '5', '6', '7':
			var buf []byte
			for k := 0; k < 3 && j < len(s) && s[j] >= '0' && s[j] <= '7'; k++ {
				if k > 0 {
					j++
				}
				if j < len(s) && s[j] >= '0' && s[j] <= '7' {
					buf = append(buf, s[j])
				}
			}
			if v, err := strconv.ParseUint(string(buf), 8, 32); err == nil {
				out = append(out, byte(v))
			}
		default:
			out = append(out, s[j])
		}
	}
	return string(out), len(s) - 1
}

func isBase(b byte, base int) bool {
	if base == 16 {
		return isHex(b)
	}
	return b >= '0' && b <= '9'
}

// cookJudge 去掉注释、删掉行尾续行（单引号里除外），返回 shell 真正看到的代码。
// 注释只在词首成立：`foo#bar` 里的 # 是普通字符；转义的空白/元字符之后的 # 也是字面量。
func cookJudge(s string) string {
	var b strings.Builder
	var quote byte
	inComment := false
	wordStart := true
	arith := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\n' {
			inComment = false
			wordStart = true
			b.WriteByte(c)
			continue
		}
		if inComment {
			continue
		}
		if c == '\\' && quote != '\'' && i+1 < len(s) && s[i+1] == '\n' {
			// 单引号外的「反斜杠+换行」在词法阶段就消失。
			i++
			continue
		}
		if quote == '\'' {
			b.WriteByte(c)
			if c == '\'' {
				quote = 0
			}
			continue
		}
		if quote == '"' {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				b.WriteByte(s[i+1])
				i++
				continue
			}
			if c == '"' {
				quote = 0
			}
			continue
		}
		if c == '\\' && i+1 < len(s) {
			b.WriteString(s[i : i+2])
			i++
			wordStart = false
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			b.WriteByte(c)
			wordStart = false
			continue
		}
		if c == '#' && wordStart {
			inComment = true
			continue
		}
		if c == '$' && i+2 < len(s) && s[i+1] == '(' && s[i+2] == '(' {
			// 算术展开：内部的 ) 属于同一个词，`$((1+0))#` 的 # 是字面量。
			b.WriteString("$((")
			i += 2
			arith += 2
			wordStart = false
			continue
		}
		if arith > 0 {
			b.WriteByte(c)
			if c == '(' {
				arith++
			} else if c == ')' {
				arith--
			}
			continue
		}
		b.WriteByte(c)
		switch c {
		case ' ', '\t', ';', '&', '|', '(':
			wordStart = true
		case ')':
			wordStart = true
		default:
			wordStart = false
		}
	}
	return b.String()
}

func lintJudge(cmd string) error {
	code, cooked, cmdSub := lexJudge(cmd)
	if cmdSub {
		return fmt.Errorf("判据里不要用 `$( )`/反引号命令替换，也不要用进程替换 `<( )`/`>( )`——双引号里命令替换照样执行，而它们的失败都不会让命令本身非零；只有单引号里才是字面量（算术表达式例外：`$(( ... ))` 里按双引号规则处理，单引号不豁免）")
	}
	// ${IFS} 是空格的字面写法，`bash${IFS}-c` 在 shell 里就是 `bash -c`。
	words := shellWords(strings.ReplaceAll(strings.ReplaceAll(cooked, "${IFS}", " "), "$IFS", " "))
	s := code
	if err := compound(s, words); err != nil {
		return err
	}
	switch {
	case strings.Contains(s, "||"):
		return fmt.Errorf("判据里不能用 `||`（它会把前一条的失败咽掉）；每行写一条命令")
	case strings.Contains(s, "|"):
		return fmt.Errorf("判据里不要用管道 `|`——需要看输出就写进文件，或把逻辑写进脚本（sh 环境没有 pipefail，管道失败会变成通过）")
	case hasSingleAmpersand(s):
		return fmt.Errorf("判据里不能用后台 `&`（后台任务的失败不会被等待）；要并行请写进脚本并 wait")
	case splittingEnv(words):
		return fmt.Errorf("判据里不能用 env -S：它会把一个字符串再拆成命令，等于又开了一层 shell")
	case disablesProtection(words):
		return fmt.Errorf("判据里不能关掉保护（`set +e` / `set +o errexit` 等于允许失败）")
	case hasEval(words):
		return fmt.Errorf("判据里不能用 `eval`（它会把失败吞成最后一条命令的退出码）")
	case nestedShell(words):
		return fmt.Errorf("判据里不能再调用一层 shell（`bash -c`、`bash -lc`、`/bin/sh -c` 都算：内层 shell 不受 pipefail 约束）")
	}
	return nil
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}
