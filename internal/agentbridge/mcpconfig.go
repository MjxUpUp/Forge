package agentbridge

// 清理已 init 旧项目 .mcp.json 的 forge server 残留。
//
// 历史：Forge 曾提供 MCP server（forge mcp serve），各 translator 在 Translate 末尾
// write*MCP 写项目级 .mcp.json。2026-07-24 dogfood 证明 MCP 无不可替代价值（connected
// server 来源不明 + 迭代摩擦），全拆 MCP——write*MCP 生成逻辑随之删除。但已 init 过的
// 旧项目 .mcp.json 仍残留 forge server 条目，需清理：plugin 在 user-level 装好后，
// project-level 重复会让 Claude Code 双重加载同名 forge server。StripForgeMCPServer
// 由命令层（init/sync 的 dedupeProjectLevelIfPlugin + forge plugin dedupe）统一调用。
//
// 仅删 forge 条目，保留用户其他 MCP server。幂等：无 .mcp.json / 无 mcpServers / 无
// forge 条目时 no-op。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

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
