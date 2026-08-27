package attribution

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/hostcap"
)

// hookEventInput is the minimal hook-payload shape RecordHookEvent needs (declared locally
// to avoid importing cli — attribution is a leaf service). Field names match cli.HookInput's
// JSON contract.
//
// hookEventInput 是 RecordHookEvent 需要的最小 hook 载荷形状（本地声明避免 import
// cli——attribution 是叶子服务）。字段名与 cli.HookInput 的 JSON 契约一致。
type hookEventInput struct {
	HookEventName string          `json:"hook_event_name"`
	SessionID     string          `json:"session_id"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
}

type toolInputShape struct {
	FilePath string `json:"file_path"`
	Command  string `json:"command"`
}

// RecordHookEvent is the dispatcher seam (multi-task-concurrency §6): called for every
// hook invocation, it turns PostToolUse write-ish events into ledger entries. Silent by
// design — the ledger is observability input; recording must never break a hook. Skips:
// non-PostToolUse events, empty session ids (no-identity hosts — degraded mode, nothing
// to attribute to), read-only tools.
//
// RecordHookEvent 是分发器挂点（multi-task-concurrency §6）：每次 hook 调用都会进
// 来，把 PostToolUse 的写入类事件转成台账条目。设计上静默——台账是可观测性输入，
// 记账绝不能打断 hook。跳过：非 PostToolUse 事件、空 session id（无身份宿主——降级
// 模式，无从归属）、只读工具。
func RecordHookEvent(root string, hookEventName, sessionID, toolName string, toolInput json.RawMessage) {
	if root == "" || sessionID == "" || hookEventName != "PostToolUse" {
		return
	}
	var fields toolInputShape
	if len(toolInput) > 0 {
		_ = json.Unmarshal(toolInput, &fields)
	}
	now := time.Now()
	switch {
	case toolName == "Write":
		if fields.FilePath != "" {
			Record(root, Event{Ts: now, Sid: sessionID, Kind: KindWrite, Path: fields.FilePath})
		}
	case toolName == "Edit" || hostcap.IsPatchTool(toolName):
		// Patch tools (codex apply_patch) carry the target only inside the patch text
		// (tool_input.command) — synthesize the FIRST Add/Update/Delete File header's path,
		// mirroring cli.applyPatchFilePath (duplicated as a leaf package, same discipline
		// as checklog's Detail-prefix literals; multi-file patches take the first target —
		// documented limitation).
		//
		// patch 工具（codex apply_patch）的目标只在 patch 文本里（tool_input.command）——
		// 合成第一个 Add/Update/Delete File 头的路径，镜像 cli.applyPatchFilePath
		//（叶子包内重复，与 checklog 的 Detail 前缀字面量同纪律；多文件 patch 取第一个
		// 目标——已文档化的限制）。
		if fields.FilePath == "" {
			fields.FilePath = patchFilePath(fields.Command)
		}
		if fields.FilePath != "" {
			Record(root, Event{Ts: now, Sid: sessionID, Kind: KindEdit, Path: fields.FilePath})
		}
	case toolName == "Bash":
		if fields.Command == "" {
			return
		}
		var events []Event
		for _, p := range bashWriteTargets(fields.Command) {
			events = append(events, Event{Ts: now, Sid: sessionID, Kind: KindBashInfer, Path: p})
		}
		if len(events) > 0 {
			Record(root, events...)
		}
	}
}

// patchFilePath extracts the first "*** Add/Update/Delete File: <path>" header from a
// patch body; empty when none. Mirrors cli.applyPatchFilePath (leaf-package duplication).
//
// patchFilePath 从 patch 文本抽第一个 "*** Add/Update/Delete File: <path>" 头；
// 没有则空。镜像 cli.applyPatchFilePath（叶子包重复）。
func patchFilePath(patch string) string {
	for _, line := range strings.Split(patch, "\n") {
		body := strings.TrimPrefix(strings.TrimSpace(line), "*** ")
		for _, header := range []string{"Add File:", "Update File:", "Delete File:"} {
			if strings.HasPrefix(body, header) {
				if p := strings.TrimSpace(strings.TrimPrefix(body, header)); p != "" {
					return p
				}
			}
		}
	}
	return ""
}
