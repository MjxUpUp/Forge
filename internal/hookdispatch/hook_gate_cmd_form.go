package hookdispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/gatecmdform"
	"github.com/MjxUpUp/Forge/internal/util"
)

// runGateCmdFormHook is the in-process PreToolUse Bash hook for gate-cmd-form (design C,
// docs/design/harness-fixes-a-g-2026-09.md): a forge gate command embedded in a shell pipeline
// with `;` continuation, truncation pipes, or multi-gate rushing weakens the BLOCKED
// prefix/exit-code contract (machine-乙 evidence: pipe-truncation 84%, semicolon 26%).
//
// v1.56 advisory: stderr line + checklog warn row; the BLOCKED ratchet ships with 1.58 per the
// compat commitments' ≥2-minor notice. Never blocks in this version.
//
// runGateCmdFormHook 是 gate-cmd-form 的进程内 PreToolUse Bash hook（设计 C）：嵌在
// 分号续行/截断管道/多门禁连刷形态里的 forge 门禁命令会削弱 BLOCKED 前缀与退出码契约
// （乙机实证：管道截断 84%、分号 26%）。1.56 为 advisory：stderr 一行 + checklog warn 行；
// BLOCKED 化随 1.58 ratchet（承诺表 ≥2 minor 预告）。本版本永不阻断。
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
	if form.Gates == 0 {
		return nil
	}
	if form.Compliant {
		return nil
	}

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
	detail := fmt.Sprintf("gate-cmd-form: %d 个门禁命令嵌在组合形态里（%s）。请单独执行门禁命令或仅接 &&（自 1.58 起 advisory 转 BLOCKED）",
		form.Gates, strings.Join(parts, "；"))

	meta := map[string]string{
		"gates":      fmt.Sprintf("%d", form.Gates),
		"semicolon":  fmt.Sprintf("%t", form.Semicolon),
		"pipe":       fmt.Sprintf("%t", form.PipeTruncated),
		"grep":       fmt.Sprintf("%t", form.GrepMasked),
		"or_chain":   fmt.Sprintf("%t", form.OrChain),
		"multi_gate": fmt.Sprintf("%t", form.MultiGate),
	}
	entry := &checklog.Entry{
		Check:     checklog.CheckGateCmdForm,
		Passed:    false, // advisory 形态违规（本版不阻断）
		Checked:   true,
		Level:     checklog.LevelWarn,
		ToolName:  hookInput.ToolName,
		SessionID: hookInput.SessionID,
		Detail:    util.TruncateRunes(detail, 400),
		Source:    checklog.EvidenceDeterministic,
		Meta:      meta,
	}
	attr := taskAttributionForSession(root, hookInput.SessionID)
	attr.stamp(entry)
	if err := checklog.Record(root, entry); err != nil {
		fmt.Fprintf(os.Stderr, "[gate-cmd-form] warning: checklog record failed: %v\n", err)
	}
	// stderr 提示（stdout 是 allow-path 的 additionalContext 通道，本 hook 走 allow——
	// 提示打到 stderr 与 task_gate 的 advisory 同款，不污染上下文通道）。
	fmt.Fprintf(os.Stderr, "ADVISORY: [gate-cmd-form] %s\n", detail)
	return nil
}
