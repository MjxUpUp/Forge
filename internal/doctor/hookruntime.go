package doctor

// hookruntime.go — 「hook 命令在桌面端最小 PATH 下跑得起来吗」的主动检查
// （docs/plans/hook-silent-failure-visibility-2026-10.md，REQ-1）。
//
// 背景：hook 命令串写的是裸 `forge hook ...`，由宿主经 `/bin/sh -c` 执行。桌面端
// 宿主的 PATH 与交互 shell 不同，常见三种「forge 自己都没跑起来」的失败：
//   - `/bin/sh: forge: command not found`（退出码 127）——forge 不在 hook 的 PATH 里；
//   - `env: node: No such file or directory`——npm 全局安装的 forge 是 node 垫片
//     （run.js 的 shebang 是 `#!/usr/bin/env node`），node（nvm/fnm 装的）不在 hook
//     的 PATH 里；
//   - 退出码 126——forge 解析到临时目录（/private/tmp 下）里不可执行的副本。
// 这些发生在 forge 进程之前或之外，进程内无法自报；宿主只会逐次报一条原始错误。
// doctor 是它们唯一能被主动看见的地方：在假定的 hook PATH 下复现 sh 的解析过程。
//
// hookruntime.go — active check of "can the hook command start under a desktop
// host's minimal PATH". The hook command is a bare `forge hook ...` run by the
// host via `/bin/sh -c`; a desktop host's PATH differs from an interactive
// shell's, giving three "forge never started" failures: forge off the hook PATH
// (127), the npm shim's `#!/usr/bin/env node` with node (nvm/fnm) off that PATH,
// and a forge copy in a temp dir that is not executable (126). All happen before
// or outside the forge process, so nothing in-process can report them; doctor is
// the one place they can be seen proactively, by replaying sh's resolution under
// an assumed hook PATH.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// DefaultHookPath is the assumed PATH of a desktop-launched host: macOS /etc/paths
// order. Override with Options.HookPath (the CLI wires FORGE_DOCTOR_HOOK_PATH) to
// replay a PATH you measured from a real hook run.
//
// DefaultHookPath 是桌面端宿主进程的假定 PATH：macOS /etc/paths 的顺序。用
// Options.HookPath（CLI 接 FORGE_DOCTOR_HOOK_PATH）换成从真实 hook 运行量到的 PATH。
const DefaultHookPath = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

// Hook runtime problem codes.
//
// hook 运行环境问题码。
const (
	// HookRuntimeForgeUnresolved: forge is not resolvable under the hook PATH (exit 127).
	//
	// HookRuntimeForgeUnresolved：forge 在 hook PATH 下不可解析（退出码 127）。
	HookRuntimeForgeUnresolved = "forge-unresolved"
	// HookRuntimeNodeUnresolved: forge is a node shim and node is not resolvable under the hook PATH.
	//
	// HookRuntimeNodeUnresolved：forge 是 node 垫片，而 node 在 hook PATH 下不可解析。
	HookRuntimeNodeUnresolved = "node-unresolved"
	// HookRuntimeNotExecutable: the forge file exists under the hook PATH but lacks an exec bit (exit 126).
	//
	// HookRuntimeNotExecutable：forge 文件在 hook PATH 下存在但缺执行位（退出码 126）。
	HookRuntimeNotExecutable = "forge-not-executable"
	// HookRuntimeInTempDir: forge resolves into a temp dir (cleaned up / sandbox-restricted).
	//
	// HookRuntimeInTempDir：forge 解析到临时目录里（会被清理或受沙盒限制）。
	HookRuntimeInTempDir = "forge-in-temp-dir"
	// HookRuntimeUnknownHooks: settings wire hooks this binary does not know (version skew).
	//
	// HookRuntimeUnknownHooks：settings 接了本二进制不认识的 hook（版本错位）。
	HookRuntimeUnknownHooks = "unknown-hooks"
)

// HookRuntimeProblem is one actionable finding with its repair advice.
//
// HookRuntimeProblem 是一条可处置的发现，附修复办法。
type HookRuntimeProblem struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
}

// HookRuntime is the hook-runtime section of the doctor report.
//
// HookRuntime 是 doctor 报告的 hook 运行环境节。
type HookRuntime struct {
	// PathUsed is the assumed hook PATH the resolution ran under.
	//
	// PathUsed 是解析所用的假定 hook PATH。
	PathUsed string `json:"path_used"`
	// Forge is the forge executable resolved under PathUsed ("" when unresolved).
	//
	// Forge 是在 PathUsed 下解析到的 forge 可执行文件（不可解析时为空）。
	Forge string `json:"forge,omitempty"`
	// Interpreter is the shim's interpreter name ("node"); empty for a native binary.
	//
	// Interpreter 是垫片的解释器名（"node"）；原生二进制为空。
	Interpreter string `json:"interpreter,omitempty"`
	// Node is the interpreter resolved under PathUsed ("" when unresolved or not needed).
	//
	// Node 是在 PathUsed 下解析到的解释器（不可解析或不需要时为空）。
	Node string `json:"node,omitempty"`
	// UnknownHooks lists forge hooks wired in the user-level settings that this
	// binary does not know (version skew; heal with forge sync --force).
	//
	// UnknownHooks 列出用户级 settings 接了、但本二进制不认识的 forge hook（版本错位；
	// forge sync --force 自愈）。
	UnknownHooks []string             `json:"unknown_hooks,omitempty"`
	Problems     []HookRuntimeProblem `json:"problems,omitempty"`
}

// resolveInPath replays sh's command lookup of `name` over a PATH string: the
// first directory entry that is a regular file wins; an entry without an exec
// bit is remembered and reported separately (126, not 127). Relative PATH
// elements are skipped — a hook host runs from an arbitrary cwd.
//
// resolveInPath 在 PATH 串上复现 sh 对 `name` 的查找：第一个是普通文件的目录项胜出；
// 缺执行位的项被记下、单独上报（126 而非 127）。相对 PATH 元素跳过——hook 宿主的
// cwd 任意。
func resolveInPath(name, pathVar string) (found string, noExec string) {
	for _, dir := range filepath.SplitList(pathVar) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if info.Mode().Perm()&0o111 == 0 {
			if noExec == "" {
				noExec = p
			}
			continue
		}
		return p, ""
	}
	return "", noExec
}

// shebangInterpreter returns the interpreter a script asks for: the argument of
// `#!/usr/bin/env <name>` (skipping env flags), or the base name of a direct
// interpreter path. abs is that direct path when the shebang names one. "" = not
// a script (native binary).
//
// shebangInterpreter 返回脚本要求的解释器：`#!/usr/bin/env <name>` 的参数（跳过 env
// 的 flag），或直接解释器路径的基名。shebang 直接给出路径时 abs 即该路径。""=不是
// 脚本（原生二进制）。
func shebangInterpreter(path string) (name string, abs string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	buf := make([]byte, 256)
	n, _ := f.Read(buf)
	line := string(buf[:n])
	if !strings.HasPrefix(line, "#!") {
		return "", ""
	}
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	fields := strings.Fields(strings.TrimPrefix(line, "#!"))
	if len(fields) == 0 {
		return "", ""
	}
	if filepath.Base(fields[0]) == "env" {
		for _, a := range fields[1:] {
			if strings.HasPrefix(a, "-") || strings.Contains(a, "=") {
				continue
			}
			return a, ""
		}
		return "", ""
	}
	return filepath.Base(fields[0]), fields[0]
}

// nativeSibling finds the platform binary shipped as a sibling npm package of the
// forge shim (`.../@agent_forge/forge/run.js` ↔ `.../@agent_forge/forge-<os>-<arch>/bin/forge`)
// — the node-free fix target. "" when the layout is not an npm install.
//
// nativeSibling 找 forge 垫片的同级 npm 平台包里的原生二进制（
// `.../@agent_forge/forge/run.js` ↔ `.../@agent_forge/forge-<os>-<arch>/bin/forge`）
// ——不依赖 node 的修复目标。目录结构不是 npm 安装时返回 ""。
func nativeSibling(realShim string) string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	p := filepath.Join(filepath.Dir(realShim), "..", "forge-"+runtime.GOOS+"-"+arch, "bin", "forge")
	if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
		return filepath.Clean(p)
	}
	return ""
}

// underAny reports whether p lies inside any of dirs, comparing symlink-resolved
// forms too (macOS: /var/folders → /private/var/folders).
//
// underAny 报告 p 是否位于 dirs 任一目录之内，同时比较符号链接解析后的形态
// （macOS：/var/folders → /private/var/folders）。
func underAny(p string, dirs []string) bool {
	forms := []string{filepath.Clean(p)}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		forms = append(forms, r)
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		dForms := []string{filepath.Clean(d)}
		if r, err := filepath.EvalSymlinks(d); err == nil {
			dForms = append(dForms, r)
		}
		for _, f := range forms {
			for _, df := range dForms {
				if f == df || strings.HasPrefix(f, df+string(filepath.Separator)) {
					return true
				}
			}
		}
	}
	return false
}

// CheckHookRuntime replays how a host's `/bin/sh -c "forge hook ..."` would
// resolve forge — and, for a node shim, its interpreter — under pathVar, and
// returns the findings. tempDirs are the roots that count as "temp" (a forge copy
// there is flagged). lookCurrent resolves forge on the doctor's own PATH purely to
// make the fix advice concrete. Windows is not checked (cmd/PowerShell resolution
// differs from sh) and returns nil. Read-only.
//
// CheckHookRuntime 复现宿主的 `/bin/sh -c "forge hook ..."` 在 pathVar 下如何解析
// forge——node 垫片则连同其解释器——并返回发现。tempDirs 是算作「临时」的根（forge
// 副本落在其中会被标出）。lookCurrent 在 doctor 自己的 PATH 上解析 forge，仅用于让
// 修复办法具体。Windows 不检查（cmd/PowerShell 的解析与 sh 不同），返回 nil。只读。
func CheckHookRuntime(pathVar string, tempDirs []string, lookCurrent func(string) (string, error)) *HookRuntime {
	if runtime.GOOS == "windows" {
		return nil
	}
	rt := &HookRuntime{PathUsed: pathVar}
	forge, noExec := resolveInPath("forge", pathVar)
	switch {
	case forge == "" && noExec != "":
		rt.Problems = append(rt.Problems, HookRuntimeProblem{
			Code:   HookRuntimeNotExecutable,
			Detail: "hook PATH 下的 forge 缺执行位（" + noExec + "）——hook 命令会以退出码 126 失败",
			Fix:    "chmod +x " + noExec + "；或重新安装 forge（npm i -g @agent_forge/forge）",
		})
	case forge == "":
		fix := "把 forge 所在目录加进桌面端宿主的 PATH，或把二进制链到 hook PATH 内的目录（如 /usr/local/bin）"
		if lookCurrent != nil {
			if cur, err := lookCurrent("forge"); err == nil {
				fix = "ln -sf " + cur + " /usr/local/bin/forge（让 hook PATH 内的 /usr/local/bin 能解析到 forge）；或把 " + filepath.Dir(cur) + " 加进桌面端宿主的 PATH"
			}
		}
		rt.Problems = append(rt.Problems, HookRuntimeProblem{
			Code:   HookRuntimeForgeUnresolved,
			Detail: "假定 hook PATH（" + pathVar + "）下找不到 forge——hook 命令会报 `forge: command not found`（退出码 127）",
			Fix:    fix + "。真实 hook PATH 不同时用 FORGE_DOCTOR_HOOK_PATH=<量到的 PATH> forge doctor 复核",
		})
	default:
		rt.Forge = forge
		real := forge
		if r, err := filepath.EvalSymlinks(forge); err == nil {
			real = r
		}
		if underAny(forge, tempDirs) {
			rt.Problems = append(rt.Problems, HookRuntimeProblem{
				Code:   HookRuntimeInTempDir,
				Detail: "hook PATH 解析到的 forge 在临时目录里（" + forge + "）——临时目录副本会被系统清理，沙盒下常不可执行（退出码 126）",
				Fix:    "把 forge 安装到固定位置（npm i -g @agent_forge/forge），并清掉临时目录里的副本",
			})
		}
		if interp, abs := shebangInterpreter(real); interp != "" {
			rt.Interpreter = interp
			var node string
			if abs != "" && filepath.IsAbs(abs) {
				if info, err := os.Stat(abs); err == nil && info.Mode().IsRegular() {
					node = abs
				}
			} else {
				node, _ = resolveInPath(interp, pathVar)
			}
			rt.Node = node
			if node == "" {
				fix := "让 hook 的 PATH 含 " + interp + " 所在目录（nvm/fnm 装的 " + interp + " 只在交互 shell 的 PATH 里），或改用不依赖 " + interp + " 的原生 forge 二进制"
				if native := nativeSibling(real); native != "" {
					fix = "ln -sf " + native + " /usr/local/bin/forge（原生二进制不依赖 " + interp + "；确保它排在 hook PATH 内垫片之前）；或让 hook 的 PATH 含 " + interp + " 所在目录"
				}
				rt.Problems = append(rt.Problems, HookRuntimeProblem{
					Code:   HookRuntimeNodeUnresolved,
					Detail: "forge（" + forge + "）是 " + interp + " 垫片，但假定 hook PATH（" + pathVar + "）下找不到 " + interp + "——hook 会报 `env: " + interp + ": No such file or directory`（退出码 127），forge 进程根本没启动",
					Fix:    fix,
				})
			}
		}
	}
	rt.UnknownHooks = hooks.UserSettingsUnknownHooks()
	if len(rt.UnknownHooks) > 0 {
		rt.Problems = append(rt.Problems, HookRuntimeProblem{
			Code:   HookRuntimeUnknownHooks,
			Detail: "用户级 settings 接了本 forge 二进制不认识的 hook：" + strings.Join(rt.UnknownHooks, ", ") + "（版本错位，hook 会报 unknown hook）",
			Fix:    "forge sync --force 按当前二进制重生成接线；仍报错说明 PATH 上有旧版 forge，升级到与接线一致的版本",
		})
	}
	return rt
}
