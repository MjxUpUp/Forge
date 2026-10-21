package hooks

import (
	"slices"
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
	// The data-context release is silent on stdout (it is diagnostics, not an instruction to
	// the model); its proof is the ledger event. The other legitimate release is the early
	// PASS on the stripped head.
	if !slices.Contains(loggedHazardTypes(t, shimDir), "data") && !strings.HasPrefix(strings.TrimSpace(out), "PASS") {
		t.Fatalf("release must either log a data-context event or early-PASS on the stripped head, got stdout:\n%s", out)
	}
	if strings.Contains(out, "数据上下文") {
		t.Fatalf("a data-context release must not narrate itself on stdout (the host injects stdout into the conversation), got:\n%s", out)
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

// TestHazardGuardScript_InterpHeredocBacktickScope pins the backtick primitive's scope: only a
// python-only command is exempt — a python backtick is inert text (a Go raw string being
// written to a test file, the 2026-10-20 false block). Any ruby / perl / node in the command
// keeps the veto: ruby/perl backticks run a shell, and node tagged templates (zx $, $.sync,
// $({...}), aliases) do too in more shapes than can be enumerated. Every other primitive is
// unchanged.
//
// TestHazardGuardScript_InterpHeredocBacktickScope 钉住反引号原语的作用域：只豁免纯 python
// 命令——python 里反引号是惰性文本（往测试文件写 Go 原始字符串，2026-10-20 误拦实录）。
// 命令含 ruby / perl / node 任一即维持否决：ruby/perl 反引号执行 shell，node 的标签模板
// （zx 的 $、$.sync、$({...})、别名）同样执行且形态无法穷举。其余原语不变。
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
		// Node stays conservative: a plain template literal is indistinguishable from a
		// zx-style tagged template at this layer (review R1).
		//
		// node 保持保守：本层分不清普通模板字符串与 zx 式标签模板（审查 R1）。
		{"node-template-literal-conservative", "node - <<'JS'" + nl + "const msg = " + bt + "docs mention " + danger + bt + ";" + nl + "console.log(msg)" + nl + "JS", true},
		{"node-zx-options-call", "node --input-type=module - <<'JS'" + nl + "import { $ } from 'zx'" + nl + "await $({nothrow:true})" + bt + danger + bt + nl + "JS", true},
		{"node-zx-aliased", "node --input-type=module - <<'JS'" + nl + "import { $ as sh } from 'zx'" + nl + "await sh" + bt + danger + bt + nl + "JS", true},
		{"ruby-backtick-executes", "ruby - <<'RB'" + nl + "puts " + bt + danger + bt + nl + "RB", true},
		{"perl-backtick-executes", "perl - <<'PL'" + nl + "print " + bt + danger + bt + ";" + nl + "PL", true},
		// Scoping the backtick must not let another primitive through (same bare-danger
		// shape as InterpHeredocExecPrimitiveBlocks).
		//
		// 收窄反引号不得放过其他原语（与 InterpHeredocExecPrimitiveBlocks 同款裸危险串形态）。
		{"python-backtick-plus-subprocess", "python3 - <<'EOF'" + nl + "subprocess.run([" + danger + "])  # " + bt + "x" + bt + nl + "EOF", true},
		// zx / Bun.$ tagged template runs a shell even without child_process.
		//
		// zx / Bun.$ 标签模板即便不含 child_process 也执行 shell。
		{"node-zx-tagged-template", "node --input-type=module - <<'JS'" + nl + "import { $ } from 'zx'" + nl + "await $" + bt + danger + bt + nl + "JS", true},
		{"node-execa-template", "node - <<'JS'" + nl + "const { execaCommand } = await import('execa')" + nl + "await execaCommand(" + danger + ")" + nl + "JS", true},
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
				if !slices.Contains(loggedHazardTypes(t, shimDir), "data") {
					t.Fatalf("release must come from the interpreter-heredoc data path (a data event in the ledger), got stdout:\n%s", out)
				}
				if strings.TrimSpace(out) != "" {
					t.Fatalf("the data-path release is silent on stdout, got:\n%s", out)
				}
			}
		})
	}
}

// TestHazardGuardScript_InterpHeredocOnlyFirstBodyStripped pins F.1's strip boundary: only the
// first heredoc's body is data. After its delimiter, a later "<<" or "<<<" must not re-enter
// body mode — the old awk then waited for the first tag forever and swallowed every following
// line, including commands that really execute.
//
// TestHazardGuardScript_InterpHeredocOnlyFirstBodyStripped 钉住 F.1 的剥离边界：只有第一个
// heredoc 的正文是数据。其 delimiter 之后再出现的「<<」或「<<<」不得重新进入正文态——旧 awk
// 会苦等永不再现的首个 tag，把其后所有行（包括真执行的命令）整段吞掉。
func TestHazardGuardScript_InterpHeredocOnlyFirstBodyStripped(t *testing.T) {
	shimDir := writeForgeShim(t, "block")
	nl := string(rune(10))
	danger := "r" + "m -r" + "f ./important-data"
	for _, tc := range []struct {
		name      string
		cmd       string
		wantBlock bool
	}{
		{"then-bash-heredoc", "python3 - <<'PY'" + nl + "print(1)" + nl + "PY" + nl + "bash <<'SH'" + nl + danger + nl + "SH", true},
		{"then-herestring-then-bare", "python3 - <<'PY'" + nl + "print(1)" + nl + "PY" + nl + "cat <<<x" + nl + danger, true},
		{"single-heredoc-data-still-released", "python3 - <<'PY'" + nl + "print('docs mention " + danger + "')" + nl + "PY" + nl + "echo done", false},
		// A primitive in a LATER heredoc still vetoes stripping the first body (review N2).
		//
		// 后面 heredoc 的执行原语仍否决首段剥离（审查 N2）。
		{"later-heredoc-primitive-vetoes", "python3 - <<'A'" + nl + danger + nl + "A" + nl + "python3 - <<'B'" + nl + "import subprocess" + nl + "subprocess.run(['bash','x.sh'])" + nl + "B", true},
		// The first << must belong to the interpreter command (review N3): an interpreter
		// name elsewhere in the command does not make a bash heredoc data.
		//
		// 第一个 << 须归属解释器命令（审查 N3）：命令别处提到解释器名不能让 bash heredoc 变数据。
		{"bash-heredoc-after-node-path", "ls node_modules && bash <<'SH'" + nl + danger + nl + "SH", true},
		// The opener is the first heredoc <<, never a <<< herestring; and a command
		// substitution in the owner segment cannot lend it an interpreter name.
		//
		// opener 是第一个 heredoc 的 <<，绝不是 <<< herestring；归属段里的命令替换不能
		// 借给它解释器名。
		{"herestring-then-bash-heredoc", "python3 - <<< 'print(1)'" + nl + "bash <<'SH'" + nl + danger + nl + "SH", true},
		{"cmdsubst-node-owner", "V=$(node -v) bash <<'SH'" + nl + danger + nl + "SH", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runHazardScript(t, shimDir, tc.cmd)
			if tc.wantBlock && err == nil {
				t.Fatalf("command after the first heredoc must stay visible and BLOCK, got pass:\n%s", out)
			}
			if !tc.wantBlock && err != nil {
				t.Fatalf("first heredoc body is data, must PASS, got block:\n%s", out)
			}
		})
	}
}

// TestHazardGuardScript_ExecWrappedFlagIsStandalone pins is_exec_wrapped's code-exec flags as
// segment-local: the interpreter and its -c/-e/-E/-r flag must sit in the SAME command
// segment (backslash continuations joined; split on newline ; && || |). The old whole-command
// glob (*python*\ *-c*) matched "go test -count=1" in a later segment and vetoed both
// data-release paths — the 2026-10-20 16:51 false block of a python heredoc that only wrote a
// test file. Recall side: every real form with the danger string quoted still blocks,
// including code glued to the flag (-c__import__ / -esystem) and a continuation line.
//
// TestHazardGuardScript_ExecWrappedFlagIsStandalone 钉住 is_exec_wrapped 的代码执行 flag 按
// 命令段判定：解释器与其 -c/-e/-E/-r 须在**同一命令段**（续行先拼接；按换行 ; && || | 切分）。
// 旧的整条命令 glob（*python*\ *-c*）会命中后一段的 go test -count=1，否决两条数据放行路径
// ——2026-10-20 16:51 只写测试文件的 python heredoc 被误拦。召回侧：每种真实形态（危险串在
// 引号内）都仍拦，含代码紧贴 flag（-c__import__ / -esystem）与续行。
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
		// Quote-aware split: the | inside the grep pattern and the & in 2>&1 do not cut segments.
		//
		// 引号感知切段：grep 模式里的 | 与 2>&1 里的 & 不切段。
		{"python-heredoc-then-piped-go-test", "python3 - <<'EOF'" + nl + "s = \"" + danger + "\"" + nl + "EOF" + nl + "go test ./x -count=1 -v 2>&1 | grep -E \"^(--- |ok|FAIL)\" | head -5", false},
		// A segment running a script (node build.js) can do anything: the original verdict
		// stands even though the -e belongs to grep (review R2: only provably inert segments
		// explain a hit away).
		//
		// 跑脚本的段（node build.js）可做任何事：即便 -e 属于 grep 也维持原判（审查 R2：只有
		// 可证明不执行代码的段才能解释掉命中）。
		{"node-script-then-grep-e-keeps-original", "node build.js; grep -e foo \"" + danger + "\" notes.txt", true},
		// Lone & is a separator (final review R1); every segment must be allowlisted (R2).
		//
		// 单个 & 是分隔符（终审 R1）；每一段都须在白名单（R2）。
		{"background-amp-git-alias", "python3 x.py; go test ./x -count=1 & git -c alias.x='!" + danger + "' x", true},
		{"osascript-e-after-python", "python3 - <<'EOF'" + nl + "print(1)" + nl + "EOF" + nl + "go test ./x -count=1; osascript -e \"do shell script \\\"" + danger + "\\\"\"", true},
		{"go-test-exec-flag", "python3 - <<'EOF'" + nl + "print(1)" + nl + "EOF" + nl + "go test ./x -count=1 -exec \"sh -x " + danger + "\"", true},
		{"go-vet-vettool-flag", "python3 - <<'EOF'" + nl + "print(1)" + nl + "EOF" + nl + "go vet -vettool=\"" + danger + "\" ./x; go test ./x -count=1", true},
		// The literal 16:51 prefix: a hyphenated path segment (fix-windows-ci-flakes) is not a
		// -c flag.
		//
		// 16:51 原样前缀：路径里的连字符段（fix-windows-ci-flakes）不是 -c flag。
		{"hyphen-path-then-python-heredoc", "cd /w/fix-windows-ci-flakes && python3 - <<'EOF'" + nl + "s = \"" + danger + "\"" + nl + "EOF" + nl + "go test ./x -count=1", false},
		{"python-c", "python3 -c \"import os; os.system('" + danger + "')\"", true},
		{"python-c-attached-quote", "python -c'print(1)' && echo \"" + danger + "\"", true},
		{"python-c-glued-code", "python3 -c__import__\"('os').system('" + danger + "')\"", true},
		{"python-c-continuation", "python3 \\" + nl + "  -c \"import os; os.system('" + danger + "')\"", true},
		{"python-c-at-end", "echo \"" + danger + "\" | python3 -c", true},
		// Interpreter held in a variable (review N1): the name is not in the segment.
		//
		// 解释器名在变量里（审查 N1）：段内看不到解释器名。
		{"variable-held-interpreter", "PY=python3; $PY -c \"import os; os.system('" + danger + "')\"", true},
		{"loop-variable-interpreter", "for py in python3; do $py -c \"x('" + danger + "')\"; done", true},
		// Cross-segment indirection (review R2): any $ / function definition falls back to
		// whole-command matching, so these keep the original verdict.
		//
		// 跨段间接调用（审查 R2）：出现 $ / 函数定义即回落整条命令匹配，保持原判定。
		{"function-wrapped-interpreter", "p() { python3 \"$@\"; }; p -c \"x('" + danger + "')\"", true},
		{"variable-held-flag", "p=python3; f=-c; $p $f \"x('" + danger + "')\"", true},
		// No-$ indirection (review R3): only an allowlisted tool's flag segment can explain
		// away the whole-command hit; git -c can run a shell alias, so git is not listed.
		//
		// 不带 $ 的间接调用（审查 R3）：只有白名单工具的 flag 段能解释掉整条命令命中；
		// git -c 可执行 shell 别名，故 git 不在白名单。
		{"xargs-placeholder-interpreter", "echo python3 | xargs -I{} {} -c \"x('" + danger + "')\"", true},
		{"xargs-quoted-flag", "echo python3 | xargs -I{} {} \"-c\" \"x('" + danger + "')\"", true},
		{"copied-interpreter-path", "cp /usr/bin/python3 ./py && ./py -c \"x('" + danger + "')\"", true},
		{"git-config-alias-after-python", "python3 --version; git -c alias.x='!" + danger + "' x", true},
		{"node-e", "node -e \"require('child_process').execSync('" + danger + "')\"", true},
		{"node-eval-long", "node --eval \"x('" + danger + "')\"", true},
		{"ruby-e", "ruby -e \"system('" + danger + "')\"", true},
		{"perl-E", "perl -E \"system('" + danger + "')\"", true},
		{"perl-e-glued-code", "perl -esystem\"('" + danger + "')\"", true},
		{"php-r", "php -r \"system('" + danger + "');\"", true},
		{"lua-e", "lua -e \"os.execute('" + danger + "')\"", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runHazardScript(t, shimDir, tc.cmd)
			if tc.wantBlock && err == nil {
				t.Fatalf("code-exec flag with quoted danger must BLOCK, got pass:\n%s", out)
			}
			if !tc.wantBlock && err != nil {
				t.Fatalf("a flag in another command segment must not veto the data release, got block:\n%s", out)
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
