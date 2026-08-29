package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Script-level tests for the embedded HazardGuardHook diagnostics path
// (weekly-hardening fix a): when `forge hazard confirmed` itself fails with a
// stderr error (confirm-chain fault — kimi 30s timeout, autoSync slowing forge
// startup, environment breakage), the block output must carry the stderr
// snippet instead of swallowing it (the old >/dev/null 2>&1 made the failure
// invisible). A shim `forge` on PATH simulates the confirm chain — the real
// binary is not needed, mirroring the sentinel_script_test pattern.

// writeForgeShim installs a fake `forge` executable into a temp dir. The shim
// answers the three hazard subcommands the hook script calls:
//   - hazard fingerprint → a fixed 64-hex fingerprint on stdout
//   - hazard confirmed   → behavior controlled by confirmedMode:
//     "release" = exit 0 (confirmed), anything else = stderr error + exit 1
//   - hazard log         → exit 0 (audit sink)
func writeForgeShim(t *testing.T, confirmedMode string) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH — skipping script-level hazard test")
	}
	dir := t.TempDir()
	shim := `#!/bin/bash
if [ "$1" = "hazard" ] && [ "$2" = "fingerprint" ]; then
  echo "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  exit 0
fi
if [ "$1" = "hazard" ] && [ "$2" = "confirmed" ]; then
  if [ "` + confirmedMode + `" = "release" ]; then
    exit 0
  fi
  echo "[hazard] simulated confirm-chain store corruption" >&2
  exit 1
fi
if [ "$1" = "hazard" ] && [ "$2" = "log" ]; then
  exit 0
fi
exit 1
`
	path := filepath.Join(dir, "forge")
	if err := os.WriteFile(path, []byte(shim), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runHazardScript executes the embedded HazardGuardHook with FORGE_COMMAND and
// a shim forge on PATH. extraEnv entries (KEY=VALUE) are appended to the child
// environment — e.g. FORGE_ALLOW_HAZARD=1 in the env-bypass-removed guard.
func runHazardScript(t *testing.T, shimDir, command string, extraEnv ...string) (string, error) {
	t.Helper()
	f, err := os.CreateTemp("", "forge-hook-*.sh")
	if err != nil {
		t.Fatalf("createtemp: %v", err)
	}
	if _, err := f.WriteString(HazardGuardHook); err != nil {
		t.Fatalf("write script: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())

	cmd := exec.Command("bash", f.Name())
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(),
		"FORGE_COMMAND="+command,
		"PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, runErr := cmd.CombinedOutput()
	return string(out), runErr
}

// TestHazardGuardScript_ConfirmedFailureDiagnostic: a confirmed-call fault
// (stderr + exit 1) must surface in the block output — the pre-fix
// >/dev/null 2>&1 swallowed it, leaving the kimi confirm-chain failure with no
// trace.
//
// TestHazardGuardScript_ConfirmedFailureDiagnostic：confirmed 调用本身故障
// （stderr + exit 1）必须出现在 block 输出里——修复前的 >/dev/null 2>&1 会吞掉，
// kimi 确认链失败曾无迹可查。
func TestHazardGuardScript_ConfirmedFailureDiagnostic(t *testing.T) {
	shimDir := writeForgeShim(t, "fail")
	out, err := runHazardScript(t, shimDir, "rm -rf ./important-data")
	if err == nil {
		t.Fatalf("hazard-guard must block hazardous command when confirmed fails, got exit 0:\n%s", out)
	}
	if !strings.Contains(out, "确认链诊断") {
		t.Errorf("block output must carry the confirm-chain diagnostic section, got:\n%s", out)
	}
	if !strings.Contains(out, "simulated confirm-chain store corruption") {
		t.Errorf("block output must include the confirmed stderr snippet (first 200 chars), got:\n%s", out)
	}
	if !strings.Contains(out, "forge hazard status") {
		t.Errorf("block output must point at 'forge hazard status' for further diagnosis, got:\n%s", out)
	}
}

// TestHazardGuardScript_ConfirmedCleanRelease: a successful confirmed call
// (exit 0) must release — the diagnostic plumbing must not break the normal
// HITL release path.
//
// TestHazardGuardScript_ConfirmedCleanRelease：confirmed 成功（exit 0）必须
// 放行——诊断管线不得破坏正常 HITL 放行路径。
func TestHazardGuardScript_ConfirmedCleanRelease(t *testing.T) {
	shimDir := writeForgeShim(t, "release")
	out, err := runHazardScript(t, shimDir, "rm -rf ./important-data")
	if err != nil {
		t.Fatalf("hazard-guard must release after confirm, got block:\n%s", out)
	}
	if !strings.Contains(out, "已确认放行") {
		t.Errorf("release path must report the confirmed-pass message, got:\n%s", out)
	}
}

// TestHazardGuardScript_EnvBypassRemoved: FORGE_ALLOW_HAZARD=1 must have no
// effect on the script anymore — the env escape branch was removed (agent
// self-release abuse). With an unconfirmed fingerprint the command still
// blocks even when the env is set.
//
// TestHazardGuardScript_EnvBypassRemoved：FORGE_ALLOW_HAZARD=1 对脚本不再
// 生效——env 豁免分支已移除（agent 自我放行滥用）。未确认时即便设了 env 仍
// 必须拦截。
func TestHazardGuardScript_EnvBypassRemoved(t *testing.T) {
	shimDir := writeForgeShim(t, "fail")
	out, runErr := runHazardScript(t, shimDir, "rm -rf ./important-data", "FORGE_ALLOW_HAZARD=1")
	if runErr == nil {
		t.Fatalf("hazard-guard must block with FORGE_ALLOW_HAZARD=1 (env escape removed), got exit 0:\n%s", out)
	}
	if strings.Contains(out, "FORGE_ALLOW_HAZARD=1 跳过") {
		t.Errorf("env escape branch must be gone from the script output, got:\n%s", out)
	}
}

// TestHazardGuardScript_MktempSelfCleanupExempt pins the 2026-08 two-week
// usage-log fix (mktemp/self-created temp-dir cleanup was ~1/3 of all blocks):
// d=$(mktemp -d); ...; rm -rf "$d" is a build-then-delete self-cleanup loop and
// must pass; the blocked agent used to silently drop the rm half and leak /tmp
// garbage. Controls pin the conservative edge: unverifiable sources (unassigned
// var, non-mktemp assignment, reassignment attack, direct $() target, mixed
// targets, a later hazardous segment) must still block. The second block batch
// is the review battery (critical): every rebinding/forgery shape that must NOT
// let the mktemp whitelist attach to an arbitrary path. The third batch is the
// round-3 closure: the $d/sub form is no longer exempt at all (d empty →
// rm -rf $d is a harmless missing-operand, $d/x is /x) which kills the whole
// "assignment never takes effect" family, and dynamic-rebind vocab
// (eval/source/./sh -c) voids the whitelist outright.
//
// TestHazardGuardScript_MktempSelfCleanupExempt 钉死 2026-08 两周 usage 日志
// 修复（mktemp/自建临时目录清理约占 1/3 误拦）：d=$(mktemp -d); ...; rm -rf "$d"
// 是建后即删的自清理闭环，必须放行——被拦的 agent 此前会悄悄删掉 rm 半截、留下
// /tmp 垃圾。对照组钉住保守边界：来源不可验证的形态（未赋值变量、非 mktemp 赋值、
// 再赋值攻击、直接 $() 目标、混合目标、后续高危段）必须仍拦。第二批 block 是
// 审查攻击 battery（critical）：一切不得让 mktemp 白名单套到任意路径的
// 重绑/伪造形态。第三批是第三轮闭合：$d/sub 形态整体不再豁免（d 为空时
// rm -rf $d 缺操作数无害、$d/x 才是 /x）灭掉"赋值不生效"全家族；动态重绑词面
// （eval/source/./sh -c）整体作废白名单。
func TestHazardGuardScript_MktempSelfCleanupExempt(t *testing.T) {
	shimDir := writeForgeShim(t, "fail")
	pass := []string{
		`d=$(mktemp -d); cd "$d"; echo hi > f.txt; cd -; rm -rf "$d"`,
		`tmp=$(mktemp -d) && tar xzf a.tgz -C "$tmp" && rm -rf $tmp`,
		`d=$(mktemp -d); e=$(mktemp -d); rm -rf "$d" "$e"`,
	}
	for _, cmd := range pass {
		out, err := runHazardScript(t, shimDir, cmd)
		if err != nil {
			t.Errorf("hazard-guard must pass mktemp self-cleanup %q, got block:\n%s", cmd, out)
		}
	}
	block := []string{
		`rm -rf $d`,                                      // unassigned var — unverifiable source
		`d=$(mktemp -d); d=/; rm -rf $d`,                 // reassignment attack: whitelist must not extend to /
		`d=$(echo /tmp/x); rm -rf $d`,                    // non-mktemp assignment
		`rm -rf $(mktemp -d)`,                            // direct command substitution target — unparsable, conservative
		`d=$(mktemp -d); rm -rf $d /important`,           // mixed targets
		`d=$(mktemp -d); rm -rf "$d"; rm -rf /important`, // later segment is hazardous
		// review battery (critical): rebinding/forgery shapes
		//
		// 审查攻击 battery（critical）：重绑/伪造形态
		`d=$(mktemp -d); for d in /; do :; done; rm -rf $d`, // for-loop rebind
		`d=$(mktemp -d); d[0]=/path; rm -rf "$d"`,           // array assignment ($d == ${d[0]})
		`d=$(mktemp -d); d+=/../victim; rm -rf "$d"`,        // += append with traversal in value
		`d=$(mktemp -d); printf -v d /; rm -rf "$d"`,        // printf -v rebind without =
		`d=$(mktemp -d); read d <<< /; rm -rf "$d"`,         // read rebind without =
		`echo 'd=$(mktemp -d)'; rm -rf $d/x`,                // quoted fake assignment (d is empty → rm -rf /x)
		`x=d=$(mktemp -d); rm -rf $d/x`,                     // assigns x, not d
		"cat <<'EOF'\nd=$(mktemp -d)\nEOF\nrm -rf $d",       // heredoc-body "assignment" never executes
		`d=$(mktemp -d); x=../..; rm -rf $d/$x`,             // verified var + unverified suffix var
		`x=/; rm -rf $TMPDIR/$x`,                            // $TMPDIR arm: same suffix-var rule
		// round-3 closure (C1): the $d/sub form is NOT exempt — d empty makes
		// rm -rf $d a harmless missing-operand, but $d/x is /x. Every
		// "assignment never takes effect" shape (short-circuit / function body /
		// env prefix / pipe subshell / use-before-assign) leaves d empty and only
		// bites via the subpath form — all six pinned BLOCK (confirm chain releases).
		//
		// 第三轮闭合（C1）：$d/sub 形态不再豁免——d 为空时 rm -rf $d 缺操作数无害，
		// $d/x 才是 /x 危险形态。"赋值不生效"全家族（短路/函数体/env 前缀/管道
		// 子 shell/赋值在用后）d 皆为空、只在 $d/x 上显形——六形态全钉 BLOCK
		// （confirm 链放行）。
		`d=$(mktemp -d); mkdir "$d"/sub; rm -rf "$d"/sub`, // the subpath form itself
		`false && d=$(mktemp -d); rm -rf $d/x`,            // short-circuit: assignment never runs
		`f(){ d=$(mktemp -d); }; rm -rf $d/x`,             // function body: never called
		`d=$(mktemp -d) rm -rf $d/x`,                      // env prefix: $d expands before assignment
		`d=$(mktemp -d) | cat; rm -rf $d/x`,               // pipe subshell: parent d stays empty
		`rm -rf $d/x; d=$(mktemp -d)`,                     // use before assign
		// round-3 closure (C2): dynamic-rebind vocab (eval / source / . / sh -c)
		// voids the whole mktemp whitelist — the value can be rewritten after the
		// lexical check.
		//
		// 第三轮闭合（C2）：动态重绑词面（eval/source/./sh -c）整体作废 mktemp
		// 白名单——词法检查之后值仍可被改写。
		`d=$(mktemp -d); eval 'd=/'; rm -rf "$d"`,
		`d=$(mktemp -d); source /tmp/rebind; rm -rf "$d"`,
		`d=$(mktemp -d); . /tmp/rebind; rm -rf "$d"`,
		`d=$(mktemp -d); sh -c 'true'; rm -rf "$d"`,
	}
	for _, cmd := range block {
		out, err := runHazardScript(t, shimDir, cmd)
		if err == nil {
			t.Errorf("hazard-guard must block %q (unverifiable/hazardous rm target), got exit 0:\n%s", cmd, out)
		}
	}
}

// TestHazardGuardScript_TmpdirEnvExempt pins the $TMPDIR whitelist arm: macOS
// mktemp -d lands in $TMPDIR, so rm -rf "$TMPDIR/pack" (rebuild-a-scratch-dir
// pattern) is the same one-shot temp cleanup as the literal /tmp whitelist.
// Controls: bare $TMPDIR (wiping the whole user temp dir), .. traversal, ~, /,
// and repo-relative paths must still block. The bare/traversal quoted forms
// also pin the nowl tightening: rm -rf "$TARGET" with the target quoted away is
// NOT a data context (the danger is rm itself, not a quoted string).
//
// TestHazardGuardScript_TmpdirEnvExempt 钉死 $TMPDIR 白名单分支：macOS 的
// mktemp -d 默认落在 $TMPDIR，rm -rf "$TMPDIR/pack"（重建自建目录形态）与字面
// /tmp 白名单同属一次性临时区清理。对照：裸 $TMPDIR（清整个用户临时目录）、..
// 穿越、~、/、仓库相对路径必须仍拦。裸删/穿越的引号形态同时钉住 nowl 收紧：
// rm -rf "$TARGET" 目标被引号剥走后不是数据上下文（危险的是 rm 本身）。
func TestHazardGuardScript_TmpdirEnvExempt(t *testing.T) {
	shimDir := writeForgeShim(t, "fail")
	pass := []string{
		`rm -rf "$TMPDIR/packsmoke" && mkdir -p "$TMPDIR/packsmoke"`,
		`rm -rf $TMPDIR/probe`,
		`rm -rf ${TMPDIR}/probe`,
	}
	for _, cmd := range pass {
		out, err := runHazardScript(t, shimDir, cmd)
		if err != nil {
			t.Errorf("hazard-guard must pass $TMPDIR subpath cleanup %q, got block:\n%s", cmd, out)
		}
	}
	block := []string{
		`rm -rf "$TMPDIR"`,        // bare: wipes the whole user temp dir
		`rm -rf "$TMPDIR/../etc"`, // traversal
		`rm -rf ~`,
		`rm -rf /`,
		`rm -rf ./important-data`,
		`rm -rf "$HOME"`, // quoted-target execution is not a data context (nowl)
		// review M2: a subpath of only / and . chars folds to the temp dir itself
		// (kernel collapses double slashes) — not exempt.
		//
		// 复审 M2：全为 / 或 . 的子路径经内核折叠后等于临时目录本身——不豁免。
		`rm -rf /tmp//`,
		`rm -rf /tmp/./`,
		`rm -rf $TMPDIR//`,
		`rm -rf "$TMPDIR/./"`,
	}
	for _, cmd := range block {
		out, err := runHazardScript(t, shimDir, cmd)
		if err == nil {
			t.Errorf("hazard-guard must block %q, got exit 0:\n%s", cmd, out)
		}
	}
}

// TestHazardGuardScript_MultilineQuotedDangerIsData pins the substring false
// positive fix (read-only python3 heredoc analysis scripts blocked twice in the
// usage logs): a danger string inside a multi-line quoted string (triple-quoted
// python docstring, multi-line commit message) is data, not execution — quote
// state now persists across lines. Controls pin the conservative edges: a bare
// unquoted danger line in a heredoc body still blocks (could be cat > script.sh
// authoring an executable), an apostrophe inside double quotes must not leak
// quote state onto the next line's real hazard (nesting-aware toggling), and
// exec-wrapped quotes stay blocked.
//
// TestHazardGuardScript_MultilineQuotedDangerIsData 钉死 substring 误报修复
// （usage 日志里只读 python3 heredoc 分析脚本被拦 2 次）：多行引号字符串
// （python 三引号 docstring、多行 commit message）里的危险串是数据不是执行——
// 引号状态现已跨行持久。对照组钉住保守边界：heredoc 体内裸露未引号的危险行
// 仍拦（可能是 cat > script.sh 在写可执行脚本）；双引号内的撇号不得把引号状态
// 泄漏到下一行的真危险命令（嵌套感知开合）；exec 包裹的引号仍拦。
func TestHazardGuardScript_MultilineQuotedDangerIsData(t *testing.T) {
	shimDir := writeForgeShim(t, "fail")
	pass := []string{
		// the real blocked FP: rm -rf <target> inside a triple-quoted python string
		"python3 <<'EOF'\npattern = \"\"\"\nrm -rf ./build\n\"\"\"\nprint(len(pattern))\nEOF",
		"git commit -m \"fix: cleanup\n\nmentions rm -rf in prose\"",
	}
	for _, cmd := range pass {
		out, err := runHazardScript(t, shimDir, cmd)
		if err != nil {
			t.Errorf("hazard-guard must pass multi-line data-context %q, got block:\n%s", cmd, out)
		}
	}
	block := []string{
		"cat <<'EOF' > /tmp/x.sh\nrm -rf /important\nEOF", // bare danger line in heredoc body
		"echo \"don't\"\nrm -rf /important",               // apostrophe must not swallow the next line
		`bash -c "rm -rf ./vault"`,                        // exec-wrapped quotes stay blocked
		// review battery (critical): escape/heredoc quote-state bypasses — each must
		// restore the BLOCK the pre-review build leaked (real deletion sandbox-proven).
		//
		// 审查攻击 battery（critical）：转义/heredoc 引号态旁路——每条都必须恢复
		// 拦截（复审前的构建曾漏放，沙箱实证真删）。
		"echo \\\"\nrm -rf /important",                                       // \" outside quotes is a literal quote char, not an opener
		"echo \\'\nrm -rf /important",                                        // \' outside quotes likewise
		`echo "a\"b"; rm -rf /important`,                                     // \" inside dq does not close the string
		"cat <<'EOF' > /tmp/notes.txt\nHe said \"hi\nEOF\nrm -rf /important", // heredoc-body stray quote must not leak past the delimiter
		"cat <<2\nHe said \"hi\n2\nrm -rf /important",                        // M1: digit-tag heredoc → fail-closed (vs $((1<<2)) ambiguity)
	}
	for _, cmd := range block {
		out, err := runHazardScript(t, shimDir, cmd)
		if err == nil {
			t.Errorf("hazard-guard must block %q, got exit 0:\n%s", cmd, out)
		}
	}
}

// TestHazardGuardScript_GitBranchDScope pins the merge-cleanup granularity
// decision (9 blocks in the usage logs): git branch -d (lowercase) refuses
// unmerged branches and is not hazardous on its own; git push origin --delete
// is remote-irreversible and keeps blocking even in the compound cleanup
// command — the pre-authorization copy path (confirm --last without re-asking)
// is the designated friction reducer there.
//
// TestHazardGuardScript_GitBranchDScope 钉死合并后清分支的豁免粒度决策
// （usage 日志 9 次拦截）：git branch -d（小写）对未合并分支会拒绝，本身不
// 高危；git push origin --delete 远程不可逆，即便在复合清理命令里也保留拦截
// ——该场景的降摩擦走授权路径文案（本回合已授权则 confirm --last 免二次确认）。
func TestHazardGuardScript_GitBranchDScope(t *testing.T) {
	shimDir := writeForgeShim(t, "fail")
	out, err := runHazardScript(t, shimDir, `git branch -d fix/foo`)
	if err != nil {
		t.Errorf("hazard-guard must pass 'git branch -d' (safe local delete), got block:\n%s", out)
	}
	out, err = runHazardScript(t, shimDir, `git branch -d fix/foo && git push origin --delete fix/foo`)
	if err == nil {
		t.Errorf("hazard-guard must still block the compound with push --delete (remote irreversible), got exit 0:\n%s", out)
	}
}

// TestHazardGuardScript_BlockGuidanceCopy pins the 2026-08 block-message copy
// fix: the authorization path (user already instructed/confirmed this turn →
// confirm --last directly, no second ask), the generalized tool reference (no
// per-tool enumeration that missed kimi/copilot/zcode), and the removal of the
// trailing FORGE_ALLOW_HAZARD migration note (changelog, not action guidance).
//
// TestHazardGuardScript_BlockGuidanceCopy 钉死 2026-08 block 文案修复：授权
// 路径（用户本回合已明确指令/确认过 → 直接 confirm --last，无需二次确认）、
// 泛化的工具指代（不再逐一枚举而漏掉 kimi/copilot/zcode）、以及删除尾部
// FORGE_ALLOW_HAZARD 迁移说明（changelog 不是行动指引）。
func TestHazardGuardScript_BlockGuidanceCopy(t *testing.T) {
	shimDir := writeForgeShim(t, "fail")
	out, err := runHazardScript(t, shimDir, "rm -rf ./important-data")
	if err == nil {
		t.Fatalf("hazard-guard must block rm -rf ./important-data, got exit 0:\n%s", out)
	}
	for _, anchor := range []string{"授权判定", "无需二次确认", "你所在", "forge hazard confirm --last", "逐字"} {
		if !strings.Contains(out, anchor) {
			t.Errorf("block guidance missing %q, got:\n%s", anchor, out)
		}
	}
	for _, gone := range []string{"FORGE_ALLOW_HAZARD", "AskUserQuestion"} {
		if strings.Contains(out, gone) {
			t.Errorf("block guidance must not contain %q anymore, got:\n%s", gone, out)
		}
	}
}

// TestHazardGuardScript_InterpDeleteBypass: script-level pin for the
// interpreter inline-delete bypass (weekly-hardening c), including the node
// require('fs').rmSync quote-normalization case that "fs.rm" substring matching
// alone misses (normalized form is require(fs).rmsync( — a ")" intervenes).
// Script-level only (2026-08-30 slimming: the e2e twin was retired — the e2e
// layer keeps wiring smokes, classification lives here); fast iteration without
// a forge binary build.
//
// TestHazardGuardScript_InterpDeleteBypass：解释器内联删除旁路的脚本级钉
// （周复盘 c），含 node require('fs').rmSync 的引号归一案例——仅匹配 "fs.rm"
// 子串会漏（归一形态是 require(fs).rmsync(，中间隔了 ")"）。仅脚本级
// （2026-08-30 瘦身：e2e 对照已退役——e2e 层只留守接线冒烟，分类逻辑归此）；
// 不用构建 forge 二进制，迭代更快。
func TestHazardGuardScript_InterpDeleteBypass(t *testing.T) {
	shimDir := writeForgeShim(t, "fail")
	block := []string{
		`python -c "import os;os.remove('./important.txt')"`,
		`python3 -c "import shutil;shutil.rmtree('./build')"`,
		`node -e "require('fs').rmSync('./data',{recursive:true})"`,
		`node -e "require('fs').unlink('./f')"`,
	}
	for _, cmd := range block {
		out, err := runHazardScript(t, shimDir, cmd)
		if err == nil {
			t.Errorf("hazard-guard must block interpreter inline-delete %q, got exit 0:\n%s", cmd, out)
		}
	}
	pass := []string{
		`python -c "print(1)"`,
		`node -e "console.log('ok')"`,
		`python scripts/train.py --epochs 3`,
	}
	for _, cmd := range pass {
		out, err := runHazardScript(t, shimDir, cmd)
		if err != nil {
			t.Errorf("hazard-guard must pass benign interpreter command %q, got block:\n%s", cmd, out)
		}
	}
}
