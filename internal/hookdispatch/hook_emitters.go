package hookdispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/hostcap"
	"github.com/MjxUpUp/Forge/internal/util"
)

// HookOutput represents the structured JSON that Claude Code expects to receive on stdout.
//
// HookOutput 表示 Claude Code 期望在 stdout 收到的结构化 JSON。
// 字段语义参见 Claude Code hook 文档。
type HookOutput struct {
	// Decision/reason are both omitempty: the allow path emits a bare hookSpecificOutput (no decision) so the host's default flow is untouched.
	//
	// Decision/reason 均 omitempty：allow 路径发裸 hookSpecificOutput（无 decision），
	// 不触碰宿主默认流程——decision:"approve" 在 Claude PreToolUse 会绕过权限系统，
	// 在 codex 会被判 hook failed（见 emitAgentOutput）。
	Decision           string              `json:"decision,omitempty"`
	Reason             string              `json:"reason,omitempty"`
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// HookSpecificOutput holds fields that steer Claude Code behavior.
//
// HookSpecificOutput 含控制 Claude Code 行为的字段。
//
// PermissionDecision/PermissionDecisionReason 是 PreToolUse 的现行 schema
// （2026-08-22 #4-B 迁移，additive）：官方文档把 deny/ask/defer/allow 判决放在
// hookSpecificOutput.permissionDecision——PreToolUse 上遗留的顶层
// decision:"block" 已不被采纳（社区实证旧 hook 因此静默失效）。exit 2 仍是
// 一等阻断通道（"routes the same way as deny"），既有阻断从不依赖遗留字段；
// 填现行字段让 deny 在活 schema 下显式化。保持 omitempty：非 PreToolUse 事件
// 与 allow 路径的序列化与之前逐字节一致。
type HookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

// maxAdditionalContextLen 是 Claude Code additionalContext 的上限（10,000 字符）。
// 这里取 9,500 给 JSON envelope 留余量。
const maxAdditionalContextLen = 9500

// outputEmitters 把宿主名映射到其输出协议 emitter。emitter **函数本体**留在
// cli（它们直接写 os.Stdout 并返回 cli 的 *HookBlockError）；注册表无法持有
// 它们——hostcap 是纯数据叶子包，不能 import cli（见 hostcap 包文档）。未知
// agent——claude-code 及所有不带 --agent 的 Claude-JSON 兼容宿主
// （codebuddy/opencode）——在 emitAgentOutput 里走 emitClaudeOutput 默认。
var outputEmitters = map[string]func(eventName, hookName string, passed bool, detail string) error{
	"kimi": func(_, _ string, passed bool, detail string) error { return emitKimiOutput(passed, detail) },
	"codex": func(eventName, _ string, passed bool, detail string) error {
		return emitCodexOutput(eventName, passed, detail)
	},
	"cursor": func(eventName, _ string, passed bool, detail string) error {
		return emitCursorOutput(eventName, passed, detail)
	},
	"copilot": emitCopilotOutput,
	"windsurf": func(_, hookName string, passed bool, detail string) error {
		return emitWindsurfOutput(hookName, passed, detail)
	},
	"cline": func(_, _ string, passed bool, detail string) error { return emitClineOutput(passed, detail) },
}

// emitAgentOutput 把 hook 结论分发到宿主的输出协议。agent==""（claude-code 及所有
// 不带 --agent flag 的 Claude-JSON 兼容宿主：codebuddy/opencode）走 claude 默认。
// 关键不变式：
//   - allow 绝不发 decision:"approve"——Claude PreToolUse 上它会绕过权限系统
//     （allow hook 不得授予权限），codex 则解析它但把 hook 判为 FAILED。
//   - block 恒返回 *HookBlockError → exit 2（唯一例外 copilot Stop：那里 exit 2 是
//     warning，decision JSON + exit 0 才是唯一阻断通道）。exit 2 是 codex（stderr+
//     exit2）、cursor（等价 deny）、copilot preToolUse（deny、fail-closed）共同认可
//     的阻断码；旧 generic error（exit 1）在它们上面都不构成阻断。
func EmitAgentOutput(agent, eventName, hookName string, passed bool, detail string) error {
	detail = util.TruncateRunes(detail, maxAdditionalContextLen)
	if emit, ok := outputEmitters[agent]; ok {
		return emit(eventName, hookName, passed, detail)
	}
	return emitClaudeOutput(eventName, passed, detail)
}

// contextChannelDelivered reports whether an ALLOW-path detail emission on (agent, event) actually reaches the model's context on that host, plus a short channel label.
//
// contextChannelDelivered 报告 (agent, event) 上 allow 路径的 detail 输出是否真到达该
// 宿主的模型上下文，并给出简短通道标签。每宿主通道数据住在 hostcap 注册表
// （ContextChannels/DefaultChannel 列，各行注明出处 emitter）；保留这个薄包装，记录点
// （recordSkillTriggerHits）就能把 Delivered/Channel 落章进 checklog 而无需重推路由
// 表，分析侧（usage 漏斗）也拿到真实的送达分母，而不是把模型从未见过的条目也计成
// 送达。这是 kimi 2026-08-15 虚假繁荣修复（不可见通道先 bail 再记录）的泛化：记录
// 可以留（审计轨迹），但 delivered 必须说真话。
func contextChannelDelivered(agent, eventName string) (bool, string) {
	return hostcap.ContextChannel(agent, eventName)
}

// emitClaudeOutput 渲染 claude-code 默认形态（也覆盖 codebuddy/opencode——所有
// 解析 Claude stdout JSON 但不带 --agent flag 的宿主）：allow = 静默（exit 0，默认
// 流程不动；有 detail 时发裸 hookSpecificOutput，Claude 会注入其 additionalContext
// ——无 decision 字段）；block = decision:block JSON + 原因写 stderr +
// HookBlockError（Execute 映射为 exit 2——Claude 的阻断错误码，stderr 会展示给模型）。
func emitClaudeOutput(eventName string, passed bool, detail string) error {
	if passed {
		if detail == "" {
			return nil
		}
		out := HookOutput{HookSpecificOutput: &HookSpecificOutput{
			HookEventName:     eventName,
			AdditionalContext: detail,
		}}
		data, _ := json.Marshal(out)
		fmt.Println(string(data))
		return nil
	}
	if detail == "" {
		detail = "forge hook blocked the action"
	}
	// #4-B：PreToolUse 上 deny 同时走现行 hookSpecificOutput.permissionDecision
	// 字段——遗留顶层 decision:"block" 在该事件上已不被采纳（仍为读它的宿主/事件
	// 发出；Stop 继续用顶层 decision）。下方 exit 2 仍是承重阻断通道
	// （"routes the same way as deny"），本改动是 additive 的 schema 对齐，非行为变更。
	hso := &HookSpecificOutput{
		HookEventName:     eventName,
		AdditionalContext: detail,
	}
	if eventName == "PreToolUse" {
		hso.PermissionDecision = "deny"
		hso.PermissionDecisionReason = detail
	}
	out := HookOutput{
		Decision:           "block",
		Reason:             detail,
		HookSpecificOutput: hso,
	}
	data, _ := json.Marshal(out)
	fmt.Println(string(data))
	fmt.Fprintln(os.Stderr, detail)
	return &HookBlockError{Reason: detail}
}

// emitCodexOutput 渲染 codex 协议（developers.openai.com/codex/hooks）：
// hookSpecificOutput.additionalContext 仅在 SessionStart/PreToolUse/PostToolUse/
// UserPromptSubmit 上被采纳（SubagentStart 非 forge 事件）；Stop/PostCompact 无上下文
// 通道。decision:"approve" 会被解析但**不支持**——codex 把 hook 判为 failed——故
// allow 路径发**裸** hookSpecificOutput（Claude 合法且 codex 合法）。阻断 =
// stderr + exit 2（codex 唯一可靠的阻断通道；stdout decision:"block" 是遗留行为，
// 不依赖它）。
func emitCodexOutput(eventName string, passed bool, detail string) error {
	if passed {
		switch eventName {
		case "SessionStart", "PreToolUse", "PostToolUse", "UserPromptSubmit":
			if detail != "" {
				out := HookOutput{HookSpecificOutput: &HookSpecificOutput{
					HookEventName:     eventName,
					AdditionalContext: detail,
				}}
				data, _ := json.Marshal(out)
				fmt.Println(string(data))
			}
		}
		return nil
	}
	if detail == "" {
		detail = "forge hook blocked the action"
	}
	fmt.Fprintln(os.Stderr, detail)
	return &HookBlockError{Reason: detail}
}

// emitCursorOutput 渲染 cursor 协议（cursor.com/docs/agent/hooks）：
// postToolUse/sessionStart 读**顶层** snake_case additional_context；其余事件的
// allow 路径无上下文通道。阻断 = stderr + exit 2（cursor 在所有事件上把 exit 2 当
// 等价 permission deny）。
func emitCursorOutput(eventName string, passed bool, detail string) error {
	if passed {
		switch eventName {
		case "PostToolUse", "SessionStart":
			if detail != "" {
				fmt.Printf(`{"additional_context":%s}`+"\n", jsonString(detail))
			}
		}
		return nil
	}
	if detail == "" {
		detail = "forge hook blocked the action"
	}
	fmt.Fprintln(os.Stderr, detail)
	return &HookBlockError{Reason: detail}
}

// emitCopilotOutput 渲染 GitHub Copilot 协议
// （docs.github.com/en/copilot/reference/hooks-reference——本会话全文核实）。退出码：
// 0 = 成功（stdout 按 JSON 解析）、2 = warning（stderr 上浮、继续执行）——唯一例外
// preToolUse 的 exit 2 = deny 并与 stdout decision 合并。agentStop/subagentStop 只能
// 经 stdout {"decision":"block"} + exit 0 阻断——那里的 exit 2 是 warning 不是阻断。
// 上下文通道：sessionStart/postToolUse 顶层 camelCase additionalContext（多 hook 以
// 双换行拼接、10KB 上限）；userPromptSubmitted 的 stdout 对 command hook 会被丢弃。
// PascalCase 事件键（plugin pack 的接线方式）给出 Claude matcher 语义与 snake_case
// payload，故 stdin 侧无需 normalizer。
func emitCopilotOutput(eventName, hookName string, passed bool, detail string) error {
	if passed {
		switch eventName {
		case "PostToolUse", "SessionStart":
			if detail != "" {
				fmt.Printf(`{"additionalContext":%s}`+"\n", jsonString(detail))
			}
		}
		return nil
	}
	if detail == "" {
		detail = "forge hook blocked the action"
	}
	switch eventName {
	case "PreToolUse":
		// fail-closed 事件：任何非零都 deny，且 exit 2 与 stdout deny decision 合并
		// ——两者都发，让原因在合并后仍可见。
		fmt.Printf(`{"permissionDecision":"deny","permissionDecisionReason":%s}`+"\n", jsonString(detail))
		return &HookBlockError{Reason: detail}
	case "Stop":
		// agentStop 上 exit 2 只是 warning——decision JSON + exit 0 是唯一阻断通道
		// （block 强制再来一轮；连续 8 次阻断后有 runaway guard）。
		fmt.Printf(`{"decision":"block","reason":%s}`+"\n", jsonString(detail))
		return nil
	default:
		// 其余事件（postToolUse/...）：无文档化阻断通道；exit 2 表现为 warning、
		// stderr 可达模型。task-verify/review-stop（forge 的 Stop hook）已在上面
		// 处理；PostToolUse hook 极少阻断。
		fmt.Fprintln(os.Stderr, detail)
		return &HookBlockError{Reason: detail}
	}
}

// emitWindsurfOutput 渲染 Windsurf Cascade 协议：完全没有 stdout JSON 协议（hook
// 条目以 show_output:false 运行；stdout JSON 即便被显示也只是噪声）——allow 静默、
// 阻断 = stderr + exit 2。pre_* hook 上 exit 2 deny；post_cascade_response（Stop 组
// 挂载处）是异步 post-hook，**无法阻断**——那里 exit 2 只把 stderr 原因以 advisory
// 形式上浮给 agent（已在 buildWindsurfHooks 诚实文档化）。
func emitWindsurfOutput(hookName string, passed bool, detail string) error {
	if passed {
		return nil
	}
	if detail == "" {
		detail = "forge hook blocked the action"
	}
	fmt.Fprintln(os.Stderr, detail)
	return &HookBlockError{Reason: detail}
}

// emitClineOutput 渲染 Cline 的文件 hook 协议（v3.36+ hooks 博客）：hook 通过打印
// {"cancel":bool,"errorMessage":...,"contextModification":...} 表态——cancel 阻断动作、
// contextModification 向任务注入文本。forge 的 wrapper 脚本
// （~/Documents/Cline/Rules/Hooks/<Event>）把一个 Cline 事件扇出到多个 forge hook
// 并合并结论，用**退出码**作稳健的阻断信号（阻断 = 经 HookBlockError 的 exit 2，
// 且这份现成的 cancel JSON 已在 stdout）；allow 路径转发 contextModification。
// 刻意输出紧凑 JSON（{"cancel":true,…）——没有谁在带内解析它，但 wrapper 的上下文
// 嗅探依赖该字段名不会碰巧出现在 forge 自身的 allow 输出形态里。
func emitClineOutput(passed bool, detail string) error {
	if passed {
		if detail == "" {
			return nil
		}
		fmt.Printf(`{"cancel":false,"contextModification":%s}`+"\n", jsonString(detail))
		return nil
	}
	if detail == "" {
		detail = "forge hook blocked the action"
	}
	fmt.Printf(`{"cancel":true,"errorMessage":%s}`+"\n", jsonString(detail))
	return &HookBlockError{Reason: detail}
}

// jsonString 把 s 编组为 JSON 字符串字面量（转义、引号），用于嵌进手工组合的协议
// envelope。对字符串输入不会失败。
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// HookBlockError signals an intentional hook block for hosts whose protocol selects behavior by exit code (kimi: 2 = intentional block, any other non-zero = fail-open allow).
//
// HookBlockError 表示一次有意的 hook 阻断，面向按退出码区分行为的宿主
// （kimi：2 = 有意阻断，其他非零 = fail-open 放行）。Execute 把它映射为
// os.Exit(2)；原因在输出处写入 stderr（kimi 把 stderr 作为阻断原因展示给模型）。
type HookBlockError struct {
	Reason string
}

func (e *HookBlockError) Error() string { return e.Reason }

// promoteAdvisory 报告给定 hook 的 advisory（passed=true、detail 非空）结果在给定宿主上
// 是否应提升为阻断。各 hook 的规则住在 hostcap 注册表（PromoteAdvisory 列——
// dsh 2026-08-22 与 zcode 2026-08-30 双事故实证入列：通道送达但 task-guard
// advisory 被实证无视，见各注册表行；kimi 的规则已于 2026-08-24 退役，改为 pending 队列 +
// UserPromptSubmit 攒发，见 hook_kimi_advisory.go）。规则是声明式
// Contains/Excludes 对（非裸名字白名单），因为每个 hook 在同一 hook 名下同时发
// advisory 分支与成功/干净分支——名字白名单会过度阻断（task-guard 的
// "Auto-created task" 是成功路径；assertion-check 的干净分支无 advisory）。把真
// advisory 提升为阻断让它改走所有宿主都认的 PreToolUse 通道：exit 2（stderr 展示给
// 模型）。
//
// 以下返回 false：逃生舱（FORGE_ADVISORY_PROMOTION=soft 覆盖所有提升宿主；
// FORGE_KIMI_ADVISORY=soft 保留向后兼容——目前因 kimi 无规则而惰性，但若未来
// kimi 规则落地仍会被遵守——env 开关，留在 cli 而非注册表——它们是
// 运维配置而非宿主能力）、已阻断结果（不二次翻转）、空/纯空白 detail（干净/静默
// PASS）、无提升规则的宿主。
func promoteAdvisory(agent, name string, passed bool, detail string) bool {
	if advisoryPromotionDisabled(agent) {
		return false
	}
	if !passed || strings.TrimSpace(detail) == "" {
		return false
	}
	h := hostcap.Lookup(agent)
	if h == nil {
		return false
	}
	return h.ShouldPromoteAdvisory(name, detail)
}

// advisoryPromotionDisabled 是所有 advisory 提升消费方（promoteAdvisory、
// taskGuardPromotionActive）共享的唯一逃生舱检查，使逃生舱不可能在一处开着
// 另一处关着——例如 FORGE_TASKGUARD_PROMOTED 已设（脚本放弃去噪、每次 edit 都
// WARN）而提升本身被抑制，会复活 139 次 WARN 刷屏（dogfood 3.1）且背后无执法。
// 按 host 划定：FORGE_ADVISORY_PROMOTION=soft 覆盖所有提升宿主；已发布的
// FORGE_KIMI_ADVISORY=soft 保持仅 kimi——软化 kimi 的运维意图不得静默波及 dsh
// （新宿主的提升也不得搭一个以别的宿主命名的逃生舱）。
func advisoryPromotionDisabled(host string) bool {
	if os.Getenv("FORGE_ADVISORY_PROMOTION") == "soft" {
		return true
	}
	return host == "kimi" && os.Getenv("FORGE_KIMI_ADVISORY") == "soft"
}

// taskGuardPromotionActive reports whether task-guard advisories on this host are currently promoted (a task-guard rule exists in hostcap AND the escape hatch is closed).
//
// taskGuardPromotionActive 报告该宿主的 task-guard advisory 当前是否被提升
// （hostcap 存在 task-guard 规则且逃生舱关闭）——与 detail 无关，让 runHook 能在
// 任何脚本输出产生前为脚本设置 FORGE_TASKGUARD_PROMOTED。脚本为何需要知道，见
// runHook 调用处注释。
func taskGuardPromotionActive(agent string) bool {
	if advisoryPromotionDisabled(agent) {
		return false
	}
	h := hostcap.Lookup(agent)
	return h != nil && h.PromotesHook("task-guard")
}

// emitKimiOutput 按 kimi 的 hook 协议渲染结果：放行 = exit 0，detail 以纯文本打
// stdout；阻断 = 原因写 stderr + HookBlockError（→ exit 2）。返回 HookBlockError 而非
// 在此 os.Exit，是为了让 runHook 的 defer（临时脚本清理）照常执行。
//
// 注意——kimi 0.35.0 并不像 Claude Code 那样把 allow 路径 stdout 注入模型上下文
// （additionalContext）。只有 UserPromptSubmit 的 stdout 能到模型（下一 prompt 送达）；
// PreToolUse 的 stdout 会被当 **deny**（阻断工具调用），PostToolUse/SessionStart 的
// allow 路径 stdout 是 observation-only（丢弃）。故 advisory（allow 路径）输出在不可
// 送达事件上根本不会到达本函数——emitAdvisoryRouted（hook_kimi_advisory.go）会先把它
// 入队、留待 UserPromptSubmit 攒发。仍到达本函数的 allow 路径只有 UserPromptSubmit
// detail（注入）与静默放行；阻断路径不变（设计内 deny：read-before-edit、
// hazard-guard、freeze-guard）。
func emitKimiOutput(passed bool, detail string) error {
	if passed {
		if detail != "" {
			fmt.Println(detail)
		}
		return nil
	}
	if detail == "" {
		detail = "forge hook blocked the action"
	}
	fmt.Fprintln(os.Stderr, detail)
	return &HookBlockError{Reason: detail}
}

// emitInfraAllow 对基础设施失败 fail-open：警告必须可见（静默失效的门禁比吵闹的
// 更糟）但不阻断当轮。经 emitAdvisoryRouted 分发，让每个宿主走自己的上下文通道：
// kimi 在不可送达事件上把警告入队、留待 UserPromptSubmit 攒发（PreToolUse 上直接
// 打印 stdout 会被当 **deny**——hook fail-open 了却把编辑拦下，两头最坏；见
// hook_kimi_advisory.go）；claude 默认裸 hookSpecificOutput（hookEventName 必在——
// Claude schema 要求它，否则 additionalContext 被丢弃）；codex 在四个可带上下文的
// 事件上发同样的裸形态；cursor 顶层 additional_context；copilot 顶层
// additionalContext；cline contextModification；windsurf 静默（无 stdout 通道——
// 可见性与之前一致）。任何宿主都不会收到 decision:"approve"（见 emitAgentOutput）。
//
// 调用点（统一，fix/cleanup-batch 2026-08-29）：step 5 的 bash 起不来/126/127 失败，
// 以及更早的「脚本永远跑不起来」失败（临时文件创建/写入、findBash）——同一 infra
// 类，全部 fail-open 并带可见的、按宿主路由的警告。
func emitInfraAllow(agent, eventName, hookName, root, sessionID, warning string) error {
	return EmitAdvisoryRouted(agent, eventName, hookName, root, sessionID, true, warning)
}
