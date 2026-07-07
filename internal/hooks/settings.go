package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// embeddedHooks maps script names (without .sh suffix) to their embedded content.
var embeddedHooks = map[string]string{
	"auto-compile":        AutoCompileHook,
	"assertion-check":     AssertionCheckHook,
	"task-verify":         TaskVerifyHook,
	"review-stop":         ReviewStopHook,
	"task-guard":          TaskGuardHook,
	"bash-guard":          BashGuardHook,
	"hazard-guard":        HazardGuardHook,
	"file-sentinel":       FileSentinelHook,
	"tool-track":          ToolTrackHook,
	"skill-scan":          SkillScanHook,
	"init-suggest":        InitSuggestHook,
	"workflow-test-guard": WorkflowTestGuardHook,
}

// EmbeddedContent returns the hook script content for the given name
// (e.g. "auto-compile"). Returns the content and true if found.
func EmbeddedContent(name string) (string, bool) {
	content, ok := embeddedHooks[name]
	return content, ok
}

// ForgeHookSpec is the single source of truth for which forge hooks run at
// which Claude Code tool event. It returns the hooks object exactly as it
// appears under the "hooks" key of .claude/settings.local.json. The plugin-pack
// generator (internal/agentbridge/pluginpack.go) writes the SAME object as the
// hooks field of plugins/forge/.claude-plugin/plugin.json, so `claude plugin install forge` produces
// byte-identical hook wiring to `forge init` — one shared payload, per-host
// thin manifests pointing at it. Any
// wiring change here propagates to both paths; do not duplicate the
// matcher→hook roster elsewhere. Drift is guarded by
// TestPluginPack_HooksMirrorSettings (plugin pack) and TestOpencodePluginWiring
// (opencode's TS roster mirrors this set).
// HookEntry is one hook command run under a matcher. Exported so other packages
// (internal/agentbridge codex/cursor translators) can iterate the spec to derive
// their native hook formats from ForgeHookSpec — the single source of truth —
// instead of hand-maintaining parallel copies that drift.
type HookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// HookMatcher groups hook commands sharing a tool-name matcher.
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
				Matcher: "Read",
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
					{Type: "command", Command: "forge gate --current --silent"},
					{Type: "command", Command: "forge hook task-verify"},
					{Type: "command", Command: "forge hook review-stop"},
				},
			},
		},
		"SessionStart": []HookMatcher{
			{
				Hooks: []HookEntry{
					{Type: "command", Command: "forge hook skill-scan"},
					{Type: "command", Command: "forge hook init-suggest"},
				},
			},
		},
	}
}

// GenerateSettings creates/updates .claude/settings.local.json with hook integration.
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

// StripForgeHooks 移除 .claude/settings.local.json 中 ForgeHookSpec 来源的 hooks
// （command 以 "forge hook " 或 "forge gate " 开头的条目）。当 forge plugin 在
// user-level 已装，plugin 的 plugin.json 已注册同样的 ForgeHookSpec（全机器所有项目），
// project-level 保留它们只会让 Claude Code 双重执行同一 hook。
//
// 仅删 forge 来源的 hook 条目，保留用户自定义 hooks（command 不以 forge hook/forge gate
// 开头）。移除所有 forge hooks 后（hooks 字段空且无其他顶层字段，即整文件只剩 forge 来源）：
//   - keepEmpty=true（自动路径：init-suggest SessionStart / autoSync / init·sync）→ 写空对象
//     {} 保留文件壳,绝不删——settings.local.json 是 gitignored 个人配置,用户常主动放置/
//     正在编辑,forge 在自动 dedupe 时静默删整个文件是用户痛点。空 {} 对 Claude Code 无害。
//   - keepEmpty=false（手动 forge plugin dedupe,显式清理）→ 删除整个文件,恢复无 project 配置。
// hooks 字段空但有用户自定义顶层字段 → 写回（无 hooks）。仍有用户自定义 hooks → 写回（仅用户 hooks）。
//
// 幂等：无 settings.local.json / 无 hooks 字段 / 无 forge hooks 时均 no-op（changed=false）。
// 返回 changed 表示是否实际改动了文件（供 forge plugin dedupe 决定是否输出提示）。
// GenerateSettings 保持纯函数（永远写 hooks）。plugin 已装时,project-level 重复由命令层
// （init/sync 的 dedupeProjectLevelIfPlugin,所有写入后统一调用）清理——避免单元测试依赖
// 全局 IsClaudePluginInstalled 状态。
func StripForgeHooks(projectDir string, keepEmpty bool) (changed bool, err error) {
	path := filepath.Join(projectDir, ".claude", "settings.local.json")
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

// WriteHookTemplates writes embedded hook scripts to .forge/hooks/.
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
		"bash-guard.sh":          BashGuardHook,
		"hazard-guard.sh":        HazardGuardHook,
		"file-sentinel.sh":       FileSentinelHook,
		"tool-track.sh":          ToolTrackHook,
		"skill-scan.sh":          SkillScanHook,
		"init-suggest.sh":        InitSuggestHook,
		"workflow-test-guard.sh": WorkflowTestGuardHook,
	}

	// Remove stale hook scripts no longer in the embedded set. This directory is
	// Forge-managed (populated only by WriteHookTemplates), so any .sh not in the
	// current set is leftover from a prior version — e.g. read-check.sh /
	// scope-guard.sh / clone-check.sh after they were sunk to skill text, or
	// experience-check.sh after deletion. Without this, removed hooks linger on
	// disk forever (WriteHookTemplates otherwise only writes the current set).
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

// HookNames returns the list of hook script filenames managed by Forge.
func HookNames() []string {
	return []string{
		"auto-compile.sh",
		"assertion-check.sh",
		"task-verify.sh",
		"review-stop.sh",
		"task-guard.sh",
		"bash-guard.sh",
		"hazard-guard.sh",
		"file-sentinel.sh",
		"tool-track.sh",
		"skill-scan.sh",
		"init-suggest.sh",
		"workflow-test-guard.sh",
	}
}
