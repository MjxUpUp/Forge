package agentbridge

import (
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// Copilot plugin-hooks wiring（Wave 2c）。Copilot 官方支持 lifecycle hooks
// （docs.github.com/en/copilot/reference/hooks-reference），plugin 以"plugin 自己的
// hooks.json（或 hooks/hooks.json），位于 plugin 安装目录内"贡献 hooks。Forge 把
// manifest 放在 plugin 根（plugins/<name>/hooks.json）——刻意不用 hooks/hooks.json：
// Claude Code 也会从已安装 plugin 加载 hooks/hooks.json，而 .claude-plugin/plugin.json
// 已带同一 hooks 字段，hooks/ 位置会让每个 hook 在 Claude 上双跑。根位置是 copilot
// 文档化的另一条 plugin-hook 路径，Claude 忽略它。
//
// 格式：copilot 的 hook 配置 schema {"version":1,"hooks":{<Event>:[entry]}}，event 键
// PascalCase——copilot 的 Claude/VS Code 兼容模式（文档原话 "as used in Claude Code
// plugins and the Open Plugins format"）。PascalCase 键给出 Claude matcher 语义与
// snake_case payload，故 stdin 侧无需 normalizer，Claude 工具名 matcher 原样透传：
// copilot 在匹配前把运行时工具映射到 Claude 名（bash/powershell→Bash、view→Read、
// create→Write、edit/str_replace_editor/apply_patch→Edit、task→Agent）。apply_patch
// 上报为 Edit（与 codex 不同）——此处无需 matcher 扩宽。
//
// 与 spec 相比两处 copilot 专属 delta：命令带 ` --agent copilot`（输出协议选择——
// copilot 不解析 decision:"approve"，上下文仅在 sessionStart/postToolUse 以顶层
// camelCase additionalContext 注入，agentStop 只能经 stdout
// {"decision":"block"} + exit 0 阻断；见 internal/cli/hook.go 的 emitCopilotOutput）；
// 每个条目设 timeoutSec 60（copilot 默认 30s，重型门禁——task-verify 要 fork 多个
// forge 子进程——有被杀风险）。
//
// 已知缺口——VS Code（Wave 2c 代码审查发现，已对照 code.visualstudio.com/docs/
// agent-customization/agent-plugins 文档核实）：VS Code 按 manifest 标记自动检测
// plugin 格式——.claude-plugin/plugin.json 即 CLAUDE 格式，而 Claude 格式的 hooks
// 只从 hooks/hooks.json 读取；根 hooks.json 是 Copilot 格式的位置。Forge 带 Claude
// 标记（Claude Code 需要），故 VS Code 上根 hooks.json 很可能无效（VS Code 去解析
// hooks/hooks.json，而本 pack 不提供）。Copilot CLI 自身文档是"plugin 自己的
// hooks.json（或 hooks/hooks.json）"——两条路径都收，CLI 路线可用。为补 VS Code
// 缺口而发 hooks/hooks.json 会让 Claude Code 上每个 hook 双跑（它在 plugin.json
// hooks 字段之外也加载 hooks/hooks.json）；安全重构需要两宿主活体验证，本环境
// 做不到——在此之前在此 + README 诚实文档化（与 codex"未官方确认"caveat 同款
// 模式）。

// copilotHookEntry 是 copilot 的扁平 hook 条目形态：{type, command, matcher,
// timeoutSec}。copilot 的配置格式比 Claude Code 的嵌套 {matcher, hooks:[{type,command}]}
// 更扁平：command 直接挂在条目上，matcher 作为兄弟字段。matcher 用 omitempty，
// 使会话级 event（SessionStart、Stop——无 matcher）序列化时不带该键。
type copilotHookEntry struct {
	Type       string `json:"type"`
	Command    string `json:"command"`
	Matcher    string `json:"matcher,omitempty"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}

// copilotEventName 白名单化 copilot hook 系统接受的 Claude-Code PascalCase event
// （PascalCase 键是 copilot camelCase 名册——preToolUse/postToolUse/agentStop/
// sessionStart/userPromptSubmitted——文档化的 Claude 兼容形态）。PostCompact 无
// copilot 对应物（名册只有 observe-only 的 preCompact，其输出不被处理），返回
// ok=false——发未知 event 键有载入期条目级丢弃风险。spec 里其他无 copilot 对应物
// 的 event 同样被过滤。
//
// PostToolUseFailure/SubagentStop 于 2026-08-22 加入：两者都在 copilot 官方
// hooks reference（14 事件名册，spec-research4），承载 failure-track/
// subagent-track（#4-A 后续）。payload 保持 Claude 形 snake_case（已接线事件
// 文档化的 PascalCase 兼容模式），stdin 侧无需 normalizer。subagentStop 可经
// stdout {"decision":"block"} + exit 0 阻断——对 observe-only 的 subagent-track
// 无关。postToolUseFailure 文档化的 exit-2-stdout→additionalContext 通道**不
// 使用**：failure-track 保持 allow 路径，该通道需先活体验证再接（与 kimi 白名单
// 同纪律——未验证通道不接线）。
func copilotEventName(event string) (string, bool) {
	switch event {
	case "PreToolUse", "PostToolUse", "Stop", "SessionStart", "UserPromptSubmit",
		"PostToolUseFailure", "SubagentStop":
		return event, true
	default:
		return "", false
	}
}

// buildCopilotHooks 从 hooks.ForgeHookSpec（单一真相源）派生 copilot 的 plugin-hooks
// manifest。镜像 buildReasonixHooks 的扁平化循环：遍历 spec，跳过未过 copilot 白名单
// 的 event，把每个 matcher 的 hook 列表扁平化为每 hook 一个条目——matcher 原样带到
// 每个条目（copilot 匹配 Claude 工具名，见文件头注释），补上 type 包装，并给 forge
// 命令追加 ` --agent copilot`（输出协议选择）。无手工副本 → 与 ForgeHookSpec 无 drift。
func buildCopilotHooks() map[string]any {
	spec := hooks.ForgeHookSpec()
	hooksMap := map[string][]copilotHookEntry{}
	for event, matchers := range spec {
		ce, ok := copilotEventName(event)
		if !ok {
			continue
		}
		for _, m := range matchers {
			for _, h := range m.Hooks {
				cmd := h.Command
				if isForgeBridgeCommand(cmd) {
					cmd += " --agent copilot"
				}
				hooksMap[ce] = append(hooksMap[ce], copilotHookEntry{
					Type:       "command",
					Command:    cmd,
					Matcher:    m.Matcher,
					TimeoutSec: 60,
				})
			}
		}
	}
	return map[string]any{
		`version`: 1,
		`hooks`:   hooksMap,
	}
}

// writeCopilotHooksManifest 写 plugins/<name>/hooks.json——位于 plugin 根，即 copilot
// 文档化的 "每个 plugin 自己的 hooks.json" 位置（不用 hooks/hooks.json——Claude Code
// 会与 .claude-plugin/plugin.json 的 hooks 字段双跑；见文件头注释）。copilot 由此成为
// plugin pack 服务 的第 6 个 host：marketplace 安装即接线 gate 集，无需手动 init。
// TestPluginPack_CommittedCopilotManifestMatchesGenerator 守卫 committed 副本不偏离
// 生成器输出。
func writeCopilotHooksManifest(pluginDir string) error {
	return writeJSONIndent(filepath.Join(pluginDir, "hooks.json"), buildCopilotHooks())
}
