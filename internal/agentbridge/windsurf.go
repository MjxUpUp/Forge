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
// (~/.codeium/windsurf/hooks.json — Windsurf natively supports user-level hooks,
// loaded and merged with system/workspace levels by Cascade; see
// https://docs.windsurf.com/windsurf/cascade/hooks), and updates .windsurfrules
// (guidance fallback). Cascade has built-in lifecycle hooks; exit-code-2 means deny,
// so it stands alongside claude-code/codex/cursor as an agent that Forge gates can truly enforce. Its stdin
// schema differs from Claude Code, so hook commands carry `--agent windsurf` and are normalized by forge
// (see internal/cli/hook_normalize.go).
//
// The user-level location mirrors the kimi/claude-code model: one machine-wide
// registration instead of a per-project .windsurf/hooks.json copy, so forge init/sync
// no longer writes hook config into the project directory (user-level assets
// migration). Merge semantics: entries whose command is not forge-sourced (see
// isForgeBridgeCommand) are preserved verbatim; forge entries are replaced wholesale
// with the current generated set, making Translate idempotent.
//
// WindsurfTranslator 把 forge hook 接线进 windsurf 的 user-level hooks.json
// （~/.codeium/windsurf/hooks.json——Windsurf 官方支持 user-level hooks，Cascade
// 会把 system/workspace 各级加载合并；见
// https://docs.windsurf.com/windsurf/cascade/hooks），并更新 .windsurfrules
// （guidance 兜底）。Cascade 内置 lifecycle hooks，exit-code-2 即 deny，
// 故与 claude-code/codex/cursor 并列为 Forge gate 真正能 enforce 的 agent。其 stdin
// schema 与 Claude Code 不同，故 hook 命令带 `--agent windsurf`，由 forge 归一化
// （见 internal/cli/hook_normalize.go）。
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

	// Real Cascade lifecycle hooks — the enforcement interface. Windsurf's hooks.json
	// is a flat structure: hooks.<event>[].{command,show_output}, with snake_case event names and a stdin
	// schema (tool_info/agent_action_name) different from Claude Code, so commands carry `--agent windsurf`,
	// normalized by forge (internal/cli/hook_normalize.go). pre-event exit 2 = deny.
	// Written at user level (~/.codeium/windsurf/hooks.json) — projectDir only feeds the
	// .windsurfrules guidance below.
	//
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
	if err := os.WriteFile(hooksPath, merged, 0644); err != nil {
		return fmt.Errorf("windsurf: write hooks.json: %w", err)
	}

	// Guidance rules, fallback for Windsurf versions that do not support hooks.
	// Written to Windsurf's USER-LEVEL global rules file
	// (~/.codeium/windsurf/memories/global_rules.md — Cascade always loads it),
	// NOT the project-level .windsurfrules (zero-project-write default). The forge
	// section is marked-section upserted; the original file is backed up before
	// forge's first write (rollback via forge uninstall --restore).
	//
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
		return os.WriteFile(rulesPath, []byte(updated), 0644)
	}

	// Create a new file.
	//
	// 创建新文件
	return os.WriteFile(rulesPath, []byte(content), 0644)
}

// WindsurfGlobalRulesPath resolves Windsurf's user-level global rules file
// (~/.codeium/windsurf/memories/global_rules.md — loaded by Cascade for every
// workspace; the user-level counterpart of project .windsurfrules).
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
// (~/.codeium/windsurf/hooks.json, per the official Cascade hooks docs). Windsurf
// has no documented env override for this location, so the path derives from the
// user home directly.
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

// mergeWindsurfHooks merges the generated forge wiring into an existing windsurf
// hooks.json. Unknown top-level fields are preserved via json.RawMessage; within the
// flat hooks section, entries whose command is not forge-sourced are kept
// byte-for-byte (unknown entry fields such as powershell/working_directory intact —
// see merge_raw.go), and forge entries are replaced wholesale with the current
// generated set. The output is deterministic, so Translate is idempotent. A
// nil/empty existing input produces a fresh file.
//
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
// ~/.codeium/windsurf/hooks.json (uninstall path). User-defined entries (unknown
// fields intact, see merge_raw.go) and unknown top-level fields are preserved; the
// file itself is never deleted. Reports whether the file was actually modified; a
// missing file or a file without forge hooks is a clean no-op.
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
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return false, fmt.Errorf("windsurf: failed to write hooks.json: %w", err)
	}
	return true, nil
}

// StripWindsurfGlobalRules removes the FORGE:START/END marked section from the
// user-level global_rules.md (uninstall path). Content outside the markers is
// preserved; the file itself is never deleted. Reports whether the file changed;
// a missing file or one without the markers is a clean no-op.
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
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		return false, fmt.Errorf("windsurf: failed to write global_rules.md: %w", err)
	}
	return true, nil
}

type windsurfHookEntry struct {
	Command    string `json:"command"`
	ShowOutput bool   `json:"show_output"`
}

// buildWindsurfHooks corresponds to GenerateSettings in hooks/settings.go, generating the
// native Cascade hook format for Windsurf. Windsurf's hooks.json is a flat structure —
// hooks.<event>[].{command,show_output} — with snake_case event names, in contrast to
// Claude Code's PascalCase. The official Cascade hook roster is
// pre/post_read_code, pre/post_write_code, pre/post_run_command, pre/post_mcp_tool_use,
// pre_user_prompt, post_cascade_response(+_with_transcript), post_setup_worktree — there
// is NO session_start/session_end, so the Claude SessionStart group is wired to
// pre_user_prompt (fires as each session's first prompt arrives) and the Stop group
// (task-verify/review-stop) to post_cascade_response (the closest session-end
// equivalent Cascade actually emits).
// Multiple hooks on the same event (task-guard + assertion-check on pre_write_code) run in order;
// pre-event exit 2 = deny. Kept in sync with settings.go manually — TestWindsurfWiringMirrorsClaudeSettings
// guards against drift.
//
// buildWindsurfHooks 对应 hooks/settings.go 的 GenerateSettings，针对 Windsurf 原生
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
func buildWindsurfHooks() map[string]any {
	// Both task-guard and assertion-check gate write operations; Windsurf runs all entries
	// under the same event in order, so putting both in a single pre_write_code list is correct
	// (Windsurf matches by command rather than by event, unlike Claude Code's per-event independent matchers).
	//
	// task-guard 与 assertion-check 都 gate 写操作；Windsurf 按顺序跑同 event 的所有
	// 条目，故单个 pre_write_code 列表里放两者是正确的（Windsurf 用 command 而非 event
	// 匹配，与 Claude Code 的 per-event 独立 matcher 不同）。
	return map[string]any{
		"hooks": map[string][]windsurfHookEntry{
			"pre_write_code": {
				{Command: "forge hook task-guard --agent windsurf", ShowOutput: false},
				{Command: "forge hook assertion-check --agent windsurf", ShowOutput: false},
				{Command: "forge hook read-before-edit --agent windsurf", ShowOutput: false},
				{Command: "forge hook skill-trigger --agent windsurf", ShowOutput: false},
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
			},
			"post_run_command": {
				{Command: "forge hook file-sentinel --agent windsurf", ShowOutput: false},
				{Command: "forge hook skill-trigger --agent windsurf", ShowOutput: false},
			},
			"post_read_code": {
				{Command: "forge hook tool-track --agent windsurf", ShowOutput: false},
			},
			// No session_start exists in Cascade: the SessionStart group (skill-scan /
			// mcp-scan / init-suggest / task-resume / skill-trigger) hangs on
			// pre_user_prompt instead — it fires on the first prompt of every session.
			//
			// Cascade 没有 session_start：SessionStart 组（skill-scan / mcp-scan /
			// init-suggest / task-resume / skill-trigger）改挂 pre_user_prompt——
			// 每个会话首个 prompt 时触发。
			"pre_user_prompt": {
				{Command: "forge hook skill-scan", ShowOutput: false},
				{Command: "forge hook mcp-scan", ShowOutput: false},
				{Command: "forge hook init-suggest", ShowOutput: false},
				{Command: "forge hook task-resume", ShowOutput: false},
				{Command: "forge hook skill-trigger", ShowOutput: false},
			},
			// No session_end exists in Cascade: the Stop group (task-verify /
			// review-stop / skill-trigger) hangs on post_cascade_response — the
			// closest session-end equivalent Cascade actually emits.
			//
			// Cascade 没有 session_end：Stop 组（task-verify / review-stop /
			// skill-trigger）改挂 post_cascade_response——Cascade 真实存在的、
			// 最接近会话结束的事件。
			"post_cascade_response": {
				{Command: "forge hook task-verify", ShowOutput: false},
				{Command: "forge hook review-stop", ShowOutput: false},
				{Command: "forge hook skill-trigger", ShowOutput: false},
			},
		},
	}
}

const (
	forgeRulesStart = "<!-- FORGE:START -->"
	forgeRulesEnd   = "<!-- FORGE:END -->"
)

// windsurfUserLevelPreamble heads the forge section written into the USER-LEVEL
// global_rules.md. Cascade loads that file for EVERY workspace, so the section must
// not unconditionally assert "this project uses Forge" — it activates only when the
// current project is forge-initialized, and must be ignored otherwise. Same contract
// as skillgen's userLevelPreamble (~/.claude/CLAUDE.md, ~/.codex/AGENTS.md).
//
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

	// Quality standards.
	//
	// 质量标准
	protocol.RenderStandards(&sb, input.Protocol.Standards, protocol.StandardRenderStyle{
		SeverityLabel:  protocol.WordSeverityLabel,
		HookInfoFormat: " (enforced: %s)",
		LineFormat:     "- [%s] **%s**: %s%s\n",
	})
	sb.WriteString("\n")

	// Session rules.
	//
	// 会话规则
	protocol.RenderSessionRules(&sb, input.Protocol.SessionRules, protocol.SessionRuleRenderStyle{
		MandatoryLabel: protocol.AlwaysPreferLabel,
		LineFormat:     "- %[1]s: %[2]s\n",
	})
	sb.WriteString("\n")

	sb.WriteString(forgeRulesEnd + "\n")
	return sb.String()
}

// replaceForgeRules replaces the content between FORGE:START and FORGE:END markers; content outside the markers is preserved as-is.
// Thin wrapper over util.ReplaceMarkedSection (shared with skillgen's CLAUDE.md/AGENTS.md upsert).
//
// replaceForgeRules 替换 FORGE:START 与 FORGE:END 标记之间的内容，标记外的内容原样保留。
// util.ReplaceMarkedSection 的薄封装（与 skillgen 的 CLAUDE.md/AGENTS.md upsert 共享）。
func replaceForgeRules(content, newSection string) string {
	return util.ReplaceMarkedSection(content, newSection, forgeRulesStart, forgeRulesEnd)
}
