package agentbridge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// KimiTranslator wires forge hooks into kimi-code's user-level config.toml.
//
// kimi-code has no plugin marketplace dependency for forge: the committed
// .kimi-plugin/plugin.json at the repo root registers the full hook set when installed
// via /plugins install. Without the plugin, lifecycle hooks live in the [[hooks]] array
// of $KIMI_CODE_HOME/config.toml (~/.kimi-code/config.toml) — written by this translator.
// When the plugin is installed, Translate strips the config.toml section instead
// (plugin wins, no double-run). Both paths are user-level and machine-wide — like the
// Claude Code plugin model.
//
// The kimi hook protocol differs from Claude Code's in two ways, both handled at the
// `forge hook <name> --agent kimi` layer (internal/cli/hook.go, hook_normalize.go):
//   - stdin: Claude-shaped except prompt (content-block array) and tool_output (string) —
//     normalized by kimiNormalize.
//   - result: exit 0 = allow (stdout text → context), exit 2 = block (stderr = reason) —
//     rendered by emitKimiOutput; every generated command carries `--agent kimi`.
//
// Merge strategy: config.toml always carries the user's own model/provider/permission
// settings, so whole-file overwrite is out of the question; and the project has no TOML
// dependency (vendored modules) — so the generated entries live inside a
// `# FORGE:START` / `# FORGE:END` marked section, upserted idempotently like the AGENTS.md
// marker contract (internal/skillgen/claudemd.go). TOML allows [[hooks]] array-of-tables
// entries to appear anywhere in the file, so appending the section is always valid.
//
// KimiTranslator 把 forge hook 接线进 kimi-code 的 user-level config.toml。
//
// kimi-code 对 forge 无 plugin marketplace 依赖：仓库根提交的
// .kimi-plugin/plugin.json 经 /plugins install 安装即注册全部 hook。未装 plugin 时，
// lifecycle hook 写在 $KIMI_CODE_HOME/config.toml（~/.kimi-code/config.toml）的
// [[hooks]] 数组——由本 translator 写入。plugin 已装时 Translate 改为剥除
// config.toml 标记段（plugin 优先，不双跑）。两条路径都是 user-level 全机器生效——
// 与 Claude Code plugin 模型同类：hook 在每个项目触发，forge 的 global hook
// （init-suggest/mcp-scan/skill-scan）以及项目级 hook 在非 forge 项目的
// allow-and-exit 行为正是为此设计。
//
// kimi 的 hook 协议与 Claude Code 有两处差异，都在
// `forge hook <name> --agent kimi` 层处理（internal/cli/hook.go、hook_normalize.go）：
//   - stdin：与 Claude 同构，除 prompt（content-block 数组）与 tool_output（字符串）
//     ——由 kimiNormalize 归一化。
//   - 结果：exit 0 = 放行（stdout 文本进上下文），exit 2 = 阻断（stderr = 原因）
//     ——由 emitKimiOutput 渲染；每条生成的 command 都带 `--agent kimi`。
//
// 合并策略：config.toml 必含用户自己的 model/provider/permission 配置，整文件覆盖
// 不可行；项目也没有 TOML 依赖（vendored modules）——故生成的条目放在
// `# FORGE:START` / `# FORGE:END` 标记段内，按 AGENTS.md 标记段契约
// （internal/skillgen/claudemd.go）幂等 upsert。TOML 允许 [[hooks]] 数组表条目
// 出现在文件任意位置，故追加该段永远合法。
type KimiTranslator struct{}

const (
	kimiMarkStart = "# FORGE:START"
	kimiMarkEnd   = "# FORGE:END"
)

func (t *KimiTranslator) AgentType() AgentType {
	return AgentKimi
}

func (t *KimiTranslator) Translate(projectDir string, input *TranslationInput) error {
	// Plugin wins: when forge is installed as a kimi plugin (/plugins install), its
	// manifest already registers every hook machine-wide — the config.toml section
	// would double-run every hook (same dedupe philosophy as claude-code's plugin vs
	// settings.local.json, internal/hooks/plugin_detect.go). Strip it and stop.
	//
	// Plugin 优先：forge 已作为 kimi plugin 安装（/plugins install）时，其 manifest
	// 已在全机器注册全部 hook——config.toml 标记段会让每个 hook 双跑（与
	// claude-code 的 plugin vs settings.local.json 同款 dedupe 哲学，见
	// internal/hooks/plugin_detect.go）。剥除标记段后返回。
	if IsKimiPluginInstalled() {
		_, err := StripKimiHooks()
		return err
	}
	path, err := KimiConfigPath()
	if err != nil {
		return fmt.Errorf("kimi: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("kimi: failed to create config dir: %w", err)
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("kimi: failed to read config.toml: %w", err)
	}
	merged, err := upsertKimiSection(string(existing), BuildKimiHooksTOML())
	if err != nil {
		return err
	}
	if merged == string(existing) {
		return nil // already up to date — idempotent no-op
	}
	if err := os.WriteFile(path, []byte(merged), 0644); err != nil {
		return fmt.Errorf("kimi: failed to write config.toml: %w", err)
	}
	return nil
}

// KimiConfigHome resolves kimi-code's config home: $KIMI_CODE_HOME when set,
// otherwise ~/.kimi-code (see kimi-code docs, config-files).
//
// KimiConfigHome 解析 kimi-code 的 config home：设了 $KIMI_CODE_HOME 用它，
// 否则 ~/.kimi-code（见 kimi-code 文档 config-files）。
func KimiConfigHome() (string, error) {
	if h := os.Getenv("KIMI_CODE_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user home: %w", err)
	}
	return filepath.Join(home, ".kimi-code"), nil
}

// KimiConfigPath resolves the config.toml path inside KimiConfigHome.
//
// KimiConfigPath 解析 KimiConfigHome 下的 config.toml 路径。
func KimiConfigPath() (string, error) {
	home, err := KimiConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "config.toml"), nil
}

// BuildKimiHooksTOML derives the [[hooks]] TOML block from hooks.ForgeHookSpec — the
// single source of truth shared with settings.local.json, the plugin pack, and every
// other translator (codex/cursor/...). kimi supports the full event set forge uses
// (PreToolUse/PostToolUse/Stop/SessionStart/PostCompact/UserPromptSubmit) and its matcher
// is a regex against the tool name, with tool names identical to Claude Code's
// (Read/Write/Edit/Bash/Skill/Agent) — so matchers migrate verbatim. Events are sorted
// for deterministic output (map iteration order would otherwise break idempotency and
// golden tests). TestKimiWiringMirrorsClaudeSettings guards command-set parity.
//
// BuildKimiHooksTOML 从 hooks.ForgeHookSpec 派生 [[hooks]] TOML 块——该 spec 是与
// settings.local.json、plugin pack 及其他 translator（codex/cursor/...）共享的
// 单一真相源。kimi 支持 forge 用到的全部事件（PreToolUse/PostToolUse/Stop/
// SessionStart/PostCompact/UserPromptSubmit），其 matcher 是针对工具名的正则，
// 且工具名与 Claude Code 一致（Read/Write/Edit/Bash/Skill/Agent）——故 matcher
// 原样迁移。事件排序保证输出确定（否则 map 迭代顺序会破坏幂等与 golden 测试）。
// TestKimiWiringMirrorsClaudeSettings 守卫命令集对等。
func BuildKimiHooksTOML() string {
	spec := hooks.ForgeHookSpec()
	events := make([]string, 0, len(spec))
	for ev := range spec {
		events = append(events, ev)
	}
	sort.Strings(events)

	var b strings.Builder
	for _, ev := range events {
		for _, m := range spec[ev] {
			for _, entry := range m.Hooks {
				b.WriteString("[[hooks]]\n")
				fmt.Fprintf(&b, "event = %s\n", tomlBasicString(ev))
				if m.Matcher != "" {
					fmt.Fprintf(&b, "matcher = %s\n", tomlBasicString(m.Matcher))
				}
				fmt.Fprintf(&b, "command = %s\n", tomlBasicString(kimiCommand(entry.Command)))
				fmt.Fprintf(&b, "timeout = %d\n", kimiTimeout(entry.Command))
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// kimiCommand rewrites a spec command for kimi: `forge hook <name>` gains `--agent kimi`
// so runHook selects the kimi stdin normalizer and the kimi output protocol (exit codes,
// not the Claude JSON envelope). The --agent flag form (not FORGE_HOOK_AGENT env) follows
// the windsurf precedent — Windows cmd cannot parse `ENV=val cmd`.
//
// kimiCommand 把 spec command 改写为 kimi 版：`forge hook <name>` 追加 `--agent kimi`，
// 让 runHook 选择 kimi 的 stdin normalizer 与输出协议（退出码而非 Claude JSON
// envelope）。用 --agent flag（而非 FORGE_HOOK_AGENT env）遵循 windsurf 先例——
// Windows cmd 无法解析 `ENV=val cmd`。
func kimiCommand(cmd string) string {
	if strings.HasPrefix(cmd, "forge hook ") {
		return cmd + " --agent kimi"
	}
	return cmd
}

// kimiTimeout no longer serves a compile workload: auto-compile was demoted to
// a pure advisory in v0.25 (it never invokes a compiler — it only forks git to
// check whether source was touched), so the original "a full project compile
// can exceed 30s" rationale is gone. The 60s entry is retained as harmless
// headroom: kimi fails open on hook timeout, so a larger budget costs nothing
// but a slightly later advisory on pathologically slow machines (Windows
// process-spawn storms), while a timeout would silently drop the reminder.
//
// kimiTimeout 不再为编译场景服务：auto-compile 自 v0.25 起降为纯提醒（不调编译器，
// 只 fork git 判断是否触及源码），原"全量项目编译可能超过 30s"的理由已失效。
// 保留 60s 是无害余量：kimi 超时 fail-open，更大预算的代价只是极端慢机器
// （Windows 进程创建风暴）上提醒晚到一点，而超时会让提醒静默丢失。
func kimiTimeout(cmd string) int {
	if strings.HasPrefix(cmd, "forge hook auto-compile") {
		return 60
	}
	return 30
}

// tomlBasicString renders s as a TOML basic string (only backslash and double quote can
// appear in our event/matcher/command values and need escaping).
//
// tomlBasicString 把 s 渲染为 TOML basic string（我们的 event/matcher/command 值里
// 只有反斜杠与双引号可能出现并需要转义）。
func tomlBasicString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

// upsertKimiSection replaces the marked forge section in content, or appends it when no
// markers exist. Everything outside the markers (the user's own config) is preserved
// byte-for-byte; an unchanged result means Translate can skip the write. Unpaired or
// inverted markers are reported as corruption instead of guessing — the region between an
// orphaned START and a later END would be user config (model/provider/API keys), and
// replacing it would be data loss.
//
// upsertKimiSection 替换 content 中的 forge 标记段，无标记时追加。标记外的内容
// （用户自己的配置）逐字节保留；结果不变意味着 Translate 可以跳过写入。标记不成对
// 或顺序颠倒时报损坏错误而非猜测——孤儿 START 与后续 END 之间的区域是用户配置
// （model/provider/API key），替换它就是数据丢失。
func upsertKimiSection(content, block string) (string, error) {
	section := kimiMarkStart + " — managed by `forge init --agents kimi`; do not edit between markers\n" +
		block + kimiMarkEnd + "\n"
	start := strings.Index(content, kimiMarkStart)
	end := strings.Index(content, kimiMarkEnd)
	if (start >= 0) != (end >= 0) || (start >= 0 && end <= start) {
		return "", fmt.Errorf("kimi: config.toml forge marker section corrupt (unpaired or inverted %s/%s); fix or remove the markers manually", kimiMarkStart, kimiMarkEnd)
	}
	if start >= 0 {
		end += len(kimiMarkEnd)
		if end < len(content) && content[end] == '\n' {
			end++
		}
		return content[:start] + section + content[end:], nil
	}
	if content != "" {
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n"
	}
	return content + section, nil
}

// StripKimiHooks removes the forge marked section from kimi's config.toml (uninstall
// path). Reports whether a section was found and removed; a missing file or missing
// markers is a clean no-op.
//
// StripKimiHooks 从 kimi 的 config.toml 删除 forge 标记段（卸载路径）。返回是否
// 找到并删除了标记段；文件不存在或无标记均为干净 no-op。
func StripKimiHooks() (bool, error) {
	path, err := KimiConfigPath()
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("kimi: failed to read config.toml: %w", err)
	}
	stripped, found, err := removeKimiSection(string(existing))
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(stripped), 0644); err != nil {
		return false, fmt.Errorf("kimi: failed to write config.toml: %w", err)
	}
	return true, nil
}

// removeKimiSection deletes the marked section (and the blank line that precedes it when
// it was appended by upsertKimiSection) from content. Unpaired or inverted markers are
// reported as corruption (same data-loss guard as upsertKimiSection); no markers at all is
// a clean (content, false, nil).
//
// removeKimiSection 从 content 删除标记段（以及 upsertKimiSection 追加时在其前方
// 加入的空行）。标记不成对或颠倒时报损坏错误（与 upsertKimiSection 同款防数据
// 丢失守卫）；完全没有标记是干净的 (content, false, nil)。
func removeKimiSection(content string) (string, bool, error) {
	start := strings.Index(content, kimiMarkStart)
	end := strings.Index(content, kimiMarkEnd)
	if (start >= 0) != (end >= 0) || (start >= 0 && end <= start) {
		return "", false, fmt.Errorf("kimi: config.toml forge marker section corrupt (unpaired or inverted %s/%s); fix or remove the markers manually", kimiMarkStart, kimiMarkEnd)
	}
	if start < 0 {
		return content, false, nil
	}
	end += len(kimiMarkEnd)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	// Swallow one preceding blank line so a strip after an append restores the file
	// byte-for-byte.
	//
	// 吞掉前一行空行，使"追加后删除"能逐字节还原文件。
	if start >= 2 && content[start-2:start] == "\n\n" {
		start--
	}
	return content[:start] + content[end:], true, nil
}
