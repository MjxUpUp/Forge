package e2e

import "testing"

// TestHook_HazardGuard_FlagScopedToRmSegment regressions 2026-07 task2 残留：flag 检测
// -[a-z]*r[a-z]*f / -[a-z]*f[a-z]*r 扫全命令串，误中跨命令分隔符另一段的 hyphen-token
// （git checkout fix/hazard-confirm-x 的 -confirm 含 f...r）。fix：flag 检测按命令段
// （&&/||/|/;/换行 分隔）隔离，只查 rm-token 所在段。跨段 hyphen-token 不算 rm flag；
// 段隔离不放过真高危——rm safe && rm -rf other 第二段仍须 block。
func TestHook_HazardGuard_FlagScopedToRmSegment(t *testing.T) {
	dir := freshProject(t)
	// rm 段无 rf flag；另一段（git/cd）的 hyphen-token 含 r/f（-confirm/-formatter/--prefix）
	// 但被段隔离排除——修复前误判 rm -rf，修复后必须放行。
	pass := []string{
		`rm tmp.txt && git checkout fix/hazard-confirm-fp-validation`,
		`rm scratch.log && git checkout fix/formatter-refactor`,
		`rm a.txt && git rebase --prefix origin`,
		// 段内白名单仍有效：纯 /tmp 段（单段或双段）必须放行，证明白名单下沉没误伤合法临时清理
		`rm -rf /tmp/forge-probe`,
		`rm -rf /tmp/a && rm -rf /tmp/b`,
		// 多参数全 /tmp 放行（arg-aware：逐参数查白名单，非 substring 整段放行）
		`rm -rf /tmp/a /tmp/b`,
		// 长选项形式等价 rm -rf（白名单看目标路径不依赖 flag 写法，NIT-2）
		`rm --recursive --force /tmp/x`,
		// -- 终止符后白名单路径仍放行（past_dd 后按目标查，MINOR-B 回归）
		`rm -rf -- /tmp/x`,
		// -- 出现在路径后不破坏白名单（past_dd 在末尾置位但已无后续目标）
		`rm -rf /tmp/a /tmp/b --`,
	}
	for _, cmd := range pass {
		in := hookStdin(t, "sess-flagscope", "PreToolUse", "Bash", map[string]any{
			"command": cmd,
		})
		stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
		if err != nil {
			t.Fatalf("hazard-guard must pass %q (cross-cmd hyphen-token is not rm flag), got block. stdout: %s", cmd, stdout)
		}
	}
	// 段隔离不能放过真高危：覆盖单段 rm -rf、sudo rm -rf、以及混合（安全段 + 高危段）。
	block := []string{
		`rm -rf ./important-data`,
		`sudo rm -rf ./data`,
		`rm tmp.txt && rm -rf other-dir`,
		`rm clean.tmp && sudo rm -rf ./data`,
		// 跨段绕过（task3 补审根因）：旧全串白名单的 return 1 会吞整条命令，连第二段
		// 非 /tmp 的 rm -rf 也放行；段隔离后第二段必须独立 block（; 与 && 两种分隔符）
		`rm -rf /tmp/foo; rm -rf /important`,
		`rm -rf /tmp/foo && rm -rf /important`,
		// 路径穿越：含 .. 不享白名单，rm -rf /tmp/../etc 仍 block
		`rm -rf /tmp/../etc`,
		// || / 三段链分隔（NIT-1：tr 切 | 与 ; 段隔离，混合段不放过高危段）
		`rm -rf /tmp/foo || rm -rf /important`,
		`rm -rf /tmp/a; echo ok; rm -rf /important`,
		// newline 分隔（MINOR-1：squeeze 前换行映射成 ; 才能切段，否则合并同段漏检）
		`rm -rf /tmp/foo
rm -rf /important`,
		// 多参数含非白名单目标（MINOR-2：arg-aware 不能因 /tmp 子串放行整条 rm）
		`rm -rf /tmp/x /important`,
		`rm -rf /important /tmp/x`,
		// -- 终止符后 - 开头文件名是字面目标（rm 真删），不能当 flag 跳过（MINOR-B）
		`rm -rf -- -sensitive-file`,
		// -- 后系统路径同样 block（past_dd 分支按目标查，/etc 非 /tmp 白名单）
		`rm -rf -- /etc/passwd`,
		// -- 后白名单前缀含 .. 路径穿越：past_dd=1 分支也须检测 .. 不放行
		// （与 rm -rf /tmp/../etc 的 past_dd=0 分支对称，锁死两分支 .. 检测一致）
		`rm -rf -- /tmp/../etc`,
		// 长选项 flag + -- 终止符组合：--recursive/--force 走 flag 跳过不触发 past_dd，
		// 真 -- 才触发，其后 -sensitive 按目标查 block（锁长选项与 past_dd 的交互路径）
		`rm --recursive --force -- -sensitive`,
	}
	for _, cmd := range block {
		in := hookStdin(t, "sess-flagscope-real", "PreToolUse", "Bash", map[string]any{
			"command": cmd,
		})
		stdout, _, err := forgeHook(t, dir, "hazard-guard", in)
		if err == nil {
			t.Fatalf("hazard-guard must block %q (real rm -rf in a segment), got exit 0. stdout: %s", cmd, stdout)
		}
	}
}
