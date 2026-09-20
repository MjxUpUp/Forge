package hookdispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/gatecmdform"
	"github.com/MjxUpUp/Forge/internal/util"
)

// hook_gate_cmd_form.go — gate-cmd-form 的进程内 PreToolUse Bash hook（设计 C）：
// 嵌在分号续行/截断管道/多门禁连刷形态里的 forge 门禁命令会削弱 BLOCKED 前缀与
// 退出码契约（乙机实证：管道截断 84%、分号 26%）。
//
// BLOCK ratchet（delivery-hardening 墙-1，2026-09-20）：1.56 的 advisory 文案已带
// ≥2 minor 预告（「自 1.58 起 advisory 转 BLOCKED」）；机械版本门定 1.67.0 生效
// （task-drift 先例——不再留文字债）。逃生 FORGE_GATE_CMD_FORM=0（审计行+会话
// 节流）；会话阻断上限 5（有界 deny，防死循环——CC Stop 8 次强制放行同哲学）。

// gateCmdFormBlockVersion/Cap/EscapeEnv 是 ratchet 常量（delivery-hardening 墙-1）。
const (
	gateCmdFormBlockVersion = "1.67.0"
	gateCmdFormBlockCap     = 5
	gateCmdFormEscapeEnv    = "FORGE_GATE_CMD_FORM"
)

// runGateCmdFormHook 判定链：门禁命令 × 非合规形态 → checklog 行；BLOCK 模式
// （版本门+非逃生+未超会话上限）→ deny（exit 2，当场生效，不经 advisory 队列）。
func runGateCmdFormHook(hookInput HookInput, root, version, agent string) error {
	if hookInput.ToolName != "Bash" || len(hookInput.ToolInput) == 0 {
		return nil
	}
	var fields struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(hookInput.ToolInput, &fields); err != nil || fields.Command == "" {
		return nil
	}
	form := gatecmdform.Classify(fields.Command)
	if form.Gates == 0 || form.Compliant {
		return nil
	}

	parts := gateCmdFormBadParts(form)
	detail := fmt.Sprintf("gate-cmd-form: %d 个门禁命令嵌在组合形态里（%s）。请单独执行门禁命令或仅接 &&（自 1.67 起 advisory 转 BLOCKED）",
		form.Gates, strings.Join(parts, "；"))

	entry := &checklog.Entry{
		Check:     checklog.CheckGateCmdForm,
		Passed:    false, // 形态违规事实
		Checked:   true,
		Level:     checklog.LevelWarn,
		ToolName:  hookInput.ToolName,
		SessionID: hookInput.SessionID,
		Detail:    util.TruncateRunes(detail, 400),
		Source:    checklog.EvidenceDeterministic,
		Meta:      gateCmdFormMeta(form),
	}

	// BLOCK ratchet：版本门 + env 逃生（会话节流审计行）+ 会话上限（有界）。
	// HasPrefix "1." 限定与 task-drift 同理：2.x 主版本到来时版本门失明回落
	// advisory，主版本升级须重估本 ratchet。
	// 版本串剥离构建后缀（审查 P2-1）：发布构建的 Version 是
	// "1.67.0 (commit: …, built: …)"——后缀含 "-" 会被 CompareVersions 按
	// 预发布规则判小于目标版本，ratchet 跳过自己的首发版。取首个空白分隔
	// 字段做纯版本比较。
	verCore := version
	if fields := strings.Fields(version); len(fields) > 0 {
		verCore = fields[0]
	}
	blockMode := strings.HasPrefix(verCore, "1.") && util.CompareVersions(verCore, gateCmdFormBlockVersion) >= 0
	if blockMode && strings.TrimSpace(os.Getenv(gateCmdFormEscapeEnv)) == "0" {
		recordGateCmdFormEscape(root, hookInput.SessionID, version)
		blockMode = false
	}
	if blockMode && gateCmdFormBlockCount(root, hookInput.SessionID, false) >= gateCmdFormBlockCap {
		blockMode = false // 会话阻断上限已过——降级 advisory（有界，防 deny 死循环）
	}
	if blockMode {
		entry.Level = checklog.LevelFail
		entry.Meta["blocked"] = "true"
		// deny 行刻意不盖 Delivered/Channel（task-drift 同注：deny 的送达面语义不同）。
	} else {
		delivered, channel := AdvisoryEmissionChannel(agent, hookInput.HookEventName)
		entry.Delivered = &delivered
		entry.Channel = channel
	}
	if err := checklog.Record(root, entry); err != nil {
		fmt.Fprintf(os.Stderr, "[gate-cmd-form] warning: checklog record failed: %v\n", err)
	}
	if blockMode {
		_ = gateCmdFormBlockCount(root, hookInput.SessionID, true)
		blockDetail := fmt.Sprintf("gate-cmd-form：本命令已拦截——%d 个门禁命令嵌在组合形态里（%s）。分号让前一门禁 BLOCKED 后链条照走、截断管道吞掉 BLOCKED 文本面，退出码契约被系统性削弱。重发为单独一条门禁命令，或仅用 && 串联。逃生（落 checklog 审计）: FORGE_GATE_CMD_FORM=0；本会话阻断上限 %d 次，超限自动降级 advisory。",
			form.Gates, strings.Join(parts, "；"), gateCmdFormBlockCap)
		return EmitAgentOutput(agent, hookInput.HookEventName, "gate-cmd-form", false, blockDetail)
	}

	// stderr 提示（stdout 是 allow-path 的 additionalContext 通道——不污染上下文）。
	fmt.Fprintf(os.Stderr, "ADVISORY: [gate-cmd-form] %s\n", detail)
	return nil
}

// gateCmdFormBadParts 渲染形态违规清单（advisory 文案与 deny 文案共用）。
func gateCmdFormBadParts(form gatecmdform.Form) []string {
	var parts []string
	if form.Semicolon {
		parts = append(parts, "分号续行——前一门禁 BLOCKED 后链条照常执行，退出码契约失效")
	}
	if form.PipeTruncated {
		parts = append(parts, "管道截断——BLOCKED 文本面可能被吞（tail/head/grep/cut/sed/awk/wc）")
	}
	if form.GrepMasked {
		parts = append(parts, "grep 过滤——BLOCKED 行会被过滤掉的最强掩蔽形态")
	}
	if form.OrChain {
		parts = append(parts, "|| 链——门禁退出码被短路")
	}
	if form.MultiGate {
		parts = append(parts, "多门禁连刷——单次调用应只跑一道门禁")
	}
	return parts
}

// gateCmdFormMeta 是 checklog Meta 的单一构造点（block/advisory 两侧共用）。
func gateCmdFormMeta(form gatecmdform.Form) map[string]string {
	return map[string]string{
		"gates":      fmt.Sprintf("%d", form.Gates),
		"semicolon":  fmt.Sprintf("%t", form.Semicolon),
		"pipe":       fmt.Sprintf("%t", form.PipeTruncated),
		"grep":       fmt.Sprintf("%t", form.GrepMasked),
		"or_chain":   fmt.Sprintf("%t", form.OrChain),
		"multi_gate": fmt.Sprintf("%t", form.MultiGate),
	}
}

// gateCmdFormBlockCount 会话阻断计数（markers/forge-gatecmdform-blocks-<session>）；
// bump=true 自增后返回序号，false 只读（判上限）。
func gateCmdFormBlockCount(root, sessionID string, bump bool) int {
	path := filepath.Join(forgedata.DataDirFor(root), "markers", "forge-gatecmdform-blocks-"+util.SanitizeSessionID(sessionID))
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	n := 0
	if data, err := os.ReadFile(path); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &n)
	}
	if bump {
		n++
		_ = util.AtomicWrite(path, []byte(fmt.Sprintf("%d", n)), 0644)
	}
	return n
}

// recordGateCmdFormEscape 落 FORGE_GATE_CMD_FORM=0 逃生行（会话节流一次——env 常开
// 时每条形态违规都落行只会刷 checklog，首行已定性入审计）。
func recordGateCmdFormEscape(root, sessionID, version string) {
	marker := filepath.Join(forgedata.DataDirFor(root), "markers", "forge-gatecmdform-escape-"+util.SanitizeSessionID(sessionID))
	if _, err := os.Stat(marker); err == nil {
		return
	}
	entry := &checklog.Entry{
		Check:        checklog.CheckEscapeHatch,
		Passed:       true,
		Checked:      true,
		SessionID:    sessionID,
		Detail:       "escape-hatch: gate-cmd-form BLOCK 降级为 advisory(env FORGE_GATE_CMD_FORM=0)",
		Source:       checklog.EvidenceDeterministic,
		Level:        checklog.LevelWarn,
		ForgeVersion: version,
		Meta:         map[string]string{"gate": "gate-cmd-form", "reason": "env", "owner": "env"},
	}
	if err := checklog.Record(root, entry); err != nil {
		fmt.Fprintf(os.Stderr, "[gate-cmd-form] warning: escape-hatch record failed: %v\n", err)
	}
	_ = os.MkdirAll(filepath.Dir(marker), 0755)
	_ = util.AtomicWrite(marker, []byte("1"), 0o644)
}
