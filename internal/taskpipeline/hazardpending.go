package taskpipeline

// hazardpending.go — delivery-hardening 墙-1b：hazard 清账（等账未清不得交付）。
// 会话取证（sess_cbe4047c）实证：7 次高危拦截 0 次人工确认，agent 拆命令绕行，
// "HITL 等人"只在 gate 输出里念叨——账悬着照样交。本检查把它接进 complete
// pre-flight：自最近一次重置点（confirm / halt-release）以来存在未确认拦截
// → 拒绝完成。出口只有真人路径：forge hazard confirm --last（用户终端）/ forge
// hazard halt release --yes（人工核查后解锁）——两者都已加真人终端判别，
// agent 管道 stdin 无法自我放行（--trust-foreign 同款判别器）。

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hazard"
	"github.com/MjxUpUp/Forge/internal/util"
)

// CheckNameHazardPending 是清账 pre-flight 的判定行（taskpipeline 字面量——
// 阻断语义行，不入验证白名单：负向等待信号不是正向证据）。
const CheckNameHazardPending checklog.CheckName = "hazard-pending"

// hazardPendingDisableEnv 是清账门的逃生舱（沿 FORGE_SELF_REPORT 模式：v1 仅
// env，逃生必落 checklog 审计）。合法场景：CI 无人环境里历史拦截悬账阻断流水。
const hazardPendingDisableEnv = "FORGE_HAZARD_PENDING"

// CheckHazardPending is the task-complete hazard-ledger pre-flight: it refuses delivery when unconfirmed blocks since the latest reset point belong to this task — the creating session, non-imported SessionLinks, the calling session, or no session at all. Blocks from other sessions of the same project only leave a LevelWarn audit row. A destroyed event stream blocks; a missing project or unreadable stream fails open.
//
// CheckHazardPending 是 task-complete 的清账 pre-flight：自最近重置点以来、归属本任务的
// 未确认高危拦截（创建会话、非导入 SessionLink、本次调用会话 callerSession，以及无会话
// 归属的行）→ 拒绝。同项目其他会话的悬账不阻断，只落 LevelWarn 审计行（2026-10-15：
// 项目级计数曾让一个无关调研会话的拦截卡住所有任务）。callerSession 由调用方解析后显式
// 下传（本包函数不读 env）；未挂接到任务的调用会话也算本任务——在未挂接会话里执行
// complete 不得把自己的拦截洗成「外部悬账」。事件流曾落盘后被删 → 按悬账阻断；项目/
// 事件流解析失败 → fail-open（观察类语义，与 CheckHalt 一致）。
func CheckHazardPending(root string, state *TaskState, callerSession string) (ok bool, reasons []string) {
	if state == nil {
		return true, nil
	}
	if escapeDisabled(state, escapeHazardPending, hazardPendingDisableEnv) {
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckEscapeHatch,
			Passed:  true,
			Checked: true,
			Level:   checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  `escape-hatch: hazard pending gate bypassed (FORGE_HAZARD_PENDING=disable); 存在未经人工确认的高危拦截仍可完成——review 须核查`,
			Meta:    map[string]string{"escape.gate": "hazard-pending", "escape.reason": checklog.EscapeReasonEnv, "escape.owner": "env"},
		})
		return true, nil
	}
	p, err := forgedata.ProjectFor(root)
	if err != nil {
		return true, nil // fail-open：项目/事件流解析失败不阻断交付
	}
	// 审查 P1-2 加固：「事件流存在过又消失」与「从未有事件」必须可区分——
	// 判定单源在 hazard.EventsDestroyed（cli halt release 共用）。按悬账阻断
	// （清账证据被毁不是清白证明）；目录也缺 = 从未有事件，走 PendingBlocksFor 自然
	// 放行（fail-open 边界不变，已知边界见 hazard.EventsDestroyed 注释）。
	if hazard.EventsDestroyed(p) {
		return false, []string{"hazard 事件流文件缺失而 hazards 目录仍在——事件曾落盘后被清除（清账证据被毁不是清白证明）。人工核查后在用户终端执行 forge hazard halt release --yes"}
	}
	// 作用域（2026-10-15）：只有本任务会话（及无会话归属）的拦截阻断交付；同项目其他
	// 会话（如并行的调研会话）的悬账降为 advisory 审计行——项目级计数曾让一个无关
	// 会话的一次拦截卡住所有任务的 complete。
	sp := hazard.PendingBlocksFor(p, taskSessionOwner(state, callerSession))
	if sp.Own <= 0 {
		if sp.Foreign > 0 {
			recordAudit(root, &checklog.Entry{
				Check:   CheckNameHazardPending,
				Passed:  true,
				Checked: true,
				Level:   checklog.LevelWarn,
				TaskRef: state.TaskRef,
				Detail: fmt.Sprintf("hazard-pending: 本任务会话无悬账；同项目其他会话有 %d 个未确认拦截（最近 %s），不阻断本任务交付——须在用户终端 forge hazard confirm --last 或 halt release 核销",
					sp.Foreign, sp.LastForeign.Format("2006-01-02 15:04")),
				Meta: map[string]string{"pending.scope": "foreign", "pending.foreign": fmt.Sprint(sp.Foreign)},
			})
		}
		return true, nil
	}
	return false, []string{fmt.Sprintf(
		"本任务会话自最近确认/解锁以来有 %d 个未经人工确认的高危拦截（最近 %s）——等账未清不得交付。出口（须在用户终端执行，agent 管道无法自我放行）：人工核查被拦命令后 forge hazard confirm --last（放行该命令）；或人工核查全部悬账后 forge hazard halt release --yes 整账核销（safe-halt 停机与未停机悬账均适用）。逃生（无人 CI，落审计）: FORGE_HAZARD_PENDING=disable",
		sp.Own, sp.LastOwn.Format("2006-01-02 15:04"))}
}

// taskSessionOwner returns the ownership predicate for a task's sessions: the creating session, every non-imported SessionLink and the calling session, compared both raw and sanitized. When none of them is known it returns nil, which makes every block count as the task's own (legacy behaviour, conservative).
//
// taskSessionOwner 返回任务会话归属判定：创建会话 + 全部非导入 SessionLink + 本次调用会话，
// 原样与 SanitizeSessionID 后两种形态都比对（hook 事件与任务状态的会话 id 来源不同）。
// 三者都未知时返回 nil——全部拦截计入本任务（旧行为，保守方向）。
func taskSessionOwner(state *TaskState, callerSession string) func(string) bool {
	set := map[string]bool{}
	add := func(id string) {
		if id == "" {
			return
		}
		set[id] = true
		set[util.SanitizeSessionID(id)] = true
	}
	add(state.SessionID)
	add(callerSession)
	for _, l := range state.SessionLinks {
		if !l.Imported {
			add(l.SessionID)
		}
	}
	if len(set) == 0 {
		return nil
	}
	return func(id string) bool { return set[id] || set[util.SanitizeSessionID(id)] }
}
