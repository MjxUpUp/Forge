package doctor

import (
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

// TestForgeToken 各 token 形态：裸名/带路径/带引号/JSON 值内嵌命令。
func TestForgeToken(t *testing.T) {
	cases := []struct {
		line string
		want string
		ok   bool
	}{
		{`"command": "forge hook stop"`, `forge`, true},
		{`forge.exe hook pre`, `forge.exe`, true},
		{`"C:\\tools\\forge.exe" hook`, `C:\\tools\\forge.exe`, true},
		{`command = 'forge hook'`, `forge`, true},
		// 说明性文本也命中 token——宁多勿漏的既定取舍（见 TestSanitizeCommand_NonCommand）
		{`echo forge is great`, `forge`, true},
	}
	for _, c := range cases {
		tok, ok := forgeToken(c.line)
		if ok != c.ok {
			t.Fatalf("forgeToken(%q) ok=%v want=%v", c.line, ok, c.ok)
		}
		if c.ok && tok != c.want {
			t.Fatalf("forgeToken(%q) = %q, want %q", c.line, tok, c.want)
		}
	}
}

// TestNormalizeVersion 版本归一：带前缀/commit 后缀/裸 semver/无 semver 回退原文。
func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"forge version 1.30.0 (commit: abc)", "1.30.0"},
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

// TestSanitizeCommand_NonCommand 说明性文本（如 "echo forge is great" 这类 forge 出现
// 但无命令语义的行）仍会被 forgeToken 命中——这是已知取舍：按行扫描是格式无关的
// 近似，宁可多报一条 ForgeCmds 也不漏报接线。该测试把这一取舍钉住，防止未来无意识
// 改变行为。
func TestSanitizeCommand_NonCommand(t *testing.T) {
	if _, ok := forgeToken(`echo forge is great`); !ok {
		t.Fatal("forge 出现即命中是既定取舍（宁多勿漏），行为不应漂移")
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
	if !strings.Contains(bins[0], "forge") {
		t.Fatalf("bin 应含 forge，got %q", bins[0])
	}
}
