package hookdispatch

import (
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// taskAttribution is the resolved active-task context a hook stamps onto every checklog row it writes: which task, via which detection path, whether that task's evidence is already sealed, and the task's creating session (session backfill source).
//
// taskAttribution 是 hook 写 checklog 行时盖上去的活跃任务归因上下文（docs/design/
// harness-fixes-a-g-2026-09.md E.1/E.2/E.4）：归到哪个任务、经哪条探测路径解析到、该任务
// 证据是否已封印、任务创建会话（空 session 行的回填来源）。hookdispatch 共 6 处
// checklog 写点（噪声门主记录、kimi-plugin-stale、failure-track、subagent-track、
// test-nudge、conventions）——全部经 taskAttributionForSession + stamp 走同一份归因，
// 探针覆盖面才是全集，M 度量的 E1/E2 计数不被系统性低估。
type taskAttribution struct {
	TaskRef     string
	ResolvePath string
	TaskSession string
	Sealed      bool
}

// taskAttributionForSession resolves the active task for a host session once (ActiveTaskStateWithPath) and packages the attribution fields.
//
// taskAttributionForSession 为宿主 session 解析一次活跃任务（ActiveTaskStateWithPath）并打包
// 归因字段；未解析到任务返回零值（stamp 为 no-op）。
func taskAttributionForSession(root, sessionID string) taskAttribution {
	active, path, err := taskpipeline.ActiveTaskStateWithPath(root, sessionID)
	if err != nil || active == nil {
		return taskAttribution{}
	}
	return taskAttribution{
		TaskRef:     active.TaskRef,
		ResolvePath: path,
		TaskSession: active.SessionID,
		Sealed:      active.EvidenceSealed(),
	}
}

// taskRefForSession returns the active task ref for a session ("" when none) — thin wrapper kept for the call sites that only need the ref.
//
// taskRefForSession 返回 session 的活跃任务 ref（无则空串）——只需 ref 的调用点沿用的
// 薄包装。写 checklog 行的调用点应改用 taskAttributionForSession + stamp，否则探针缺失。
func taskRefForSession(root, sessionID string) string {
	return taskAttributionForSession(root, sessionID).TaskRef
}

// stamp merges the attribution probe into e.Meta (existing keys kept) and backfills an empty SessionID from the task's creating session; no-op without a resolved task.
//
// stamp 把归因探针并入 e.Meta（既有键保留）并为空 SessionID 回填任务创建会话；未解析到
// 任务时 no-op。Meta 合并而非覆盖：failure-track/test-nudge 等写点自带 tool/agent_id
// 等键。
func (a taskAttribution) stamp(e *checklog.Entry) {
	if a.TaskRef == "" || e == nil {
		return
	}
	for k, v := range attributionMeta(a.ResolvePath, a.Sealed) {
		if e.Meta == nil {
			e.Meta = map[string]string{}
		}
		e.Meta[k] = v
	}
	e.SessionID = attributedSession(e.SessionID, e.TaskRef, a.TaskSession)
}

// attributionMeta builds the attribution-probe Meta for a hook checklog row: which detection path resolved the active task and whether that task's evidence is already sealed.
//
// attributionMeta 构造 hook checklog 行的归因探针 Meta：active task 经哪条路径解析到
// （taskpipeline.ResolvePath*），以及该任务证据是否已封印（task-complete 门禁已过——此后
// 落到任务名下的行保留供 trace，评分/结论按 SealedAt 截断）。无 active task 返回 nil
// （Meta omitempty，旧形态不变）。
func attributionMeta(resolvePath string, sealed bool) map[string]string {
	if resolvePath == "" {
		return nil
	}
	m := map[string]string{checklog.MetaKeyResolvePath: resolvePath}
	if sealed {
		m[checklog.MetaKeyPostSeal] = "true"
	}
	return m
}

// attributedSession returns the host session id, falling back to the attributed task's creating session when the host sent none.
//
// attributedSession 返回宿主 session id；宿主未传（cursor Stop/tool 事件、legacy 宿主、
// 无 env 的 CLI 派生 hook）而行已归属某任务时，回填该任务的创建会话——乙机 59% 的
// checklog 行无 session_id，评分的会话级读方因此只能靠 TaskRef 归属（E.4）。未归属任务
// 的行保持空串：没有事实依据就不编造。
func attributedSession(hostSession, taskRef, taskSession string) string {
	if hostSession != "" || taskRef == "" {
		return hostSession
	}
	return taskSession
}
