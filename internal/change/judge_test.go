package change

import (
	"strings"
	"testing"
)

func TestShellWords(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"go test ./... -run X", []string{"go", "test", "./...", "-run", "X"}},
		{`bash "-c" 'false | true'`, []string{"bash", "-c", "false | true"}},
		{`bash \-c 'x'`, []string{"bash", "-c", "x"}},
		{"set +e\nfalse", []string{"set", "+e", "false"}},
		{"echo a#b # comment here", []string{"echo", "a#b"}},
		{"A=1 bash -lc 'x'", []string{"A=1", "bash", "-lc", "x"}},
		{"echo \"two  spaces\"", []string{"echo", "two  spaces"}},
	}
	for _, c := range cases {
		got := shellWords(c.in)
		var vals []string
		for _, w := range got {
			vals = append(vals, w.val)
		}
		if strings.Join(vals, "\x00") != strings.Join(c.want, "\x00") {
			t.Errorf("%q: got %q, want %q", c.in, vals, c.want)
		}
	}
}

func TestNestedShellPathsAndFlags(t *testing.T) {
	reject := []string{
		"bash -lc 'false | true'",
		"/bin/sh -c 'false | true'",
		`"C:/Git/bin/bash.exe" -c 'true'`,
		`C:\\Git\\bin\\bash.exe -c 'true'`,
		"bash${IFS}-c 'true'",
		"sh -ec 'true'",
		"bash --command 'true'",
		"$SHELL -c 'true'",
	}
	for _, in := range reject {
		words := shellWords(strings.ReplaceAll(strings.ReplaceAll(in, "${IFS}", " "), "$IFS", " "))
		if !nestedShell(words) {
			t.Errorf("%q 应被认成嵌套 shell", in)
		}
	}
	allow := []string{
		"go test ./... -c",
		`echo "bash -c"`,
		"bash script.sh",
		"grep -c foo file.txt",
	}
	for _, in := range allow {
		if nestedShell(shellWords(in)) {
			t.Errorf("%q 不该被认成嵌套 shell", in)
		}
	}
}

func TestDisablesProtection(t *testing.T) {
	for _, in := range []string{"set +e", "set +o errexit", `set "+o" errexit`, "set -o pipefail; set +e"} {
		if !disablesProtection(shellWords(in)) {
			t.Errorf("%q 应被认成关掉保护", in)
		}
	}
	for _, in := range []string{"set -e", "set -eo pipefail", "settings +e"} {
		if disablesProtection(shellWords(in)) {
			t.Errorf("%q 不该被认成关掉保护", in)
		}
	}
}

func TestHasEvalOnlyInCommandPosition(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"eval 'false || true'", true},
		{"true; eval 'false'", true},
		{"git log --grep eval", false},
		{"echo eval", false},
	}
	for _, c := range cases {
		words := shellWords(c.in)
		if got := hasEval(words); got != c.want {
			t.Errorf("%q: got %v, want %v", c.in, got, c.want)
		}
	}
}

func TestLintJudgeAllowsQuotedOperators(t *testing.T) {
	if err := lintJudge(`echo "a||b"`); err != nil {
		t.Errorf("引号内的 || 是字面量：%v", err)
	}
	if err := lintJudge("echo a\\|b"); err != nil {
		t.Errorf("转义的 | 是字面量：%v", err)
	}
	if err := lintJudge("false || true"); err == nil {
		t.Error("引号外的 || 应被拒")
	}
}

// 判据只该是"跑一条命令、看退出码"。条件、串联、替换都会让退出码不再代表
// 判据本身——`test -f x && echo ok` 后面再跟一行，整段就是 0。
func TestLintJudgeRejectsCompoundPrograms(t *testing.T) {
	bad := []string{
		"test -f x && echo ok",
		"if ! test -f x; then echo missing; fi",
		"for f in a b; do test -f $f; done",
		"! grep -q foo bar.txt",
		"command eval 'false || true'",
		"FOO=1 eval 'false || true'",
		"echo $(false)",
		"echo `false`",
		"bash >/dev/null -c 'false | true'",
		"bash \\\n-c 'false | true'",
		"env bash -c 'false || true'",
		"timeout 5 bash -c 'false || true'",
		"sudo bash -lc 'false || true'",
		"time eval 'false || true'",
		"true",
		":",
		"exit 0",
		"/usr/bin/true",
		"true true",
		"sudo -u root bash -c 'false || true'",
		"find . -maxdepth 0 -exec bash -c 'false || true' {} \\;",
		"{ eval 'false || true'; }",
	}
	for _, cmd := range bad {
		if err := lintJudge(cmd); err == nil {
			t.Errorf("应被拒绝：%q", cmd)
		}
	}
}

// 反过来，参数里出现 bash/eval/sh -c 的字面量不是违规：判据是 grep，不是
// 它搜索的文本。命令位置才是关键词。
func TestLintJudgeAllowsLookalikeArguments(t *testing.T) {
	ok := []string{
		"grep -rn eval internal/",
		"grep -n \"sh -c\" README.md",
		"echo bash -c",
		"go test ./... -c",
		"go build ./...",
		"timeout 300 go test ./...",
		"command -v go",
	}
	for _, cmd := range ok {
		if err := lintJudge(cmd); err != nil {
			t.Errorf("不该被拒：%q（%v）", cmd, err)
		}
	}
}

func TestLintJudgeRejectsSubstitutionInDoubleQuotes(t *testing.T) {
	// 双引号里的替换在 shell 里照样执行：抹掉引号内容再查会漏掉它。
	for _, cmd := range []string{
		`echo "$(false)"`,
		`printf "%s" "$(false)"`,
		"echo \"`false`\"",
		`go test "$(echo ./internal/change/)"`,
	} {
		if err := lintJudge(cmd); err == nil || !strings.Contains(err.Error(), "命令替换") {
			t.Fatalf("%q must be rejected for command substitution, got %v", cmd, err)
		}
	}
	// 单引号里是字面量，不该误杀。
	if err := lintJudge(`echo '$(false)'`); err != nil {
		t.Fatalf("single-quoted text must stay allowed: %v", err)
	}
}

func TestLintJudgeCommentDoesNotShiftQuotes(t *testing.T) {
	// 注释里落单的引号不该把引号状态带进下一行——那是 review 找到的绕过：
	// `# don't` 之后的 `echo "$(false)"` 曾经直接封印。
	reject := []string{
		"# don't\necho \"$(false)\"",
		"echo ok # \"\necho \"$(false)\"",
		"echo ok # it's fine\nprintf '%s' \"$(false)\"",
	}
	for _, cmd := range reject {
		if err := lintJudge(cmd); err == nil || !strings.Contains(err.Error(), "命令替换") {
			t.Fatalf("%q must be rejected despite comments, got %v", cmd, err)
		}
	}
	// 注释里的替换是字面量，不该误杀。
	for _, cmd := range []string{
		"echo ok # \"$(false)\"",
		"echo ok # don't run $(false)",
	} {
		if err := lintJudge(cmd); err != nil {
			t.Fatalf("%q is only a comment, got %v", cmd, err)
		}
	}
	// `foo#bar` 里的 `#` 不是注释。
	if err := lintJudge("echo foo#bar; echo \"$(false)\""); err == nil {
		t.Fatalf("`#` mid-word is not a comment; substitution must be rejected")
	}
}

func TestLintJudgeLineContinuationAndArithmetic(t *testing.T) {
	// bash 在词法阶段删掉「反斜杠+换行」：这两行在 shell 里就是 `echo "$(false)"`。
	joined := "echo \"$\\\n(false)\""
	if err := lintJudge(joined); err == nil || !strings.Contains(err.Error(), "命令替换") {
		t.Fatalf("backslash-newline continuation must be rejected, got %v", err)
	}
	// 单引号里的反斜杠+换行是字面量，不该误杀。
	if err := lintJudge("echo 'a\\\nb'"); err != nil {
		t.Fatalf("single-quoted continuation is literal: %v", err)
	}
	// 算术展开不是命令替换。
	for _, cmd := range []string{`echo "$((1+0))"`, `test "$((1+1))" -eq 2`, `echo $((2*3))`} {
		if err := lintJudge(cmd); err != nil {
			t.Fatalf("%q is arithmetic, not substitution: %v", cmd, err)
		}
	}
	// 算术里套命令替换仍要拒。
	if err := lintJudge(`echo "$(( $(false) ))"`); err == nil {
		t.Fatalf("nested command substitution inside arithmetic must be rejected")
	}
}

func TestLintJudgeEscapedSpaceBeforeHash(t *testing.T) {
	// shell 里 `foo\ #` 的空格是字面量，`#` 不是注释开头，后面的 $(false) 真会执行。
	for _, cmd := range []string{
		`echo foo\ #; echo $(false)`,
		`echo foo\ #; echo "$(false)"`,
	} {
		if err := lintJudge(cmd); err == nil {
			t.Fatalf("%q: escaped space makes # literal, substitution must be rejected", cmd)
		}
	}
	// 真注释里的替换不该误杀。
	if err := lintJudge(`echo foo # $(false)`); err != nil {
		t.Fatalf("plain comment must stay allowed: %v", err)
	}
}

func TestLintJudgeHashAfterArithmeticIsLiteral(t *testing.T) {
	// `$((1+0))#` 的 `#` 紧贴 `)`，是字面量不是注释，后面的替换真会执行。
	if err := lintJudge(`echo $((1+0))#; echo "$(false)"`); err == nil {
		t.Fatalf("hash right after arithmetic is literal, substitution must be rejected")
	}
	// 真正的行首/空白后注释仍放行。
	if err := lintJudge(`echo $((1+0)) # $(false)`); err != nil {
		t.Fatalf("comment after a space is still a comment: %v", err)
	}
}

func TestLintJudgeCommentAndContinuationInterplay(t *testing.T) {
	// 注释在换行处就结束，行尾反斜杠不会把注释续到下一行——下一行照跑。
	for _, cmd := range []string{
		"# note \\\necho \"$(touch pwned)\"",
		"echo ok # note \\\necho \"$(touch pwned)\"",
	} {
		if err := lintJudge(cmd); err == nil {
			t.Fatalf("%q: comment ends at newline, substitution on next line must be rejected", cmd)
		}
	}
	// 转义的分号/与号/管道/左括号都是字面量，紧跟的 # 不是注释。
	for _, cmd := range []string{
		`echo a\;#$(touch pwned)`,
		`echo a\&#$(touch pwned)`,
		`echo a\|#$(touch pwned)`,
		`echo a\(#$(touch pwned)`,
	} {
		if err := lintJudge(cmd); err == nil {
			t.Fatalf("%q: escaped metachar makes # literal, substitution must be rejected", cmd)
		}
	}
	// 子 shell 结束后的 # 是真注释。
	if err := lintJudge(`(echo x)# $(touch pwned)`); err != nil {
		t.Fatalf("comment after subshell must stay allowed: %v", err)
	}
	// $$ 是 PID，后面的括号是字面量。
	if err := lintJudge(`echo "$$(false)"`); err != nil {
		t.Fatalf("$$ is a pid, (false) is literal: %v", err)
	}
	// 进程替换会执行命令，失败看不见。
	if err := lintJudge(`echo <(touch pwned)`); err == nil {
		t.Fatalf("process substitution executes a command, must be rejected")
	}
}

func TestLintJudgeDollarRunsAndArithmeticLeak(t *testing.T) {
	// `$$` 成对消耗：奇数个 $ 才开替换。
	for _, cmd := range []string{
		`echo "$$$(touch pwned)"`,
		`echo "$$$$$(touch pwned)"`,
		`echo $$$(touch pwned)`,
	} {
		if err := lintJudge(cmd); err == nil {
			t.Fatalf("%q: an odd $ run still starts a substitution", cmd)
		}
	}
	for _, cmd := range []string{
		`echo "$$(false)"`,
		`echo "$$$$(false)"`,
	} {
		if err := lintJudge(cmd); err != nil {
			t.Fatalf("%q: $ pairs are a pid, parentheses are literal: %v", cmd, err)
		}
	}
	// 双引号里的算术不该吃掉后面的操作符。
	if err := lintJudge(`echo "$((1))"; false || touch pwned`); err == nil {
		t.Fatalf("|| after arithmetic must still be rejected")
	}
	if err := lintJudge(`echo "$((1))"; false | cat`); err == nil {
		t.Fatalf("pipe after arithmetic must still be rejected")
	}
	// >( ) 也是进程替换。
	if err := lintJudge(`echo >(touch pwned)`); err == nil {
		t.Fatalf(">( ) is process substitution too")
	}
	// 算术里的 < 不是进程替换：位移与比较照常。
	for _, cmd := range []string{`echo $((1<<(2+1)))`, `echo $((1<(2)))`, `echo "$((1<<(2+1)))"`} {
		if err := lintJudge(cmd); err != nil {
			t.Fatalf("%q: < inside arithmetic is a shift/compare: %v", cmd, err)
		}
	}
}
