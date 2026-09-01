package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/userassets"
	"github.com/MjxUpUp/Forge/internal/util"
)

// embeddedHooks 把脚本名（不带 .sh 后缀）映射到其嵌入内容。它是 hook 名册的单一
// 真相源：WriteHookTemplates 的文件集与 HookNames() 都从它派生（名字加 ".sh" 后缀）。
var embeddedHooks = map[string]string{
	"auto-compile":        AutoCompileHook,
	"assertion-check":     AssertionCheckHook,
	"task-verify":         TaskVerifyHook,
	"review-stop":         ReviewStopHook,
	"task-guard":          TaskGuardHook,
	"freeze-guard":        FreezeGuardHook,
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

// EmbeddedContent returns the hook script content for the given name (e.g. auto-compile).
//
// EmbeddedContent 返回指定名字（如 auto-compile）对应的 hook 脚本内容。
// 命中时返回内容和 true。
func EmbeddedContent(name string) (string, bool) {
	content, ok := embeddedHooks[name]
	return content, ok
}

// HookEntry is a single hook command running under one matcher.
//
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

// HookMatcher groups the hook commands that share one tool-name matcher.
//
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
					{Type: "command", Command: "forge hook skill-trigger"},
					// #4-E 事中测试提醒：按会话计数非测试源码写入；>=3 且 0 测试
					// 写入时注入一次事实性 test-discipline 指引（任何测试写入即
					// 重置）。advisory——执法在 task-verify test-coverage 门禁；
					// 本 hook 只把修复提前到代码还热的时机。
					{Type: "command", Command: "forge hook test-nudge"},
				},
			},
			{
				// #5 审计盲区修复：Bash 未挂 tool-track，27.7k 次 Bash 调用在
				// toollog 零行（遵循度审计最大的数据窟窿——行为/hazard 分析对
				// 使用最多的工具结构性失明）。tool-track 记截断命令，与
				// Skill/Agent 同待遇；同 matcher 的 file-sentinel 不受影响——
				// 一个 matcher 下的 hook 各自独立运行。
				Matcher: "Bash",
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook file-sentinel"},
					{Type: "command", Command: "forge hook tool-track"},
					{Type: "command", Command: "forge hook skill-trigger"},
				},
			},
			{
				// 方案 C：matcher 从 Read 扩为 Read|Skill|Agent，让 toollog 审计也能记录
				// agent 加载了哪个 skill、派了哪类子 agent（dispatch 在 hook.go:6b 填 tool_input）。
				// 调研根因：DevWorkbench toollog 里 Skill 全是业务 skill，质量 skill（test-discipline/
				// tdd-cycle/implementation-discipline）0 触发——纯靠 agent 自觉必漏。记录 Skill/Agent
				// 让「质量 skill 是否被驱动」可追溯（与方案 A 的 blocking 驱动互补：A 强制触发，C 留痕审计）。
				// tool-track.sh 永远 PASS（非 scoring check），不阻塞；readsFilePath 副通道严格限定
				// Read，Skill/Agent 不污染 read-before-edit 日志。
				//
				// Grep|Glob（2026-08-23 文档-实现漂移修复）：CLAUDE.md 错误表建议门禁间
				// 「用 Read/Grep/Glob 探索」，但 Grep/Glob 此前不在任何 matcher、进不了
				// toollog——纯探索段落被 work-activity 计为零工作照样拦截（本日实测：Grep
				// 探索两次 BLOCKED，靠 Read 才放行）。补 matcher 后 toollog 记 Grep/Glob，
				// toolusage.ExploreCounts 才有数据可数；readsFilePath 副通道仍严格限定
				// Read——read-before-edit 的「改前必读」不因探索计数稀释。
				Matcher: "Read|Skill|Agent|Grep|Glob",
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook tool-track"},
				},
			},
		},
		"PreToolUse": []HookMatcher{
			{
				Matcher: "Write|Edit",
				Hooks: []HookEntry{
					// freeze-guard 排最前：`forge freeze` 激活时优先给出 freeze
					// 阻断原因，而非 task-guard 的告警/自保护判定（freeze 优先判定契约）。
					{Type: "command", Command: "forge hook freeze-guard"},
					{Type: "command", Command: "forge hook task-guard"},
					{Type: "command", Command: "forge hook assertion-check"},
					{Type: "command", Command: "forge hook read-before-edit"},
					{Type: "command", Command: "forge hook skill-trigger"},
					// conventions-profile 层 2 写入时刻注入（2026-08-28）：写源码文件前
					// 注入本仓库规范文件指针 + 同目录范例。advisory（emit 走 allow-with-detail
					// 的 additionalContext 通道，与 skill-trigger 同款——本 matcher 上已验证）。
					{Type: "command", Command: "forge hook conventions-write"},
				},
			},
			{
				Matcher: "Bash",
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook bash-guard"},
					{Type: "command", Command: "forge hook hazard-guard"},
					{Type: "command", Command: "forge hook skill-trigger"},
				},
			},
		},
		"Stop": []HookMatcher{
			{
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook task-verify"},
					{Type: "command", Command: "forge hook review-stop"},
					{Type: "command", Command: "forge hook skill-trigger"},
				},
			},
		},
		// #4-A 新事件（2026-08-22）。官方 31 事件名册里这两个补的是价值最高的
		// 缺口；其余未接事件刻意跳过（蓝图 cc-hooks-31events-wiring-plan.md）。
		// 宿主覆盖注（2026-08-22 #4-A 后续更新，spec-research4 跨宿主矩阵）：
		// PostToolUseFailure/SubagentStop 在 claude-code、cursor、copilot 存在
		// （后两家官方名册在列——2026-08-22 已接线）；codex 只接 SubagentStop
		// （其名册把工具失败折叠进 PostToolUse，无失败事件）；kimi/windsurf/
		// cline 的 translator 按白名单丢弃未知事件（kimi 的
		// BuildKimiPluginHooks 显式锁死 6 个既有事件——未知事件流进其 manifest
		// 会 schema 校验失败、静默杀掉全部 hook，即 dsh-win32 失败类），故此处
		// 接线对有能力的宿主是增量、对其余宿主是 no-op。
		"PostToolUseFailure": []HookMatcher{
			{
				// Bash：命令失败携带编译/测试启发式所需的错误文本。不加
				// PowerShell token：copilot 匹配前把 powershell 运行时工具映射为
				// Bash、cursor 名册无 PowerShell 工具——该 token 全宿主死码（复审
				// NIT-4，2026-08-22）。
				// Write/Edit 失败（罕见、权限类）信号太少。
				Matcher: "Bash",
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook failure-track"},
				},
			},
		},
		"SubagentStop": []HookMatcher{
			{
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook subagent-track"},
				},
			},
		},
		// SessionStart 链含 skill-trigger，但当前无 canonical skill 声明 SessionStart trigger
		// （MVP 4 condition + 6 dogfood skill 均未用此 event），故每次会话启动 LoadAll 扫所有
		// SKILL.md 后必然 0 命中。保留挂载是为未来 SessionStart 触发器（如会话开始即提示加载某
		// skill）预留接入点，避免届时再改 ForgeHookSpec 触发 plugin.json 重生成。
		// FORGE_SKILL_TRIGGER=0 可全局早返跳过 LoadAll（F8）。接受这点 hook 链延迟换取触发点完备性。
		"SessionStart": []HookMatcher{
			{
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook skill-scan"},
					{Type: "command", Command: "forge hook mcp-scan"},
					{Type: "command", Command: "forge hook init-suggest"},
					{Type: "command", Command: "forge hook task-resume"},
					{Type: "command", Command: "forge hook skill-trigger"},
					// conventions-profile 层 2 会话摘要（2026-08-28）：有档案注入 ≤15 行
					// 摘要（always-on 层保持极小），无档案且仓库已声明规范时每会话一次
					// 建档建议。与 init-suggest 同挂点（都是 SessionStart 检测+引导类）。
					{Type: "command", Command: "forge hook conventions-context"},
				},
			},
		},
		// PostCompact + UserPromptSubmit 构成 gap#2 的根治层（压缩后自动重注入
		// 完整接续上下文，不靠 agent 主动 forge task resume）。宿主覆盖（2026-08
		// 按官方文档核实，见 buildCodexHooks/cursorEventName）：claude-code 与
		// codex 两个 event 都接；cursor 接 UserPromptSubmit（映射
		// beforeSubmitPrompt）但没有 post-compaction 事件（preCompact 仅观察型，
		// 承载不了重注入契约）——cursor 在压缩场景靠 SessionStart 的 tl;dr tier 缓解。
		// windsurf 的 Cascade 名册同样没有 PostCompact，其 UserPromptSubmit 组
		// （resume-reinject/skill-trigger）刻意未接：没有 PostCompact 则 compact-resume
		// 永不置重注入标志，resume-reinject 挂上也恒静默——pre_user_prompt 通道改载
		// SessionStart 组（由 TestWindsurfWiringMirrorsClaudeSettings 的负向断言钉住）。
		// opencode 的 TS plugin 只接 tool-hook 入口、不读 ForgeHookSpec，故
		// SessionStart/Stop/UserPromptSubmit/PostCompact 四组在其上永不触发（已知缺口，
		// 登记于 opencode.go 头注释；压缩场景接续在可用处依赖 SessionStart tier）。
		"PostCompact": []HookMatcher{
			{
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook compact-resume"},
					// conventions-profile：压缩刚清掉上下文，摘要重注入是最便宜的定向恢复
					// （此事件恒注入、不吃 SessionStart 的每会话 marker）。cursor 无
					// PostCompact 事件——压缩场景在其上回落 SessionStart tier（见上方
					// 宿主覆盖注）。
					{Type: "command", Command: "forge hook conventions-context"},
				},
			},
		},
		"UserPromptSubmit": []HookMatcher{
			{
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook resume-reinject"},
					{Type: "command", Command: "forge hook skill-trigger"},
				},
			},
		},
	}
}

// GenerateUserSettings merges ForgeHookSpec into the user-level Claude settings (~/.claude/settings.json, respecting CLAUDE_CONFIG_DIR via ClaudeHome()).
//
// GenerateUserSettings 把 ForgeHookSpec 合并进 user-level Claude settings
// （~/.claude/settings.json，经 ClaudeHome() 尊重 CLAUDE_CONFIG_DIR）。merge 语义：
// json.RawMessage 保留用户其他顶层字段，hooks 段内用户自定义条目保留——只替换
// forge 来源条目——是 mergeForgeHooksIntoSettings 的薄路径封装。user-level 文件是
// settings.json（非 settings.local.json）：Claude Code 对每个项目都读这份全机器配置。
// 首次写入前经 userassets.BackupOriginal 备份（首次备份为回滚锚点；
// `forge uninstall --restore` 回滚）。
func GenerateUserSettings() error {
	home := ClaudeHome()
	if home == "" {
		return fmt.Errorf("cannot resolve Claude config home (CLAUDE_CONFIG_DIR unset and user home unavailable)")
	}
	// 检测自毒防线：DetectAgents 以"~/.claude 存在"判定"claude 已安装"。在
	// 没装 claude 的机器上创建该目录会让后续检测误接一个不存在的工具。
	// CLAUDE_CONFIG_DIR 显式设置时用户（或测试）已声明位置——写入；否则仅在
	// ~/.claude 已存在（claude 已安装）时写入。
	if os.Getenv("CLAUDE_CONFIG_DIR") == "" {
		if info, err := os.Stat(home); err != nil || !info.IsDir() {
			return nil
		}
	}
	if err := os.MkdirAll(home, 0755); err != nil {
		return fmt.Errorf("create Claude config dir: %w", err)
	}
	path := filepath.Join(home, "settings.json")
	if err := userassets.BackupOriginal(path); err != nil {
		return fmt.Errorf("backup user-level settings.json: %w", err)
	}
	return mergeForgeHooksIntoSettings(path)
}

// mergeForgeHooksIntoSettings 读 path 处的 settings 文件，用 json.RawMessage 保留
// 所有顶层字段（用户 env/model 等）——避免往返序列化改动用户字段格式——并**合并**
// hooks 段：从既有段中剥除 forge 来源条目（isForgeHookCommand），用户自定义条目
// 原样保留（未知字段不丢——类型化往返会静默剥掉它们），再把当前 ForgeHookSpec
// 条目按事件追加（同事件下用户条目在前、forge 在后）。先剥后追加使重生成幂等。
// 整段替换 hooks 会静默销毁用户自己的 hooks；整文件覆盖会丢失用户配置
// （1.2.0 回归，1.2.1 修）。文件不存在则新建；
// 非 NotExist 的读/解析错误原样返回（绝不静默覆盖读不出的用户配置）。
func mergeForgeHooksIntoSettings(path string) error {
	cfg := map[string]json.RawMessage{}
	if existing, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(existing, &cfg); err != nil {
			return fmt.Errorf("parse existing %s: %w", filepath.Base(path), err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}

	kept := map[string][]json.RawMessage{}
	if raw, ok := cfg["hooks"]; ok {
		var spec map[string][]json.RawMessage
		if err := json.Unmarshal(raw, &spec); err != nil {
			return fmt.Errorf("parse existing hooks section in %s: %w", filepath.Base(path), err)
		}
		kept, _ = stripForgeMatchersRaw(spec)
	}
	for event, matchers := range ForgeHookSpec() {
		raw, err := json.Marshal(matchers)
		if err != nil {
			return fmt.Errorf("marshal generated hooks: %w", err)
		}
		var raws []json.RawMessage
		if err := json.Unmarshal(raw, &raws); err != nil {
			return fmt.Errorf("reparse generated hooks: %w", err)
		}
		kept[event] = append(kept[event], raws...)
	}
	hooksJSON, err := json.Marshal(kept)
	if err != nil {
		return fmt.Errorf("marshal hooks: %w", err)
	}
	cfg["hooks"] = hooksJSON

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	return util.AtomicWrite(path, data, 0644)
}

// stripForgeMatchersRaw 从 Claude-Code 形嵌套 {event: [matcher]} spec 中移除 forge
// 来源的 hook 条目（isForgeHookCommand）。用户自定义条目保留原始字节（未知字段
// 不丢）；被掏空的 matcher/event 一并移除。镜像 agentbridge 的 raw strip helper——
// 因那些 helper 未导出且 hooks 不能 import agentbridge（agentbridge 已 import
// hooks，反向会成环）而在此复制。
func stripForgeMatchersRaw(spec map[string][]json.RawMessage) (map[string][]json.RawMessage, bool) {
	kept := make(map[string][]json.RawMessage, len(spec))
	removedAny := false
	for event, matchers := range spec {
		var keptMatchers []json.RawMessage
		for _, rawMatcher := range matchers {
			var probe struct {
				Hooks []json.RawMessage `json:"hooks"`
			}
			if err := json.Unmarshal(rawMatcher, &probe); err != nil || probe.Hooks == nil {
				// Unparseable or hooks-less matcher — user content, keep as-is.
				keptMatchers = append(keptMatchers, rawMatcher)
				continue
			}
			var keptEntries []json.RawMessage
			removed := false
			for _, rawEntry := range probe.Hooks {
				var cmd struct {
					Command string `json:"command"`
				}
				if err := json.Unmarshal(rawEntry, &cmd); err == nil && IsForgeHookCommand(cmd.Command) {
					removed = true
					removedAny = true
					continue
				}
				keptEntries = append(keptEntries, rawEntry)
			}
			if !removed {
				keptMatchers = append(keptMatchers, rawMatcher)
				continue
			}
			if len(keptEntries) == 0 {
				continue // matcher held only forge entries — drop it
			}
			// Mixed matcher: rebuild it, preserving the matcher's other fields
			// (matcher name, etc.) and the kept entries' raw bytes.
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(rawMatcher, &obj); err != nil {
				continue
			}
			entriesJSON, err := json.Marshal(keptEntries)
			if err != nil {
				continue
			}
			obj["hooks"] = entriesJSON
			rebuilt, err := json.Marshal(obj)
			if err != nil {
				continue
			}
			keptMatchers = append(keptMatchers, rebuilt)
		}
		if len(keptMatchers) > 0 {
			kept[event] = keptMatchers
		}
	}
	return kept, removedAny
}

// StripForgeHooks removes the forge hooks from projectDir/.claude/settings.local.json.
//
// StripForgeHooks 移除 projectDir/.claude/settings.local.json 的 forge hooks。project-level
// 路径约定的薄封装(拼 <projectDir>/.claude/settings.local.json);user-level 去重用
// StripForgeHooksUserLevel(直接定位 ClaudeHome 路径,正确处理 CLAUDE_CONFIG_DIR 自定义目录)。
func StripForgeHooks(projectDir string, keepEmpty bool) (changed bool, err error) {
	return StripForgeHooksAt(filepath.Join(projectDir, ".claude", "settings.local.json"), keepEmpty)
}

// StripForgeHooksAt removes the hooks sourced from ForgeHookSpec in the settings.local.json at the given path (entries whose command starts with forge hook or forge gate).
//
// StripForgeHooksAt 移除指定路径 settings.local.json 中 ForgeHookSpec 来源的 hooks
// （command 以"forge hook "或"forge gate "开头的条目）。当 forge plugin 在
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
// GenerateUserSettings 保持纯函数（永远写 hooks）。plugin 已装时,重复由命令层
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
	// Raw 剥离（与 merge 路径同一助手）：类型化 HookMatcher/HookEntry 往返会静默
	// 丢弃用户条目上的未知字段（per-hook timeout 等）——正是 merge 路径改用 raw
	// 处理要避免的损害。
	var hookSpec map[string][]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &hookSpec); err != nil {
		return false, fmt.Errorf("parse hooks: %w", err)
	}
	cleaned, removedAny := stripForgeMatchersRaw(hookSpec)
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
			return true, util.AtomicWrite(path, []byte("{}\n"), 0644)
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
	return true, util.AtomicWrite(path, out, 0644)
}

// IsForgeHookCommand reports whether a hook command was written by forge (any ForgeHookSpec command).
//
// IsForgeHookCommand 报告 hook command 是否来自 forge（ForgeHookSpec 写入的命令）。
// ForgeHookSpec 的命令都是"forge hook <name>"或"forge gate ..."。用户自定义 hook
// （如"npx prettier"/"./scripts/lint.sh"）不被识别为 forge 来源，StripForgeHooks 保留。
// 判定的单一真相源：cli（cleanup 剥 JSON 形态）与 agentbridge（各 agent 的 merge
// 路径）都消费本函数——曾经的三处逐字镜像已收敛于此（2026-09 代码普查 R1）。
func IsForgeHookCommand(cmd string) bool {
	return strings.HasPrefix(cmd, "forge hook ") ||
		strings.HasPrefix(cmd, "forge gate ") ||
		cmd == "forge hook" || cmd == "forge gate"
}

// WriteHookDeployStamp writes the hook-deploy grace marker.
//
// WriteHookDeployStamp 在 Forge 重写 hook 脚本副本前一刻写部署 grace marker
// （<dataDir>/stamps/hook-deploy，内容 "<epoch> <projectTag>"）。file-sentinel
// 的 CONFIG 分支读它：manifest drift 全部位于 .forge/hooks/ 且 marker 新鲜
// （<120s）且 project tag 匹配时，drift 视为 Forge 自身部署写入而非未授权改写
// ——2026-08-02 自伤事故（被监控的非 forge Bash 命令触发 hook 链，链上 forge
// 子进程 autoSync 恰好重写项目级 .forge/hooks/*.sh，整目录被 quarantine）。
// marker 写后刻意不删：grace 判定基于时间戳，与 task-complete grace stamp 同款。
// dataDir 在用户级（agent 可写——与 snapshot/.cfg 基线同一信任边界，可接受）；
// project tag 是廉价的防跨项目校验（FORGE_PROJECT_TAG 先例）。
func WriteHookDeployStamp(dataDir, projectTag string) error {
	stampsDir := filepath.Join(dataDir, "stamps")
	if err := os.MkdirAll(stampsDir, 0755); err != nil {
		return err
	}
	content := fmt.Sprintf("%d %s\n", time.Now().Unix(), projectTag)
	return util.AtomicWrite(filepath.Join(stampsDir, "hook-deploy"), []byte(content), 0644)
}

// WriteHookTemplates writes the embedded hook scripts into .forge/hooks/.
//
// WriteHookTemplates 把嵌入的 hook 脚本写入 .forge/hooks/。
func WriteHookTemplates(forgeDir string) error {
	hooksDir := filepath.Join(forgeDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return err
	}

	fileHooks := make(map[string]string, len(embeddedHooks))
	for name, content := range embeddedHooks {
		fileHooks[name+".sh"] = content
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
		// 刻意不用 AtomicWrite：Windows 上 rename 到一个【正在被 bash 执行】的
		// 脚本上会 Access Denied——而本重写就在 autoSync 里，恰会被运行中的
		// hook 脚本调 `forge data-dir` 触发（2026-08-29 e2e 回归）。截断式原地
		// 写才是"自改写可执行副本"的正确原语。
		if err := os.WriteFile(path, []byte(content), 0755); err != nil {
			return fmt.Errorf("failed to write hook %s: %w", name, err)
		}
	}
	return nil
}

// HookNames returns the list of hook script file names owned by Forge.
//
// HookNames 返回 Forge 接管的 hook 脚本文件名列表。从 embeddedHooks（hook 名册的
// 单一真相源）加 .sh 后缀派生，排序保证确定性——增删 hook 只需改 embeddedHooks。
// 注意：输出为字母序（原字面量是插入序），cursor 等生成文件中的 hook 列表展示序
// 会随之变化——纯展示序，无顺序敏感消费方。
func HookNames() []string {
	names := make([]string, 0, len(embeddedHooks))
	for name := range embeddedHooks {
		names = append(names, name+".sh")
	}
	slices.Sort(names)
	return names
}
