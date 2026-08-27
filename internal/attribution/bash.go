package attribution

import (
	"regexp"
	"strings"
)

// bashWriteTargets conservatively extracts file paths a shell command plausibly writes.
// The multi-task-concurrency design (§6) spawns this as the T2-measured `bash-infer`
// signal: shell is Turing-complete, so this parser only trusts unambiguous syntactic
// shapes and deliberately misses everything else — a miss degrades to orphan (honest),
// a false hit would misattribute (dishonest). Precision over recall.
//
// Recognized shapes (per whitespace-token, shell-quote-naive):
//   - `cmd > path` / `cmd >> path` — redirection targets (the token after > / >>)
//   - `path` following `tee`/`cp`/`mv`'s LAST operand rule: cp/mv destination is the
//     final path operand; tee writes its operand
//   - `sed -i[ext] … path` — every non-flag trailing operand of an in-place sed
//
// Excluded on purpose: `echo x >file` with no space (rare in agent commands), command
// substitution outputs, process substitution >(...), anything inside $() — parsing those
// reliably needs a real shell grammar.
//
// bashWriteTargets 保守提取 shell 命令可能写入的文件路径。multi-task-concurrency
// 设计（§6）把它作为 T2 度量的 `bash-infer` 信号：shell 图灵完备，解析器只信无歧义
// 的语法形状、刻意漏掉其余——漏掉的降级为无主（诚实），误命中会错误归属（不诚实）。
// 精确率优先于召回率。
//
// 刻意排除：无空格的 `echo x >file`（agent 命令里罕见）、命令替换输出、进程替换
// >(...)、$() 内部——可靠解析那些需要真正的 shell 文法。
var (
	redirRe = regexp.MustCompile(`(?:^|\s)(?:>>?)\s*([^\s;|&]+)`)
	// sedFlagRe matches -i, -i.bak, -e 'expr' style flags so trailing operands stay clean.
	sedFlagRe = regexp.MustCompile(`^-[a-zA-Z](\.[^\s]+)?$|^-$`)
)

// isPathLike reports whether a token can be a file operand: contains a path separator or
// a dot-extension, and is not a flag / shell syntax. Filters command words (cp, mv) and
// variables ($X).
//
// isPathLike 报告 token 是否可能是文件操作数：含路径分隔符或点扩展名，且不是
// flag / shell 语法。过滤命令词（cp、mv）与变量（$X）。
func isPathLike(tok string) bool {
	if tok == "" || strings.HasPrefix(tok, "-") || strings.HasPrefix(tok, "$") ||
		strings.ContainsAny(tok, "<>();|&`'\"") {
		return false
	}
	return strings.ContainsRune(tok, '/') || strings.ContainsRune(tok, '.')
}

func bashWriteTargets(command string) []string {
	var targets []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			targets = append(targets, p)
		}
	}
	for _, m := range redirRe.FindAllStringSubmatch(command, -1) {
		// Redirection targets get the same operand filter: $(), process substitution and
		// flag-shaped tokens are not file paths (misattribution is worse than a miss).
		//
		// 重定向目标走同一操作数过滤：$()、进程替换、flag 形状的 token 不是文件路径
		//（误归属比漏掉更糟）。
		if isPathLike(m[1]) {
			add(m[1])
		}
	}
	// Tokenize per command segment (split on ; && || | — good-enough approximation of
	// where a new simple command starts).
	for _, seg := range strings.FieldsFunc(command, func(r rune) bool {
		return r == ';' || r == '|' || r == '&' || r == '\n'
	}) {
		toks := strings.Fields(seg)
		if len(toks) == 0 {
			continue
		}
		base := strings.TrimSuffix(toks[0], "/bin/") // handles /usr/bin/sed too
		base = lastSegment(base)
		switch base {
		case "sed":
			inPlace := false
			for _, t := range toks[1:] {
				if strings.HasPrefix(t, "-i") {
					inPlace = true
					continue
				}
				if sedFlagRe.MatchString(t) {
					continue // -n -e 'expr' 等 flag
				}
				if inPlace && isPathLike(t) {
					add(t)
				}
			}
		case "cp", "mv":
			// destination is the final operand
			var operands []string
			for _, t := range toks[1:] {
				if strings.HasPrefix(t, "-") {
					continue
				}
				if isPathLike(t) {
					operands = append(operands, t)
				}
			}
			if len(operands) >= 2 {
				add(operands[len(operands)-1])
			}
		case "tee":
			for _, t := range toks[1:] {
				if isPathLike(t) {
					add(t)
				}
			}
		}
	}
	return targets
}

func lastSegment(p string) string {
	if idx := strings.LastIndexByte(p, '/'); idx >= 0 {
		return p[idx+1:]
	}
	return p
}
