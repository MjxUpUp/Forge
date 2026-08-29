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

// SkillsDriftItem is one actionable skills-distribution gap: a canonical skill that
// is missing or content-drifted in a target directory.
//
// SkillsDriftItem 是单条可处置的 skills 分发缺口：canonical 有而目标缺失或内容分叉
// 的 skill。
type SkillsDriftItem struct {
	Skill  string `json:"skill"`
	Target string `json:"target"`
	State  string `json:"state"` // missing | drift
}

// SkillsDriftSummary is the skills-distribution section of the doctor report.
// Counts only actionable states (missing/drift) in Items; linked/copy-in-sync
// are healthy and only surface as totals. Target-only orphans are excluded —
// they are usually the user's own non-forge skills and would drown the signal.
//
// Error is set when the audit itself could not run (canonical resolve /
// DriftCheck failure) — zero counts plus a green check would misreport a dead
// probe as a healthy fleet, the exact silence this section exists to kill.
// TargetErrors carries per-target partial failures (unreadable dirs etc.) from
// DriftCheck: the counts are still meaningful, but coverage was not complete.
//
// Skipped lists targets NOT audited because the target's agent home does not
// exist (agent not installed on this machine — M-3, 2026-08-21). Without this
// gate, `forge doctor` on a single-agent machine reported every canonical skill
// as missing from every uninstalled target: a wall of unactionable noise that
// drowned the real gaps. Doctor audits the environment as-installed;
// `forge skills drift-check` keeps full all-target coverage for the explicit ask.
//
// SkillsDriftSummary 是 doctor 报告的 skills 分发节。Items 只收可处置态
// （missing/drift）；linked/copy-in-sync 健康态仅以总数出现。target-only 孤儿排除
// ——通常是用户自己的非 forge skill，收进来会淹没信号。
//
// Error 在审计本身跑不起来（canonical 解析 / DriftCheck 报错）时设置——零计数
// 加绿色对勾会把死探针误报成健康，恰是本节要消灭的静默。TargetErrors 承载
// DriftCheck 的 per-target 部分失败（目录不可读等）：计数仍有意义，但覆盖不全。
//
// Skipped 列出未审计的目标——目标 agent home 不存在（本机未装该 agent——M-3，
// 2026-08-21）。没有这道门，单 agent 机器上的 `forge doctor` 会把每个 canonical
// skill 报成在每个未安装目标上 missing：一墙不可处置的噪声，淹没真实缺口。
// doctor 审计"按已安装现状"的环境；`forge skills drift-check` 在显式全量问询下
// 保留全目标覆盖。
type SkillsDriftSummary struct {
	Canonical    string            `json:"canonical"`
	Error        string            `json:"error,omitempty"`
	TargetErrors []string          `json:"target_errors,omitempty"`
	Linked       int               `json:"linked"`
	CopySync     int               `json:"copy_in_sync"`
	Missing      int               `json:"missing"`
	Drifted      int               `json:"drift"`
	Items        []SkillsDriftItem `json:"items,omitempty"`
	Skipped      []string          `json:"skipped,omitempty"`
}

// Report is the full doctor output.
//
// Report 是 doctor 的完整输出。
type Report struct {
	SelfVersion string              `json:"self_version"`
	Resolved    string              `json:"resolved_on_path"` // exec.LookPath("forge") 结果
	PathForge   []PathEntry         `json:"path_forge"`       // PATH 上全部 forge 可执行文件（按 PATH 顺序）
	Hosts       []HostReport        `json:"hosts"`
	Skills      *SkillsDriftSummary `json:"skills,omitempty"`
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
	// SkillsDriftProbe collects the skills-distribution drift summary. Default: nil
	// (section skipped) — the CLI layer injects the real probe (doctor stays free of
	// skillsdist/skillscanonical imports, matching the LookPath injection style).
	// Errors are reported inside the summary's Canonical field prefix, never abort
	// the whole audit — skills drift must not take down host version auditing.
	//
	// SkillsDriftProbe 收集 skills 分发 drift 摘要。默认 nil（跳过该节）——CLI 层
	// 注入真实探针（doctor 保持不依赖 skillsdist/skillscanonical，与 LookPath 注入
	// 风格一致）。错误以摘要 Canonical 字段前缀上报，绝不中止整个审计——skills
	// drift 不得拖垮 host 版本审计。
	SkillsDriftProbe func() *SkillsDriftSummary
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
	host   string
	target func() ([]scanTarget, error)
}

// scanTarget is one candidate location: a file, or a directory walked with a depth cap.
// names != nil switches file filtering from extension-based (shallow plugin trees) to
// basename-whitelist (deep trees like the claude plugin cache, where an extension filter
// would scan a full repo copy's internals).
//
// scanTarget 是一个候选位置：文件，或带深度上限遍历的目录。names 非 nil 时文件过滤从
// 按扩展名（浅 plugin 树）切换为按基名白名单（claude plugin cache 这类深树——扩展名
// 过滤会把整个仓库副本的内部文件都扫进来）。
type scanTarget struct {
	path  string
	depth int
	names map[string]bool
}

// defaultScanDepth is the plugin-tree walk cap (see expandHookFiles for why ≥4).
//
// defaultScanDepth 是 plugin 树遍历深度上限（为何 ≥4 见 expandHookFiles）。
const defaultScanDepth = 4

// hookCarrierNames is the basename whitelist for deep-tree walks: only files that can
// actually carry hook wiring. Inside a plugin cache's repo copy this skips every other
// .json/.toml (golden fixtures, manifests) while still reaching hooks.json/plugin.json.
//
// hookCarrierNames 是深树遍历的基名白名单：只收真正可能承载 hook 接线的文件。plugin
// cache 里的仓库副本中，其他一切 .json/.toml（golden fixture、manifest）都被跳过，
// hooks.json/plugin.json 仍可达。
var hookCarrierNames = map[string]bool{
	"plugin.json": true,
	"hooks.json":  true,
	"hooks.toml":  true,
}

func hostSpecs() []hostSpec {
	return []hostSpec{
		{"claude-code", func() ([]scanTarget, error) {
			// 两代接线载体：settings.json 内联 hooks（老）与 plugins/cache/ 下的
			// marketplace 插件（plugin pack + autotakeover 后的主分发模型——settings.json
			// 被 dedupe 清干净，hook 全在 cache/forge/forge/<sha>/ 里）。只扫 settings.json
			// 会把 plugin 接线的机器误报 missing（评审二轮 MEDIUM，本机实测）。
			//
			// Two wiring carriers: inline hooks in settings.json (legacy) and the
			// marketplace plugin under plugins/cache/ (the primary distribution model
			// since plugin pack + autotakeover — settings.json is deduped clean, hooks
			// live in cache/forge/forge/<sha>/). Scanning settings.json-only misreports
			// plugin-wired machines as missing (round-2 review MEDIUM, hit on this
			// machine).
			home := agentbridge.ClaudeConfigHomeDir()
			targets := []scanTarget{{path: filepath.Join(home, "settings.json")}}
			if live := liveClaudePluginTargets(home); len(live) > 0 {
				// 注册表指向的活副本优先（评审三轮 MEDIUM-1）：cache 按 sha 累积历史
				// 副本（本机 13 个），全树扫描会引用字典序第一个死副本当 HookPath、
				// ForgeCmds 按副本数膨胀，更糟的是活副本坏了会被完好的死副本掩盖成
				// 假 ok。installed_plugins.json 是 claude-code 自己判定加载哪份的真
				// 相源，以它为准。
				//
				// Prefer the registry-named LIVE copy (round-3 review MEDIUM-1): the
				// cache accumulates one copy per historical sha (13 on this machine);
				// a full-tree scan cites whichever stale copy sorts first as HookPath,
				// multiplies ForgeCmds by copy count, and — worse — a broken live copy
				// gets masked into a false ok by intact stale ones.
				// installed_plugins.json is the source of truth claude-code itself uses
				// to decide what loads.
				return append(targets, live...), nil
			}
			// 回退：注册表不可读/未提 forge/指向的路径已消失时，全树深扫（depth 8 +
			// 基名白名单——cache 树深：sha 层 + 仓库子树）。
			//
			// Fallback: registry unreadable / silent about forge / pointing at a
			// vanished path — deep-scan the whole tree (depth 8 + basename whitelist;
			// the cache tree is deep: sha layer + repo subtree).
			return append(targets, scanTarget{
				path:  filepath.Join(home, "plugins", "cache"),
				depth: 8,
				names: hookCarrierNames,
			}), nil
		}},
		{"codex", func() ([]scanTarget, error) {
			p, err := agentbridge.CodexHooksPath()
			return []scanTarget{{path: p}}, err
		}},
		{"cursor", func() ([]scanTarget, error) {
			p, err := agentbridge.CursorHooksPath()
			return []scanTarget{{path: p}}, err
		}},
		{"windsurf", func() ([]scanTarget, error) {
			p, err := agentbridge.WindsurfHooksPath()
			return []scanTarget{{path: p}}, err
		}},
		{"kimi", func() ([]scanTarget, error) {
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
			return []scanTarget{{path: p}, {path: filepath.Join(home, "plugins")}}, nil
		}},
		{"reasonix", func() ([]scanTarget, error) {
			home, err := agentbridge.ReasonixConfigHome()
			if err != nil {
				return nil, err
			}
			return []scanTarget{{path: filepath.Join(home, "plugins")}}, nil
		}},
		{"codebuddy", func() ([]scanTarget, error) {
			home, err := agentbridge.CodeBuddyWorkBuddyHome()
			if err != nil {
				return nil, err
			}
			return []scanTarget{{path: filepath.Join(home, "plugins")}}, nil
		}},
		{"cline", func() ([]scanTarget, error) {
			d, err := agentbridge.ClineHooksDir()
			return []scanTarget{{path: d}}, err
		}},
		{"opencode", func() ([]scanTarget, error) {
			p, err := agentbridge.OpencodePluginPath()
			return []scanTarget{{path: p}}, err
		}},
		{"zcode", func() ([]scanTarget, error) {
			p, err := agentbridge.ZcodeConfigPath()
			return []scanTarget{{path: p}}, err
		}},
	}
}

// liveClaudePluginTargets resolves the LIVE forge plugin install(s) claude-code's own
// registry points at (~/.claude/plugins/installed_plugins.json, schema v2:
// plugins["forge@forge"][].installPath). Mining stays token-oriented like the rest of
// doctor — JSON shapes vary and only the path matters. Tokens qualify when they name
// forge, sit under the cache dir, and actually exist on disk; JSON-escaped doubled
// backslashes are collapsed. Returns nil (→ full-tree fallback) when the registry is
// unreadable or names no existing forge path.
//
// liveClaudePluginTargets 解析 claude-code 自己的注册表（~/.claude/plugins/
// installed_plugins.json，schema v2：plugins["forge@forge"][].installPath）指向的
// 活 forge 插件安装。挖掘保持与 doctor 其余部分同级的 token 导向——JSON 形态多变，
// 需要的只有路径。token 需同时满足：含 forge、位于 cache 目录下、磁盘上确实存在；
// JSON 转义的双反斜杠折叠为单。注册表不可读或未指向任何存在的 forge 路径时返回
// nil（→ 全树回退）。
func liveClaudePluginTargets(home string) []scanTarget {
	data, err := os.ReadFile(filepath.Join(home, "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	cachePrefix := strings.ToLower(filepath.ToSlash(filepath.Join(home, "plugins", "cache")))
	seen := map[string]bool{}
	var out []scanTarget
	for _, ln := range strings.Split(string(data), "\n") {
		if !strings.Contains(strings.ToLower(ln), "forge") {
			continue
		}
		for _, m := range tokenRe.FindAllStringIndex(ln, -1) {
			tok := strings.ReplaceAll(ln[m[0]:m[1]], `\\`, `\`)
			low := strings.ToLower(filepath.ToSlash(tok))
			// forge 性在路径段（cache/forge/forge/<sha>），不在末段（sha）——按整路
			// 径子串判，非 basename。
			//
			// The forge-ness is in path segments (cache/forge/forge/<sha>), not the
			// last segment (the sha) — substring over the whole path, not basename.
			if !strings.Contains(low, "forge") {
				continue
			}
			if !strings.HasPrefix(low, cachePrefix+"/") {
				continue
			}
			if _, err := os.Stat(tok); err != nil {
				continue
			}
			key := low
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, scanTarget{path: tok, depth: 8, names: hookCarrierNames})
		}
	}
	return out
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
	// Skills distribution is an optional section: probe injected by the CLI layer,
	// absent under hermetic tests. A failing probe reports inside the summary —
	// host auditing must survive skillsdist errors.
	//
	// skills 分发是可选节：探针由 CLI 层注入，密封测试下缺席。探针失败在摘要内
	// 上报——host 审计必须能扛住 skillsdist 错误。
	if opts.SkillsDriftProbe != nil {
		if s := opts.SkillsDriftProbe(); s != nil {
			rep.Skills = s
		}
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
	targets, err := spec.target()
	if err != nil {
		r.Err = fmt.Sprintf("路径解析失败: %v", err)
		return r
	}
	var files []string
	for _, tgt := range targets {
		files = append(files, expandHookFiles(tgt)...)
	}
	if len(files) == 0 {
		return r
	}
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
		// HookPath 只在扫到真接线时才赋值（见上）——missing host 不展示 files[0] 当
		// 伪证据（评审二轮 NIT #4）。
		//
		// HookPath is only assigned when real wiring is found (see above) — a missing
		// host never shows files[0] as pseudo-evidence (round-2 review NIT #4).
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

// expandHookFiles turns a candidate target (file or dir) into the concrete file list to
// scan. Dirs are walked with the target's depth cap (default defaultScanDepth); unreadable
// entries are skipped, not fatal. File filtering follows the target's mode: extension
// whitelist by default, basename whitelist when names is set (deep trees).
//
// expandHookFiles 把候选目标（文件或目录）展开成待扫描的具体文件列表。目录按目标的
// 深度上限遍历（默认 defaultScanDepth）；不可读条目跳过、不致命。文件过滤按目标模式：
// 默认扩展名白名单，names 已设时按基名白名单（深树）。
func expandHookFiles(tgt scanTarget) []string {
	p := tgt.path
	fi, err := os.Stat(p)
	if err != nil {
		return nil
	}
	if !fi.IsDir() {
		return []string{p}
	}
	maxDepth := tgt.depth
	if maxDepth == 0 {
		maxDepth = defaultScanDepth
	}
	root := filepath.ToSlash(p)
	var out []string
	_ = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // 跳过不可读条目，不中断整体遍历
		}
		if d.IsDir() {
			// plugins 树深度上限，防病态深树拖慢审计。默认上限必须 ≥4：kimi 的 plugin
			// 载体在 plugins/managed/forge/.kimi-plugin/plugin.json（相对深度 3 的目录 +
			// 其下文件），卡 3 会剪掉 .kimi-plugin/ 整棵——doctor 对 kimi 的头条用例
			// （plugin 接线版本漂移）静默失效（评审 #1 实证复现）。
			//
			// Depth cap for plugin trees, guarding against pathologically deep trees.
			// The default cap MUST be ≥4: kimi's plugin carrier lives at plugins/managed/
			// forge/.kimi-plugin/plugin.json (depth-3 dir + file beneath); capping at 3
			// prunes the whole .kimi-plugin/ tree — silently defeating doctor's headline
			// kimi use case (plugin-wiring version drift), empirically reproduced in
			// review #1.
			if strings.Count(strings.TrimPrefix(filepath.ToSlash(path), root), "/") >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if tgt.names != nil {
			// 深树基名白名单：claude plugin cache 里是完整仓库副本，扩展名过滤会把
			// golden fixture/manifest 等一切 .json 都扫进来。
			//
			// Deep-tree basename whitelist: the claude plugin cache holds full repo
			// copies; an extension filter would sweep in every golden fixture/manifest
			// .json in them.
			if tgt.names[filepath.Base(path)] {
				out = append(out, path)
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
// invoke. Line-oriented scanning is format-agnostic across every host's hook
// schema (nested JSON / flat TOML / shell wrappers) — precision is not needed, the
// report only has to be right about "wired or not" and "which binary". lookPath is
// injected (Options.LookPath) so tests control bare-name resolution.
//
// scanFile 统计引用 forge 的行数并收集其调用的 forge 二进制 token。按行扫描对各
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
		if !strings.Contains(strings.ToLower(ln), "forge") {
			continue
		}
		// 含 forge 的行未必是接线命令：注册表/文档行（kimi installed.json 的
		// "id": "forge"、URL、codebuddy known_marketplaces.json 里 "Forge loop-engineering
		// quality gates: …" 这类 description）只是元数据/文案。各 host 的 forge 接线
		// 命令一律是 `forge hook <event>` 的调用形态（含 --agent 变体；`forge gate <id>`
		// 是 settings 层认可的等价前缀，见 internal/hooks/settings.go isForgeHookCommand 的合法命令判定），
		// 故只认"紧跟在 forge token 后的子命令词位上是 hook/gate"的行——词在任何位置
		// 出现都不算。否则 plugin 接线坏了但注册表还在的机器会假报 ok（评审 #1 的失效
		// 场景），bare 词门槛则被 "quality gates" 文案击穿（本轮 E2E 实证）。
		//
		// A forge-carrying line is not necessarily wiring: registry/doc lines (kimi
		// installed.json's "id": "forge", URLs, and descriptions like "Forge
		// loop-engineering quality gates: …" in codebuddy's known_marketplaces.json) are
		// metadata/prose. Every host's forge wiring is an invocation of the shape
		// `forge hook <event>` (--agent variants included; `forge gate <id>` is the
		// settings layer's accepted equivalent prefix — see the legal-command check in
		// internal/hooks/settings.go), so only lines with hook/gate in the subcommand
		// position right after the forge token count — the word appearing anywhere else
		// doesn't. Otherwise a machine whose plugin hooks broke while the registry stayed
		// intact false-reports ok (review #1's scenario), and a bare-word gate is defeated
		// by "quality gates" prose (empirically hit in this round's E2E).
		if tok, ok := forgeInvocation(ln); ok {
			cmds++
			bin, resolved := resolveBin(tok, lookPath)
			if !seen[bin] {
				seen[bin] = true
				bins = append(bins, binCandidate{path: bin, resolved: resolved})
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

// invocationSubcommands are the subcommand words that turn a forge token into wiring:
// `forge hook <event>` (every host's hook wiring) and `forge gate <id>` (the settings
// layer's accepted equivalent prefix — internal/hooks/settings.go's isForgeHookCommand, mirrored in agentbridge/codex.go).
//
// invocationSubcommands 是让 forge token 构成接线的子命令词：`forge hook <event>`
// （所有 host 的 hook 接线）与 `forge gate <id>`（settings 层认可的等价前缀——见
// internal/hooks/settings.go 的 isForgeHookCommand，agentbridge/codex.go 有镜像）。
var invocationSubcommands = []string{"hook", "gate"}

// forgeInvocation extracts the first forge token that sits in an invocation position —
// a legal subcommand word immediately after it — and reports ok=true. Prose/registry
// lines name forge but never in the subcommand position ("Forge loop-engineering
// quality gates: …", "id": "forge"), so they are rejected here rather than counted.
//
// forgeInvocation 提取行内第一个处于调用位的 forge token——其后紧跟合法子命令词——
// 并返回 ok=true。文案/注册表行会提到 forge 但从不落在子命令位（"Forge
// loop-engineering quality gates: …"、"id": "forge"），在此被拒、不计入接线。
func forgeInvocation(line string) (string, bool) {
	for _, m := range tokenRe.FindAllStringIndex(line, -1) {
		tok := line[m[0]:m[1]]
		if !strings.Contains(strings.ToLower(filepath.Base(tok)), "forge") {
			continue
		}
		if subcommandAt(line, m[1]) {
			return tok, true
		}
	}
	return "", false
}

// subcommandAt reports whether a legal subcommand sits at a word boundary right after
// position end in line. JSON/shell separators (quotes, whitespace, commas, colons,
// equals) between the binary and its argument are skipped; the matched word must also
// END at a boundary so "gateway"/"hooks" prose doesn't count. Known leniency (accepted,
// round-3 review): the separator set admits `,=:` (so `forge=hook` counts) and isWordByte
// excludes `.` (so `forge hook.json` counts) — both contrived for the scanned file
// types; the empirically observed prose class is rejected.
//
// subcommandAt 判定 line 中 end 位之后是否恰好是合法子命令且带词边界。二进制与参数间
// 的 JSON/shell 分隔符（引号、空白、逗号、冒号、等号）跳过；命中的词还必须在尾部也
// 有边界——"gateway"/"hooks" 这类文案不算。已知宽容（接受，评审三轮）：分隔集含 `,=:`
// （`forge=hook` 会计）、isWordByte 不含 `.`（`forge hook.json` 会计）——对所扫文件类型
// 皆为构造形态，实证观察到的文案类已被拒。
func subcommandAt(line string, end int) bool {
	rest := strings.TrimLeft(line[end:], "\"' \t\r\n,=:")
	low := strings.ToLower(rest)
	for _, w := range invocationSubcommands {
		if !strings.HasPrefix(low, w) {
			continue
		}
		tail := low[len(w):]
		if tail == "" || !isWordByte(tail[0]) {
			return true
		}
	}
	return false
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
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
// ("forge version 1.30.0 (commit..)" → "1.30.0"). When no semver is present it strips
// a trailing "… version X" envelope so a bare `forge --version` line still yields just
// the version part ("forge version dev" → "dev"), not the whole sentence (round-2 NIT #3).
//
// normalizeVersion 把任意版本输出归一为 semver 形态 token（"forge version 1.30.0
// (commit..)" → "1.30.0"）。无 semver 时剥掉 "… version X" 外壳，裸 `forge --version`
// 行也只留版本部分（"forge version dev" → "dev"）而非整句（评审二轮 NIT #3）。
func normalizeVersion(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if m := semver.FindString(s); m != "" {
		return m
	}
	if i := strings.LastIndex(s, " version "); i >= 0 {
		s = strings.TrimSpace(s[i+len(" version "):])
	}
	return s
}
