package agentbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadJSON reads path into v, failing the test if the file is missing or not
// valid JSON. Used to assert MCP config files are parseable (not just present).
func loadJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
}

// forgeServerAt navigates cfg[topKey][name] and returns the server def map.
func forgeServerAt(t *testing.T, cfg map[string]any, topKey, name string) map[string]any {
	t.Helper()
	servers, ok := cfg[topKey].(map[string]any)
	if !ok {
		t.Fatalf("topKey %q missing or not an object: %v", topKey, cfg[topKey])
	}
	srv, ok := servers[name].(map[string]any)
	if !ok {
		t.Fatalf("server %q missing under %q", name, topKey)
	}
	return srv
}

func assertStringArgs(t *testing.T, got any, want ...string) {
	t.Helper()
	args, ok := got.([]any)
	if !ok {
		t.Fatalf("args not a JSON array: %T", got)
	}
	if len(args) != len(want) {
		t.Fatalf("args length = %d, want %d (%v)", len(args), len(want), args)
	}
	for i, w := range want {
		if args[i] != w {
			t.Errorf("args[%d] = %v, want %q", i, args[i], w)
		}
	}
}

// TestMCP_ClaudeFormat：.mcp.json 顶层 mcpServers.forge 含 command="forge" +
// args=["mcp","serve"]，且【不带】type 字段（type 是 cursor 的要求，claude 省略）。
func TestMCP_ClaudeFormat(t *testing.T) {
	dir := t.TempDir()
	if err := writeClaudeMCP(dir); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	loadJSON(t, filepath.Join(dir, ".mcp.json"), &cfg)
	srv := forgeServerAt(t, cfg, "mcpServers", "forge")
	if srv["command"] != "forge" {
		t.Errorf("command = %v, want forge", srv["command"])
	}
	assertStringArgs(t, srv["args"], "mcp", "serve")
	if _, has := srv["type"]; has {
		t.Error("claude .mcp.json forge server should NOT carry a type field (cursor does, claude omits)")
	}
}

// TestMCP_CursorFormat：.cursor/mcp.json forge server 带 type="stdio"（cursor 要求，
// 区别于 claude）。
func TestMCP_CursorFormat(t *testing.T) {
	dir := t.TempDir()
	if err := writeCursorMCP(dir); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	loadJSON(t, filepath.Join(dir, ".cursor", "mcp.json"), &cfg)
	srv := forgeServerAt(t, cfg, "mcpServers", "forge")
	if srv["type"] != "stdio" {
		t.Errorf("cursor forge type = %v, want stdio", srv["type"])
	}
	if srv["command"] != "forge" {
		t.Errorf("command = %v, want forge", srv["command"])
	}
	assertStringArgs(t, srv["args"], "mcp", "serve")
}

// TestMCP_OpencodeFormat：opencode.json 顶层是 mcp（非 mcpServers），forge server 的
// command 是【数组】（command+args 合并）且 enabled=true——均与 claude/cursor 不同，
// 写错任一字段 opencode 调不起 forge。
func TestMCP_OpencodeFormat(t *testing.T) {
	dir := t.TempDir()
	if err := writeOpencodeMCP(dir); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	loadJSON(t, filepath.Join(dir, "opencode.json"), &cfg)
	srv := forgeServerAt(t, cfg, "mcp", "forge")
	if srv["type"] != "local" {
		t.Errorf("opencode forge type = %v, want local", srv["type"])
	}
	// opencode command 是数组（合并 command+args），不是 claude 的分开字符串。
	assertStringArgs(t, srv["command"], "forge", "mcp", "serve")
	if srv["enabled"] != true {
		t.Errorf("opencode forge enabled = %v, want true (opencode 需显式启用)", srv["enabled"])
	}
}

// TestMCP_CodexTOML：.codex/config.toml 含 [mcp_servers.forge] 段，command/args 用
// ASCII 双引号。回归守卫 [[windows-input-quote-corruption]]：codexMCPServerTOML 若被
// 改回普通字符串字面量，Edit 腐蚀会把 ASCII 双引号 " 转成中文弯引号 “ ”，TOML 解析失败。
func TestMCP_CodexTOML(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".codex"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexMCP(dir); err != nil {
		t.Fatal(err)
	}
	content := readOrFail(t, filepath.Join(dir, ".codex", "config.toml"))
	if !strings.Contains(content, "[mcp_servers.forge]") {
		t.Error("config.toml missing [mcp_servers.forge] section header")
	}
	// ASCII 双引号（rune 34）构造期望串——不依赖测试源码的双引号是否被腐蚀。
	q := string(rune(34))
	if !strings.Contains(content, "command = "+q+"forge"+q) {
		t.Errorf("config.toml missing ASCII-quoted command line; got:\n%s", content)
	}
	wantArgs := "args = [" + q + "mcp" + q + ", " + q + "serve" + q + "]"
	if !strings.Contains(content, wantArgs) {
		t.Errorf("config.toml missing ASCII-quoted args line; got:\n%s", content)
	}
	// 守卫腐蚀：弯引号 U+201C/U+201D 绝不能出现（转义码点，绕过源码双引号）。
	if strings.ContainsAny(content, "“”") {
		t.Errorf("config.toml contains curly quotes (Windows input corruption); got:\n%s", content)
	}
}

// TestMCP_Idempotent：反复调用不重复添加 forge server（init/sync 反复跑安全）。
// JSON 工具 + codex TOML 各测一次。
func TestMCP_Idempotent(t *testing.T) {
	// 3 个 JSON 工具共享 mergeMCPServerJSON，表驱动各跑两遍断言 forge server 不重复。
	t.Run("json", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			write  func(string) error
			path   string
			topKey string
		}{
			{"claude", writeClaudeMCP, ".mcp.json", "mcpServers"},
			{"cursor", writeCursorMCP, ".cursor/mcp.json", "mcpServers"},
			{"opencode", writeOpencodeMCP, "opencode.json", "mcp"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				dir := t.TempDir()
				for i := 0; i < 2; i++ {
					if err := tc.write(dir); err != nil {
						t.Fatal(err)
					}
				}
				var cfg map[string]any
				loadJSON(t, filepath.Join(dir, tc.path), &cfg)
				servers, ok := cfg[tc.topKey].(map[string]any)
				if !ok {
					t.Fatalf("topKey %q missing after writes", tc.topKey)
				}
				if _, dup := servers["forge"]; !dup {
					t.Fatal("forge server missing after second write")
				}
				if len(servers) != 1 {
					t.Errorf("idempotent write duplicated server: %v", servers)
				}
			})
		}
	})
	t.Run("codex", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, ".codex"), 0755)
		for i := 0; i < 2; i++ {
			if err := writeCodexMCP(dir); err != nil {
				t.Fatal(err)
			}
		}
		content := readOrFail(t, filepath.Join(dir, ".codex", "config.toml"))
		if cnt := strings.Count(content, "[mcp_servers.forge]"); cnt != 1 {
			t.Errorf("codex TOML section duplicated (%d occurrences); rewrite is not idempotent", cnt)
		}
	})
}

// TestMCP_CodexTOML_NotFooledByComment：用户在 TOML 注释里写 `# [mcp_servers.forge]`
// 时，hasTOMLSection 行首匹配不应误判为段已存在（那会让 writeCodexMCP 跳过、漏配 forge
// server）。回归守卫 codex 幂等检查的注释假阳性（hasTOMLSection 用行首匹配而非 Contains）。
func TestMCP_CodexTOML_NotFooledByComment(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".codex"), 0755)
	pre := "# see [mcp_servers.forge] docs\n# just a comment, not a real section\n[model]\nname = \"gpt\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".codex", "config.toml"), []byte(pre), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexMCP(dir); err != nil {
		t.Fatal(err)
	}
	content := readOrFail(t, filepath.Join(dir, ".codex", "config.toml"))
	// 真段特征：\n[mcp_servers.forge]\ncommand（注释里 [mcp_servers.forge] 后跟 ]\n#，
	// 非 \ncommand）。注释不该让幂等误跳过——真段必须被添加。
	if !strings.Contains(content, "\n[mcp_servers.forge]\ncommand") {
		t.Errorf("注释假阳性：真段未被添加（hasTOMLSection 行首匹配失效？）\n%s", content)
	}
	if !strings.Contains(content, "[model]") {
		t.Error("原有 [model] 段丢失")
	}
}

// TestMCP_MergesExisting：用户已有其他 server / 其他顶层字段时，merge 保留它们，只加 forge。
func TestMCP_MergesExisting(t *testing.T) {
	dir := t.TempDir()
	// 用户已有 github server + 无关的顶层 enabled 字段。
	pre := `{"mcpServers":{"github":{"command":"gh","args":[]}},"version":1}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(pre), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeClaudeMCP(dir); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	loadJSON(t, filepath.Join(dir, ".mcp.json"), &cfg)
	if cfg["version"] != float64(1) {
		t.Errorf("existing top-level field 'version' not preserved: %v", cfg["version"])
	}
	servers := cfg["mcpServers"].(map[string]any)
	if _, ok := servers["github"]; !ok {
		t.Error("existing github server dropped by merge")
	}
	if _, ok := servers["forge"]; !ok {
		t.Error("forge server not added")
	}
}

// TestMCP_PreservesUserForgeConfig：若用户已手写 forge server（如自定义路径/环境），
// merge 不得覆盖——保留用户配置。
func TestMCP_PreservesUserForgeConfig(t *testing.T) {
	dir := t.TempDir()
	pre := `{"mcpServers":{"forge":{"command":"/custom/forge","args":["--debug"]}}}`
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(pre), 0644)
	if err := writeClaudeMCP(dir); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	loadJSON(t, filepath.Join(dir, ".mcp.json"), &cfg)
	srv := forgeServerAt(t, cfg, "mcpServers", "forge")
	if srv["command"] != "/custom/forge" {
		t.Errorf("user's custom forge command overwritten: %v", srv["command"])
	}
}

// TestMCP_RejectsCorruptJSON：现有配置非合法 JSON 时返回 error 且【不覆盖】文件
// （保护用户文件，而非默默吞掉损坏重写成 forge-only）。
func TestMCP_RejectsCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	os.WriteFile(path, []byte("{not valid json"), 0644)
	err := writeClaudeMCP(dir)
	if err == nil {
		t.Fatal("corrupt JSON should return an error, not silently overwrite")
	}
	// 文件必须原样保留（未被覆盖成 forge-only）。
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "not valid json") {
		t.Error("corrupt JSON file was overwritten (user data lost)")
	}
}

// TestTranslatorsEmitMCP：各 translator 的 Translate 末尾必须生成对应 MCP 配置。
// 这是接入点守卫——若有人删掉 Translate 里的 write*MCP 调用，agent 就拿不到 forge MCP 工具。
func TestTranslatorsEmitMCP(t *testing.T) {
	for _, tc := range []struct {
		name      string
		translate func(string) error
		mcpFile   string
		// want 存在于文件内容（JSON 用 server 名，TOML 用段头）
		want string
	}{
		{"claude", func(d string) error { return (&ClaudeCodeTranslator{}).Translate(d, testInput()) },
			".mcp.json", `"forge"`},
		{"cursor", func(d string) error { return (&CursorTranslator{}).Translate(d, testInput()) },
			".cursor/mcp.json", `"forge"`},
		{"opencode", func(d string) error { return (&OpencodeTranslator{}).Translate(d, testInput()) },
			"opencode.json", `"forge"`},
		{"codex", func(d string) error { return (&CodexTranslator{}).Translate(d, testInput()) },
			".codex/config.toml", "[mcp_servers.forge]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := tc.translate(dir); err != nil {
				t.Fatalf("%s Translate: %v", tc.name, err)
			}
			content := readOrFail(t, filepath.Join(dir, tc.mcpFile))
			if !strings.Contains(content, tc.want) {
				t.Errorf("%s: MCP config %s missing %q (Translate not wiring MCP?)\n%s",
					tc.name, tc.mcpFile, tc.want, content)
			}
		})
	}
}
