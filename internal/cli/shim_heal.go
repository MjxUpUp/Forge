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
// through) repairs the shim for the next one.
//
// healNpmShimIfNeeded 对当前可执行文件执行自愈。由 PersistentPreRunE 调用，使每次
// forge 调用（包括 hook spawn，只要能进来一次）都为下一次修好垫片。
func healNpmShimIfNeeded() {
	if runtime.GOOS != "windows" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	healed, err := healNpmShim(exe)
	if err == nil && healed {
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

	// Replace atomically (temp file + same-dir rename) so a concurrent hook spawn never
	// reads a half-written shim.
	//
	// 原子替换（临时文件 + 同目录 rename），并发 hook spawn 不会读到写了一半的垫片。
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
		// Windows rename fails when the target exists — remove and retry once.
		//
		// Windows 上目标已存在时 rename 失败——删除后重试一次。
		os.Remove(shim)
		if err2 := os.Rename(tmpPath, shim); err2 != nil {
			os.Remove(tmpPath)
			return false, err2
		}
	}
	return true, nil
}

// npmPrefixFor walks exePath upward looking for a "node_modules" segment and returns
// the directory above it (the npm prefix whose root holds the global bin shims on
// Windows). ok=false when exePath is not in an npm layout.
//
// npmPrefixFor 沿 exePath 向上找 "node_modules" 段，返回其上级的目录（Windows 上
// npm 全局 bin 垫片所在的 prefix 根）。exePath 不在 npm 布局时 ok=false。
func npmPrefixFor(exePath string) (prefix string, ok bool) {
	dir := filepath.Clean(exePath)
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		if strings.EqualFold(filepath.Base(dir), "node_modules") {
			return parent, true
		}
		dir = parent
	}
}
