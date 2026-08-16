package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate redirects every agentbridge path helper into a temp root via env overrides,
// so tests never touch the real user-level agent configs (the same isolation strategy
// agentbridge's own tests use). Returns the temp root.
//
// isolate 经 env 覆盖把所有 agentbridge 路径 helper 重定向进临时根，测试永不触碰
// 真实用户级 agent 配置（与 agentbridge 自身测试同一隔离策略）。返回临时根。
func isolate(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("USERPROFILE", root)
	t.Setenv("HOME", root) // os.UserHomeDir 在 Windows 读 USERPROFILE，双保险
	t.Setenv("APPDATA", filepath.Join(root, "AppData", "Roaming"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, ".codex"))
	t.Setenv("KIMI_CODE_HOME", filepath.Join(root, ".kimi-code"))
	t.Setenv("REASONIX_HOME", filepath.Join(root, "AppData", "Roaming", "reasonix"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("WORKBUDDY_CONFIG_DIR", filepath.Join(root, ".workbuddy"))
	// 隔离 PATH：host resolveBin 的 LookPath 兜底不会命中真实 forge。
	t.Setenv("PATH", filepath.Join(root, "bin"))
	return root
}

// fakeEnv 把 VersionRunner/LookPath 固定成可控假实现：版本表按路径精确匹配，其余
// 一律 "9.9.9"；LookPath 只认 "forge" → /fake/forge.exe。
func fakeEnv(versionByPath map[string]string) Options {
	return Options{
		VersionRunner: func(bin string) (string, error) {
			if v, ok := versionByPath[bin]; ok {
				return v, nil
			}
			return "forge version 9.9.9 (commit: x)", nil
		},
		LookPath: func(name string) (string, error) {
			if name == "forge" {
				return `/fake/forge.exe`, nil
			}
			return "", &os.PathError{Op: "lookpath", Path: name}
		},
		ScanPATH: func() []string { return nil },
	}
}

// hostOf 从 Report 中取指定 host 的报告；不存在时 fail。
func hostOf(t *testing.T, rep Report, host string) HostReport {
	t.Helper()
	for _, h := range rep.Hosts {
		if h.Host == host {
			return h
		}
	}
	t.Fatalf("报告中没有 host %q", host)
	return HostReport{}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRun_HostMissing 未接线 host：配置文件不存在 → status=missing，无 Err。
func TestRun_HostMissing(t *testing.T) {
	root := isolate(t)
	rep := Run("1.30.0", fakeEnv(nil))
	h := hostOf(t, rep, "claude-code")
	if h.Status != StatusMissing {
		t.Fatalf("无 settings.json 时应为 missing，got %q (root=%s)", h.Status, root)
	}
	if h.Err != "" {
		t.Fatalf("missing 不应有 Err，got %q", h.Err)
	}
	// 评审二轮 NIT #4：missing 不展示候选首文件当伪证据。
	//
	// Round-2 review NIT #4: missing hosts never show the first candidate file as
	// pseudo-evidence.
	if h.HookPath != "" {
		t.Fatalf("missing 时 HookPath 应为空，got %q", h.HookPath)
	}
}

// TestRun_HostOK 已接线且版本一致：settings.json 含 forge hook 命令、hook 引用的
// forge 解析到 /fake/forge.exe（版本 1.30.0 与 self 一致）→ status=ok。
func TestRun_HostOK(t *testing.T) {
	root := isolate(t)
	writeFile(t, filepath.Join(root, ".claude", "settings.json"),
		`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"forge hook pre-tool-use"}]}]}}`)
	rep := Run("1.30.0", fakeEnv(map[string]string{`/fake/forge.exe`: "1.30.0"}))
	h := hostOf(t, rep, "claude-code")
	if h.Status != StatusOK {
		t.Fatalf("版本一致应为 ok，got %q (bin=%q ver=%q)", h.Status, h.Bin, h.Version)
	}
	if h.ForgeCmds != 1 {
		t.Fatalf("ForgeCmds 应为 1，got %d", h.ForgeCmds)
	}
}

// TestRun_HostDrift 已接线但版本不一致（kimi 停旧版事故的形状）→ status=drift。
func TestRun_HostDrift(t *testing.T) {
	root := isolate(t)
	writeFile(t, filepath.Join(root, ".claude", "settings.json"),
		`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"forge hook stop"}]}]}}`)
	rep := Run("1.30.0", fakeEnv(map[string]string{`/fake/forge.exe`: "forge version 1.28.4 (commit: old)"}))
	h := hostOf(t, rep, "claude-code")
	if h.Status != StatusDrift {
		t.Fatalf("版本 1.28.4 vs self 1.30.0 应为 drift，got %q (ver=%q)", h.Status, h.Version)
	}
	if h.Version != "1.28.4" {
		t.Fatalf("版本应归一为 1.28.4，got %q", h.Version)
	}
}

// TestRun_HostNoVer hook 引用了 forge 但二进制解析不到（VersionRunner 报错）→
// status=nover，不误报 drift 也不误报 ok。
func TestRun_HostNoVer(t *testing.T) {
	root := isolate(t)
	writeFile(t, filepath.Join(root, ".claude", "settings.json"),
		`{"command": "forge hook pre-tool-use"}`)
	opts := fakeEnv(nil)
	opts.VersionRunner = func(bin string) (string, error) {
		return "", &os.PathError{Op: "exec", Path: bin}
	}
	rep := Run("1.30.0", opts)
	h := hostOf(t, rep, "claude-code")
	if h.Status != StatusNoVer {
		t.Fatalf("版本探测失败应为 nover，got %q", h.Status)
	}
}

// TestRun_SelfVersionDev self 为 dev 构建（无 semver）时不做 drift 判定 → 保持 nover
// 而非误判。dev 构建无版本可比对，drift 判定必须跳过。
func TestRun_SelfVersionDev(t *testing.T) {
	root := isolate(t)
	writeFile(t, filepath.Join(root, ".claude", "settings.json"),
		`{"command": "forge hook pre-tool-use"}`)
	rep := Run("dev", fakeEnv(map[string]string{`/fake/forge.exe`: "1.30.0"}))
	h := hostOf(t, rep, "claude-code")
	if h.Status != StatusNoVer {
		t.Fatalf("self=dev 不应判 drift/ok，got %q", h.Status)
	}
}

// TestRun_PluginsDirHost reasonix/codebuddy 的 plugins 目录树：hook 文件在深层子目录
// 也能被扫到并计数。
func TestRun_PluginsDirHost(t *testing.T) {
	root := isolate(t)
	writeFile(t, filepath.Join(root, ".workbuddy", "plugins", "forge", "hooks", "hooks.json"),
		`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"forge hook --agent codebuddy pre-tool-use"}]}]}}`)
	rep := Run("1.30.0", fakeEnv(map[string]string{`/fake/forge.exe`: "1.30.0"}))
	h := hostOf(t, rep, "codebuddy")
	if h.Status != StatusOK {
		t.Fatalf("plugins 树内 hook 应为 ok，got %q (ForgeCmds=%d hookPath=%q)", h.Status, h.ForgeCmds, h.HookPath)
	}
}

// TestRun_NonForgeFile 文件存在但不含 forge 命令 → missing（接线了别的 hook，不关
// doctor 的事）。
func TestRun_NonForgeFile(t *testing.T) {
	root := isolate(t)
	writeFile(t, filepath.Join(root, ".claude", "settings.json"),
		`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"other-tool guard"}]}]}}`)
	rep := Run("1.30.0", fakeEnv(nil))
	h := hostOf(t, rep, "claude-code")
	if h.Status != StatusMissing {
		t.Fatalf("非 forge hook 应为 missing，got %q", h.Status)
	}
	if h.HookPath != "" {
		t.Fatalf("非 forge hook 时 HookPath 应为空（无伪证据），got %q", h.HookPath)
	}
}

// TestRun_NineHosts 报告覆盖 9 个 host（copilot 刻意不在列——其 VS Code 扩展配置
// 无稳定文件路径约定，见 CLI 注释）。
func TestRun_NineHosts(t *testing.T) {
	isolate(t)
	rep := Run("1.30.0", fakeEnv(nil))
	if len(rep.Hosts) != 9 {
		t.Fatalf("应有 9 个 host，got %d: %v", len(rep.Hosts), rep.Hosts)
	}
}

// TestScanPATH_MultipleForges PATH 上多个 forge 可执行文件按 PATH 顺序全部列出、
// 版本探测上限 5 个——游离 exe/shim 并存（PATHEXT 事故形状）一眼可见。
func TestScanPATH_MultipleForges(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	writeFile(t, filepath.Join(a, "forge.exe"), "binary")
	writeFile(t, filepath.Join(b, "forge.exe"), "binary")
	writeFile(t, filepath.Join(b, "forge.bat"), "@echo off")
	opts := fakeEnv(map[string]string{
		filepath.Join(a, "forge.exe"): "1.29.0",
		filepath.Join(b, "forge.exe"): "1.30.0",
		filepath.Join(b, "forge.bat"): "1.30.0",
	})
	opts.ScanPATH = func() []string { return []string{a, b} }
	rep := Run("1.30.0", opts)
	if len(rep.PathForge) != 3 {
		t.Fatalf("PATH 上应找到 3 个 forge 可执行文件，got %d: %v", len(rep.PathForge), rep.PathForge)
	}
	if rep.PathForge[0].Path != filepath.Join(a, "forge.exe") {
		t.Fatalf("应按 PATH 顺序，首个为 a/forge.exe，got %q", rep.PathForge[0].Path)
	}
	if rep.PathForge[0].Version != "1.29.0" || rep.PathForge[1].Version != "1.30.0" {
		t.Fatalf("版本探测错误: %+v", rep.PathForge)
	}
	if rep.Resolved != `/fake/forge.exe` {
		t.Fatalf("Resolved 应为 LookPath 结果，got %q", rep.Resolved)
	}
}

// TestForgeInvocation 各调用形态与拒绝形态：裸名/带路径/带引号/JSON 值内嵌命令命中；
// 文案/注册表元数据（子命令位无 hook/gate）拒绝。
func TestForgeInvocation(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
		ok   bool
	}{
		{("json命令"), `"command": "forge hook stop"`, `forge`, true},
		{("exe裸名"), `forge.exe hook pre`, `forge.exe`, true},
		{("带盘符路径"), `"C:\\tools\\forge.exe" hook`, `C:\\tools\\forge.exe`, true},
		// JSON 转义形态（生产 settings.json/插件清单的真实字节）：token 吸收转义引号的
		// 反斜杠成尾缀，Base 剥掉后仍认 forge——钉住该行为，防 tokenRe 改动静默丢合法调用。
		//
		// JSON-escaped shape (the real bytes in production settings.json/plugin
		// manifests): the token absorbs the escape-quote backslash as a tail; Base
		// strips it and forge still matches — pinned so tokenRe tweaks can't silently
		// drop legal invocations (round-3 review test gap A).
		{("json转义引号路径"), `{"command":"\"C:\\tools\\forge.exe\" hook stop"}`, `C:\\tools\\forge.exe\`, true},
		{("toml赋值"), `command = 'forge hook'`, `forge`, true},
		{("gate等价前缀"), `forge gate review-pass`, `forge`, true},
		{("agent变体"), `forge hook --agent kimi pre-tool-use`, `forge`, true},
		{("minified引号紧邻"), `"command":"forge hook stop"`, `forge`, true},
		// 拒绝形态：forge 出现但子命令位不是 hook/gate
		{("说明性文本"), `echo forge is great`, ``, false},
		{("注册表id"), `"id": "forge", "version": "1.30.0"`, ``, false},
		{("描述文案带gates"), `"description": "Forge loop-engineering quality gates: task-tracked"`, ``, false},
		{("hooks复数不算"), `"description": "Forge hooks for quality"`, ``, false},
		{("gateway词不算"), `forge gateway config`, ``, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tok, ok := forgeInvocation(c.line)
			if ok != c.ok {
				t.Fatalf("forgeInvocation(%q) ok=%v want=%v", c.line, ok, c.ok)
			}
			if c.ok && tok != c.want {
				t.Fatalf("forgeInvocation(%q) = %q, want %q", c.line, tok, c.want)
			}
		})
	}
}

// TestNormalizeVersion 版本归一：带前缀/commit 后缀/裸 semver/无 semver 回退原文。
func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"forge version 1.30.0 (commit: abc)", "1.30.0"},
		{"forge version dev (built 2026-08-16)", "dev (built 2026-08-16)"},
		{"1.28.4", "1.28.4"},
		{"v1.30.0-beta.1", "1.30.0-beta.1"},
		{"dev", "dev"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Fatalf("normalizeVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestSanitizeCommand_NonCommand 说明性文本（如 "echo forge is great"、codebuddy
// known_marketplaces.json 的 "quality gates" description——forge 出现但无调用语义）
// 在调用位判定层被拒：不构成接线。该测试钉住防误报不变量，防止退回 bare 词门槛
// （"hook"/"gate" 词在任何位置出现即计）——bare 门槛已被 "quality gates" 文案实证击穿。
//
// TestSanitizeCommand_NonCommand pins the false-positive guard: prose (like "echo forge
// is great", or codebuddy known_marketplaces.json's "quality gates" description — forge
// present, no invocation semantics) is rejected at the invocation-position layer. This
// prevents regression to a bare-word gate ("hook"/"gate" counted anywhere on the line)
// — empirically defeated by "quality gates" prose.
func TestSanitizeCommand_NonCommand(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "known_marketplaces.json")
	writeFile(t, p, "{\"name\":\"Forge\",\"description\":\"Forge loop-engineering quality gates: task-tracked source changes, and review-gated completion.\"}")
	cmds, bins := scanFile(p, nil)
	if cmds != 0 || len(bins) != 0 {
		t.Fatalf("描述文案不应计为接线，got cmds=%d bins=%v", cmds, bins)
	}
}

// TestScanFile_QuotedJSON settings.json 的真实形态：JSON 转义路径 + 引号包裹命令。
func TestScanFile_QuotedJSON(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "hooks.json")
	writeFile(t, p, `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"\"C:\\forge\\forge.exe\" hook session-start"}]}]}}`)
	cmds, bins := scanFile(p, nil)
	if cmds != 1 {
		t.Fatalf("ForgeCmds 应为 1，got %d", cmds)
	}
	if len(bins) != 1 {
		t.Fatalf("应收集到 1 个 bin，got %v", bins)
	}
	if !strings.Contains(bins[0].path, "forge") {
		t.Fatalf("bin 应含 forge，got %q", bins[0].path)
	}
}

// TestRun_KimiPluginLayout 钉 kimi plugin 载体的真实目录深度（评审 #1 的回归守卫）：
// hook 在 plugins/managed/forge/.kimi-plugin/plugin.json（深度 3 目录 + 文件）。WalkDir
// 深度上限若卡 3，.kimi-plugin/ 整棵被剪、plugin.json 永远扫不到——kimi 接线误报
// missing。config.toml 无 [[hooks]]（plugin 模型机器的真实形态）。
//
// TestRun_KimiPluginLayout pins kimi's plugin carrier at its REAL directory depth
// (regression guard for review #1): hooks live at plugins/managed/forge/.kimi-plugin/
// plugin.json (depth-3 dir + file). With the WalkDir cap at 3 the whole .kimi-plugin/
// tree is pruned, plugin.json is never scanned — kimi wiring misreported as missing.
// config.toml has no [[hooks]] (the real shape of a plugin-model machine).
func TestRun_KimiPluginLayout(t *testing.T) {
	root := isolate(t)
	writeFile(t, filepath.Join(root, ".kimi-code", "config.toml"), "# no hooks section\n")
	writeFile(t, filepath.Join(root, ".kimi-code", "plugins", "managed", "forge", ".kimi-plugin", "plugin.json"),
		`{"name":"forge","hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"forge hook --agent kimi pre-tool-use"}]}]}}`)
	rep := Run("1.30.0", fakeEnv(map[string]string{`/fake/forge.exe`: "1.30.0"}))
	h := hostOf(t, rep, "kimi")
	if h.Status != StatusOK {
		t.Fatalf("kimi plugin 载体应被扫到且为 ok，got %q (ForgeCmds=%d hookPath=%q)", h.Status, h.ForgeCmds, h.HookPath)
	}
	if !strings.Contains(filepath.ToSlash(h.HookPath), "plugins/managed/forge/.kimi-plugin/plugin.json") {
		t.Fatalf("HookPath 应指向 plugin.json（证据源归位），got %q", h.HookPath)
	}
}

// TestScanPATH_ProbeCap 版本探测上限（评审 #6）：7 个 forge.exe 只探测前 5 个。
//
// TestScanPATH_ProbeCap version-probe cap (review #6): 7 forge.exe files, only the
// first 5 get probed.
func TestScanPATH_ProbeCap(t *testing.T) {
	isolate(t)
	var dirs []string
	versions := map[string]string{}
	for i := 0; i < 7; i++ {
		d := filepath.Join(t.TempDir(), fmt.Sprintf("d%d", i))
		writeFile(t, filepath.Join(d, "forge.exe"), "binary")
		dirs = append(dirs, d)
		versions[filepath.Join(d, "forge.exe")] = "1.30.0"
	}
	calls := 0
	opts := fakeEnv(versions)
	opts.VersionRunner = func(bin string) (string, error) {
		calls++
		return "1.30.0", nil
	}
	opts.ScanPATH = func() []string { return dirs }
	rep := Run("1.30.0", opts)
	if len(rep.PathForge) != 7 {
		t.Fatalf("应列出全部 7 个条目，got %d", len(rep.PathForge))
	}
	if calls > maxVersionProbes {
		t.Fatalf("版本探测应 ≤%d 次，got %d", maxVersionProbes, calls)
	}
}

// TestAuditHost_UnresolvedTokenNeverProbed 评审 #2 守卫：文档衍生的垃圾 token
// （Stat/LookPath 都确认不了）原样展示但绝不执行。
//
// TestAuditHost_UnresolvedTokenNeverProbed review #2 guard: garbage tokens mined out
// of docs (confirmed by neither Stat nor LookPath) are displayed verbatim but never
// executed.
func TestAuditHost_UnresolvedTokenNeverProbed(t *testing.T) {
	root := isolate(t)
	writeFile(t, filepath.Join(root, ".claude", "settings.json"),
		`{"command": "/.claude/plugins/cache/forge/forge/ hook doc-path"}`)
	probed := map[string]bool{}
	opts := fakeEnv(nil)
	opts.VersionRunner = func(bin string) (string, error) {
		probed[bin] = true
		return "", fmt.Errorf("no such binary")
	}
	rep := Run("1.30.0", opts)
	h := hostOf(t, rep, "claude-code")
	if h.Status != StatusNoVer {
		t.Fatalf("不可解析 token 应为 nover，got %q (bin=%q)", h.Status, h.Bin)
	}
	if len(probed) != 0 {
		t.Fatalf("无任何可解析 token 时不应执行版本探测，却探测了 %v", probed)
	}
}

// TestRun_ClaudePluginCache_LiveRegistryPreferred 评审三轮 MEDIUM-1 守卫：cache 按 sha
// 累积历史副本（本机 13 个），全树字典序扫描会引用字典序第一个死副本当 HookPath、
// ForgeCmds 按副本数膨胀。installed_plugins.json 指向活副本时必须只认活副本——字典序
// 在后的 zzz-live 被点名，aaaa-stale 虽排前且同样带接线也不得贡献计数。
//
// TestRun_ClaudePluginCache_LiveRegistryPreferred round-3 review MEDIUM-1 guard: the
// cache accumulates one copy per historical sha (13 on this machine); a lexical
// full-tree scan cites whichever stale copy sorts first as HookPath and multiplies
// ForgeCmds by copy count. When installed_plugins.json names the live copy, only that
// copy counts — lexically-later zzz-live is named; aaaa-stale sorts first and carries
// wiring too, yet must not contribute.
func TestRun_ClaudePluginCache_LiveRegistryPreferred(t *testing.T) {
	root := isolate(t)
	writeFile(t, filepath.Join(root, ".claude", "settings.json"), `{"enabledPlugins":{"forge@forge":true}}`)
	cache := filepath.Join(root, ".claude", "plugins", "cache", "forge", "forge")
	stale := filepath.Join(cache, "aaaa-stale")
	live := filepath.Join(cache, "zzzz-live")
	for _, d := range []string{stale, live} {
		writeFile(t, filepath.Join(d, ".claude-plugin", "plugin.json"),
			`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"forge hook --agent claude-code pre-tool-use"}]}]}}`)
	}
	// installPath 原生 %q 写入（Windows 上即 JSON 双反斜杠字节）——钉住 liveClaudePluginTargets
	// 的 \\ 折叠 + ToSlash + 前缀检查链条在真实字节形状下工作；用 ToSlash 会让夹具永远是
	// 正斜杠、折叠逻辑只在手动 E2E 里被执行（评审四轮 LOW，shape-contract 教训）。
	//
	// installPath written natively via %q (on Windows that's the JSON doubled-backslash
	// bytes) — pins the \\-fold + ToSlash + prefix-check chain in liveClaudePluginTargets
	// against the REAL byte shape; a ToSlash fixture is always forward slashes, leaving
	// the fold exercised only by manual E2E (round-4 review LOW, shape-contract lesson).
	writeFile(t, filepath.Join(root, ".claude", "plugins", "installed_plugins.json"),
		fmt.Sprintf(`{"version":2,"plugins":{"forge@forge":[{"scope":"user","installPath":%q}]}}`,
			live))
	rep := Run("1.30.0", fakeEnv(map[string]string{`/fake/forge.exe`: "1.30.0"}))
	h := hostOf(t, rep, "claude-code")
	if h.Status != StatusOK {
		t.Fatalf("活副本应被扫到且为 ok，got %q (ForgeCmds=%d hookPath=%q)", h.Status, h.ForgeCmds, h.HookPath)
	}
	if h.ForgeCmds != 1 {
		t.Fatalf("只应计活副本一份（stale 不参与），ForgeCmds 应为 1，got %d", h.ForgeCmds)
	}
	if !strings.HasSuffix(filepath.ToSlash(h.HookPath), "zzzz-live/.claude-plugin/plugin.json") {
		t.Fatalf("HookPath 应指向注册表点名的活副本，got %q", h.HookPath)
	}
}

// TestRun_ClaudePluginCache_RegistryVanished 回退路径：注册表指向的路径已不存在 →
// 回退全树深扫，接线仍可见（stale 副本总比瞎报 missing 好）。
//
// TestRun_ClaudePluginCache_RegistryVanished fallback: the registry names a vanished
// path → fall back to the full-tree deep scan; wiring is still visible (a stale copy
// beats a blind missing report).
func TestRun_ClaudePluginCache_RegistryVanished(t *testing.T) {
	root := isolate(t)
	writeFile(t, filepath.Join(root, ".claude", "settings.json"), `{"enabledPlugins":{"forge@forge":true}}`)
	cache := filepath.Join(root, ".claude", "plugins", "cache", "forge", "forge")
	writeFile(t, filepath.Join(cache, "abc123def", ".claude-plugin", "plugin.json"),
		`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"forge hook --agent claude-code pre-tool-use"}]}]}}`)
	// 同上：原生 %q，注册表字节形状与生产一致（评审四轮 LOW）。
	//
	// Same as above: native %q, registry byte shape matches production (round-4 LOW).
	writeFile(t, filepath.Join(root, ".claude", "plugins", "installed_plugins.json"),
		fmt.Sprintf(`{"version":2,"plugins":{"forge@forge":[{"scope":"user","installPath":%q}]}}`,
			filepath.Join(cache, "deleted0sha")))
	rep := Run("1.30.0", fakeEnv(map[string]string{`/fake/forge.exe`: "1.30.0"}))
	h := hostOf(t, rep, "claude-code")
	if h.Status != StatusOK {
		t.Fatalf("注册表失效应回退全树扫描并报 ok，got %q (ForgeCmds=%d)", h.Status, h.ForgeCmds)
	}
	if !strings.HasSuffix(filepath.ToSlash(h.HookPath), "abc123def/.claude-plugin/plugin.json") {
		t.Fatalf("回退扫描应找到树内接线，got %q", h.HookPath)
	}
}

// TestRun_ClaudePluginCacheLayout 评审二轮 MEDIUM 的回归守卫（现钉回退路径）：plugin pack + autotakeover
// 后的 claude-code 机器上 settings.json 被 dedupe 清干净，hook 全在 plugins/cache/forge/
// forge/<sha>/ 深树里。doctor 必须经深扫（depth 8 + 基名白名单）到达 plugin.json——只扫
// settings.json 会把这类机器误报 missing。树里再放一个同深的 golden .json 钉住白名单：
// 扩展名过滤会把它扫进来，基名过滤必须跳过。
//
// TestRun_ClaudePluginCacheLayout regression guard for round-2 review MEDIUM: on a
// claude-code machine after plugin pack + autotakeover, settings.json is deduped clean
// and hooks live in the deep plugins/cache/forge/forge/<sha>/ tree. doctor must reach
// plugin.json via the deep scan (depth 8 + basename whitelist) — settings.json-only
// misreports such machines as missing. A same-depth golden .json pins the whitelist:
// an extension filter would sweep it in, the basename filter must skip it.
func TestRun_ClaudePluginCacheLayout(t *testing.T) {
	root := isolate(t)
	writeFile(t, filepath.Join(root, ".claude", "settings.json"), `{"enabledPlugins":{"forge@forge":true}}`)
	sha := filepath.Join(root, ".claude", "plugins", "cache", "forge", "forge", "abc123def")
	writeFile(t, filepath.Join(sha, ".claude-plugin", "plugin.json"),
		`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"forge hook --agent claude-code pre-tool-use"}]}]}}`)
	writeFile(t, filepath.Join(sha, "internal", "testdata", "golden.json"),
		`{"hint":"forge hook fake","bin":"/does/not/exist/forge"}`)
	rep := Run("1.30.0", fakeEnv(map[string]string{`/fake/forge.exe`: "1.30.0"}))
	h := hostOf(t, rep, "claude-code")
	if h.Status != StatusOK {
		t.Fatalf("plugin cache 载体应被扫到且为 ok，got %q (ForgeCmds=%d hookPath=%q)", h.Status, h.ForgeCmds, h.HookPath)
	}
	if h.ForgeCmds != 1 {
		t.Fatalf("golden.json 应被基名白名单跳过，ForgeCmds 应为 1，got %d", h.ForgeCmds)
	}
	if !strings.HasSuffix(filepath.ToSlash(h.HookPath), "plugins/cache/forge/forge/abc123def/.claude-plugin/plugin.json") {
		t.Fatalf("HookPath 应指向 cache 内 plugin.json，got %q", h.HookPath)
	}
}

// TestScanFile_GatePrefix `forge gate <id>` 是 settings 层认可的等价前缀（internal/cli
// settings.go 的合法命令判定），接线门槛必须同样接受——否则仅用 gate 接线的 host 假报
// missing（评审二轮 LOW #1）。
//
// TestScanFile_GatePrefix `forge gate <id>` is an accepted equivalent prefix at the
// settings layer (isForgeHookCommand in internal/hooks/settings.go (mirrored in agentbridge/codex.go)); the wiring gate
// must accept it too — otherwise a host wired only via gate false-reports missing
// (round-2 review LOW #1).
func TestScanFile_GatePrefix(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "hooks.json")
	writeFile(t, p, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"forge gate review-pass"}]}]}}`)
	cmds, bins := scanFile(p, nil)
	if cmds != 1 {
		t.Fatalf("forge gate 行应计为接线，ForgeCmds 应为 1，got %d", cmds)
	}
	if len(bins) != 1 || bins[0].path != "forge" {
		t.Fatalf("应解析出 forge 裸名 bin，got %v", bins)
	}
}

// TestRun_RegistryMetadataNotWiring 评审 #1 失效场景守卫：plugin 注册表条目
// （"id": "forge"、repo URL——字面含 forge 但无 hook 命令语义）不构成接线。插件
// hook 坏了但注册表完好的机器必须报 missing/nover，绝不因注册表元数据假报 ok。
//
// TestRun_RegistryMetadataNotWiring guard for review #1's failure scenario: plugin
// registry entries ("id": "forge", repo URLs — literally containing forge but with no
// hook-command semantics) do NOT constitute wiring. A machine whose plugin hooks broke
// while the registry stayed intact must report missing/nover, never a registry-
// metadata-induced ok.
func TestRun_RegistryMetadataNotWiring(t *testing.T) {
	root := isolate(t)
	writeFile(t, filepath.Join(root, ".kimi-code", "plugins", "installed.json"),
		`{"plugins":[{"id":"forge","source":"github.com/MjxUpUp/Forge","version":"1.30.0"}]}`)
	rep := Run("1.30.0", fakeEnv(map[string]string{`/fake/forge.exe`: "1.30.0"}))
	h := hostOf(t, rep, "kimi")
	if h.Status != StatusMissing {
		t.Fatalf("纯注册表元数据不构成接线，应为 missing，got %q (cmds=%d)", h.Status, h.ForgeCmds)
	}
}
