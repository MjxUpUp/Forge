package agentbridge

import (
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// Copilot plugin-hooks wiring (Wave 2c). Copilot officially supports lifecycle hooks
// (docs.github.com/en/copilot/reference/hooks-reference), and plugins contribute
// hooks via "each plugin's own hooks.json (or hooks/hooks.json) inside the plugin's
// installation directory". Forge ships the manifest at the plugin ROOT
// (plugins/<name>/hooks.json) — deliberately NOT hooks/hooks.json: Claude Code also
// loads hooks/hooks.json from an installed plugin, and .claude-plugin/plugin.json
// already carries the same hooks field, so the hooks/ location would make every
// hook double-fire on Claude. The root location is copilot's other documented
// plugin-hook path and Claude ignores it.
//
// Format: copilot's hook configuration schema {"version":1,"hooks":{<Event>:[entry]}}
// with PascalCase event keys — copilot's Claude/VS Code compatibility mode, which
// the docs name as "as used in Claude Code plugins and the Open Plugins format".
// PascalCase keys give Claude matcher semantics and snake_case payloads, so the
// stdin side needs no normalizer and Claude tool-name matchers pass through
// VERBATIM: copilot maps its runtime tools onto Claude names before matching
// (bash/powershell→Bash, view→Read, create→Write, edit/str_replace_editor/
// apply_patch→Edit, task→Agent). apply_patch reports as Edit (unlike codex) — no
// matcher widening needed here.
//
// Two copilot-specific deltas vs the spec: commands carry ` --agent copilot`
// (output-protocol selection — copilot parses no decision:"approve", injects context
// as top-level camelCase additionalContext on sessionStart/postToolUse only, and
// agentStop blocks ONLY via stdout {"decision":"block"} + exit 0; see
// emitCopilotOutput in internal/cli/hook.go), and each entry sets timeoutSec 60
// (copilot's default 30s risks killing heavier gates — task-verify forks several
// forge subprocesses).
//
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

// copilotHookEntry is copilot's flat hook-entry shape: {type, command, matcher,
// timeoutSec}. Copilot's config format is flatter than Claude Code's nested
// {matcher, hooks:[{type,command}]}: the command sits directly on the entry and the
// matcher rides along as a sibling field. matcher is omitempty so session-level
// events (SessionStart, Stop — no matcher) serialize without the key.
//
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

// copilotEventName whitelists the Claude-Code PascalCase events copilot's hook
// system accepts (PascalCase keys are the documented Claude-compat form of
// copilot's camelCase roster: preToolUse/postToolUse/agentStop/sessionStart/
// userPromptSubmitted). PostCompact has NO copilot analogue (the roster ships only
// the observe-only preCompact, whose output is not processed), so it returns ok=false
// — shipping an unknown event key risks item-level drops at load. Anything else in
// the spec without a copilot analogue is filtered the same way.
//
// copilotEventName 白名单化 copilot hook 系统接受的 Claude-Code PascalCase event
// （PascalCase 键是 copilot camelCase 名册——preToolUse/postToolUse/agentStop/
// sessionStart/userPromptSubmitted——文档化的 Claude 兼容形态）。PostCompact 无
// copilot 对应物（名册只有 observe-only 的 preCompact，其输出不被处理），返回
// ok=false——发未知 event 键有载入期条目级丢弃风险。spec 里其他无 copilot 对应物
// 的 event 同样被过滤。
func copilotEventName(event string) (string, bool) {
	switch event {
	case "PreToolUse", "PostToolUse", "Stop", "SessionStart", "UserPromptSubmit":
		return event, true
	default:
		return "", false
	}
}

// buildCopilotHooks derives the copilot plugin-hooks manifest from hooks.ForgeHookSpec
// (single source of truth). Mirrors buildReasonixHooks' flatten loop: iterate the
// spec, skip events failing the copilot whitelist, flatten each matcher's hook list
// to one entry per hook — carrying the matcher onto each entry verbatim (copilot
// matches Claude tool names, see the file-header comment), wrapping the type, and
// appending ` --agent copilot` to forge commands (output-protocol selection). No
// manual copy → no drift vs ForgeHookSpec.
//
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

// writeCopilotHooksManifest writes plugins/<name>/hooks.json — at the plugin ROOT,
// copilot's documented "each plugin's own hooks.json" location (NOT hooks/hooks.json,
// which Claude Code would double-fire alongside .claude-plugin/plugin.json's hooks
// field; see the file-header comment). Copilot is thus the 6th host served by the
// plugin pack: the marketplace install wires the gate set with no manual init step.
// TestPluginPack_CommittedCopilotManifestMatchesGenerator guards the committed copy
// against generator drift.
//
// writeCopilotHooksManifest 写 plugins/<name>/hooks.json——位于 plugin 根，即 copilot
// 文档化的 "每个 plugin 自己的 hooks.json" 位置（不用 hooks/hooks.json——Claude Code
// 会与 .claude-plugin/plugin.json 的 hooks 字段双跑；见文件头注释）。copilot 由此成为
// plugin pack 服务 的第 6 个 host：marketplace 安装即接线 gate 集，无需手动 init。
// TestPluginPack_CommittedCopilotManifestMatchesGenerator 守卫 committed 副本不偏离
// 生成器输出。
func writeCopilotHooksManifest(pluginDir string) error {
	return writeJSONIndent(filepath.Join(pluginDir, "hooks.json"), buildCopilotHooks())
}
