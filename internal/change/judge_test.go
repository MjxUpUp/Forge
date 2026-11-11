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
