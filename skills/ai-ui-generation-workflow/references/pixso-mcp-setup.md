# Pixso MCP 接入（2026 实证，国内团队首选）

> 本文件是 `ai-ui-generation-workflow` 模板 C 的 Pixso 接入细节，从 SKILL.md 拆出（progressive disclosure）。

Pixso 有**官方 MCP**，两条路：

**路径 A：本地 MCP（18 工具，需 Pixso 桌面端运行）**
- Server: `http://127.0.0.1:3667/mcp`（HTTP + SSE，mcp-session-id 鉴权）
- 18 个工具含 `design_to_code` / `get_node_dsl` / `get_variables` / `get_variable_sets` / `get_local_styles` / `get_all_components` / `create_instance` / `set_bound_variables` / `code_to_design` / `refine_generated_code` 等
- 支持框架：React / Vue / HTML / Flutter / ArkUI
- 可用 jiaweiwei1961/pixso-design-skill（Claude Code skill 封装，含 CLI）

**路径 B：云端 MCP（6 工具，不开 Pixso 也能用）**
- Server: `https://pixso.cn/api/mcp/mcp`（Streamable HTTP，Token Header 鉴权）
- 需 Personal Access Token（Pixso web → 用户中心 → Personal Access Tokens）
- 6 个核心工具：`getCode` / `getNodeDSL` / `get_local_styles` / `get_variable_sets` / `get_variables` / `get_variants`
- 可用 jiaweiwei1961/pixso-remote-skill

> **外部仓库 fallback**：`jiaweiwei1961/pixso-design-skill` / `jiaweiwei1961/pixso-remote-skill` 是社区 0★ 封装（非官方，2026-08 快照），用前先验证仓库可用（clone 后跑通一次工具调用）；不可用走通用 Pixso MCP 路径——按下方配置直接连官方 MCP server（本地或云端），工具调用本身不依赖社区封装。

**4 IDE 配置（实证可用）**：

```bash
# Claude Code
claude mcp add --transport http --header "Token:YOUR_TOKEN" pixso-remote https://pixso.cn/api/mcp/mcp
```
```json
// Cursor (~/.cursor/mcp.json)
{"mcpServers":{"pixso-remote":{"url":"https://pixso.cn/api/mcp/mcp","headers":{"Token":"YOUR_TOKEN"}}}}
```
```json
// Cline (VS Code) — autoApprove getCode/getNodeDSL
{"pixso-remote":{"type":"streamableHttp","url":"https://pixso.cn/api/mcp/mcp","headers":{"Token":"YOUR_TOKEN"}}}
```
```json
// Windsurf (~/.codeium/windsurf/mcp_config.json)
{"mcpServers":{"pixso-remote":{"serverUrl":"https://pixso.cn/api/mcp/mcp","headers":{"Token":"YOUR_TOKEN"}}}}
```

**为什么 Pixso MCP 比 Builder.io 更贴 AI coding**：MCP 是 Cursor/Claude/Cline 原生协议，agent 直接调工具读设计稿（含 tokens/styles/DSL），不需复制粘贴设计链接到插件；Figma 还在追赶 MCP（Figma Dev Mode 付费 + 第三方 Figma MCP）。但 Pixso MCP 生态较新（社区 repo 多为 2026 新建、0★，快照 @2026-08），生产前需验证稳定性。
