package hazard

// halt.go — safe-halt 语义（focus-batches §2b，方向 B）：hazard-guard 连续拦截
// 达阈值 → 会话进入 safe-halt：停止自修复、要求人审解锁。学术依据 ASE 2026
// （arXiv 2605.30777，547 起真实安全事件）：护栏必须"enforce environmental
// constraints, failure transparency, and safe-halt behaviors"——连续撞墙的 agent
// 正在盲试破坏性操作，让它继续等于把爆炸半径交给运气。
//
// 判定：events.jsonl 里最近一次 EventConfirm/EventHaltRelease 之后的 EventBlock
// 计数 ≥ HaltThreshold（默认 3）→ halted。confirm（单命令受信）与 halt-release
// （人审解锁）都是重置点——确认过的高危命令不是盲试。
//
// 诚实边界：v1 的"停"是门禁侧信号（task-verify 输出 + CLI 状态），不是进程级
// SIGSTOP——宿主进程控制不在 forge 手里（红线：不替代 agent 循环）。

import (
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// HaltThreshold 是触发 safe-halt 的连续未确认拦截数。3 = ASE 2026 "反复盲试"
// 的经验量级：第一次可能是误会，第二次是执念，第三次是模式。
const HaltThreshold = 3

// EventHaltRelease 是 safe-halt 的人工解锁事件（forge hazard halt release）。
const EventHaltRelease = "halt-release"

// HaltState 报告本项目的 safe-halt 状态：自最近一次重置点（confirm / halt-release）
// 以来的连续拦截数，以及是否达阈值。events 为空/读失败 → 未停机（fail-open，
// 审计缺失不该瘫痪工作流——观察类语义）。
type HaltState struct {
	Halted    bool      `json:"halted"`
	Blocks    int       `json:"blocks"`
	LastBlock time.Time `json:"last_block,omitempty"`
	LastReset time.Time `json:"last_reset,omitempty"`
}

// CheckHalt 从事件流计算 safe-halt 状态。
func CheckHalt(p *forgedata.Project) HaltState {
	var st HaltState
	events, err := LoadEvents(p)
	if err != nil {
		return st
	}
	// 事件文件内时间序：从头累计，遇重置点清零。最后的 Block 计数即"自最近重置
	// 以来的连续拦截"。
	for _, e := range events {
		switch e.Type {
		case EventBlock:
			st.Blocks++
			st.LastBlock = e.Ts
		case EventConfirm, EventHaltRelease:
			st.Blocks = 0
			st.LastReset = e.Ts
		}
	}
	st.Halted = st.Blocks >= HaltThreshold
	return st
}

// ReleaseHalt 记录一次人工解锁（追加 EventHaltRelease——既是重置点也是审计行：
// 谁、何时解的锁可回溯）。
func ReleaseHalt(p *forgedata.Project) error {
	return AppendEvent(p, Event{Type: EventHaltRelease})
}
