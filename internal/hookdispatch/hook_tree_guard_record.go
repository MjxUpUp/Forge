package hookdispatch

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/MjxUpUp/Forge/internal/worktree"
)

// recordTreeGuardEscape 把「关掉错树守护」写成 escape-hatch 行（docs/design/task-tree-guard.md §5）。
// 为什么在 hook 侧也要记：guard 脚本在 FORGE_TASK_TREE_GUARD=0 时整个判定被跳过——只有
// Write|Edit 而不推进门禁的会话，CLI 侧那条记录永远不会产生，守护就成了静默闸门。
// 每会话每任务至多一条（marker 节流，与 recordGateCmdFormEscape 同形）。
//
// recordTreeGuardEscape records "guard disabled" as an escape-hatch row. The hook side
// needs it too: under FORGE_TASK_TREE_GUARD=0 the bash guard skips the check entirely, so
// a session that only edits files (never runs a gate) would leave no trace at all.
func recordTreeGuardEscape(root, taskRef, sessionID string) {
	marker := filepath.Join(forgedata.DataDirFor(root), "markers",
		"forge-treeguard-escape-"+util.SanitizeSessionID(sessionID)+"-"+util.SanitizeSessionID(taskRef))
	if _, err := os.Stat(marker); err == nil {
		return
	}
	if err := checklog.Record(root, &checklog.Entry{
		Check:     checklog.CheckEscapeHatch,
		Passed:    true,
		Checked:   true,
		Level:     checklog.LevelWarn,
		TaskRef:   taskRef,
		SessionID: sessionID,
		Source:    checklog.EvidenceDeterministic,
		Detail:    "escape-hatch: task-tree-guard disabled (env FORGE_TASK_TREE_GUARD=0) — 错树 guard 放行",
		Meta: map[string]string{
			"escape.gate":   "task-tree-guard",
			"escape.reason": checklog.EscapeReasonEnv,
			"escape.owner":  "env",
		},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "[task-tree] warning: escape-hatch record failed: %v\n", err)
	}
	_ = os.MkdirAll(filepath.Dir(marker), 0o755)
	_ = util.AtomicWrite(marker, []byte("1"), 0o644)
}

// recordTreeGuardBlock 把一次 off-tree 编辑拦截记成 tree-guard 行（`forge trace` 可见）。
// 每条编辑都会被 guard 拦，所以按会话+任务节流一次——记录的目的是「有多少会话真的在
// 错树上写代码」，不是逐次告警计数。
//
// recordTreeGuardBlock records one blocked off-tree edit (throttled once per session+task:
// the point is "which sessions really edit on the wrong tree", not per-edit spam).
func recordTreeGuardBlock(root, taskRef, sessionID, anchor string) {
	marker := filepath.Join(forgedata.DataDirFor(root), "markers",
		"forge-treeguard-block-"+util.SanitizeSessionID(sessionID)+"-"+util.SanitizeSessionID(taskRef))
	if _, err := os.Stat(marker); err == nil {
		return
	}
	if err := checklog.Record(root, &checklog.Entry{
		Check:     checklog.CheckTreeGuard,
		Passed:    false,
		Checked:   true,
		Level:     checklog.LevelWarn,
		TaskRef:   taskRef,
		SessionID: sessionID,
		Source:    checklog.EvidenceDeterministic,
		Detail:    fmt.Sprintf("tree-guard: off-tree（当前树不是任务锚树；锚=%s）", anchor),
		Meta: map[string]string{
			"tree_guard":   "off-tree",
			"anchor":       anchor,
			"wtid":         worktree.ID(root),
			"resolve_path": "active-file-offtree",
			"layer":        "task-guard",
		},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "[task-tree] warning: tree-guard record failed: %v\n", err)
	}
	_ = os.MkdirAll(filepath.Dir(marker), 0o755)
	_ = util.AtomicWrite(marker, []byte("1"), 0o644)
}
