package hookdispatch

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
)

// hook_lazy_attach.go：首次源码写入懒挂接（按需拉取方案 REQ-7/INV-4）。SessionStart 不再自动
// 锚定 session 之后，没跑 `forge task resume --ref` 就直接改代码的 session 不会出现在任务的
// SessionLinks 里——分支误挂守卫（taskAnchoredByOtherActiveSession）看不到它，节点租约也不会
// 转过来。task-guard 放行一次源码写入时补上这条记录。挂在 task-guard 上而非 PostToolUse：
// task-guard 在 lite 档也接线（profileLiteHooks），PostToolUse Write|Edit 组的 hook 都不在。

// lazyAttachOnSourceWrite anchors the session to the active task the first time task-guard allows a source write in it; silent, advisory, never blocks.
//
// lazyAttachOnSourceWrite 在 task-guard 放行本 session 的源码写入时，若该 session 尚未锚定到
// 活跃任务，静默锚定一次。只写 SessionLinks 与租约（taskpipeline.AttachSession），不改门禁、
// 不查产物链、不向模型输出——任务做到哪一步都不要求补材料。已锚定时只做一次内存判断、不取锁；
// 锚定失败（锁超时/磁盘错误）只记 stderr，不影响放行。agent 为空（claude-code 形宿主）时
// AttachSession 回落项目标记弱信号定 tool。
func lazyAttachOnSourceWrite(root, sessionID, agent, filePath string, active *taskpipeline.TaskState) {
	sid := util.SanitizeSessionID(sessionID)
	if active == nil || sid == "" || filePath == "" || !taskpipeline.IsSourceExt(filePath) {
		return
	}
	if active.HasSession(sid) {
		return
	}
	if _, err := taskpipeline.AttachSession(root, active.TaskRef, sid, agent); err != nil {
		fmt.Fprintf(os.Stderr, "[forge] lazy attach to %s failed (advisory): %v\n", active.TaskRef, err)
	}
}
