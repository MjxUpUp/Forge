package agentbridge

// MCP 配置生成：让 forge MCP server（forge mcp serve，15 工具：接续 resume/decide/attach
// + 任务/看板/经验）对各 agent 可用。agent 通过 MCP 工具结构化调用 forge 能力，而非靠
// 用户敲 CLI——loop engineering「让 agent 自动调用验证+状态+学习」的接入层。
//
// 各工具 MCP 配置格式差异大（顶层 key、command 形状、文件类型都不同），故每个 translator
// 在 Translate 末尾调本工具的 write* 函数，而非统一层。共享的是 forge server 定义
// （forgeMCPCommand/forgeMCPArgs）与 JSON merge 逻辑（mergeMCPServerJSON）。
//
// 幂等：所有 write* 函数检查 forge server 是否已存在，已存在则跳过（保留用户其他 server），
// 所以 forge init/sync 反复调用安全、不破坏用户手写配置。
//
// 专精 4 家 native MCP 接入（refactor-data-home 2026-07 名单；pi 已退出，见 commit 34c68b8）：
//   - claude-code  .mcp.json          {"mcpServers":{"forge":{command,args}}}
//   - cursor       .cursor/mcp.json   {"mcpServers":{"forge":{type:"stdio",command,args}}}
//   - opencode     opencode.json      {"mcp":{"forge":{type:"local",command:[...],enabled}}}
//   - codex        .codex/config.toml [mcp_servers.forge] command/args (TOML)
//
// 拒绝扩展 MCP 接入（4 家专精外的 agent——copilot/windsurf/cline 等，translator 保留以
// 兼容已用用户，但不再新增 MCP 实现；具体见各 xxx.go 顶部注释）：
//   - copilot   VS Code .vscode/mcp.json `servers` vs `mcpServers` schema 演进中；cloud
//               agent 走 repo Settings JSON（autonomous，无审批）——两套分开核实成本高。
//   - windsurf  Cascade 用全局 ~/.codeium/windsurf/mcp_config.json，与本包"写项目目录"
//               约定冲突；Devin Local（next-gen）schema 与 Claude 兼容但仍过渡期。
//   - cline     仅读全局 ~/.cline/data/settings/cline_mcp_settings.json；项目级 MCP 是
//               未实现的 feature request（cline/cline#2418, 2026-07 核实），ClineTranslator
//               改为 guidance-only——不写 .cline/mcp.json，改为在 .clinerules/ 指引
//               用户手动接 MCP server。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// forgeMCPCommand / forgeMCPArgs 是 forge MCP stdio server 的启动定义。各工具按自己格式
// 写入。forge 必须在 PATH——与 hooks 同样前提（hooks 也 spawn `forge hook`）。
const forgeMCPCommand = "forge"

var forgeMCPArgs = []string{"mcp", "serve"}

// writeClaudeMCP 生成/合并项目根 .mcp.json（Claude Code 项目级 MCP 配置）：
//
//	{"mcpServers":{"forge":{"command":"forge","args":["mcp","serve"]}}}
func writeClaudeMCP(projectDir string) error {
	return mergeMCPServerJSON(
		filepath.Join(projectDir, ".mcp.json"),
		"mcpServers", "forge",
		map[string]any{
			"command": forgeMCPCommand,
			"args":    forgeMCPArgs,
		},
	)
}

// StripForgeMCPServer 移除项目根 .mcp.json 的 forge server 条目。当 forge plugin 在
// user-level 已装，plugin 的 .mcp.json 已注册同名 forge MCP server（全机器所有项目），
// project-level 保留会让 Claude Code 双重加载同名 forge server。
//
// 仅删 forge 条目，保留用户其他 MCP server。移除后：
//   - mcpServers 空 + 无其他顶层字段 → 删整个 .mcp.json
//   - mcpServers 空 + 有其他顶层字段 → 写回（无 mcpServers）
//   - 仍有其他 server → 写回（无 forge）
//
// 幂等：无 .mcp.json / 无 mcpServers / 无 forge 条目时 no-op（changed=false）。
// writeClaudeMCP 保持纯函数（永远 merge 写 forge）。plugin 已装时,project-level 重复由
// 命令层（init/sync 的 dedupeProjectLevelIfPlugin,所有写入后统一调用）清理——避免单元测试
// 依赖全局 IsClaudePluginInstalled 状态。
func StripForgeMCPServer(projectDir string) (changed bool, err error) {
	path := filepath.Join(projectDir, ".mcp.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read .mcp.json: %w", err)
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false, fmt.Errorf("parse .mcp.json: %w", err)
	}
	serversRaw, hasServers := cfg["mcpServers"]
	if !hasServers {
		return false, nil
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(serversRaw, &servers); err != nil {
		return false, fmt.Errorf("parse mcpServers: %w", err)
	}
	if _, exists := servers["forge"]; !exists {
		return false, nil
	}
	delete(servers, "forge")
	if len(servers) > 0 {
		serversJSON, mErr := json.Marshal(servers)
		if mErr != nil {
			return false, fmt.Errorf("marshal mcpServers: %w", mErr)
		}
		cfg["mcpServers"] = serversJSON
	} else {
		delete(cfg, "mcpServers")
	}
	if len(cfg) == 0 {
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("remove empty .mcp.json: %w", err)
		}
		return true, nil
	}
	out, mErr := json.MarshalIndent(cfg, "", "  ")
	if mErr != nil {
		return false, fmt.Errorf("marshal .mcp.json: %w", mErr)
	}
	return true, os.WriteFile(path, out, 0644)
}

// writeCursorMCP 生成/合并 .cursor/mcp.json（Cursor 项目级 MCP，type=stdio）：
//
//	{"mcpServers":{"forge":{"type":"stdio","command":"forge","args":["mcp","serve"]}}}
func writeCursorMCP(projectDir string) error {
	return mergeMCPServerJSON(
		filepath.Join(projectDir, ".cursor", "mcp.json"),
		"mcpServers", "forge",
		map[string]any{
			"type":    "stdio",
			"command": forgeMCPCommand,
			"args":    forgeMCPArgs,
		},
	)
}

// writeOpencodeMCP 生成/合并 opencode.json（opencode 项目级配置）：
//
//	{"mcp":{"forge":{"type":"local","command":["forge","mcp","serve"],"enabled":true}}}
//
// opencode 的 command 是【数组】（command+args 合并），顶层 key 是 mcp（非 mcpServers），
// server 需 enabled:true——均与 claude/cursor 不同，按 opencode 文档写入。
func writeOpencodeMCP(projectDir string) error {
	cmd := append([]string{forgeMCPCommand}, forgeMCPArgs...)
	return mergeMCPServerJSON(
		filepath.Join(projectDir, "opencode.json"),
		"mcp", "forge",
		map[string]any{
			"type":    "local",
			"command": cmd,
			"enabled": true,
		},
	)
}

// writeCodexMCP 追加/合并 .codex/config.toml 的 [mcp_servers.forge] 段。codex 用 TOML
// （Go 标准库无 TOML marshal），文本追加 + 段名存在性检查（幂等）。.codex/ 目录由
// CodexTranslator.Translate 已建（hooks.json 同目录）。
//
//	[mcp_servers.forge]
//	command = "forge"
//	args = ["mcp", "serve"]
func writeCodexMCP(projectDir string) error {
	path := filepath.Join(projectDir, ".codex", "config.toml")
	content := ""
	if data, err := os.ReadFile(path); err == nil {
		content = string(data)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("codex: read config.toml: %w", err)
	}
	if hasTOMLSection(content, "[mcp_servers.forge]") {
		return nil // 幂等：已有 forge server 段
	}
	content = strings.TrimRight(content, "\n") + codexMCPServerTOML()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("codex: write config.toml: %w", err)
	}
	return nil
}

// codexMCPServerTOML 返回 codex config.toml 的 forge MCP server 段。双引号用显式码点
// 构造，绕过 Windows 输入引号腐蚀（ASCII " 被转中文弯引号会让 TOML 解析失败）。
// args 用循环 join 构造（不假设 forgeMCPArgs 元素数），改 args 长度不会 panic。
func codexMCPServerTOML() string {
	q := string(rune(34)) // ASCII 双引号
	quoted := make([]string, len(forgeMCPArgs))
	for i, a := range forgeMCPArgs {
		quoted[i] = q + a + q
	}
	return "\n[mcp_servers.forge]\ncommand = " + q + forgeMCPCommand + q +
		"\nargs = [" + strings.Join(quoted, ", ") + "]\n"
}

// hasTOMLSection 报告 content 是否含 section 段头（行首匹配，非注释）。用行首而非朴素
// Contains，避免用户在 TOML 注释里写 `# [mcp_servers.forge]` 时被误判为段已存在——
// 那会让 writeCodexMCP 跳过、forge MCP server 漏配。section 必须是行首（前导 \n 或
// 文件开头）。
func hasTOMLSection(content, section string) bool {
	if strings.HasPrefix(content, section) {
		return true
	}
	return strings.Contains(content, "\n"+section)
}

// mergeMCPServerJSON 读 path（不存在则新建），在 topKey（mcpServers/mcp）下设
// serverName → serverDef，写回。保留其他 key 和其他 server。已存在同名 server 则跳过
// （幂等，不覆盖用户配置）。现有文件非合法 JSON 时返回错误（不覆盖，保护用户文件）。
func mergeMCPServerJSON(path, topKey, serverName string, serverDef map[string]any) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s dir: %w", dir, err)
		}
	}
	cfg := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("%s exists but is not valid JSON (left untouched): %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	servers, _ := cfg[topKey].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
		cfg[topKey] = servers
	}
	if _, exists := servers[serverName]; exists {
		return nil // 幂等：保留用户已有同名 server 配置
	}
	servers[serverName] = serverDef
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	return os.WriteFile(path, append(out, '\n'), 0644)
}
