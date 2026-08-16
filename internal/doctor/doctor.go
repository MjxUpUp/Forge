// Package doctor audits the cross-agent forge environment: which agent hosts have
// forge hooks wired, which forge binary each host's hooks resolve to, and whether
// those binaries agree on version with the running forge and with each other.
//
// Motivation (2026-08 dogfood): version drift between hosts is a recurring real
// incident class — kimi stuck on 1.28.4 while 1.29.0 shipped, a stray manually-built
// forge.exe in the npm-global root winning PATHEXT resolution over the run.js shim,
// stale PATH binaries producing false-positive "README dead link" audits. None of
// these are visible until a host silently misbehaves; doctor makes them a one-command
// check.
//
// Package doctor 审计跨 agent 的 forge 环境：哪些 agent host 接了 forge hook、各 host
// 的 hook 解析到哪个 forge 二进制、这些二进制的版本与运行中的 forge 及彼此是否一致。
//
// 动机（2026-08 dogfood）：host 间版本漂移是反复发生的真实事故类——kimi 停在 1.28.4
// 而 1.29.0 已发、npm-global 根下手动 build 的 forge.exe 靠 PATHEXT 抢在 run.js shim
// 前面、PATH 上的旧二进制产出 "README 断链" 假阳性审计。这些在 host 静默出错前都
// 不可见；doctor 把它们变成一条命令可查。
package doctor

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
)

// Host statuses. "ok" means wired AND version agrees with the running forge;
// "drift" means wired but version disagrees (the headline finding);
// "nover" means wired but the binary or its version could not be resolved;
// "missing" means no forge wiring found for that host.
//
// Host 状态。"ok" = 已接线且版本与运行中的 forge 一致；"drift" = 已接线但版本不一致
// （头条发现）；"nover" = 已接线但二进制或其版本无法解析；"missing" = 该 host 未发现
// forge 接线。
const (
	StatusOK      = "ok"
	StatusDrift   = "drift"
	StatusNoVer   = "nover"
	StatusMissing = "missing"
)

// HostReport is one agent host's audit result.
//
// HostReport 是单个 agent host 的审计结果。
type HostReport struct {
	Host      string `json:"host"`
	Status    string `json:"status"`
	HookPath  string `json:"hook_path"`         // 扫描的 hook 文件/目录（多文件时为主文件）
	ForgeCmds int    `json:"forge_cmds"`        // 含 forge 的命令条数
	Bin       string `json:"bin,omitempty"`     // hook 命令解析到的 forge 二进制
	Version   string `json:"version,omitempty"` // 该二进制的版本
	Err       string `json:"err,omitempty"`     // 非致命诊断（路径解析失败等）
}

// PathEntry is one forge executable found on PATH.
//
// PathEntry 是 PATH 上找到的一个 forge 可执行文件。
type PathEntry struct {
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
}

// Report is the full doctor output.
//
// Report 是 doctor 的完整输出。
type Report struct {
	SelfVersion string       `json:"self_version"`
	Resolved    string       `json:"resolved_on_path"` // exec.LookPath("forge") 结果
	PathForge   []PathEntry  `json:"path_forge"`       // PATH 上全部 forge 可执行文件（按 PATH 顺序）
	Hosts       []HostReport `json:"hosts"`
}

// Options carries injectable dependencies for hermetic tests.
//
// Options 承载可注入依赖，用于密封测试。
type Options struct {
	// VersionRunner runs `<bin> --version` and returns the first line. Default: exec with 3s timeout.
	VersionRunner func(bin string) (string, error)
	// LookPath resolves a bare command name to a binary path. Default: exec.LookPath.
	LookPath func(name string) (string, error)
	// ScanPATH lists PATH directories in order. Default: os.Getenv("PATH") split.
	ScanPATH func() []string
}

// semver extracts a bare version token (1.30.0, 1.30.0-beta.1) from a version string.
// Unanchored by design — accepts date-shaped stamps (2026.08.16) too; a date-stamped
// dev build may report spurious drift against a semver self, an accepted edge (review #10).
//
// semver 从版本串中提取裸版本 token（1.30.0、1.30.0-beta.1）。刻意不锚定——日期形态
// （2026.08.16）也会被收；日期戳 dev 构建对 semver self 可能报假 drift，接受的边缘
// （评审 #10）。
var semver = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+[0-9A-Za-z.+-]*`)

func (o *Options) fill() {
	if o.VersionRunner == nil {
		o.VersionRunner = func(bin string) (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, bin, "--version").Output()
			if err != nil {
				return "", err
			}
			line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
			return strings.TrimSpace(line), nil
		}
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.ScanPATH == nil {
		o.ScanPATH = func() []string {
			return filepath.SplitList(os.Getenv("PATH"))
		}
	}
}

// hostSpec pairs a host name with candidate hook locations. Path resolution reuses
// agentbridge's exported helpers wherever they exist — doctor must not carry a second
// copy of any host-path convention (they drift; see ClaudeConfigHomeDir).
//
// hostSpec 把 host 名与其候选 hook 位置配对。路径解析一律复用 agentbridge 的既有导出
// helper——doctor 不得持有任何 host 路径约定的第二份副本（会漂移，见 ClaudeConfigHomeDir）。
type hostSpec struct {
	host  string
	paths func() ([]string, error)
}

func hostSpecs() []hostSpec {
	return []hostSpec{
		{"claude-code", func() ([]string, error) {
			return []string{filepath.Join(agentbridge.ClaudeConfigHomeDir(), "settings.json")}, nil
		}},
		{"codex", func() ([]string, error) {
			p, err := agentbridge.CodexHooksPath()
			return []string{p}, err
		}},
		{"cursor", func() ([]string, error) {
			p, err := agentbridge.CursorHooksPath()
			return []string{p}, err
		}},
		{"windsurf", func() ([]string, error) {
			p, err := agentbridge.WindsurfHooksPath()
			return []string{p}, err
		}},
		{"kimi", func() ([]string, error) {
			// kimi 有两代接线载体：config.toml 的 [[hooks]] 块（老）与 plugins/managed/
			// 树（plugin 模型，hook 在 .kimi-plugin/plugin.json）。doctor 两者都扫——只扫
			// config.toml 会把 plugin 接线的机器误报成 missing（本机实测踩到）。
			//
			// kimi has two wiring carriers: the [[hooks]] block in config.toml (legacy)
			// and the plugins/managed/ tree (plugin model, hooks inside .kimi-plugin/
			// plugin.json). doctor scans both — config.toml-only would misreport
			// plugin-wired machines as missing (hit on this very machine).
			p, err := agentbridge.KimiConfigPath()
			if err != nil {
				return nil, err
			}
			home, _ := agentbridge.KimiConfigHome()
			return []string{p, filepath.Join(home, "plugins")}, nil
		}},
		{"reasonix", func() ([]string, error) {
			home, err := agentbridge.ReasonixConfigHome()
			if err != nil {
				return nil, err
			}
			return []string{filepath.Join(home, "plugins")}, nil
		}},
		{"codebuddy", func() ([]string, error) {
			home, err := agentbridge.CodeBuddyWorkBuddyHome()
			if err != nil {
				return nil, err
			}
			return []string{filepath.Join(home, "plugins")}, nil
		}},
		{"cline", func() ([]string, error) {
			d, err := agentbridge.ClineHooksDir()
			return []string{d}, err
		}},
		{"opencode", func() ([]string, error) {
			p, err := agentbridge.OpencodePluginPath()
			return []string{p}, err
		}},
	}
}

// Run performs the full audit. selfVersion is the running forge's version string
// (any format; normalized internally). Never returns an error — per-host resolution
// failures land in HostReport.Err; doctor degrades to reporting what it can see.
//
// Run 执行完整审计。selfVersion 是运行中 forge 的版本串（任意格式，内部归一化）。
// 永不返回 error——单 host 路径解析失败记入 HostReport.Err；doctor 降级为报告能看到的。
func Run(selfVersion string, opts Options) Report {
	opts.fill()
	self := normalizeVersion(selfVersion)
	// 空切片而非 nil：--json 时 [] 比 null 对机器消费方更友好（评审 #7）。
	//
	// Empty slices, not nil: [] beats null for machine consumers of --json (review #7).
	rep := Report{SelfVersion: self, PathForge: []PathEntry{}, Hosts: []HostReport{}}
	if p, err := opts.LookPath("forge"); err == nil {
		rep.Resolved = p
	}
	rep.PathForge = scanPATH(opts)
	for _, spec := range hostSpecs() {
		rep.Hosts = append(rep.Hosts, auditHost(spec, self, opts))
	}
	return rep
}

// maxVersionProbes caps per-source version executions (PATH scan and each host's bin
// loop alike): every probe can cost up to the 3s timeout, and tokens are mined from
// config text — bounded probing keeps a garbage-token pile from stalling the audit
// (review #6).
//
// maxVersionProbes 限制每来源的版本执行次数（PATH 扫描与各 host 的 bin 循环同限）：
// 每次探测最多耗 3s 超时，且 token 挖自配置文本——有界探测防垃圾 token 堆拖死审计
// （评审 #6）。
const maxVersionProbes = 5

// scanPATH lists every forge executable on PATH (in PATH order) with versions,
// capped at maxVersionProbes version probes. Multiple hits = the classic
// stray-exe/wins-shim setup.
//
// scanPATH 按 PATH 顺序列出其上全部 forge 可执行文件及版本，版本探测上限
// maxVersionProbes 个。多个命中 = 经典的游离 exe/shim 并存局面。
func scanPATH(opts Options) []PathEntry {
	seen := map[string]bool{}
	out := []PathEntry{}
	names := []string{"forge.exe", "forge", "forge.bat", "forge.cmd"}
	for _, dir := range opts.ScanPATH() {
		if dir == "" {
			continue
		}
		for _, name := range names {
			p := filepath.Join(dir, name)
			// Windows 大小写不敏感文件系统：C:\a 与 c:\a 是同一目录，小写键去重、
			// 展示保留原路径（评审 #8）。
			//
			// Windows case-insensitive filesystem: C:\a and c:\a are the same dir —
			// dedupe on a lowercase key, keep the original path for display (review #8).
			key := strings.ToLower(p)
			if seen[key] {
				continue
			}
			if _, err := os.Stat(p); err != nil {
				continue
			}
			seen[key] = true
			e := PathEntry{Path: p}
			if len(out) < maxVersionProbes {
				if v, err := opts.VersionRunner(p); err == nil {
					e.Version = normalizeVersion(v)
				}
			}
			out = append(out, e)
		}
	}
	return out
}

// auditHost scans one host's hook files: counts forge-carrying command lines, resolves
// the referenced forge binary, checks its version against self.
//
// auditHost 扫描单个 host 的 hook 文件：统计含 forge 的命令行数、解析其引用的 forge
// 二进制、对照 self 版本。
func auditHost(spec hostSpec, self string, opts Options) HostReport {
	r := HostReport{Host: spec.host, Status: StatusMissing}
	paths, err := spec.paths()
	if err != nil {
		r.Err = fmt.Sprintf("路径解析失败: %v", err)
		return r
	}
	var files []string
	for _, p := range paths {
		files = append(files, expandHookFiles(p)...)
	}
	if len(files) == 0 {
		return r
	}
	r.HookPath = files[0]
	var resolved, raw []string
	for _, f := range files {
		cmds, cands := scanFile(f, opts.LookPath)
		if r.ForgeCmds == 0 && cmds > 0 {
			// 证据源归位：HookPath 指向真正携带 forge 接线的第一个文件，而非候选列表
			// 首项（kimi 双载体时首项是无接线的 config.toml）（评审 #9）。
			//
			// Evidence-source honesty: HookPath points at the first file actually
			// carrying forge wiring, not the first candidate (kimi's dual-carrier list
			// starts with the unwired config.toml) (review #9).
			r.HookPath = f
		}
		r.ForgeCmds += cmds
		for _, c := range cands {
			if c.resolved {
				resolved = append(resolved, c.path)
			} else {
				raw = append(raw, c.path)
			}
		}
	}
	if r.ForgeCmds == 0 {
		return r // 文件在但没有 forge 接线 → missing
	}
	// 只对"解析成功"的 bin 做版本探测（评审 #2/#6）：解析失败的 token 原样展示但不执行
	// ——doctor 不该执行用户从未提名、仅因字面含 forge 而捞到的路径；上限
	// maxVersionProbes 防 N×3s 停顿。解析成功的 token 里取第一个真能跑出版本的。
	//
	// Version-probe ONLY tokens that resolved (review #2/#6): unresolved tokens are
	// displayed verbatim but never executed — doctor must not run paths it merely mined
	// out of text for containing "forge"; the maxVersionProbes cap prevents N×3s stalls.
	// Among resolved ones, the first to actually yield a version wins.
	sort.Strings(resolved)
	sort.Strings(raw)
	probes := 0
	for _, bin := range resolved {
		if probes >= maxVersionProbes {
			break
		}
		probes++
		v, err := opts.VersionRunner(bin)
		if err != nil || strings.TrimSpace(v) == "" {
			continue
		}
		r.Bin = bin
		r.Version = normalizeVersion(v)
		r.Status = StatusNoVer
		if self != "" && self != "dev" {
			if r.Version == self {
				r.Status = StatusOK
			} else {
				r.Status = StatusDrift
			}
		}
		return r
	}
	r.Status = StatusNoVer
	if len(resolved) > 0 {
		r.Bin = resolved[0]
	} else if len(raw) > 0 {
		r.Bin = raw[0]
	}
	return r
}

// expandHookFiles turns a candidate path (file or dir) into the concrete file list to
// scan. Dirs are walked non-recursively-bounded (plugins/ trees are shallow); unreadable
// entries are skipped, not fatal.
//
// expandHookFiles 把候选路径（文件或目录）展开成待扫描的具体文件列表。目录走有界
// 遍历（plugins/ 树很浅）；不可读条目跳过、不致命。
func expandHookFiles(p string) []string {
	fi, err := os.Stat(p)
	if err != nil {
		return nil
	}
	if !fi.IsDir() {
		return []string{p}
	}
	var out []string
	_ = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:跳过不可读条目
		}
		if d.IsDir() {
			// plugins 树深度上限 4，防病态深树拖慢审计。上限必须 ≥4：kimi 的 plugin 载体在
			// plugins/managed/forge/.kimi-plugin/plugin.json（相对深度 3 的目录 + 其下文件），
			// 卡 3 会剪掉 .kimi-plugin/ 整棵——doctor 对 kimi 的头条用例（plugin 接线版本
			// 漂移）静默失效（评审 #1 实证复现）。
			//
			// Depth cap 4 for plugin trees, guarding against pathologically deep trees.
			// The cap MUST be ≥4: kimi's plugin carrier lives at plugins/managed/forge/
			// .kimi-plugin/plugin.json (depth-3 dir + file beneath); capping at 3 prunes
			// the whole .kimi-plugin/ tree — silently defeating doctor's headline kimi
			// use case (plugin-wiring version drift), empirically reproduced in review #1.
			if strings.Count(strings.TrimPrefix(filepath.ToSlash(path), filepath.ToSlash(p)), "/") >= 4 {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		// 只扫 plausibly-hook 载体。刻意排除 .md/.ts 等：plugin 树里 README/源码满篇
		// "forge" 字样，扫它们只会产出垃圾 token（E2E 实测 reasonix 曾从文档里捞到
		// "/.claude/plugins/cache/..." 当 bin）。
		//
		// Only plausibly-hook carriers. Deliberately excludes .md/.ts etc.: READMEs and
		// sources in plugin trees mention "forge" everywhere — scanning them yields
		// garbage tokens (E2E caught reasonix picking "/.claude/plugins/cache/..." out
		// of a doc as its bin).
		case ".json", ".toml", ".sh", ".cmd", ".bat", ".ps1":
			out = append(out, path)
		}
		return nil
	})
	return out
}

// binCandidate is one forge binary token mined from a hook file: path plus whether it
// resolved to something the filesystem/PATH actually knows (doctor only ever EXECUTES
// resolved candidates — see auditHost).
//
// binCandidate 是从 hook 文件挖到的一个 forge 二进制 token：路径 + 是否解析到文件
// 系统/PATH 真正认识的东西（doctor 只执行解析成功的候选——见 auditHost）。
type binCandidate struct {
	path     string
	resolved bool
}

// scanFile counts lines referencing forge and collects the forge binary tokens they
// invoke. Line-oriented scanning is format-agnostic across the nine hosts' hook
// schemas (nested JSON / flat TOML / shell wrappers) — precision is not needed, the
// report only has to be right about "wired or not" and "which binary". lookPath is
// injected (Options.LookPath) so tests control bare-name resolution.
//
// scanFile 统计引用 forge 的行数并收集其调用的 forge 二进制 token。按行扫描对九个
// host 的 hook schema（嵌套 JSON/扁平 TOML/shell wrapper）格式无关——不需要精确解析，
// 报告只需在"接没接"与"哪个二进制"上正确。lookPath 注入（Options.LookPath）让测试
// 可控裸名解析。
func scanFile(path string, lookPath func(string) (string, error)) (int, []binCandidate) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, nil
	}
	cmds := 0
	seen := map[string]bool{}
	var bins []binCandidate
	for _, ln := range strings.Split(string(data), "\n") {
		low := strings.ToLower(ln)
		if !strings.Contains(low, "forge") {
			continue
		}
		// 含 forge 的行未必是接线命令：注册表/文档行（kimi installed.json 的
		// "id": "forge"、URL）只是元数据。九个 host 的 forge 接线命令一律形如
		// `forge hook <event>`（含 --agent 变体），故要求行内同时出现 "hook" 才计数——
		// 否则 plugin 接线坏了但注册表还在的机器会假报 ok（评审 #1 的失效场景）。
		//
		// A forge-carrying line is not necessarily wiring: registry/doc lines (kimi
		// installed.json's "id": "forge", URLs) are metadata. All nine hosts' forge hook
		// commands take the shape `forge hook <event>` (--agent variants included), so a
		// line must also contain "hook" to count — otherwise a machine whose plugin hooks
		// broke while the registry stayed intact false-reports ok (review #1's scenario).
		if !strings.Contains(low, "hook") {
			continue
		}
		if tok, ok := forgeToken(ln); ok {
			cmds++
			bin, ok := resolveBin(tok, lookPath)
			if !seen[bin] {
				seen[bin] = true
				bins = append(bins, binCandidate{path: bin, resolved: ok})
			}
		}
	}
	return cmds, bins
}

// tokenRe matches contiguous path-ish runs (letters, digits, - _ . / \ : ~ @). Every
// other character — whitespace, quotes, braces, commas, = — acts as a separator, so the
// same extractor serves pretty-printed JSON, MINIFIED single-line JSON, TOML, and shell
// lines. Whitespace splitting alone fails on minified JSON (the whole line is one token).
//
// tokenRe 匹配连续的路径形态 run（字母、数字、- _ . / \ : ~ @）。其余字符——空白、引号、
// 花括号、逗号、=——一律当分隔符，故同一提取器通吃 pretty JSON、单行 minified JSON、
// TOML 与 shell 行。仅按空白切分在 minified JSON 上失效（整行是一个 token）。
var tokenRe = regexp.MustCompile(`[A-Za-z0-9_\-./\\:~@]+`)

// forgeToken extracts the first path-ish token containing "forge". Returns ok=false
// when no such token exists.
//
// forgeToken 提取行内第一个含 "forge" 的路径形态 token。不存在此类 token 时返回
// ok=false。
func forgeToken(line string) (string, bool) {
	for _, f := range tokenRe.FindAllString(line, -1) {
		if strings.Contains(strings.ToLower(filepath.Base(f)), "forge") {
			return f, true
		}
	}
	return "", false
}

// resolveBin turns a token into something VersionRunner may execute: an existing path
// stays; a bare name resolves via the injected LookPath. Returns resolved=false for
// tokens neither Stat nor LookPath could confirm — those are reported verbatim but
// NEVER executed (doctor must not run binaries the user never nominated; review #2).
//
// resolveBin 把 token 变成 VersionRunner 可执行的东西：存在的路径保留；裸名走注入的
// LookPath。Stat 与 LookPath 都确认不了的 token 返回 resolved=false——原样上报但从
// 不执行（doctor 不运行用户从未提名的二进制；评审 #2）。
func resolveBin(tok string, lookPath func(string) (string, error)) (string, bool) {
	tok = strings.TrimPrefix(tok, `\`)
	if filepath.IsAbs(tok) || strings.HasPrefix(tok, ".") {
		if _, err := os.Stat(tok); err == nil {
			return tok, true
		}
	}
	if lookPath != nil {
		if lp, err := lookPath(tok); err == nil {
			return lp, true
		}
	}
	return tok, false
}

// normalizeVersion reduces any version output to its semver-ish token
// ("forge version 1.30.0 (commit..)" → "1.30.0"); returns "" when none found.
//
// normalizeVersion 把任意版本输出归一为 semver 形态 token（"forge version 1.30.0
// (commit..)" → "1.30.0"）；找不到时返回 ""。
func normalizeVersion(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if m := semver.FindString(s); m != "" {
		return m
	}
	return s
}
