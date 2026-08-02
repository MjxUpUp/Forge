// Package shellexec centralizes resolution of the bash interpreter used to run
// Forge's embedded hook scripts, shared by the write-time hook path (internal/cli)
// and the gate path (internal/taskpipeline). It exists because the gate path used
// to call exec.Command("bash", ...) with a bare PATH lookup, which on Windows can
// resolve to a WSL launcher (C:\Windows\System32\bash.exe or
// %LOCALAPPDATA%\Microsoft\WindowsApps\bash.exe) — WSL cannot see the Windows temp
// path of the script, so gate auto-compile checks failed with
// 'forge-gate-*.sh: No such file or directory' while the write-time hook path
// (which had the WSL-avoidance logic) worked. One resolution, one bug fix locus.
//
// Package shellexec 集中 bash 解释器解析，供 write-time hook 路径
// （internal/cli）与 gate 路径（internal/taskpipeline）共用。它存在的理由：
// gate 路径曾用裸 PATH 查找 exec.Command("bash", ...)，Windows 上可能解析到
// WSL 启动器（C:\Windows\System32\bash.exe 或
// %LOCALAPPDATA%\Microsoft\WindowsApps\bash.exe）——WSL 看不到脚本的 Windows
// 临时路径，gate 的 auto-compile 检查因此报 'forge-gate-*.sh: No such file or
// directory' 批量失败，而有 WSL 规避逻辑的 write-time hook 路径却正常。一份
// 解析逻辑，一个修复落点。
package shellexec

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// FindBash resolves the bash interpreter for hook scripts. On Windows a plain
// exec.LookPath("bash") can resolve to a WSL launcher (C:\Windows\System32\bash.exe or
// %LOCALAPPDATA%\Microsoft\WindowsApps\bash.exe) when forge runs under a native parent
// (kimi TUI, cmd, PowerShell) whose PATH orders System32 before Git's usr\bin — WSL
// cannot see the Windows temp path of the script, so every bash hook failed (and before
// the fail-open guard, failed CLOSED: kimi blocked every turn). Worse, Git for Windows
// typically puts only Git\cmd on PATH (no bash.exe at all), so after filtering WSL
// launchers there may be nothing left on PATH. Resolution order on Windows: PATH scan
// skipping known-WSL launchers → derive from the git binary's location (MSYS layout
// usr\bin/bin, up to 4 ancestors) → well-known install dirs → plain LookPath fallback
// (WSL-only machines; the infra fail-open covers the script-not-visible failure).
//
// FindBash 解析 hook 脚本的 bash 解释器。Windows 上裸 exec.LookPath("bash") 在
// forge 跑于原生父进程（kimi TUI、cmd、PowerShell）下可能解析到 WSL 启动器
// （C:\Windows\System32\bash.exe 或 %LOCALAPPDATA%\Microsoft\WindowsApps\bash.exe）
// ——这类 PATH 里 System32 排在 Git usr\bin 前面，而 WSL 看不到脚本的 Windows
// 临时路径，于是所有 bash hook 全挂（在 fail-open 守卫之前还是 fail-CLOSED：
// kimi 每轮都被硬阻断）。更糟的是 Git for Windows 通常只把 Git\cmd 加进 PATH
// （其中没有 bash.exe），过滤 WSL 启动器后 PATH 上可能一个 bash 都不剩。Windows
// 上的解析顺序：PATH 扫描（跳过已知 WSL 启动器）→ 从 git 二进制位置派生
// （MSYS 布局 usr\bin/bin，向上最多 4 级）→ 常见安装目录 → 回退普通 LookPath
// （WSL-only 机器；脚本不可见由基础设施 fail-open 兜底）。
func FindBash() (string, error) {
	if runtime.GOOS != "windows" {
		return exec.LookPath("bash")
	}
	// 1. PATH scan, skipping known-WSL launchers.
	//
	// 1. 扫 PATH，跳过已知 WSL 启动器。
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		cand := filepath.Join(dir, "bash.exe")
		if fileExistsExe(cand) && !isWSLBash(cand) {
			return cand, nil
		}
	}
	// 2. Git-derived: Git for Windows' installer typically adds only Git\cmd (and
	// Git\mingw64\bin) to PATH — bash.exe is NOT there, it lives in Git\usr\bin. Walk
	// up from the git binary looking for the MSYS layout (usr\bin, then bin).
	//
	// 2. git 派生：Git for Windows 安装器通常只把 Git\cmd（和 Git\mingw64\bin）加进
	// PATH——bash.exe 不在那里，而在 Git\usr\bin。从 git 二进制向上逐级找 MSYS
	// 布局（usr\bin，然后 bin）。
	if gitPath, err := exec.LookPath("git"); err == nil {
		dir := filepath.Dir(gitPath)
		for i := 0; i < 4; i++ {
			// usr\bin first: it holds the real MSYS bash; Git\bin\bash.exe is a thin
			// wrapper around it — both work, the real one is the surer bet.
			//
			// usr\bin 优先：那里是真正的 MSYS bash；Git\bin\bash.exe 是它的薄
			// 包装——都能用，选真身更稳。
			for _, sub := range []string{filepath.Join("usr", "bin"), "bin"} {
				cand := filepath.Join(dir, sub, "bash.exe")
				if fileExistsExe(cand) && !isWSLBash(cand) {
					return cand, nil
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	// 3. Well-known install locations (covers machines where git is not on PATH either;
	// includes the per-user Git-for-Windows layout under %LOCALAPPDATA%).
	//
	// 3. 常见安装位置（覆盖 git 也不在 PATH 的机器；含 %LOCALAPPDATA% 下的
	// per-user Git for Windows 布局）。
	for _, cand := range []string{
		`C:\Program Files\Git\usr\bin\bash.exe`,
		`D:\Program Files\Git\usr\bin\bash.exe`,
		`C:\Program Files (x86)\Git\usr\bin\bash.exe`,
		filepath.Join(os.Getenv("LOCALAPPDATA"), `Programs\Git\usr\bin\bash.exe`),
		`C:\msys64\usr\bin\bash.exe`,
		`C:\cygwin64\bin\bash.exe`,
	} {
		if fileExistsExe(cand) {
			return cand, nil
		}
	}
	// 4. Fallback to a plain lookup (WSL-only machines keep the old behavior; the
	// infra fail-open covers the script-not-visible failure).
	//
	// 4. 回退普通查找（WSL-only 机器保持旧行为；脚本不可见由基础设施
	// fail-open 兜底）。
	return exec.LookPath("bash")
}

// fileExistsExe reports whether path exists and is not a directory. It deliberately
// does not probe executability (on Windows that means a CreateProcess trial): a
// non-executable impostor selected here fails at spawn and is caught by the infra
// fail-open with a visible warning — noisy, not silent.
//
// fileExistsExe 报告 path 存在且不是目录。刻意不探测可执行性（Windows 上要
// CreateProcess 试探）：若选中了同名不可执行文件，spawn 会失败并被基础设施
// fail-open 捕获并给出可见警告——可感知，非静默。
func fileExistsExe(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// isWSLBash reports whether path points at a known WSL bash launcher rather than a
// Windows-native bash (Git Bash / MSYS2 / Cygwin). The path being classified is always
// a WINDOWS path conceptually, so both separators are normalized explicitly — on Linux
// (CI) filepath.ToSlash is a no-op and backslashes would survive, breaking the match.
//
// isWSLBash 报告 path 是否指向已知 WSL bash 启动器而非 Windows 原生 bash
// （Git Bash / MSYS2 / Cygwin）。被判断的路径概念上恒为 WINDOWS 路径，故显式归一
// 两种分隔符——Linux（CI）上 filepath.ToSlash 是 no-op，反斜杠会残留导致匹配失效。
func isWSLBash(path string) bool {
	p := strings.ToLower(strings.ReplaceAll(path, `\`, `/`))
	return strings.Contains(p, "/windows/system32/") ||
		strings.Contains(p, "/windows/syswow64/") ||
		strings.Contains(p, "/microsoft/windowsapps/")
}

// IsHookInfraFailure distinguishes "bash could not run the script" from "the script ran
// and reported FAIL". Spawn errors (bash vanished, permission) and bash exit 126/127
// (script file not readable / not found — the WSL-bash-on-Windows signature) are
// infrastructure; any other exit code comes from the script itself and keeps the
// gate-verdict semantics. Accepted trade-off: a 126/127 from INSIDE a script (an
// external command the script needs is missing, e.g. no grep) also fails open — for
// hazard-guard that is a deliberate safety downgrade, but the alternative (the old
// WSL behavior: every turn hard-blocked) is strictly worse, and the warning stays
// visible either way.
//
// IsHookInfraFailure 区分"bash 没能跑起脚本"与"脚本跑了并报告 FAIL"。spawn 错误
// （bash 消失、权限问题）与 bash exit 126/127（脚本文件不可读/不存在——WSL
// bash on Windows 的特征）属基础设施；其他退出码来自脚本本身，保留门禁结论语义。
// 已接受的权衡：脚本内部的 126/127（脚本依赖的外部命令缺失，如没有 grep）同样
// fail-open——对 hazard-guard 这是有意的安全降级，但替代方案（旧 WSL 行为：每轮
// 硬阻断）严格更糟，且警告始终可见。
func IsHookInfraFailure(err error) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return true
	}
	code := exitErr.ExitCode()
	return code == 126 || code == 127
}
