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
