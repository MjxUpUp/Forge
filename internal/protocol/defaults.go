package protocol

import "github.com/Harness/forge/internal/scoringtypes"

// DefaultProtocol returns a mode-appropriate quality protocol.
// The core standards are the same across all modes; session rules may differ.
func DefaultProtocol(mode string) *Protocol {
	p := &Protocol{
		Version: "1.0",
		Standards: []Standard{
			{
				ID:          "compile-gate",
				Name:        "代码编译",
				Description: "每次代码修改后必须通过编译",
				EnforceHook: "auto-compile.sh",
				Severity:    "error",
				Enabled:     true,
			},
			{
				ID:          "no-assertion-weaken",
				Name:        "断言保护",
				Description: "不允许移除或弱化测试断言",
				EnforceHook: "assertion-check.sh",
				Severity:    "error",
				Enabled:     true,
			},
			{
				ID:          "no-experience-violation",
				Name:        "经验合规",
				Description: "代码不得包含已知反模式",
				EnforceHook: "experience-check.sh",
				Severity:    "error",
				Enabled:     true,
			},
			{
				ID:          "test-accompany",
				Name:        "测试伴随",
				Description: "新代码应有对应测试",
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
		},
	}

	// Add mode-specific rules
	switch mode {
	case "medium", "large":
		p.SessionRules = append(p.SessionRules, SessionRule{
			ID:          "design-for-complex",
			Trigger:     "on_edit",
			Instruction: "改动超过 50 行时，先简要描述设计方案",
			Mandatory:   false,
		})
	}

	// Set default scoring config
	p.Scoring = &scoringtypes.ScoringConfig{
		Weights:    scoringtypes.DefaultWeights(),
		Thresholds: scoringtypes.DefaultThresholds(),
	}

	if mode == "large" {
		p.SessionRules = append(p.SessionRules, SessionRule{
			ID:          "review-checklist",
			Trigger:     "on_commit",
			Instruction: "提交前对照检查清单逐项确认（见 gate-6-acceptance 配置）",
			Mandatory:   false,
		})
	}

	return p
}
