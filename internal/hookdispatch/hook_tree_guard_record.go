package hookdispatch

import (
	"crypto/sha256"
	"encoding/hex"
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

// announceDeadAnchor 打印一次死锚告警（按会话+任务+锚路径节流）。
//
// 为什么需要节流：一次 Write|Edit 会依次经过 freeze-guard、task-guard、
// assertion-check、read-before-edit 四个 hook，它们都走到同一个错树解析点；不节流
// 就是同一条 stderr 连打四遍，噪声会把真正要看的那条埋掉。锚路径进 marker 名，
// 换了死锚（旧锚被清理、新锚又失效）时还能再提示一次。
//
// announceDeadAnchor prints the dead-anchor notice at most once per session+task+anchor:
// one edit passes through four hooks that reach the same resolution point.
func announceDeadAnchor(root, taskRef, sessionID, anchor string) {
	if anchor == "" {
		return
	}
	if root != "" {
		marker := filepath.Join(forgedata.DataDirFor(root), "markers",
			"forge-treeguard-deadanchor-"+util.SanitizeSessionID(sessionID)+"-"+
				util.SanitizeSessionID(taskRef)+"-"+shortDigest(anchor))
		if _, err := os.Stat(marker); err == nil {
			return
		}
		_ = os.MkdirAll(filepath.Dir(marker), 0o755)
		_ = util.AtomicWrite(marker, []byte("1"), 0o644)
	}
	fmt.Fprint(os.Stderr, deadAnchorWarning(taskRef, anchor))
}

// shortDigest 取 sha256 前 12 位十六进制。marker 名里不能直接放锚路径：路径共享前缀
// 且 SanitizeSessionID 会截断/收敛字符，长路径会撞成同一个 marker（换死锚后不再提示）。
func shortDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}
