package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/util"
)

// ZcodeTranslator wires forge hooks into ZCode's user-level config.
//
// ZcodeTranslator 把 forge hook 接线进 ZCode 的**用户级**配置
// （~/.zcode/cli/config.json 的 hooks.events）。ZCode（z.ai 的 agentic IDE，
// GLM 系列）在 hook 层刻意与 Claude Code 兼容（zcode.z.ai/en/docs/hooks，
// 2026-08 核实）：
//   - stdin 在 camelCase 字段旁携带 Claude 蛇形别名（session_id /
//     tool_name / tool_input / hook_event_name）——默认 HookInput unmarshal
//     即可，无需 StdinDialect。
//   - stdout 读 Claude 形态：hookSpecificOutput.additionalContext（上下文
//     注入）、permissionDecision allow/deny（PreToolUse）、continue:false +
//     reason（UserPromptSubmit）、decision:block + reason（Stop）。未知字段
//     被忽略。
//   - exit 2 是所有可阻断事件的阻断捷径。
//
// 故生成的命令无需协议覆盖：追加 `--agent zcode` 仅为会话归因
// （EnsureHookSession/StampSessionAgent），stdin 解析与 emitClaudeOutput
// 默认实现本已说 ZCode 的方言——codebuddy/opencode 的「Claude 形、无需
// flag」模型，此处 flag 纯粹为身份。以上为文档结论；wire 级验证（
// kimi-hook-routing.md 的标准）待有 ZCode 安装环境后补。
//
// 事件覆盖：ZCode 名册为 SessionStart / UserPromptSubmit / PreToolUse /
// PermissionRequest / PostToolUse / PostToolUseFailure / Stop——PascalCase
// 名与 Claude 逐字相同，故无需改名映射、只需白名单：
//   - PostCompact 无 ZCode 事件；压缩以 source=compact 触发 SessionStart，
//     match-all 的 SessionStart 组已覆盖（task-resume tl;dr 层——与 cursor
//     同一回落）。
//   - SubagentStop 不在名册；subagent-track 丢弃。
//   - PermissionRequest 为 ZCode 独有，无 ForgeHookSpec 对应物。
//
// 一条写进每个 Stop hook 语义的执法注意点：ZCode 连续 3 次 Stop 阻断后强制
// 结束会话，故 task-verify / review-stop 的续跑循环必须在 3 轮内收敛
// （hook 自身的 stop_hook_active 守卫本已设界）。
//
// 项目级 hooks 在当前 ZCode 版本不执行（config_project_hooks_ignored）——
// 本 translator 仅用户级，团队分发走 plugin 渠道（.zcode-plugin/plugin.json
// 缺席时 ZCode 回落读 .claude-plugin/plugin.json，现有 plugins/forge 包可
// 原样加载）。
//
// merge 语义：config.json 还承载用户自己的设置（hooks.timeoutMs/
// maxOutputBytes、模型偏好），整文件覆盖不可行。顶层与 hooks 级未知字段经
// json.RawMessage 保留；hooks.events 内（Claude 嵌套 {matcher, hooks:[]}
// 形态）用户条目保留原始字节、forge 条目整体替换（stripForgeMatchersRaw +
// 追加生成集）——幂等。hooks.enabled 强制为 true：没它 ZCode 什么都不执行，
// 而 `forge init --agents zcode` 本身就是用户接线 hook 的显式请求。
type ZcodeTranslator struct{}

func (t *ZcodeTranslator) AgentType() AgentType {
	return AgentZcode
}

// zcodeSupportedEvents 是 ZCode hook 名册（zcode.z.ai/en/docs/hooks）与
// ForgeHookSpec 的交集。事件名逐字沿用 Claude PascalCase——白名单只**丢**
// 事件（PostCompact、SubagentStop——见 translator 头注），从不改名。
var zcodeSupportedEvents = map[string]bool{
	"SessionStart":       true,
	"UserPromptSubmit":   true,
	"PreToolUse":         true,
	"PostToolUse":        true,
	"PostToolUseFailure": true,
	"Stop":               true,
}

type zcodeHookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	// Timeout is ZCode's compatibility field, in SECONDS (timeoutMs wins when both are set).
	//
	// Timeout 是 ZCode 的兼容字段，单位**秒**（两者同设时 timeoutMs 优先）。
	// 60 对齐 cursor 的逐条目惯例；ZCode 根默认本为 60s，此处只是防用户调低
	// 根 timeoutMs 后 hook 被提前掐死。
	Timeout int `json:"timeout,omitempty"`
}

type zcodeHookGroup struct {
	Matcher string           `json:"matcher,omitempty"`
	Hooks   []zcodeHookEntry `json:"hooks"`
}

func (t *ZcodeTranslator) Translate(projectDir string, input *TranslationInput) error {
	// 用户级 translator：刻意忽略 projectDir——注册全机器生效（与
	// KimiTranslator 同契约）。
	home, err := ZcodeConfigHome()
	if err != nil {
		return fmt.Errorf("zcode: %w", err)
	}
	// 检测自毒防线：DetectAgents 以「~/.zcode 存在」判定「zcode 已安装」
	// （hostcap InstallIndicators）。在未装 ZCode 的机器上创建该目录会让后续
	// 每次检测都误接一个不存在的工具（与 hooks.GenerateUserSettings 的
	// ~/.claude 检查同一防线）。故无 ZCode 机器上的显式 `--agents zcode` 为
	// no-op——与 claude/cursor 行为一致。
	if info, err := os.Stat(home); err != nil || !info.IsDir() {
		return nil
	}
	path := filepath.Join(home, "cli", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("zcode: failed to create config dir: %w", err)
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("zcode: failed to read config.json: %w", err)
	}
	merged, err := mergeZcodeConfig(existing)
	if err != nil {
		return err
	}
	if string(existing) == string(merged) {
		return nil // already up to date — idempotent no-op
	}
	if err := util.AtomicWrite(path, merged, 0644); err != nil {
		return fmt.Errorf("zcode: failed to write config.json: %w", err)
	}
	return nil
}

// ZcodeConfigHome resolves ZCode's config home (~/.zcode).
//
// ZcodeConfigHome 解析 ZCode 的 config home（~/.zcode）。ZCode 未文档化
// config home 的 env 覆盖（不像 CLAUDE_CONFIG_DIR/CODEX_HOME），故路径直接由
// 用户 home 派生——与 cursor 同约定。
func ZcodeConfigHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user home: %w", err)
	}
	return filepath.Join(home, ".zcode"), nil
}

// ZcodeConfigPath resolves the cli/config.json path inside ZcodeConfigHome.
//
// ZcodeConfigPath 解析 ZcodeConfigHome 下的 cli/config.json 路径。
func ZcodeConfigPath() (string, error) {
	home, err := ZcodeConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "cli", "config.json"), nil
}

// buildZcodeHooks 从 hooks.ForgeHookSpec——与所有 translator 共享的单一真相源
// ——派生 ZCode 的 hooks.events map。形态是 Claude 的嵌套 {matcher,
// hooks:[{type, command}]}、PascalCase 事件名，故仅有的转换为：丢弃 ZCode
// 名册外的事件（zcodeSupportedEvents）、给每条 forge 命令追加
// ` --agent zcode`（归因，见 translator 头注）、钉死 timeout:60s。matcher
// 原样透传：ZCode 匹配 Claude 工具名（官方示例 Write|Edit / Read|Bash，
// 支持 Agent/Task 别名）；ZCode 不认识的 token 只是永不命中——无害，不像
// cursor 那里 Bash 必须译成 Shell。map marshal 的 key 排序
// （encoding/json）保证输出确定。TestZcodeWiringMirrorsClaudeSettings 守卫
// 受支持事件上的命令集对等。
func buildZcodeHooks() map[string][]zcodeHookGroup {
	spec := hooks.ForgeHookSpec()
	events := make([]string, 0, len(spec))
	for ev := range spec {
		if zcodeSupportedEvents[ev] {
			events = append(events, ev)
		}
	}
	sort.Strings(events)

	out := make(map[string][]zcodeHookGroup, len(events))
	for _, ev := range events {
		for _, m := range spec[ev] {
			group := zcodeHookGroup{Matcher: m.Matcher}
			for _, h := range m.Hooks {
				cmd := h.Command
				if hooks.IsForgeHookCommand(cmd) {
					cmd += " --agent zcode"
				}
				group.Hooks = append(group.Hooks, zcodeHookEntry{
					Type:    "command",
					Command: cmd,
					Timeout: 60,
				})
			}
			out[ev] = append(out[ev], group)
		}
	}
	return out
}

// mergeZcodeConfig 把生成的 forge 接线合并进已有 ZCode config.json。未知顶层
// 字段（用户设置）与未知 hooks 级字段（timeoutMs/maxOutputBytes）经
// json.RawMessage 保留；hooks.events 内剥除 forge 来源条目后追加当前生成集
// （用户条目保留原始字节——merge_raw.go 契约）。hooks.enabled 置 true
// （没有它 ZCode 什么都不执行）。输出确定 → Translate 幂等。existing 为
// nil/空时生成一份最小新配置。
func mergeZcodeConfig(existing []byte) ([]byte, error) {
	cfg := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &cfg); err != nil {
			return nil, fmt.Errorf("zcode: parse existing config.json: %w", err)
		}
	}
	// 字面 `null` 正文（或下方的 `{"hooks": null}`）会 unmarshal 成 **nil**
	// map——往里写键即 panic。每次 unmarshal 后重坐两个 map，让被手工清空的
	// 配置走全新文件路径，而不是让 forge init 崩掉。
	if cfg == nil {
		cfg = map[string]json.RawMessage{}
	}

	hooksObj := map[string]json.RawMessage{}
	if raw, ok := cfg["hooks"]; ok {
		if err := json.Unmarshal(raw, &hooksObj); err != nil {
			return nil, fmt.Errorf("zcode: parse existing hooks section: %w", err)
		}
	}
	if hooksObj == nil {
		hooksObj = map[string]json.RawMessage{}
	}
	enabledJSON, err := json.Marshal(true)
	if err != nil {
		return nil, fmt.Errorf("zcode: marshal hooks.enabled: %w", err)
	}
	hooksObj["enabled"] = enabledJSON

	kept := map[string][]json.RawMessage{}
	if raw, ok := hooksObj["events"]; ok {
		var spec map[string][]json.RawMessage
		if err := json.Unmarshal(raw, &spec); err != nil {
			return nil, fmt.Errorf("zcode: parse existing hooks.events section: %w", err)
		}
		kept, _ = stripForgeMatchersRaw(spec)
	}
	generated, err := rawHooksSection(buildZcodeHooks())
	if err != nil {
		return nil, fmt.Errorf("zcode: marshal generated hooks: %w", err)
	}
	for event, groups := range generated {
		kept[event] = append(kept[event], groups...)
	}
	eventsJSON, err := json.Marshal(kept)
	if err != nil {
		return nil, fmt.Errorf("zcode: marshal merged events: %w", err)
	}
	hooksObj["events"] = eventsJSON
	hooksJSON, err := json.Marshal(hooksObj)
	if err != nil {
		return nil, fmt.Errorf("zcode: marshal hooks section: %w", err)
	}
	cfg["hooks"] = hooksJSON

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("zcode: marshal config.json: %w", err)
	}
	return append(data, '\n'), nil
}

// StripZcodeHooks removes forge hooks from ZCode's user-level ~/.zcode/cli/config.json (uninstall path).
//
// StripZcodeHooks 移除用户级 ~/.zcode/cli/config.json 中的 forge hooks（卸载
// 路径）。用户自定义条目（未知字段不丢，见 merge_raw.go）、未知 hooks 级字段
// （含 enabled 标志——用户自己的 hook 可能依赖它）与未知顶层字段全部保留；
// 文件本身绝不删除。返回是否实际改动了文件；文件不存在或无 forge hooks 均为
// 干净 no-op。
func StripZcodeHooks() (bool, error) {
	path, err := ZcodeConfigPath()
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("zcode: failed to read config.json: %w", err)
	}
	cfg := map[string]json.RawMessage{}
	if err := json.Unmarshal(existing, &cfg); err != nil {
		return false, fmt.Errorf("zcode: parse existing config.json: %w", err)
	}
	raw, ok := cfg["hooks"]
	if !ok {
		return false, nil
	}
	hooksObj := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &hooksObj); err != nil {
		return false, fmt.Errorf("zcode: parse existing hooks section: %w", err)
	}
	rawEvents, ok := hooksObj["events"]
	if !ok {
		return false, nil
	}
	var spec map[string][]json.RawMessage
	if err := json.Unmarshal(rawEvents, &spec); err != nil {
		return false, fmt.Errorf("zcode: parse existing hooks.events section: %w", err)
	}
	kept, removedAny := stripForgeMatchersRaw(spec)
	if !removedAny {
		return false, nil
	}
	eventsJSON, err := json.Marshal(kept)
	if err != nil {
		return false, fmt.Errorf("zcode: marshal stripped events: %w", err)
	}
	hooksObj["events"] = eventsJSON
	hooksJSON, err := json.Marshal(hooksObj)
	if err != nil {
		return false, fmt.Errorf("zcode: marshal hooks section: %w", err)
	}
	cfg["hooks"] = hooksJSON
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, fmt.Errorf("zcode: marshal config.json: %w", err)
	}
	if err := util.AtomicWrite(path, append(data, '\n'), 0644); err != nil {
		return false, fmt.Errorf("zcode: failed to write config.json: %w", err)
	}
	return true, nil
}
