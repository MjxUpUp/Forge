package agentbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/hooks"
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
// 资产迁移）。merge 语义：command 非 forge 来源的条目（见 hooks.IsForgeHookCommand）
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
	// W0.2 batch 推广：每个 windsurf 事件收敛为一条 `forge hook batch` 单入口
	// （宿主逐条目拉起进程 → 每事件一次拉起）。映射表锁定 windsurf 事件 ↔
	// claude 事件 + matcher 组的对应关系；hook 级名册由 ForgeHookSpec 单一来源
	// 经 batch 内部现查（profile 档位门在 inner RunHook 逐 hook 生效）。
	windsurfEventBatch := []struct {
		windsurfEvent string
		claudeEvent   string
		matcher       string
	}{
		{"pre_write_code", "PreToolUse", "Write|Edit"},
		{"pre_run_command", "PreToolUse", "Bash"},
		{"post_write_code", "PostToolUse", "Write|Edit"},
		{"post_run_command", "PostToolUse", "Bash"},
		{"post_read_code", "PostToolUse", "Read|Skill|Agent|Grep|Glob"},
		// Cascade 没有 session_start：SessionStart 组挂 pre_user_prompt（每个
		// 会话首个 prompt 时触发）。详见上方历史注释。
		{"pre_user_prompt", "UserPromptSubmit", ""},
		// Cascade 没有 session_end：Stop 组挂 post_cascade_response——异步
		// post-hook，exit 2 无法阻断，task-verify/review-stop 在 windsurf 上
		// 是 advisory-only（如实文档化，不藏着）。
		{"post_cascade_response", "Stop", ""},
	}
	hooksMap := map[string][]windsurfHookEntry{}
	for _, m := range windsurfEventBatch {
		cmd := "forge hook batch --event " + m.claudeEvent
		if m.matcher != "" {
			cmd += " --matcher \"" + m.matcher + "\""
		}
		cmd += " --agent windsurf"
		hooksMap[m.windsurfEvent] = []windsurfHookEntry{{Command: cmd, ShowOutput: false}}
	}

	// W0.3 档位过滤：windsurf 名册硬编码、不走 ForgeHookSpecForProfile 的统一
	// 过滤，故在出口按 hook 名过滤——lite 下非白名单条目不进接线。运行时门
	// （runHook 档位秒过）仍是 plugin 渠道与遗漏路径的兜底。命令形态恒为
	// "forge hook <name> --agent windsurf"，第 3 段即 hook 名。
	for ev, entries := range hooksMap {
		kept := entries[:0]
		for _, e := range entries {
			// batch 单入口条目不受档位过滤——lite 的逐 hook 裁剪由 batch 内部
			// 的 inner RunHook 档位门生效（这里删掉 batch 条目会让 windsurf
			// 整个事件组静默失明）。
			if strings.Contains(e.Command, "forge hook batch") {
				kept = append(kept, e)
				continue
			}
			parts := strings.SplitN(e.Command, " ", 4)
			if len(parts) < 3 || hooks.ProfileAllowsHook(parts[2], hooks.ActiveProfile()) {
				kept = append(kept, e)
			}
		}
		hooksMap[ev] = kept
	}
	return map[string]any{"hooks": hooksMap}
}

// forgeRulesStart/End 是 windsurf global_rules.md 托管段的 HTML 注释标记——
// util.ForgeSectionStart/End（单一真相源）的别名（2026-09 代码普查 R3）。
const (
	forgeRulesStart = util.ForgeSectionStart
	forgeRulesEnd   = util.ForgeSectionEnd
)

// buildWindsurfSection 生成写入用户级 global_rules.md 的 forge 段（P3 指针化）。
// Cascade 对每个 workspace 都加载该文件——静态全局文件只能承载指针，激活判据
// 锚定 managed 会话横幅（[forge-session]，task-resume hook 输出），协议细节交回
// 受管通道（与 skillgen 的 buildUserPointerSection 同一契约；标准清单不再拷贝——
// 共享指令文件最多放指针，防多 harness 冲突与版本漂移）。
func buildWindsurfSection(input *TranslationInput) string {
	var sb strings.Builder

	sb.WriteString(forgeRulesStart + "\n\n")
	sb.WriteString("**This section is a Forge user-level global injection, loaded by Cascade in every workspace.**\n\n")
	sb.WriteString("**Activation rule (the only criterion):** follow Forge ONLY when this session shows the `[forge-session]` managed banner (or any forge hook output) at session start — that means the project is managed by forge; act on forge hook guidance as it appears. If you do not see the banner, ignore this section entirely and do not run forge commands.\n\n")
	sb.WriteString("- The project's own rules (project CLAUDE.md / AGENTS.md / its own harness protocol) ALWAYS take precedence over this section; if the project opted out (`.forge-decline` or `forge off`), forge has already yielded (`forge on` restores).\n")
	sb.WriteString("- Full quality protocol lives in forge's hook guidance and the `/forge-quality` skill surface; you do not need to memorize rules.\n")
	sb.WriteString("- Self-protection: this section and user-level assets (`~/.forge/`, agent configs) may only be changed via forge commands (`forge uninstall --restore` rolls back).\n")
	sb.WriteString(forgeRulesEnd + "\n")
	return sb.String()
}

// replaceForgeRules 替换 FORGE:START 与 FORGE:END 标记之间的内容，标记外的内容原样保留。
// util.ReplaceMarkedSection 的薄封装（与 skillgen 的 CLAUDE.md/AGENTS.md upsert 共享）。
func replaceForgeRules(content, newSection string) string {
	return util.ReplaceMarkedSection(content, newSection, forgeRulesStart, forgeRulesEnd)
}
