package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/userassets"
	"github.com/MjxUpUp/Forge/internal/util"
)

// WindsurfTranslator wires forge hooks into windsurf's USER-LEVEL hooks.json
// (~/.codeium/windsurf/hooks.json.
//
// WindsurfTranslator 把 forge hook 接线进 windsurf 的 user-level hooks.json
// （~/.codeium/windsurf/hooks.json——Windsurf 官方支持 user-level hooks，Cascade
// 会把 system/workspace 各级加载合并；见
// https://docs.windsurf.com/windsurf/cascade/hooks），并更新 .windsurfrules
// （guidance 兜底）。Cascade 内置 lifecycle hooks，pre_* 事件上 exit-code-2 即
// deny，故 pre-tool 的 Forge gate（task-guard/bash-guard/...）是真 enforce。
// 诚实化警告（Wave 2b）：post_cascade_response——Stop 组（task-verify/review-stop）
// 的挂载点——是异步 post-hook，**无法阻断**，故回合末门禁在 Windsurf 上是
// advisory-only（接线说明见 buildWindsurfHooks）。也无 stdout JSON 协议：allow
// 静默、阻断原因走 stderr。其 stdin schema 与 Claude Code 不同，故 hook 命令带
// `--agent windsurf`，由 forge 归一化（见 internal/cli/hook_normalize.go）。
//
// 用户级路径对齐 kimi/claude-code 模型：一份全机器注册替代逐项目的
// .windsurf/hooks.json 副本，forge init/sync 不再往项目目录写 hook 配置（用户级
// 资产迁移）。merge 语义：command 非 forge 来源的条目（见 isForgeBridgeCommand）
// 原样保留；forge 条目整体替换为当前生成集，Translate 幂等。
type WindsurfTranslator struct{}

func (t *WindsurfTranslator) Translate(projectDir string, input *TranslationInput) error {
	if input.Protocol == nil {
		return fmt.Errorf("windsurf: protocol is required")
	}

	// 真实的 Cascade lifecycle hooks——enforcement 接口。Windsurf 的 hooks.json
	// 是扁平结构：hooks.<event>[].{command,show_output}，event 名为 snake_case，stdin
	// schema（tool_info/agent_action_name）与 Claude Code 不同，故命令带 `--agent windsurf`，
	// 由 forge 归一化（internal/cli/hook_normalize.go）。pre-event exit 2 = deny。
	// 写在用户级（~/.codeium/windsurf/hooks.json）——projectDir 只用于下方的
	// .windsurfrules guidance。
	hooksPath, err := WindsurfHooksPath()
	if err != nil {
		return fmt.Errorf("windsurf: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0755); err != nil {
		return fmt.Errorf("windsurf: failed to create config dir: %w", err)
	}
	existingHooks, err := os.ReadFile(hooksPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("windsurf: failed to read hooks.json: %w", err)
	}
	merged, err := mergeWindsurfHooks(existingHooks)
	if err != nil {
		return err
	}
	if err := util.AtomicWrite(hooksPath, merged, 0644); err != nil {
		return fmt.Errorf("windsurf: write hooks.json: %w", err)
	}

	// Guidance 规则，作为不支持 hook 的 Windsurf 版本的兜底。写到 Windsurf 的
	// 用户级全局规则文件（~/.codeium/windsurf/memories/global_rules.md——
	// Cascade 恒加载），而非项目级 .windsurfrules（零项目写入默认）。forge 段
	// 以标记段 upsert；forge 首次写入前备份原文件（forge uninstall --restore
	// 可回滚）。
	content := buildWindsurfSection(input)
	rulesPath, err := WindsurfGlobalRulesPath()
	if err != nil {
		return fmt.Errorf("windsurf: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(rulesPath), 0755); err != nil {
		return fmt.Errorf("windsurf: failed to create global rules dir: %w", err)
	}
	if err := userassets.BackupOriginal(rulesPath); err != nil {
		return fmt.Errorf("windsurf: failed to back up global_rules.md: %w", err)
	}

	existing, err := os.ReadFile(rulesPath)
	if err != nil && !os.IsNotExist(err) {
		// A read error other than NotExist (permissions, IO) must not fall
		// through to the whole-file overwrite below — that would silently
		// destroy the user's existing rules. Same contract as kimi.go.
		return fmt.Errorf("windsurf: failed to read global_rules.md: %w", err)
	}
	if len(existing) > 0 {
		updated := replaceForgeRules(string(existing), content)
		return util.AtomicWrite(rulesPath, []byte(updated), 0644)
	}

	// 创建新文件
	return util.AtomicWrite(rulesPath, []byte(content), 0644)
}

// WindsurfGlobalRulesPath resolves Windsurf's user-level global rules file
// (~/.codeium/windsurf/memories/global_rules.md.
//
// WindsurfGlobalRulesPath 解析 Windsurf 的用户级全局规则文件
// （~/.codeium/windsurf/memories/global_rules.md——Cascade 对每个 workspace
// 恒加载；项目级 .windsurfrules 的用户级对应物）。
func WindsurfGlobalRulesPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user home: %w", err)
	}
	return filepath.Join(home, ".codeium", "windsurf", "memories", "global_rules.md"), nil
}

func (t *WindsurfTranslator) AgentType() AgentType {
	return AgentWindsurf
}

// WindsurfHooksPath resolves the user-level hooks.json path
// (~/.codeium/windsurf/hooks.json, per the official Cascade hooks docs).
//
// WindsurfHooksPath 解析 user-level hooks.json 路径（~/.codeium/windsurf/hooks.json，
// 见 Cascade hooks 官方文档）。Windsurf 没有官方文档化的 env 覆盖，故路径直接由
// 用户 home 派生。
func WindsurfHooksPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user home: %w", err)
	}
	return filepath.Join(home, ".codeium", "windsurf", "hooks.json"), nil
}

// mergeWindsurfHooks 把生成的 forge 接线合并进已有的 windsurf hooks.json。未知顶层
// 字段经 json.RawMessage 保留；扁平 hooks 段内，command 非 forge 来源的条目逐字节
// 保留（powershell/working_directory 等未知条目字段不丢——见 merge_raw.go），
// forge 条目整体替换为当前生成集。输出确定，故 Translate 幂等。existing 为
// nil/空时生成新文件。
func mergeWindsurfHooks(existing []byte) ([]byte, error) {
	cfg := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &cfg); err != nil {
			return nil, fmt.Errorf("windsurf: parse existing hooks.json: %w", err)
		}
	}
	kept := map[string][]json.RawMessage{}
	if raw, ok := cfg["hooks"]; ok {
		var flat map[string][]json.RawMessage
		if err := json.Unmarshal(raw, &flat); err != nil {
			return nil, fmt.Errorf("windsurf: parse existing hooks section: %w", err)
		}
		kept, _ = stripForgeFlatEntriesRaw(flat)
	}
	generated, err := rawHooksSection(buildWindsurfHooks()["hooks"])
	if err != nil {
		return nil, fmt.Errorf("windsurf: marshal generated hooks: %w", err)
	}
	for event, entries := range generated {
		kept[event] = append(kept[event], entries...)
	}
	hooksJSON, err := json.Marshal(kept)
	if err != nil {
		return nil, fmt.Errorf("windsurf: marshal merged hooks: %w", err)
	}
	cfg["hooks"] = hooksJSON
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("windsurf: marshal hooks.json: %w", err)
	}
	return append(data, '\n'), nil
}

// StripWindsurfHooksUserLevel removes forge hooks from the user-level
// ~/.codeium/windsurf/hooks.json (uninstall path).
//
// StripWindsurfHooksUserLevel 移除 user-level ~/.codeium/windsurf/hooks.json 中的
// forge hooks（卸载路径）。用户自定义条目（未知字段不丢，见 merge_raw.go）与未知
// 顶层字段保留；文件本身绝不删除。返回是否实际改动了文件；文件不存在或无
// forge hooks 均为干净 no-op。
func StripWindsurfHooksUserLevel() (bool, error) {
	path, err := WindsurfHooksPath()
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("windsurf: failed to read hooks.json: %w", err)
	}
	cfg := map[string]json.RawMessage{}
	if err := json.Unmarshal(existing, &cfg); err != nil {
		return false, fmt.Errorf("windsurf: parse existing hooks.json: %w", err)
	}
	raw, ok := cfg["hooks"]
	if !ok {
		return false, nil
	}
	var flat map[string][]json.RawMessage
	if err := json.Unmarshal(raw, &flat); err != nil {
		return false, fmt.Errorf("windsurf: parse existing hooks section: %w", err)
	}
	kept, removedAny := stripForgeFlatEntriesRaw(flat)
	if !removedAny {
		return false, nil
	}
	hooksJSON, err := json.Marshal(kept)
	if err != nil {
		return false, fmt.Errorf("windsurf: marshal stripped hooks: %w", err)
	}
	cfg["hooks"] = hooksJSON
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, fmt.Errorf("windsurf: marshal hooks.json: %w", err)
	}
	if err := util.AtomicWrite(path, append(data, '\n'), 0644); err != nil {
		return false, fmt.Errorf("windsurf: failed to write hooks.json: %w", err)
	}
	return true, nil
}

// StripWindsurfGlobalRules removes the FORGE:START/END marked section from the
// user-level global_rules.md (uninstall path).
//
// StripWindsurfGlobalRules 移除用户级 global_rules.md 的 FORGE:START/END 标记段
// （卸载路径）。标记外内容保留；文件本身绝不删除。返回是否实际改动；文件不存在
// 或无标记均为干净 no-op。
func StripWindsurfGlobalRules() (bool, error) {
	path, err := WindsurfGlobalRulesPath()
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("windsurf: failed to read global_rules.md: %w", err)
	}
	content := string(existing)
	if !strings.Contains(content, forgeRulesStart) {
		return false, nil
	}
	updated := replaceForgeRules(content, ``)
	if err := util.AtomicWrite(path, []byte(updated), 0644); err != nil {
		return false, fmt.Errorf("windsurf: failed to write global_rules.md: %w", err)
	}
	return true, nil
}

type windsurfHookEntry struct {
	Command    string `json:"command"`
	ShowOutput bool   `json:"show_output"`
}

// buildWindsurfHooks 对应 hooks/settings.go 的 ForgeHookSpec，针对 Windsurf 原生
// Cascade hook 格式生成。Windsurf 的 hooks.json 是扁平结构——
// hooks.<event>[].{command,show_output}——event 名为 snake_case，与 Claude Code 的
// PascalCase 相对。官方 Cascade hook 名册为 pre/post_read_code、pre/post_write_code、
// pre/post_run_command、pre/post_mcp_tool_use、pre_user_prompt、
// post_cascade_response(+_with_transcript)、post_setup_worktree——**没有**
// session_start/session_end，故 Claude 的 SessionStart 组挂到 pre_user_prompt
// （每个会话首个 prompt 到达时触发），Stop 组（task-verify/review-stop）挂到
// post_cascade_response（Cascade 真实存在的、最接近会话结束的事件）。
// 同 event 多 hook（pre_write_code 上的 task-guard + assertion-check）按顺序执行；
// pre-event exit 2 = deny。与 settings.go 手动保持同步——TestWindsurfWiringMirrorsClaudeSettings
// 守卫 drift。
// UserPromptSubmit 组（resume-reinject/skill-trigger）刻意未挂：windsurf 无
// PostCompact 事件 → compact-resume 永不置重注入标志 → resume-reinject 挂上也
// 恒静默；skill-trigger 在 windsurf 的分发已被 Stop 等事件覆盖。若未来宿主
// 补压缩事件，需同步接 UserPromptSubmit 组。
func buildWindsurfHooks() map[string]any {
	//
	// task-guard 与 assertion-check 都 gate 写操作；Windsurf 按顺序跑同 event 的所有
	// 条目，故单个 pre_write_code 列表里放两者是正确的（Windsurf 用 command 而非 event
	// 匹配，与 Claude Code 的 per-event 独立 matcher 不同）。
	return map[string]any{
		"hooks": map[string][]windsurfHookEntry{
			"pre_write_code": {
				{Command: "forge hook freeze-guard --agent windsurf", ShowOutput: false},
				{Command: "forge hook task-guard --agent windsurf", ShowOutput: false},
				{Command: "forge hook assertion-check --agent windsurf", ShowOutput: false},
				{Command: "forge hook read-before-edit --agent windsurf", ShowOutput: false},
				{Command: "forge hook skill-trigger --agent windsurf", ShowOutput: false},
				// conventions-profile 写入时刻注入（2026-08-28）：advisory。windsurf 当前
				// 无 allow-stdout 上下文通道（hostcap），接线遵循「诚实记录、绝不静默
				// 投递」——Delivered=false 落账，宿主补通道即生效（skill-trigger 同款）。
				// conventions-profile write-time injection (2026-08-28): advisory.
				// windsurf has no allow-stdout context channel today (hostcap); wiring
				// follows "record honestly, never deliver silently" — lands stamped
				// Delivered=false, activates the day the host adds the channel (same
				// as skill-trigger).
				{Command: "forge hook conventions-write --agent windsurf", ShowOutput: false},
			},
			"pre_run_command": {
				{Command: "forge hook bash-guard --agent windsurf", ShowOutput: false},
				{Command: "forge hook hazard-guard --agent windsurf", ShowOutput: false},
				{Command: "forge hook skill-trigger --agent windsurf", ShowOutput: false},
			},
			"post_write_code": {
				{Command: "forge hook auto-compile --agent windsurf", ShowOutput: false},
				{Command: "forge hook workflow-test-guard --agent windsurf", ShowOutput: false},
				{Command: "forge hook skill-trigger --agent windsurf", ShowOutput: false},
				// 事中测试提醒（#4-E）。与同组 hook 一样 advisory；归一化的 stdin
				// （windsurf 方言 → file_path）以 auto-compile 同款方式到达进程内 hook。
				{Command: "forge hook test-nudge --agent windsurf", ShowOutput: false},
			},
			"post_run_command": {
				{Command: "forge hook file-sentinel --agent windsurf", ShowOutput: false},
				{Command: "forge hook skill-trigger --agent windsurf", ShowOutput: false},
			},
			"post_read_code": {
				{Command: "forge hook tool-track --agent windsurf", ShowOutput: false},
			},
			// Cascade 没有 session_start：SessionStart 组（skill-scan / mcp-scan /
			// init-suggest / task-resume / skill-trigger）改挂 pre_user_prompt——
			// 每个会话首个 prompt 时触发。--agent windsurf 后缀（Wave 2b 诚实化
			// 修复）：缺了它这些 hook 会在无 stdout 通道的宿主上发 Claude 协议的
			// stdout——恰好这两组漏了后缀，是早期给 enforcement 事件补后缀时的
			// 疏漏。
			"pre_user_prompt": {
				{Command: "forge hook skill-scan --agent windsurf", ShowOutput: false},
				{Command: "forge hook mcp-scan --agent windsurf", ShowOutput: false},
				{Command: "forge hook init-suggest --agent windsurf", ShowOutput: false},
				{Command: "forge hook task-resume --agent windsurf", ShowOutput: false},
				{Command: "forge hook skill-trigger --agent windsurf", ShowOutput: false},
				// conventions-profile 会话摘要（2026-08-28）：SessionStart 组在 windsurf
				// 的挂点即 pre_user_prompt；通道诚实性与 skill-trigger 同款。
				// conventions-profile session digest (2026-08-28): pre_user_prompt is
				// the SessionStart group's windsurf mount; channel honesty same as
				// skill-trigger.
				{Command: "forge hook conventions-context --agent windsurf", ShowOutput: false},
			},
			// Cascade 没有 session_end：Stop 组（task-verify / review-stop /
			// skill-trigger）改挂 post_cascade_response——Cascade 真实存在的、
			// 最接近会话结束的事件。诚实化说明（Wave 2b）：post_cascade_response 是
			// **异步** post-hook——它在 cascade 已经应答之后才触发，exit 2 在那里
			// **无法阻断**或强制再来一轮，只能把 stderr 原因以 advisory 形式上浮给
			// agent。故 task-verify/review-stop 门禁在 Windsurf 上是 advisory-only
			// （与 Claude 的 Stop 可阻断不同）。如实文档化，不藏着。
			"post_cascade_response": {
				{Command: "forge hook task-verify --agent windsurf", ShowOutput: false},
				{Command: "forge hook review-stop --agent windsurf", ShowOutput: false},
				{Command: "forge hook skill-trigger --agent windsurf", ShowOutput: false},
			},
		},
	}
}

const (
	forgeRulesStart = "<!-- FORGE:START -->"
	forgeRulesEnd   = "<!-- FORGE:END -->"
)

// windsurfUserLevelPreamble 前置在写入用户级 global_rules.md 的 forge 段段首。
// Cascade 对每个 workspace 都加载该文件，段文本不能无条件断言"本项目使用
// Forge"——仅当当前项目已 init 时才激活，否则必须忽略。与 skillgen 的
// userLevelPreamble（~/.claude/CLAUDE.md、~/.codex/AGENTS.md）同一契约。
const windsurfUserLevelPreamble = "**This section is a Forge user-level global injection, loaded by Cascade in every workspace. Follow the rules below ONLY when the current project has been initialized with `forge init` (i.e. it is registered in Forge's global project registry); if the current project does not use Forge, ignore this section entirely.**\n\n"

func buildWindsurfSection(input *TranslationInput) string {
	var sb strings.Builder

	sb.WriteString(forgeRulesStart + "\n\n")
	sb.WriteString(windsurfUserLevelPreamble)
	sb.WriteString("# Forge Quality Standards\n\n")

	// 质量标准
	protocol.RenderStandards(&sb, input.Protocol.Standards, protocol.StandardRenderStyle{
		SeverityLabel:  protocol.WordSeverityLabel,
		HookInfoFormat: " (enforced: %s)",
		LineFormat:     "- [%s] **%s**: %s%s\n",
	})
	sb.WriteString("\n")

	// 会话规则
	protocol.RenderSessionRules(&sb, input.Protocol.SessionRules, protocol.SessionRuleRenderStyle{
		MandatoryLabel: protocol.AlwaysPreferLabel,
		LineFormat:     "- %[1]s: %[2]s\n",
	})
	sb.WriteString("\n")

	sb.WriteString(forgeRulesEnd + "\n")
	return sb.String()
}

// replaceForgeRules 替换 FORGE:START 与 FORGE:END 标记之间的内容，标记外的内容原样保留。
// util.ReplaceMarkedSection 的薄封装（与 skillgen 的 CLAUDE.md/AGENTS.md upsert 共享）。
func replaceForgeRules(content, newSection string) string {
	return util.ReplaceMarkedSection(content, newSection, forgeRulesStart, forgeRulesEnd)
}
