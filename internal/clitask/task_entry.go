package clitask

import (
	"fmt"
	"github.com/MjxUpUp/Forge/internal/change"
	"github.com/MjxUpUp/Forge/internal/specroot"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// task_entry.go —— 入口路由（A1）：声明了 spec 流程的仓库，功能改动必须先有 ready 的
// change；一句话小修用 --kind task 显式出口。
//
// 为什么在 task start 拦：next 只会给建议，agent 可以不跑 next。把「先立 change」变成
// start 的前置条件，才和 goal 契约（implement 校验漂移）形成前后夹击：开工前要求
// 契约齐备，开工后要求契约不变。

// guardEntryRouting 检查本次 task start 是否允许放行。
func guardEntryRouting(root, kind, changeID string) error {
	if kind == tasktypes.TaskKindGeneric || kind == tasktypes.TaskKindTask {
		return nil
	}
	if changeID != "" {
		phase, missing, err := change.ComputePhase(root, changeID)
		if err != nil {
			return err
		}
		if phase != change.PhaseReady {
			hint := "forge change phase " + changeID
			if len(missing) > 0 {
				hint = missing[len(missing)-1]
			}
			return fmt.Errorf("change: %s 还停在 %s 阶段——先把产物补齐（%s）", changeID, phase, hint)
		}
		return nil
	}
	if !specroot.Declared(root) {
		return nil
	}
	return fmt.Errorf("这个仓库用 spec 流程（spec/forge.yaml）：功能改动请先立 change（forge change new <id>），再 forge task start --change <id>；确实是一句话小修，请显式加 --kind task 重跑")
}