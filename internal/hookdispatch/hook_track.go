// Package cli hook_track.go wires three Go-internal observation hooks to previously-uncovered Claude Code hook events.
//
// Package cli hook_track.go —— 接线此前未覆盖的 Claude Code hook 事件的三个
// Go 内观察 hook（#4-A，2026-08-22）：
//
//	failure-track  → PostToolUseFailure（matcher Bash）
//	subagent-track → SubagentStop
//	test-nudge     → PostToolUse Write|Edit（事中测试提醒，#4-E）
//
// 三者全部刻意 advisory/仅观察：失败已经发生，PostToolUseFailure 阻不住；
// SubagentStop 阻断假阳性不可行（多种子 agent 正常就以空消息收尾）；test-nudge
// 是 task-verify test-coverage 门禁的事中伴随——门禁执法，nudge 提醒。三者都记
// OBSERVATION 类 checklog 条目、排除出 evidence strength（见
// checklog.BuildEvidenceChain）——过程观察，绝非验证声明。
package hookdispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
)

// compileFailureMarkers 是把工具失败归类为「值得提示 compile-fix-loop 的编译/
// 测试失败」的子串（小写匹配）。刻意跨栈且保守：泛化失败（网络、lint exit 1、
// "command not found"）不命中——只有失败类别真匹配该 skill 方法论时，提示才有价值。
var compileFailureMarkers = []string{
	// Go
	"undefined:", "cannot use", "build failed", "compile", "vet: ",
	// TypeScript/JavaScript
	"error ts", "typescript error",
	// Rust
	"error[e", "error: could not compile",
	// Python
	"syntaxerror", "modulenotfounderror",
	// Test frameworks (go test / jest / pytest / cargo test)
	"--- fail", "fail\t", "failed tests", "test failed", "tests failed",
}

// runFailureTrackHook 处理 PostToolUseFailure（matcher Bash）：命令
// 失败、宿主上报了错误文本（HookInput.Error）。记录 CheckToolFailure 观察，
// 且当错误命中编译/测试启发式时，经 allow-with-detail 通道注入指向
// compile-fix-loop skill 的**事实性**提示（永不阻断——失败已发生，事后事件上
// 阻断毫无收益且 exit 2 是协议噪声）。事实化措辞遵循官方 additionalContext
// 指南：陈述事实；祈使句会被读成注入指令、可能触发 prompt-injection 防御
// （与 #5 banner 修复同类）。
func runFailureTrackHook(hookInput HookInput, root, version, agent string) error {
	taskRef := taskRefForSession(root, hookInput.SessionID)
	// 启发式文本 = 宿主 error + tool_response 尾部（部分宿主把失败输出放在那里）。
	// 有界读取：只做 marker 匹配，不存全文。
	text := hookInput.Error
	if len(hookInput.ToolOutput) > 0 {
		text += "\n" + string(hookInput.ToolOutput)
	}
	lower := strings.ToLower(text)
	kind := ""
	for _, m := range compileFailureMarkers {
		if strings.Contains(lower, m) {
			kind = "compile/test"
			break
		}
	}
	detail := fmt.Sprintf("tool-failure: %s failed (error: %s)", hookInput.ToolName, util.RedactSecrets(util.TruncateRunes(strings.TrimSpace(hookInput.Error), 200)))
	meta := map[string]string{
		"tool": hookInput.ToolName,
	}
	if hookInput.AgentID != "" {
		meta["agent_id"] = hookInput.AgentID
	}
	// cursor postToolUseFailure 的分类（error/timeout/permission_denied）在场时
	// 随行记录——即便没有错误文本，失败漏斗也能按类聚合。取 FailureType（非已
	// 填空的 Error）记录，裸字符串宿主不会把自己的文本误标成分类。
	if hookInput.FailureType != "" {
		meta["failure_type"] = hookInput.FailureType
	}
	// 只有下方 marker 分支真的发输出时才盖 Delivered 章（复审 2026-08-22：
	// 无条件盖章会把泛化失败条目标成已送达而实际零输出——虚增漏斗的送达分母；
	// 从不发射的路径与 subagent-track 同不落章契约）。
	var delivered *bool
	var channel string
	if kind != "" {
		// AdvisoryEmissionChannel（非 contextChannelDelivered）：kimi 上本 nudge
		// 经 emitAdvisoryRouted 入队，章须标 kimi/advisory-queue 而非
		// kimi/no-channel，让漏斗区分「入队待投」与「永久丢失」。
		d, ch := AdvisoryEmissionChannel(agent, hookInput.HookEventName)
		delivered = &d
		channel = ch
	}
	if err := checklog.Record(root, &checklog.Entry{
		Check:        checklog.CheckToolFailure,
		Passed:       true, // 观察语义非裁定：失败不是 hook 的 pass/fail 主张
		Checked:      true,
		ToolName:     hookInput.ToolName,
		TaskRef:      taskRef,
		SessionID:    hookInput.SessionID,
		Detail:       detail,
		Source:       checklog.EvidenceDeterministic,
		Level:        checklog.LevelAdvisory,
		Delivered:    delivered,
		Channel:      channel,
		ForgeVersion: version,
		Meta:         meta,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "[failure-track] warning: checklog record failed: %v\n", err)
	}
	if kind == "" {
		// 非编译类失败：观察已记录，无需附加——模型本就在自己的工具结果里
		// 看得到失败输出。
		return nil
	}
	// 事实性提示（非祈使）：陈述发生了什么、方法论在哪；模型自行决定。
	// 只给 skill 名：仓库相对 skills/... 路径在 Forge 仓库之外是死链，hook 手里
	// 也没有绝对 skill 目录——由宿主 skill 机制解析名字（参 skilltrigger 的绝对
	// 路径渲染）。
	nudge := fmt.Sprintf(
		"[forge] a compile/test failure was just observed (markers matched in %s tool output). "+
			"Load the compile-fix-loop skill for the failure-class-specific debugging methodology (root-cause first, not error-message whack-a-mole).",
		hookInput.ToolName)
	// emitAdvisoryRouted：kimi 把本提示入队、留待 UserPromptSubmit 攒发（本 hook
	// 在 PostToolUse/PostToolUseFailure 上触发，其 stdout 被 kimi 丢弃）；其余
	// 宿主的输出路径不变。
	return EmitAdvisoryRouted(agent, hookInput.HookEventName, "failure-track", root, hookInput.SessionID, true, nudge)
}

// runSubagentTrackHook 处理 SubagentStop：子 agent 结束（cursor 的 subagent_type/
// status/result 拼法经 runHook 填空块归一到上述字段——见 HookInput），携带
// agent_id/agent_type/last_assistant_message（官方 schema 字段）。记录
// CheckSubagentStop 观察供归因——此前子 agent 活动在 forge 侧零记录，sessions.jsonl
// 约 53% 会话缺 agent_type（2026-08 归因审计）。v1 仅观察：无输出、无阻断——
// SubagentStop 的 stdout 不是上下文通道（仅 transcript verbose 可见），任何输出
// 都是被丢弃的字节。交付摘要只存长度+脱敏首行，绝不存全文（checklog 不是消息
// 归档）。
func runSubagentTrackHook(hookInput HookInput, root, version, agent string) error {
	taskRef := taskRefForSession(root, hookInput.SessionID)
	msg := strings.TrimSpace(hookInput.LastAssistantMessage)
	firstLine := msg
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		firstLine = msg[:i]
	}
	firstLine = util.RedactSecrets(util.TruncateRunes(strings.TrimSpace(firstLine), 120))
	meta := map[string]string{
		"message_len": fmt.Sprintf("%d", len(msg)),
	}
	if hookInput.AgentID != "" {
		meta["agent_id"] = hookInput.AgentID
	}
	// cursor subagentStop 的完成状态（"completed"/"error"，官方文档）在场时随行
	// 记录——completed 与 error 之分是漏斗信号，与上面 failure_type 的按类聚合
	// 同角色。
	if hookInput.SubagentStatus != "" {
		meta["status"] = hookInput.SubagentStatus
	}
	agentType := hookInput.AgentTypeHook
	if agentType == "" {
		agentType = "unknown"
	}
	meta["agent_type"] = agentType
	detail := fmt.Sprintf("subagent stopped: type=%s delivered=%d chars", agentType, len(msg))
	if firstLine != "" {
		detail += fmt.Sprintf(" (\"%s\")", firstLine)
	}
	// 刻意省略 Delivered 章（nil）：任何东西都没被注入——与 recordSuppressed 的
	// 不落章规则同契约（对从不向模型携带内容的事件，送达语义不适用）。
	if err := checklog.Record(root, &checklog.Entry{
		Check:        checklog.CheckSubagentStop,
		Passed:       true,
		Checked:      true,
		TaskRef:      taskRef,
		SessionID:    hookInput.SessionID,
		Detail:       detail,
		Source:       checklog.EvidenceDeterministic,
		Level:        checklog.LevelAdvisory,
		ForgeVersion: version,
		Meta:         meta,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "[subagent-track] warning: checklog record failed: %v\n", err)
	}
	_ = agent
	return nil
}

// testNudgeState 是持久化在 $TMPDIR/forge-testnudge/<sanitized-session>.json 的
// 会话级计数器。与 skill-trigger marker 同寿命选择：会话 scope、短命、OS 定期
// 清理（F6）——绝不进 GlobalHome（无 GC、无限增长）。
type testNudgeState struct {
	// SourceWrites counts non-test source writes since the last test write.
	//
	// SourceWrites 计自上次测试写入以来的非测试源码写入次数。
	SourceWrites int `json:"source_writes"`
	// Nudged records whether the current streak already fired a nudge (one nudge per streak — no per-write spam; a test write re-arms it via reset).
	//
	// Nudged 记录当前连写是否已发过一次提示（每连写最多一次——不逐写刷屏；
	// 测试写入经重置重新武装）。
	Nudged bool `json:"nudged"`
}

// testNudgeThreshold 是触发提示的连写长度：3 次非测试源码写入且 0 配对测试。
// 对齐 task-verify 的小改 fudge factor（3 文件）——低于它就提示，会跟门禁自己
// 豁免的合理小修流程打架。
const testNudgeThreshold = 3

// runTestNudgeHook 处理 PostToolUse Write|Edit（#4-E）：事中测试提醒层。按会话
// 计数非测试源码写入；>=3 且 0 测试写入时注入**一次**事实性提醒
// （test-discipline skill），任何测试文件写入即重置。活跃任务外（taskRef=="")
// 静默——无任务就没有要预演的 test-coverage 门禁，探索性编辑里提醒是噪声。
// 永不阻断：task-verify 的门禁执法；nudge 只是把修复提前（代码还热时改最便宜）。
func runTestNudgeHook(hookInput HookInput, root, version, agent string) error {
	// 先做活跃任务门控：任务外静默（连计数器文件都不落）。
	taskRef := taskRefForSession(root, hookInput.SessionID)
	if taskRef == "" {
		return nil
	}
	// 从 tool_input 抽 file_path（Write/Edit 都带）。
	var fields toolInputFields
	if len(hookInput.ToolInput) > 0 {
		_ = json.Unmarshal(hookInput.ToolInput, &fields)
	}
	if fields.FilePath == "" {
		return nil
	}
	source, test := taskpipeline.ClassifyChangedPath(fields.FilePath)
	if !source && !test {
		return nil // 非源码非测试（配置/文档/资产）：不计数
	}

	stateDir := filepath.Join(os.TempDir(), "forge-testnudge")
	statePath := filepath.Join(stateDir, util.SanitizeSessionID(hookInput.SessionID)+".json")
	var state testNudgeState
	if data, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(data, &state)
	}

	if test {
		// 测试写入重置连写计数并重新武装提示。
		state = testNudgeState{}
		_ = os.MkdirAll(stateDir, 0755)
		_ = util.AtomicWrite(statePath, mustJSONState(state), 0644)
		return nil
	}

	state.SourceWrites++
	nudgeNow := state.SourceWrites >= testNudgeThreshold && !state.Nudged
	if nudgeNow {
		state.Nudged = true
	}
	_ = os.MkdirAll(stateDir, 0755)
	_ = util.AtomicWrite(statePath, mustJSONState(state), 0644)
	if !nudgeNow {
		return nil
	}

	// 事实性提醒，非祈使（与 failure-track 同措辞纪律）。措辞用「source writes」
	// （Write/Edit 事件）而非「source files」——计数器数的是事件，#5 事实性
	// 通道不能陈述错误的文件数（复审 2026-08-22）。
	nudge := fmt.Sprintf(
		"[forge] %d source writes (Write/Edit) have landed in this session with no test file written yet "+
			"(task gate task-verify checks this pairing; whitelist: entry points/generated/pure types). "+
			"Load the test-discipline skill for test quality guards: unit vs e2e split, assertion preservation, fake-test detection.",
		state.SourceWrites)
	// 记录观察（Delivered 用与输出同一通道判定盖章——PostToolUse 上已接线的宿主
	// 都把 allow-detail 送进上下文，但按宿主诚实判定而非假设；kimi 上本 nudge
	// 走 advisory 队列，章标 kimi/advisory-queue，让漏斗区分「入队待投」与
	// 「永久丢失」）。
	delivered, channel := AdvisoryEmissionChannel(agent, hookInput.HookEventName)
	if err := checklog.Record(root, &checklog.Entry{
		Check:        checklog.CheckTestNudge,
		Passed:       true,
		Checked:      true,
		ToolName:     hookInput.ToolName,
		TaskRef:      taskRef,
		SessionID:    hookInput.SessionID,
		Detail:       fmt.Sprintf("test-nudge: %d source writes with no paired test (session streak)", state.SourceWrites),
		Source:       checklog.EvidenceDeterministic,
		Level:        checklog.LevelAdvisory,
		Delivered:    &delivered,
		Channel:      channel,
		ForgeVersion: version,
		Meta:         map[string]string{"source_writes": fmt.Sprintf("%d", state.SourceWrites)},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "[test-nudge] warning: checklog record failed: %v\n", err)
	}
	// emitAdvisoryRouted：kimi 把提示入队、留待 UserPromptSubmit 攒发
	// （test-nudge 在 PostToolUse 上触发，其 stdout 被 kimi 丢弃——2026-08
	// usage 日志审计发现这些记录 100% 未送达）；其余宿主的输出路径不变。
	return EmitAdvisoryRouted(agent, hookInput.HookEventName, "test-nudge", root, hookInput.SessionID, true, nudge)
}

// mustJSONState 序列化 v，失败回落 "{}"——状态文件序列化失败绝不能拖垮 hook
// （advisory 层，设计上 fail-open）。命名 -State 以免与 hook_normalize_test.go
// 的 mustJSON 测试助手撞名。
func mustJSONState(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return data
}
