package protocol

import "github.com/MjxUpUp/Forge/internal/scoringtypes"

// DefaultProtocol returns the default quality protocol: core standards + base
// session rules + design-for-complex (universally useful). The former mode
// parameter (small/medium/large) only toggled 1-2 optional rules tied to the
// now-deleted project pipeline (review-checklist referenced gate-6); with the
// project pipeline gone, mode is meaningless and removed.
func DefaultProtocol() *Protocol {
	p := &Protocol{
		Version: "1.0",
		Standards: []Standard{
			{
				ID:          "compile-gate",
				Name:        "代码编译",
				Description: "每次代码修改后确认编译通过（advisory 提醒，由 agent 自检）",
				EnforceHook: "auto-compile.sh",
				Severity:    "warning",
				Enabled:     true,
			},
			{
				ID:          "no-assertion-weaken",
				Name:        "断言保护",
				Description: "不弱化测试断言（检测到弱化 advisory 提醒，由 agent 自检）",
				EnforceHook: "assertion-check.sh",
				Severity:    "warning",
				Enabled:     true,
			},
			{
				ID:          "test-accompany",
				Name:        "测试伴随",
				Description: "新代码应有对应测试",
				EnforceHook: "",
				Severity:    "warning",
				Enabled:     true,
			},
		},
		SessionRules: []SessionRule{
			{
				ID:          "intent-first",
				Trigger:     "always",
				Instruction: "修改代码前，先说明意图和计划",
				Mandatory:   true,
			},
			{
				ID:          "commit-message",
				Trigger:     "on_commit",
				Instruction: "commit 信息必须描述变更内容和原因",
				Mandatory:   true,
			},
			{
				ID:          "verify-before-done",
				Trigger:     "always",
				Instruction: "每次会话结束前，运行测试确认没有破坏已有功能",
				Mandatory:   true,
			},
			{
				ID:          "design-for-complex",
				Trigger:     "on_edit",
				Instruction: "改动超过 50 行时，先简要描述设计方案",
				Mandatory:   false,
			},
		},
	}

	// Set default scoring config
	p.Scoring = &scoringtypes.ScoringConfig{
		Weights:    scoringtypes.DefaultWeights(),
		Thresholds: scoringtypes.DefaultThresholds(),
	}

	return p
}
