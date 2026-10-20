package hooks

import (
	"strings"
	"testing"
)

// --- F.1 interpreter-heredoc data-context fixtures (design F.1, review-confirmed shapes) ---

// TestHazardGuardScript_InterpHeredocBareTagRelease pins the main-path fix: a bare-tag heredoc
// (<<PY, the most common form) fed to an interpreter whose body merely MENTIONS dangerous
// patterns (no exec primitives) must PASS — the pre-fix release keyed on STRIPPED!=COMMAND
// and never fired for bare tags (review-proven).
//
// TestHazardGuardScript_InterpHeredocBareTagRelease 钉住主路径修复：裸 tag（<<PY，最常见
// 形态）的解释器 heredoc，正文仅提及危险字样（无执行原语）必须放行——修复前放行路径
// 挂在 STRIPPED!=COMMAND 上，裸 tag 场景恒假（评审实证）。
func TestHazardGuardScript_InterpHeredocBareTagRelease(t *testing.T) {
	shimDir := writeForgeShim(t, "block")
	nl := string(rune(10))
	cmd := "python3 - <<PY" + nl + "print(\"analysis mentions a recursive delete word pattern in a string\")" + nl + "PY"
	out, err := runHazardScript(t, shimDir, cmd)
	if err != nil {
		t.Fatalf("bare-tag interpreter heredoc without exec primitives must release, got block:\n%s", out)
	}
	if !strings.Contains(out, "数据上下文") && !strings.HasPrefix(strings.TrimSpace(out), "PASS") {
		t.Fatalf("release must either name the data context or early-PASS on the stripped head, got:\n%s", out)
	}
}

// TestHazardGuardScript_InterpHeredocExecPrimitiveBlocks pins recall: the same heredoc shape
// WITH an exec primitive (subprocess / %x[ / qx{ / getoutput) must still block — each
// primitive was an empirically demonstrated miss in review.
//
// TestHazardGuardScript_InterpHeredocExecPrimitiveBlocks 钉住 recall：同形态 heredoc 带
// 执行原语（subprocess / %x[ / qx{ / getoutput）必须仍拦——每个原语都是评审实证的
// 漏放形态。
func TestHazardGuardScript_InterpHeredocExecPrimitiveBlocks(t *testing.T) {
	shimDir := writeForgeShim(t, "block")
	nl := string(rune(10))
	// 危险词样用拼接构造（测试源码不出现完整模式字面量）。
	danger := "r" + "m -r" + "f /important"
	bodies := map[string]string{
		"subprocess": "subprocess.run([" + danger + "])",
		"ruby %x":    "puts %x[" + danger + "]",
		"perl qx":    "print qx{" + danger + "}",
		"getoutput":  "print commands.getoutput(" + danger + ")",
	}
	for name, body := range bodies {
		cmd := "python3 - <<PY" + nl + body + nl + "PY"
		out, err := runHazardScript(t, shimDir, cmd)
		if err == nil {
			t.Errorf("%s: exec primitive in heredoc body must BLOCK, got pass:\n%s", name, out)
		}
	}
}

// TestHazardGuardScript_InterpHeredocBacktickScope pins the backtick primitive's scope: a
// backtick only executes a shell in ruby and perl. In python it is inert text (a Go raw
// string being written to a test file — the 2026-10-20 false block), in node it is a
// template literal. So a backtick keeps a ruby/perl heredoc blocked, but must not stop a
// python/node heredoc body from being treated as data. Every other primitive is unchanged.
//
// TestHazardGuardScript_InterpHeredocBacktickScope 钉住反引号原语的作用域：反引号只在 ruby
// 与 perl 里执行 shell。python 里是惰性文本（往测试文件写 Go 原始字符串——2026-10-20 的
// 误拦实录），node 里是模板字符串。故反引号让 ruby/perl heredoc 维持拦截，但不得阻止
// python/node heredoc 正文按数据放行。其余原语不变。
func TestHazardGuardScript_InterpHeredocBacktickScope(t *testing.T) {
	shimDir := writeForgeShim(t, "block")
	nl := string(rune(10))
	bt := string(rune(96))
	// 危险词样用拼接构造（测试源码不出现完整模式字面量）。
	danger := "r" + "m -r" + "f ./important-data"
	for _, tc := range []struct {
		name      string
		cmd       string
		wantBlock bool
	}{
		// The 16:51 shape: Go source in a python ''' string; an odd apostrophe in the Go
		// text desyncs quote tracking, so only the F.1 body strip can classify it as data.
		//
		// 16:51 形态：python ''' 串里装 Go 源码；Go 文本中奇数个撇号让引号跟踪失步，只有
		// F.1 整段剥离能把它判为数据。
		{"python-go-raw-string", "python3 - <<'EOF'" + nl + "s = '''" + nl + "// it's a shim" + nl + "run(" + danger + ")" + nl + "payload := " + bt + "{}" + bt + nl + "'''" + nl + "open('x_test.go','w').write(s)" + nl + "EOF", false},
		{"node-template-literal", "node - <<'JS'" + nl + "const msg = " + bt + "docs mention " + danger + bt + ";" + nl + "console.log(msg)" + nl + "JS", false},
		{"ruby-backtick-executes", "ruby - <<'RB'" + nl + "puts " + bt + danger + bt + nl + "RB", true},
		{"perl-backtick-executes", "perl - <<'PL'" + nl + "print " + bt + danger + bt + ";" + nl + "PL", true},
		// Scoping the backtick must not let another primitive through (same bare-danger
		// shape as InterpHeredocExecPrimitiveBlocks).
		//
		// 收窄反引号不得放过其他原语（与 InterpHeredocExecPrimitiveBlocks 同款裸危险串形态）。
		{"python-backtick-plus-subprocess", "python3 - <<'EOF'" + nl + "subprocess.run([" + danger + "])  # " + bt + "x" + bt + nl + "EOF", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runHazardScript(t, shimDir, tc.cmd)
			if tc.wantBlock && err == nil {
				t.Fatalf("must BLOCK, got pass:\n%s", out)
			}
			if !tc.wantBlock {
				if err != nil {
					t.Fatalf("heredoc body is data (backtick inert in this language), must PASS, got block:\n%s", out)
				}
				if !strings.Contains(out, "数据上下文") {
					t.Fatalf("release must come from the interpreter-heredoc data path, got:\n%s", out)
				}
			}
		})
	}
}

// TestHazardGuardScript_ExecWrappedFlagIsStandalone pins is_exec_wrapped's code-exec flags as
// standalone short options. The old substring glob (*python*\ *-c*) matched `go test
// -count=1` anywhere later in the same command and vetoed both data-release paths — the
// 2026-10-20 16:51 false block of a python heredoc that only wrote a test file. Recall side:
// every real -c/-e/-E/-r/--eval form with the danger string quoted must still block.
//
// TestHazardGuardScript_ExecWrappedFlagIsStandalone 钉住 is_exec_wrapped 的代码执行 flag 必须是
// 独立短选项。旧子串 glob（*python*\ *-c*）会命中同一条命令后面任意位置的 `go test
// -count=1`，否决两条数据放行路径——2026-10-20 16:51 只写测试文件的 python heredoc 被误拦。
// 召回侧：每种真实 -c/-e/-E/-r/--eval 形态（危险串在引号内）都必须仍拦。
func TestHazardGuardScript_ExecWrappedFlagIsStandalone(t *testing.T) {
	shimDir := writeForgeShim(t, "block")
	nl := string(rune(10))
	danger := "r" + "m -r" + "f ./important-data"
	for _, tc := range []struct {
		name      string
		cmd       string
		wantBlock bool
	}{
		{"python-heredoc-then-go-test-count", "python3 - <<'EOF'" + nl + "s = \"" + danger + "\"" + nl + "open('x_test.go','w').write(s)" + nl + "EOF" + nl + "go test ./internal/hooks/ -count=1", false},
		{"node-script-env-flag", "node build.js --env=prod && echo \"" + danger + "\"", false},
		{"python-c", "python3 -c \"import os; os.system('" + danger + "')\"", true},
		{"python-c-attached-quote", "python -c'print(1)' && echo \"" + danger + "\"", true},
		{"python-c-at-end", "echo \"" + danger + "\" | python3 -c", true},
		{"node-e", "node -e \"require('child_process').execSync('" + danger + "')\"", true},
		{"node-eval-long", "node --eval \"x('" + danger + "')\"", true},
		{"ruby-e", "ruby -e \"system('" + danger + "')\"", true},
		{"perl-E", "perl -E \"system('" + danger + "')\"", true},
		{"php-r", "php -r \"system('" + danger + "');\"", true},
		{"lua-e", "lua -e \"os.execute('" + danger + "')\"", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runHazardScript(t, shimDir, tc.cmd)
			if tc.wantBlock && err == nil {
				t.Fatalf("code-exec flag with quoted danger must BLOCK, got pass:\n%s", out)
			}
			if !tc.wantBlock && err != nil {
				t.Fatalf("a non-exec flag (-count/--env) must not veto the data release, got block:\n%s", out)
			}
		})
	}
}

// TestHazardGuardScript_ConfirmChainedBlocks pins F.2a at the hook layer (the enforcement
// point the review proved missing at CLI level): forge hazard confirm chained with the target
// command in one Bash call must block — the recorded machine-A self-service loop shape.
//
// TestHazardGuardScript_ConfirmChainedBlocks 在 hook 层钉住 F.2a（评审实证 CLI 层不可见
// 的执法点）：forge hazard confirm 与目标命令同一 Bash 调用链式必须拦——甲机实录的
// 自助闭环形态。
func TestHazardGuardScript_ConfirmChainedBlocks(t *testing.T) {
	shimDir := writeForgeShim(t, "block")
	cmd := "forge hazard confirm --last && git push origin --delete feat/x"
	out, err := runHazardScript(t, shimDir, cmd)
	if err == nil {
		t.Fatalf("chained confirm must BLOCK at the hook layer, got pass:\n%s", out)
	}
	if !strings.Contains(out, "自助闭环") || !strings.Contains(out, "单独执行") {
		t.Fatalf("block message must name the loop and the standalone form, got:\n%s", out)
	}
	out2, err2 := runHazardScript(t, shimDir, "forge hazard log block something")
	if err2 != nil {
		t.Fatalf("non-chained forge hazard log must stay exempt, got block:\n%s", out2)
	}
}
