package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// shim_heal.go — self-healing for npm's fragile Windows sh shim.
//
// npm installs three shims per bin on Windows: forge (a POSIX shell script), forge.cmd,
// and forge.ps1. The extensionless one is generated from npm's fixed cmd-shim template
// and requires coreutils (sed/dirname/uname) on PATH. Agents that spawn hook commands
// through a POSIX shell with the native Windows PATH (kimi-code on Windows) resolve
// `forge` to that script, which then crashes before forge ever starts — and because the
// script's failure exit becomes a hook block, every turn stops. The user fix is adding
// Git's usr\bin to PATH; this file removes the need for it: whenever forge runs from an
// npm layout (its executable under node_modules), the extensionless shim next to the
// npm prefix is replaced with a copy of the real binary. PE executables run fine from
// MSYS sh, cmd, and PowerShell alike (the .cmd/.ps1 shims are left untouched).
//
// Best-effort and idempotent: any failure is a silent no-op (forge must never break a
// command over shim hygiene), and an already-healed shim (starts with "MZ") is skipped.
//
// shim_heal.go — npm 脆弱 Windows sh 垫片的自愈。
//
// npm 在 Windows 上为每个 bin 生成三个垫片：forge（POSIX shell 脚本）、forge.cmd、
// forge.ps1。无扩展名的那个由 npm 固定的 cmd-shim 模板生成，依赖 PATH 上有
// coreutils（sed/dirname/uname）。凡是通过 POSIX shell + 原生 Windows PATH 执行
// hook 命令的 agent（Windows 上的 kimi-code），`forge` 都会解析到该脚本并在 forge
// 启动前崩溃——而脚本失败的退出码又变成 hook 阻断，每轮都停。用户侧解法是把 Git
// usr\bin 加进 PATH；本文件让这一步不再必要：每当 forge 从 npm 布局运行（可执行
// 文件位于 node_modules 下），就把 npm 前缀旁的无扩展名垫片替换为真实二进制的
// 副本。PE 可执行文件在 MSYS sh、cmd、PowerShell 下都能直接运行（.cmd/.ps1 垫片
// 不动）。
//
// 尽力而为且幂等：任何失败都静默 no-op（forge 绝不为垫片卫生弄坏一条命令），已
// 自愈的垫片（"MZ" 开头）直接跳过。

// healNpmShimIfNeeded runs the self-heal against the current executable. Called from
// PersistentPreRunE so every forge invocation (including hook spawns, when one does get
// through) repairs the shim for the next one. quiet suppresses the one-time stderr
// notice (used for hook subcommands: their stderr may surface into the agent's context
// as a block reason, and the notice must never be mistaken for one).
//
// healNpmShimIfNeeded 对当前可执行文件执行自愈。由 PersistentPreRunE 调用，使每次
// forge 调用（包括 hook spawn，只要能进来一次）都为下一次修好垫片。quiet 抑制一次
// 性 stderr 提示（hook 子命令用：其 stderr 可能作为阻断原因进入 agent 上下文，
// 提示绝不能被误读为阻断）。
func healNpmShimIfNeeded(quiet bool) {
	if runtime.GOOS != "windows" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	healed, err := healNpmShim(exe)
	if err == nil && healed && !quiet {
		fmt.Fprintf(os.Stderr, "[forge] 已将脆弱的 npm sh 垫片替换为真实二进制（kimi 等 POSIX-shell 宿主开箱即用）\n")
	}
}

// healNpmShim replaces the extensionless npm sh shim with a copy of exePath when exePath
// lives in an npm layout (<prefix>/node_modules/<pkg>/...). Reports whether a
// replacement happened. Non-npm layouts (GitHub-release binary, go install) and
// already-binary shims are clean no-ops.
//
// healNpmShim 当 exePath 位于 npm 布局（<prefix>/node_modules/<pkg>/...）时，把无
// 扩展名的 npm sh 垫片替换为 exePath 的副本。返回是否发生了替换。非 npm 布局
// （GitHub Release 二进制、go install）与已是二进制的垫片均为干净 no-op。
func healNpmShim(exePath string) (bool, error) {
	if runtime.GOOS != "windows" {
		return false, nil
	}
	prefix, ok := npmPrefixFor(exePath)
	if !ok {
		return false, nil
	}
	shim := filepath.Join(prefix, "forge")
	head := make([]byte, 2)
	f, err := os.Open(shim)
	if err != nil {
		return false, nil // no shim — nothing to heal
	}
	if _, err := f.Read(head); err != nil {
		f.Close()
		return false, nil
	}
	f.Close()
	if string(head) != "#!" {
		return false, nil // already a binary (MZ) or something we should not touch
	}
	// Only touch npm's generated cmd-shim template (sniff its signature), never a
	// script the user wrote themselves into that slot. npm's template always contains
	// BOTH markers ($basedir AND node_modules) — an OR here would swallow user-written
	// wrappers that merely mention node_modules. Keep a one-time backup so a wrong
	// sniff is recoverable.
	//
	// 只动 npm 生成的 cmd-shim 模板（嗅探其特征串），绝不替换用户自己在该槽位
	// 手写的脚本。npm 模板恒同时含两个标记（$basedir 与 node_modules）——OR 条件
	// 会吞掉仅提到 node_modules 的用户自写包装脚本。保留一次性备份，误嗅探可恢复。
	script, err := os.ReadFile(shim)
	if err != nil {
		return false, nil
	}
	if !strings.Contains(string(script), "$basedir") || !strings.Contains(string(script), "node_modules") {
		return false, nil
	}
	if _, berr := os.Stat(shim + ".forge-shim-bak"); os.IsNotExist(berr) {
		_ = os.WriteFile(shim+".forge-shim-bak", script, 0644) // best-effort one-time backup
	}

	// Replace atomically (temp file + same-dir rename; Go's os.Rename uses
	// MOVEFILE_REPLACE_EXISTING on Windows) so a concurrent hook spawn never reads a
	// half-written shim. The remove+retry below only fires when the shim is locked by a
	// running process — in that case the heal simply fails open and the next forge run
	// retries.
	//
	// 原子替换（临时文件 + 同目录 rename；Windows 上 Go 的 os.Rename 使用
	// MOVEFILE_REPLACE_EXISTING），并发 hook spawn 不会读到写了一半的垫片。下面
	// 的删除重试仅在垫片被运行中进程锁定时触发——此时自愈 fail-open，下次 forge
	// 运行再试。
	data, err := os.ReadFile(exePath)
	if err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(prefix, ".forge-heal-*")
	if err != nil {
		return false, err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return false, err
	}
	tmp.Close()
	if err := os.Rename(tmpPath, shim); err != nil {
		os.Remove(shim)
		if err2 := os.Rename(tmpPath, shim); err2 != nil {
			os.Remove(tmpPath)
			return false, err2
		}
	}
	return true, nil
}

// npmPrefixFor walks exePath upward and returns the parent of the TOPMOST
// "node_modules" segment (the npm prefix whose root holds the global bin shims on
// Windows). The topmost matters: npm may nest the platform subpackage under the main
// package (<prefix>/node_modules/@agent_forge/forge/node_modules/@agent_forge/
// forge-win32-x64/...) — taking the deepest node_modules would wrongly return the main
// package dir as the prefix. ok=false when exePath is not in an npm layout.
//
// npmPrefixFor 沿 exePath 向上返回最外层 "node_modules" 段的父目录（Windows 上
// npm 全局 bin 垫片所在的 prefix 根）。必须取最外层：npm 可能把平台子包嵌套在主
// 包下面（<prefix>/node_modules/@agent_forge/forge/node_modules/@agent_forge/
// forge-win32-x64/...）——取最深 node_modules 会把主包目录误当 prefix。exePath
// 不在 npm 布局时 ok=false。
func npmPrefixFor(exePath string) (prefix string, ok bool) {
	dir := filepath.Clean(exePath)
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return prefix, ok
		}
		if strings.EqualFold(filepath.Base(dir), "node_modules") {
			prefix, ok = parent, true
		}
		dir = parent
	}
}
