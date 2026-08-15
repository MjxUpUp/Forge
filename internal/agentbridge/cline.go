package agentbridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// Cline wiring (Wave 3b). Cline v3.36+ ships file-based lifecycle hooks: executable
// scripts NAMED exactly the hook type (no extension) loaded from
// ~/Documents/Cline/Rules/Hooks/ (global) or .clinerules/hooks/ (project). Six types
// exist (PreToolUse/PostToolUse/UserPromptSubmit/TaskStart/TaskResume/TaskCancel);
// a script receives the event JSON on stdin and speaks by printing a single JSON
// object {"cancel":bool,"errorMessage":...,"contextModification":...} — cancel blocks
// the action, contextModification injects text into the task. macOS/Linux ONLY
// (cline does not execute hook scripts on Windows — documented honestly below; the
// translator still writes the scripts on every platform so mac/linux users get them
// and the write itself is inert on Windows).
//
// Cline runs ONE script per event type, while forge wires SEVERAL hooks per event —
// so the translator writes a generic wrapper script per event that fans the cline
// event out to the event's forge hooks (`forge hook <name> --agent cline`) and merges
// verdicts: any exit 2 → forward that hook's ready-made cancel JSON (emitClineOutput
// in internal/cli/hook.go already printed it) and stop; otherwise strip the
// {"cancel":false,"contextModification":...} envelope off each allow-with-context
// output and join the (already JSON-escaped) payloads into one contextModification.
// The envelope surgery is safe because forge emits that exact compact shape — the
// field name never appears inside forge's own allow output by accident (pinned by
// emitClineOutput's comment contract).
//
// Event mapping: PreToolUse/PostToolUse/UserPromptSubmit map 1:1 from the spec;
// SessionStart's group maps onto TaskStart (a cline task == a session for hook
// purposes); TaskResume/TaskCancel have no forge analogue and stay unwired. Two spec
// events have NO cline channel and are documented honestly rather than approximated:
// Stop (no cline event fires when the agent finishes — task-verify/review-stop cannot
// enforce there) and PostCompact (no compaction event). Matchers are dropped: cline
// fires the script for every tool call, so each forge hook's own path/command logic
// decides relevance (same trade windsurf makes — hooks pass quickly on unrelated
// tools). The stdin dialect is normalized by clineNormalize (internal/cli/
// hook_normalize.go); the output dialect by emitClineOutput.
//
// The wrapper is POSIX sh (macOS /bin/sh is bash-3.2 in posix mode — no bashisms;
// inside case actions, avoid the documented bash-3.2 parse-error forms — a nested
// `case` and `[[ ]] && cmd ;;` — plain assignments and a simple `if..fi` parse fine).
//
// Cline 接线（Wave 3b）。Cline v3.36+ 提供基于文件的 lifecycle hooks：以 hook 类型
// 精确命名（无扩展名）的可执行脚本，从 ~/Documents/Cline/Rules/Hooks/（全局）或
// .clinerules/hooks/（项目级）加载。六种类型（PreToolUse/PostToolUse/
// UserPromptSubmit/TaskStart/TaskResume/TaskCancel）；脚本经 stdin 收 event JSON，
// 通过打印单个 JSON 对象 {"cancel":bool,"errorMessage":...,"contextModification":...}
// 表态——cancel 阻断动作、contextModification 向任务注入文本。仅 macOS/Linux
// （cline 在 Windows 上不执行 hook 脚本——下文诚实文档化；translator 在所有平台
// 仍写脚本，mac/linux 用户拿得到，且该写入在 Windows 上无副作用）。
//
// Cline 每 event 只跑一个脚本，而 forge 每 event 接多个 hook——故 translator 为每
// event 写一个通用 wrapper 脚本，把 cline 事件扇出给该 event 的 forge hook
// （`forge hook <name> --agent cline`）并合并结论：任一 exit 2 → 转发该 hook 现成的
// cancel JSON（internal/cli/hook.go 的 emitClineOutput 已打印它）并停止；否则剥掉
// 每条 allow-with-context 输出的 {"cancel":false,"contextModification":...} 信封，
// 把（已 JSON 转义的）payload 拼进一个 contextModification。信封手术安全的原因是
// forge 恰好输出该紧凑形态——该字段名不会碰巧出现在 forge 自身的 allow 输出里
// （emitClineOutput 注释契约钉死）。
//
// 事件映射：PreToolUse/PostToolUse/UserPromptSubmit 与 spec 一一对应；SessionStart
// 组映射到 TaskStart（hook 语境下 cline 的 task 即会话）；TaskResume/TaskCancel 无
// forge 对应物、不接。两个 spec event 无 cline 通道，诚实文档化而非近似：Stop
// （cline 无 agent 完成事件——task-verify/review-stop 在其上无法 enforce）与
// PostCompact（无压缩事件）。matcher 被丢弃：cline 对每次工具调用都触发脚本，由各
// forge hook 自身的路径/命令逻辑判定相关性（与 windsurf 同样的取舍——无关工具上
// hook 快速通过）。stdin 方言由 clineNormalize（internal/cli/hook_normalize.go）
// 归一化；输出方言由 emitClineOutput 处理。
//
// wrapper 是 POSIX sh（macOS 的 /bin/sh 是 bash-3.2 posix 模式——不用 bashism；
// case action 内避开文档化的 bash-3.2 parse-error 形态——嵌套 `case` 与
// `[[ ]] && cmd ;;`——纯赋值与简单 `if..fi` 均可正常解析）。

// clineWrapperMarker identifies a wrapper script as forge-generated. Translate only
// ever overwrites files carrying this marker — a user-authored script named e.g.
// "PreToolUse" is the user's own hook (cline runs ONE script per event type, so
// overwriting it would silently steal the channel) and makes Translate refuse.
//
// clineWrapperMarker 标识 wrapper 脚本为 forge 生成。Translate 只覆写带此标记的
// 文件——用户自写的脚本（如名为 "PreToolUse"）是用户自己的 hook（cline 每 event
// 只跑一个脚本，覆写等于静默抢占通道），会让 Translate 拒绝。
const clineWrapperMarker = "# forge-generated cline hook wrapper"

// clineEventMappings is the deterministic cline-event ← spec-event table (slice, not
// map: script write order and test rosters stay stable). Every cline event that has a
// forge analogue is listed; TaskResume/TaskCancel (no forge analogue), Stop and
// PostCompact (no cline channel — see the file-header comment) are deliberately absent.
//
// clineEventMappings 是确定性的 cline-event ← spec-event 表（用 slice 不用 map：
// 脚本写入顺序与测试 roster 保持稳定）。有 forge 对应物的 cline event 全部列出；
// TaskResume/TaskCancel（无 forge 对应物）、Stop 与 PostCompact（无 cline 通道——见
// 文件头注释）刻意缺席。
var clineEventMappings = []struct {
	clineEvent string // script filename == cline hook type, exactly
	specEvent  string // ForgeHookSpec key the roster is derived from
}{
	{"PreToolUse", "PreToolUse"},
	{"PostToolUse", "PostToolUse"},
	{"UserPromptSubmit", "UserPromptSubmit"},
	// SessionStart → TaskStart: a cline "task" is the session for hook purposes, so
	// the session-scoped forge hooks (skill-scan/mcp-scan/init-suggest/task-resume)
	// hang on TaskStart. clineNormalize maps hookEventName TaskStart back to
	// "SessionStart" so the hooks' own event dispatch still sees the Claude name.
	//
	// SessionStart → TaskStart：hook 语境下 cline 的 "task" 即会话，会话级 forge
	// hook（skill-scan/mcp-scan/init-suggest/task-resume）挂到 TaskStart。
	// clineNormalize 把 hookEventName TaskStart 映射回 "SessionStart"，让 hook
	// 自身的事件分发仍看到 Claude 名。
	{"TaskStart", "SessionStart"},
}

// clineRosters derives, per cline event, the ordered deduped forge hook-name roster
// from ForgeHookSpec (single source of truth — no manual copy to drift). A hook
// listed under several matchers (skill-trigger sits in both the Write|Edit and Bash
// groups) runs once: cline has no matchers, so per-event dedupe replaces matcher
// grouping.
//
// clineRosters 从 ForgeHookSpec（单一真相源——无手工副本可漂移）按 cline event 派生
// 有序去重的 forge hook 名 roster。列在多个 matcher 下的 hook（skill-trigger 同时在
// Write|Edit 与 Bash 组）只跑一次：cline 无 matcher，按 event 去重取代 matcher 分组。
func clineRosters() map[string][]string {
	spec := hooks.ForgeHookSpec()
	rosters := map[string][]string{}
	for _, e := range clineEventMappings {
		seen := map[string]bool{}
		for _, m := range spec[e.specEvent] {
			for _, h := range m.Hooks {
				name := strings.TrimPrefix(h.Command, "forge hook ")
				if name == h.Command || seen[name] {
					continue // not a forge bridge command, or already rostered
				}
				seen[name] = true
				rosters[e.clineEvent] = append(rosters[e.clineEvent], name)
			}
		}
	}
	return rosters
}

// buildClineWrapperScript renders the POSIX-sh fan-out wrapper for one cline event.
// Contract (mirrors emitClineOutput's emission shape exactly):
//   - feed the SAME stdin JSON to every rostered forge hook;
//   - first exit 2 → print that hook's stdout verbatim (the ready-made
//     {"cancel":true,"errorMessage":...} object) and exit 0 — cline has no
//     documented script-exit blocking channel, the JSON object IS the channel;
//   - otherwise envelope-strip each {"cancel":false,"contextModification":"…"} output
//     and join the payloads (already JSON-escaped; separator is the two literal
//     characters \n) into one final object; hooks with no output contribute nothing.
//
// Any deviation from forge's emission shape simply fails to match the case pattern
// and is skipped — fail-open on context injection, never on blocking.
//
// buildClineWrapperScript 为一个 cline event 渲染 POSIX sh 扇出 wrapper。契约（与
// emitClineOutput 的产出形态精确镜像）：
//   - 把同一份 stdin JSON 喂给 roster 里的每个 forge hook；
//   - 首个 exit 2 → 原样打印该 hook 的 stdout（现成的
//     {"cancel":true,"errorMessage":...} 对象）并 exit 0——cline 无文档化的脚本退出码
//     阻断通道，JSON 对象本身就是通道；
//   - 否则剥掉每条 {"cancel":false,"contextModification":"…"} 输出的信封，把 payload
//     （已 JSON 转义；分隔符是字面的 \n 两字符）拼进一个最终对象；无输出的 hook
//     不贡献内容。
//
// 与 forge 产出形态的任何偏离都只是不命中 case pattern 而被跳过——上下文注入
// fail-open，阻断绝不 fail-open。
func buildClineWrapperScript(event string, roster []string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString(clineWrapperMarker + " — do not edit; re-run `forge init --agents cline` to refresh.\n")
	fmt.Fprintf(&b, "# Cline runs ONE script per event type; this wrapper fans %s out to the forge\n", event)
	b.WriteString("# hooks below and merges verdicts (generated by internal/agentbridge/cline.go).\n")
	b.WriteString("stdin_json=$(cat)\n")
	b.WriteString("context=\"\"\n")
	fmt.Fprintf(&b, "for hook in %s; do\n", strings.Join(roster, " "))
	b.WriteString("\tout=$(printf '%s' \"$stdin_json\" | forge hook \"$hook\" --agent cline)\n")
	b.WriteString("\tstatus=$?\n")
	b.WriteString("\tif [ \"$status\" -eq 2 ]; then\n")
	b.WriteString("\t\tprintf '%s\\n' \"$out\"\n")
	b.WriteString("\t\texit 0\n")
	b.WriteString("\tfi\n")
	// Envelope surgery: strip the fixed prefix and suffix, then the value's outer
	// quotes, leaving the already-escaped payload — embeddable directly inside a new
	// JSON string. The case action must avoid the bash-3.2 parse-error forms (nested
	// `case`, `[[ ]] && cmd ;;`) — assignments and a simple `if..fi` are safe.
	//
	// 信封手术：剥掉固定前缀与后缀，再剥值的两侧引号，留下已转义的 payload——可直接
	// 嵌进新的 JSON 字符串。case action 须避开 bash-3.2 parse-error 形态（嵌套
	// `case`、`[[ ]] && cmd ;;`）——赋值与简单 `if..fi` 是安全的。
	b.WriteString("\tcase \"$out\" in\n")
	b.WriteString("\t'{\"cancel\":false,\"contextModification\":'*'}')\n")
	b.WriteString("\t\tpiece=${out#'{\"cancel\":false,\"contextModification\":'}\n")
	b.WriteString("\t\tpiece=${piece%?}\n")
	b.WriteString("\t\tpiece=${piece%?}\n")
	b.WriteString("\t\tpiece=${piece#?}\n")
	b.WriteString("\t\tif [ -n \"$context\" ]; then context=\"$context\\\\n\"; fi\n")
	b.WriteString("\t\tcontext=\"$context$piece\"\n")
	b.WriteString("\t\t;;\n")
	b.WriteString("\tesac\n")
	b.WriteString("done\n")
	b.WriteString("if [ -n \"$context\" ]; then\n")
	b.WriteString("\tprintf '{\"cancel\":false,\"contextModification\":\"%s\"}\\n' \"$context\"\n")
	b.WriteString("fi\n")
	b.WriteString("exit 0\n")
	return b.String()
}

// ClineHooksDir resolves cline's GLOBAL hook directory (~/Documents/Cline/Rules/Hooks/
// — the documented global location; the project location .clinerules/hooks/ is not
// used because forge writes nothing into projects since v1.22).
//
// ClineHooksDir 解析 cline 的全局 hook 目录（~/Documents/Cline/Rules/Hooks/——文档化
// 的全局位置；项目级位置 .clinerules/hooks/ 不用，因 v1.22 起 forge 零项目写入）。
func ClineHooksDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cline hooks dir: %w", err)
	}
	return filepath.Join(home, "Documents", "Cline", "Rules", "Hooks"), nil
}

// ClineTranslator writes the forge wrapper scripts into cline's global hook dir.
// Previously a no-op ("cline has no lifecycle hooks" — true before v3.36); now a real
// wiring. Refuses (before writing anything) when a target filename already exists
// WITHOUT the forge marker: cline runs one script per event type, so silently
// replacing a user script would steal their hook channel, and silently skipping
// would leave the gate absent — the error names the file so the user can merge
// manually. macOS/Linux-only at RUNTIME (cline does not execute hook scripts on
// Windows); the write itself is inert there.
//
// ClineTranslator 把 forge wrapper 脚本写进 cline 的全局 hook 目录。此前是 no-op
// （"cline 无 lifecycle hooks"——v3.36 之前为真）；现在是真接线。当目标文件名已存在
// 且不带 forge 标记时（写任何东西之前）拒绝：cline 每 event 只跑一个脚本，静默替换
// 用户脚本等于抢他们的 hook 通道，静默跳过则门禁缺席——错误里点名文件，让用户手动
// 合并。运行时仅 macOS/Linux（cline 在 Windows 上不执行 hook 脚本）；该写入在
// Windows 上无副作用。
type ClineTranslator struct{}

func (t *ClineTranslator) Translate(projectDir string, input *TranslationInput) error {
	dir, err := ClineHooksDir()
	if err != nil {
		return err
	}
	rosters := clineRosters()
	// Pre-check ALL targets first so a conflict aborts before anything is written
	// (no half-wired state).
	//
	// 先全量预检所有目标，冲突在任何写入前中止（不留半接线状态）。
	for _, e := range clineEventMappings {
		target := filepath.Join(dir, e.clineEvent)
		data, err := os.ReadFile(target)
		if err == nil && !strings.Contains(string(data), clineWrapperMarker) {
			return fmt.Errorf("cline hook %s already exists at %s without the forge marker — cline runs ONE script per event, so forge will not overwrite it; merge the forge hooks into your script or rename yours, then re-run", e.clineEvent, target)
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create cline hooks dir: %w", err)
	}
	for _, e := range clineEventMappings {
		script := buildClineWrapperScript(e.clineEvent, rosters[e.clineEvent])
		// 0755: cline requires the hook script to be executable.
		//
		// 0755：cline 要求 hook 脚本可执行。
		if err := os.WriteFile(filepath.Join(dir, e.clineEvent), []byte(script), 0755); err != nil {
			return fmt.Errorf("write cline hook %s: %w", e.clineEvent, err)
		}
	}
	return nil
}

func (t *ClineTranslator) AgentType() AgentType {
	return AgentCline
}

// StripClineHooks removes the forge wrapper scripts (uninstall path). Marker files
// are deleted; user scripts are untouched; the hooks dir itself is removed only if
// left empty. A second strip and a missing dir are clean no-ops.
//
// StripClineHooks 移除 forge wrapper 脚本（卸载路径）。带标记的文件删除；用户脚本
// 不动；hook 目录本身只在清空后移除。二次 strip 与目录缺失都是干净的 no-op。
func StripClineHooks() (bool, error) {
	dir, err := ClineHooksDir()
	if err != nil {
		return false, err
	}
	changed := false
	for _, e := range clineEventMappings {
		target := filepath.Join(dir, e.clineEvent)
		data, err := os.ReadFile(target)
		if err != nil {
			continue // missing → nothing to strip
		}
		if !strings.Contains(string(data), clineWrapperMarker) {
			continue // user script — never touched
		}
		if err := os.Remove(target); err != nil {
			return changed, fmt.Errorf("remove cline hook %s: %w", e.clineEvent, err)
		}
		changed = true
	}
	if changed {
		// Best-effort: fails (ignored) when user scripts keep the dir populated.
		//
		// 尽力而为：用户脚本仍占着目录时会失败（忽略）。
		_ = os.Remove(dir)
	}
	return changed, nil
}
