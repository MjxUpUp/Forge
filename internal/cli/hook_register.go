package cli

import (
	"github.com/MjxUpUp/Forge/internal/hookdispatch"
)

// hook_register.go —— hook 分发簇的 cli 侧注册器（2026-09 代码普查 A2-2：
// hook.go + hook_*.go 已迁 internal/hookdispatch，资产/执行两分——资产在
// internal/hooks）。两件事：
//  1. skill-trigger 进程内特例路径接缝注入（判定+渲染核心住 cli 的
//     skill_trigger.go——挂在 cliskills.Root 下；nil 时 hookdispatch 静默跳过）；
//  2. HookCmd（forge hook，隐藏命令）挂到 rootCmd。
func init() {
	hookdispatch.SkillTriggerHookFn = runSkillTriggerHook
	rootCmd.AddCommand(hookdispatch.HookCmd)
}
