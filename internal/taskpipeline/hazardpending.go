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
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hazard"
)

// CheckNameHazardPending 是清账 pre-flight 的判定行（taskpipeline 字面量——
// 阻断语义行，不入验证白名单：负向等待信号不是正向证据）。
const CheckNameHazardPending checklog.CheckName = "hazard-pending"

// hazardPendingDisableEnv 是清账门的逃生舱（沿 FORGE_SELF_REPORT 模式：v1 仅
// env，逃生必落 checklog 审计）。合法场景：CI 无人环境里历史拦截悬账阻断流水。
const hazardPendingDisableEnv = "FORGE_HAZARD_PENDING"

// CheckHazardPending 是 task-complete 的清账 pre-flight：本项目自最近重置点
// 以来有未确认的高危拦截（hazard.CheckHalt.Blocks>0）→ 拒绝。事件流缺失/
// 项目解析失败 → fail-open（观察类语义，与 CheckHalt 一致——审计缺失不该
// 瘫痪交付，但正常路径下事件流由 hook 持续写入）。
func CheckHazardPending(root string, state *TaskState) (ok bool, reasons []string) {
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
	// hazards 目录只由 AppendEvent 创建（目录在 = 事件曾落过盘），events.jsonl
	// 却缺失 = 被清除。按悬账阻断（清账证据被毁不是清白证明）。
	// 复审 P1-2 放宽：目录【存在】而 events.jsonl 缺失即阻断（不论空否——空
	// 目录同样只由 AppendEvent 创建过；confirm 标记 5min TTL 过期被清是常态，
	// events.jsonl 常是唯一残留文件，rm 它后目录为空曾被放过）。目录整删
	// （os.Stat err）与"从未有事件"不可区分，v1 保留 fail-open——已记为已知
	// 边界（目录级锚是后续项）。
	eventsPath := filepath.Join(p.DataDir, "hazards", "events.jsonl")
	if _, serr := os.Stat(eventsPath); os.IsNotExist(serr) {
		if _, derr := os.Stat(filepath.Join(p.DataDir, "hazards")); derr == nil {
			return false, []string{"hazard 事件流文件缺失而 hazards 目录仍在——事件曾落盘后被清除（清账证据被毁不是清白证明）。人工核查后在用户终端执行 forge hazard halt release --yes"}
		}
		return true, nil // 目录也不存在：从未有事件，干净
	}
	st := hazard.CheckHalt(p)
	if st.Blocks <= 0 {
		return true, nil
	}
	return false, []string{fmt.Sprintf(
		"本项目自最近确认/解锁以来有 %d 个未经人工确认的高危拦截（最近 %s）——等账未清不得交付。出口（须在用户终端执行，agent 管道无法自我放行）：人工核查被拦命令后 forge hazard confirm --last；或确认为误拦/已处置后 forge hazard halt release --yes。逃生（无人 CI，落审计）: FORGE_HAZARD_PENDING=disable",
		st.Blocks, st.LastBlock.Format("2006-01-02 15:04"))}
}
