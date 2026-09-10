package hookdispatch

import "github.com/MjxUpUp/Forge/internal/checklog"

// attributionMeta builds the attribution-probe Meta for a hook checklog row: which detection path resolved the active task and whether that task's evidence is already sealed.
//
// attributionMeta 构造 hook checklog 行的归因探针 Meta（docs/design/harness-fixes-a-g-2026-09.md
// E.1/E.2）：active task 经哪条路径解析到（taskpipeline.ResolvePath*），以及该任务证据
// 是否已封印（task-complete 门禁已过——此后落到任务名下的行保留供 trace，评分/结论按
// SealedAt 截断）。无 active task 返回 nil（Meta omitempty，旧形态不变）。
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
