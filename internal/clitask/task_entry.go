package clitask

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/change"
)

// task_entry.go —— 入口路由（A1，2026-11-15 修订）：任务是一等入口，change 可后补。
//
// 早期版本把「先立 change」做成 task start 的硬前置，把功能改动与一句话小修的两条
// 正路堵死了一半：真实使用里 agent 先开工再补记录是常态，硬拦只会换来
// 「--kind task 绕一下」或换个说法重试。所以这里只保留链接检查：**显式带
// --change 时**，那个 change 必须已经 ready（否则钉进去的 goal 契约与阶段不符）；
// 不带 --change 一律放行，由 `forge next` 的建议与人/agent 的判断决定是否补记录。
//
// 三者的关系（与 AGENTS.md 一致）：
//   - task 是执行单元，随时可开；
//   - change 是记录单元，可以在开工前立，也可以中途补，完成后 `forge change verify`；
//   - fast 是 change 的轻量形态，不是 task 的形态。

// guardEntryRouting 检查本次 task start 是否可以放行。
//
// 规则：没带 --change 直接放行；带了 --change 就要求它已经 ready——这是「链接
// 契约」的检查，不是「开工许可」的检查。
func guardEntryRouting(root, kind, changeID string) error {
	if changeID == "" {
		return nil
	}
	phase, missing, err := change.ComputePhase(root, changeID)
	if err != nil {
		return err
	}
	if phase == change.PhaseReady {
		return nil
	}
	hint := "forge change phase " + changeID
	if len(missing) > 0 {
		hint = missing[len(missing)-1]
	}
	return fmt.Errorf("change: %s 还停在 %s 阶段——先补齐产物再链接（%s；完整清单：forge change phase %s）；也可以先不带 --change 开工，事后再补 change", changeID, phase, hint, changeID)
}
