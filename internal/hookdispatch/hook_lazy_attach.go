package hookdispatch

import (
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
)

// hook_lazy_attach.go：首次源码写入懒挂接（按需拉取方案 REQ-7/INV-4）。SessionStart 不再自动
// 锚定 session 之后，没跑 `forge task resume --ref` 就直接改代码的 session 不会出现在任务的
// SessionLinks 里——任务记录里看不到它参与过，节点租约也不会转到它所在的机器。task-guard 放行
// 一次源码写入时补上这条记录。注意它只写 SessionLinks 与租约、不写 session 级 active-task-ref，
// 故不会让分支误挂守卫（taskAnchoredByOtherActiveSession，只认带新鲜 active-task-ref 的链接）
// 看见这个 session——那是 resume --ref 显式接手才有的语义。
//
// 挂在 task-guard 上而非 PostToolUse：task-guard 在 lite 档也接线（profileLiteHooks），
// PostToolUse Write|Edit 组的 hook 都不在。代价：PreToolUse 时写入尚未发生，同一事件上并行
// 的 freeze-guard / read-before-edit 若拦下写入，挂接也已发生——「打算改这个任务的代码」足以
// 说明 session 在做这个任务，接受。

// lazyAttachOnSourceWrite anchors the session to the active task the first time task-guard allows a source write in it; silent, advisory, never blocks.
//
// lazyAttachOnSourceWrite 在 task-guard 放行本 session 的源码写入时，若该 session 尚未锚定到
// 活跃任务，静默锚定一次。只写 SessionLinks 与租约（taskpipeline.AttachSession），不改门禁、
// 不查产物链、不向模型输出——任务做到哪一步都不要求补材料。以下情况不挂接：无活跃任务、宿主
// 没给 session id（须判原始值——util.SanitizeSessionID 对空串返回占位 "session"，清洗后判空
// 永不成立，会给所有无 id 的 session 挂上同一条假链接）、非源码后缀、写入目标在项目根之外。
// 已锚定时只做一次内存判断、不取锁；锚定失败（锁超时/磁盘错误）只记 stderr，不影响放行。
// agent 为空（claude-code 形宿主）时 AttachSession 回落项目标记弱信号定 tool。
func lazyAttachOnSourceWrite(root, sessionID, agent, filePath string, active *taskpipeline.TaskState) {
	if active == nil || sessionID == "" || filePath == "" || !taskpipeline.IsSourceExt(filePath) {
		return
	}
	if rel := toRelPath(root, filePath); rel == ".." || strings.HasPrefix(rel, "../") {
		return
	}
	sid := util.SanitizeSessionID(sessionID)
	if active.HasSession(sid) {
		return
	}
	if _, err := taskpipeline.AttachSession(root, active.TaskRef, sid, agent); err != nil {
		fmt.Fprintf(os.Stderr, "[forge] lazy attach to %s failed (advisory): %v\n", active.TaskRef, err)
	}
}
