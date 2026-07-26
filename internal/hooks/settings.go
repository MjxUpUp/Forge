package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// embeddedHooks 把脚本名（不带 .sh 后缀）映射到其嵌入内容。
var embeddedHooks = map[string]string{
	"auto-compile":        AutoCompileHook,
	"assertion-check":     AssertionCheckHook,
	"task-verify":         TaskVerifyHook,
	"review-stop":         ReviewStopHook,
	"task-guard":          TaskGuardHook,
	"read-before-edit":    ReadBeforeEditHook,
	"bash-guard":          BashGuardHook,
	"hazard-guard":        HazardGuardHook,
	"file-sentinel":       FileSentinelHook,
	"tool-track":          ToolTrackHook,
	"skill-scan":          SkillScanHook,
	"mcp-scan":            McpScanHook,
	"init-suggest":        InitSuggestHook,
	"task-resume":         TaskResumeHook,
	"compact-resume":      CompactResumeHook,
	"resume-reinject":     ResumeReinjectHook,
	"workflow-test-guard": WorkflowTestGuardHook,
}

// EmbeddedContent 返回指定名字（如 auto-compile）对应的 hook 脚本内容。
// 命中时返回内容和 true。
func EmbeddedContent(name string) (string, bool) {
	content, ok := embeddedHooks[name]
	return content, ok
}

// ForgeHookSpec 是哪些 forge hook 跑在哪个 Claude Code tool event 上的
// single source of truth。返回的 hooks 对象与 .claude/settings.local.json
// 中 hooks key 下的内容完全一致。plugin-pack 生成器
// （internal/agentbridge/pluginpack.go）把同一对象写入
// plugins/forge/.claude-plugin/plugin.json 的 hooks 字段，故
// `claude plugin install forge` 与 `forge init` 产生 byte-identical 的 hook
// 接线——一份共享 payload，各 host 仅用薄 manifest 指向它。任何接线
// 变更都同时传播到两条路径；不要在别处重复维护 matcher→hook 名册。
// drift 由 TestPluginPack_HooksMirrorSettings（plugin pack）与
// TestOpencodePluginWiring（opencode 的 TS 名册镜像此集合）守卫。
//
// HookEntry 是跑在一个 matcher 下的一条 hook command。导出以便其他包
// （internal/agentbridge codex/cursor translator）能遍历该 spec、
// 从 ForgeHookSpec 这一 single source of truth 派生各自的 native hook
// 格式，而非手维护易漂移的并行副本。
type HookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// HookMatcher 把共享同一 tool-name matcher 的 hook command 聚合在一起。
type HookMatcher struct {
	Matcher string      `json:"matcher,omitempty"`
	Hooks   []HookEntry `json:"hooks"`
}

func ForgeHookSpec() map[string][]HookMatcher {
	return map[string][]HookMatcher{
		"PostToolUse": []HookMatcher{
			{
				Matcher: "Write|Edit",
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook auto-compile"},
					{Type: "command", Command: "forge hook workflow-test-guard"},
				},
			},
			{
				Matcher: "Bash",
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook file-sentinel"},
				},
			},
			{
				// 方案 C：matcher 从 Read 扩为 Read|Skill|Agent，让 toollog 审计也能记录
				// agent 加载了哪个 skill、派了哪类子 agent（dispatch 在 hook.go:6b 填 tool_input）。
				// 调研根因：DevWorkbench toollog 里 Skill 全是业务 skill，质量 skill（test-discipline/
				// tdd-cycle/implementation-discipline）0 触发——纯靠 agent 自觉必漏。记录 Skill/Agent
				// 让"质量 skill 是否被驱动"可追溯（与方案 A 的 blocking 驱动互补：A 强制触发，C 留痕审计）。
				// tool-track.sh 永远 PASS（非 scoring check），不阻塞；readsFilePath 副通道严格限定
				// Read，Skill/Agent 不污染 read-before-edit 日志。
				Matcher: "Read|Skill|Agent",
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook tool-track"},
				},
			},
		},
		"PreToolUse": []HookMatcher{
			{
				Matcher: "Write|Edit",
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook task-guard"},
					{Type: "command", Command: "forge hook assertion-check"},
					{Type: "command", Command: "forge hook read-before-edit"},
				},
			},
			{
				Matcher: "Bash",
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook bash-guard"},
					{Type: "command", Command: "forge hook hazard-guard"},
				},
			},
		},
		"Stop": []HookMatcher{
			{
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook task-verify"},
					{Type: "command", Command: "forge hook review-stop"},
				},
			},
		},
		"SessionStart": []HookMatcher{
			{
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook skill-scan"},
					{Type: "command", Command: "forge hook mcp-scan"},
					{Type: "command", Command: "forge hook init-suggest"},
					{Type: "command", Command: "forge hook task-resume"},
				},
			},
		},
		// PostCompact + UserPromptSubmit 构成 gap#2 的 claude-code 根治层（压缩后自动重注入
		// 完整接续上下文，不靠 agent 主动 forge task resume）。两 event 都是 Claude Code 特有
		// lifecycle：codex/cursor 在 buildCodexHooks/buildCursorHooks 过滤，opencode 的 TS plugin
		// 不读 ForgeHookSpec（只 wire tool.execute.before 等价），故此链只对 claude-code 生效——
		// 接受的边界（其余 host 靠 SessionStart 的 tl;dr tier 缓解）。
		"PostCompact": []HookMatcher{
			{
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook compact-resume"},
				},
			},
		},
		"UserPromptSubmit": []HookMatcher{
			{
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook resume-reinject"},
				},
			},
		},
	}
}

// GenerateSettings 创建/更新 .claude/settings.local.json，写入 hook 集成。
// 合并式:读现有文件,保留用户自定义顶层字段(env/model/enabledPlugins 等),
// 只把 hooks 段更新为 ForgeHookSpec。覆盖整个文件会丢失用户配置——plugin-dedupe
// 场景下尤其致命:init 写 hooks → dedupe 删 forge hooks → 若非 hooks 字段没保留,
// 文件被删、用户 env/model 丢失(1.2.0 回归,1.2.1 修)。
func GenerateSettings(projectDir string) error {
	claudeDir := filepath.Join(projectDir, ".claude")
	os.MkdirAll(claudeDir, 0755)
	path := filepath.Join(claudeDir, "settings.local.json")

	// 读现有 settings.local.json,保留所有顶层字段(用户 env/model 等)。用
	// json.RawMessage 避免往返序列化改动用户字段格式。
	cfg := map[string]json.RawMessage{}
	if existing, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(existing, &cfg); err != nil {
			return fmt.Errorf("parse existing settings.local.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read settings.local.json: %w", err)
	}

	hooksJSON, err := json.Marshal(ForgeHookSpec())
	if err != nil {
		return fmt.Errorf("marshal hooks: %w", err)
	}
	cfg["hooks"] = hooksJSON

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// StripForgeHooks 移除 projectDir/.claude/settings.local.json 的 forge hooks。project-level
// 路径约定的薄封装(拼 <projectDir>/.claude/settings.local.json);user-level 去重用
// StripForgeHooksUserLevel(直接定位 ClaudeHome 路径,正确处理 CLAUDE_CONFIG_DIR 自定义目录)。
func StripForgeHooks(projectDir string, keepEmpty bool) (changed bool, err error) {
	return StripForgeHooksAt(filepath.Join(projectDir, ".claude", "settings.local.json"), keepEmpty)
}

// StripForgeHooksAt 移除指定路径 settings.local.json 中 ForgeHookSpec 来源的 hooks
// （command 以 "forge hook " 或 "forge gate " 开头的条目）。当 forge plugin 在
// user-level 已装，plugin 的 plugin.json 已注册同样的 ForgeHookSpec（全机器所有项目），
// 保留它们只会让 Claude Code 双重执行同一 hook。project-level(dir/.claude/...) 与
// user-level(ClaudeHome/settings.local.json) 共用本实现,只差定位路径。
//
// 仅删 forge 来源的 hook 条目，保留用户自定义 hooks（command 不以 forge hook/forge gate
// 开头）。移除所有 forge hooks 后（hooks 字段空且无其他顶层字段，即整文件只剩 forge 来源）：
//   - keepEmpty=true（自动路径：init-suggest SessionStart / autoSync / init·sync / user-level
//     全部场景）→ 写空对象 {} 保留文件壳,绝不删——settings.local.json 是 gitignored 个人配置,
//     用户常主动放置/正在编辑,forge 在自动 dedupe 时静默删整个文件是用户痛点。空 {} 对
//     Claude Code 无害。user-level 始终走此分支(StripForgeHooksUserLevel 固定 keepEmpty=true,
//     用户全局配置绝不删)。
//   - keepEmpty=false（手动 forge plugin dedupe project-level,显式清理）→ 删除整个文件,
//     恢复无 project 配置。
//
// hooks 字段空但有用户自定义顶层字段 → 写回（无 hooks）。仍有用户自定义 hooks → 写回（仅用户 hooks）。
//
// 幂等：无 settings.local.json / 无 hooks 字段 / 无 forge hooks 时均 no-op（changed=false）。
// 返回 changed 表示是否实际改动了文件（供 forge plugin dedupe 决定是否输出提示）。
// GenerateSettings 保持纯函数（永远写 hooks）。plugin 已装时,重复由命令层
// （init/sync 的 dedupeProjectLevelIfPlugin + plugin dedupe 的 runPluginDedupe,所有写入后
// 统一调用,覆盖 project-level + user-level）清理——避免单元测试依赖全局 IsClaudePluginInstalled 状态。
func StripForgeHooksAt(path string, keepEmpty bool) (changed bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read settings.local.json: %w", err)
	}
	// 用 json.RawMessage 保留未知顶层字段，只重写 hooks。
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		return false, fmt.Errorf("parse settings.local.json: %w", err)
	}
	hooksRaw, hasHooks := settings["hooks"]
	if !hasHooks {
		return false, nil
	}
	var hookSpec map[string][]HookMatcher
	if err := json.Unmarshal(hooksRaw, &hookSpec); err != nil {
		return false, fmt.Errorf("parse hooks: %w", err)
	}
	cleaned := make(map[string][]HookMatcher)
	removedAny := false
	for event, matchers := range hookSpec {
		var keptMatchers []HookMatcher
		for _, m := range matchers {
			var keptHooks []HookEntry
			for _, h := range m.Hooks {
				if isForgeHookCommand(h.Command) {
					removedAny = true
					continue
				}
				keptHooks = append(keptHooks, h)
			}
			if len(keptHooks) > 0 {
				m.Hooks = keptHooks
				keptMatchers = append(keptMatchers, m)
			}
		}
		if len(keptMatchers) > 0 {
			cleaned[event] = keptMatchers
		}
	}
	if !removedAny {
		return false, nil
	}
	if len(cleaned) > 0 {
		hooksJSON, mErr := json.Marshal(cleaned)
		if mErr != nil {
			return false, fmt.Errorf("marshal cleaned hooks: %w", mErr)
		}
		settings["hooks"] = hooksJSON
	} else {
		delete(settings, "hooks")
	}
	if len(settings) == 0 {
		// keepEmpty=true: 自动路径（init-suggest / autoSync / init·sync）——保留文件壳,写 {}。
		// keepEmpty=false: 手动 forge plugin dedupe（显式清理）——删空文件。
		if keepEmpty {
			return true, os.WriteFile(path, []byte("{}\n"), 0644)
		}
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("remove empty settings.local.json: %w", err)
		}
		return true, nil
	}
	out, mErr := json.MarshalIndent(settings, "", "  ")
	if mErr != nil {
		return false, fmt.Errorf("marshal settings: %w", mErr)
	}
	return true, os.WriteFile(path, out, 0644)
}

// isForgeHookCommand 报告 hook command 是否来自 forge（ForgeHookSpec 写入的命令）。
// ForgeHookSpec 的命令都是 "forge hook <name>" 或 "forge gate ..."。用户自定义 hook
// （如 "npx prettier" / "./scripts/lint.sh"）不被识别为 forge 来源，StripForgeHooks 保留。
func isForgeHookCommand(cmd string) bool {
	return strings.HasPrefix(cmd, "forge hook ") ||
		strings.HasPrefix(cmd, "forge gate ") ||
		cmd == "forge hook" || cmd == "forge gate"
}

// WriteHookTemplates 把嵌入的 hook 脚本写入 .forge/hooks/。
func WriteHookTemplates(forgeDir string) error {
	hooksDir := filepath.Join(forgeDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return err
	}

	fileHooks := map[string]string{
		"auto-compile.sh":        AutoCompileHook,
		"assertion-check.sh":     AssertionCheckHook,
		"task-verify.sh":         TaskVerifyHook,
		"review-stop.sh":         ReviewStopHook,
		"task-guard.sh":          TaskGuardHook,
		"read-before-edit.sh":    ReadBeforeEditHook,
		"bash-guard.sh":          BashGuardHook,
		"hazard-guard.sh":        HazardGuardHook,
		"file-sentinel.sh":       FileSentinelHook,
		"tool-track.sh":          ToolTrackHook,
		"skill-scan.sh":          SkillScanHook,
		"mcp-scan.sh":            McpScanHook,
		"init-suggest.sh":        InitSuggestHook,
		"task-resume.sh":         TaskResumeHook,
		"compact-resume.sh":      CompactResumeHook,
		"resume-reinject.sh":     ResumeReinjectHook,
		"workflow-test-guard.sh": WorkflowTestGuardHook,
	}

	// 清理已不在嵌入集合内的 stale hook 脚本。此目录由 Forge 接管
	// （仅由 WriteHookTemplates 写入），故任何不在当前集合的 .sh 都是
	// 旧版本残留——例如下沉到 skill 文本后的 read-check.sh /
	// scope-guard.sh / clone-check.sh，或删除后的 experience-check.sh。
	// 不清理则被移除的 hook 永远留在磁盘上
	// （WriteHookTemplates 否则只写当前集合，不动旧文件）。
	keep := make(map[string]bool, len(fileHooks))
	for name := range fileHooks {
		keep[name] = true
	}
	if entries, err := os.ReadDir(hooksDir); err == nil {
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".sh") || keep[name] {
				continue
			}
			os.Remove(filepath.Join(hooksDir, name))
		}
	}

	for name, content := range fileHooks {
		path := filepath.Join(hooksDir, name)
		if err := os.WriteFile(path, []byte(content), 0755); err != nil {
			return fmt.Errorf("failed to write hook %s: %w", name, err)
		}
	}
	return nil
}

// HookNames 返回 Forge 接管的 hook 脚本文件名列表。
func HookNames() []string {
	return []string{
		"auto-compile.sh",
		"assertion-check.sh",
		"task-verify.sh",
		"review-stop.sh",
		"task-guard.sh",
		"read-before-edit.sh",
		"bash-guard.sh",
		"hazard-guard.sh",
		"file-sentinel.sh",
		"tool-track.sh",
		"skill-scan.sh",
		"mcp-scan.sh",
		"init-suggest.sh",
		"task-resume.sh",
		"compact-resume.sh",
		"resume-reinject.sh",
		"workflow-test-guard.sh",
	}
}
