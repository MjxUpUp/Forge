package protocol

import "github.com/MjxUpUp/Forge/internal/scoringtypes"

// Protocol represents the project's quality-protocol configuration.
// Stored as .forge/protocol.yml——defines quality standards and session behavior,
// applies to every Claude Code session, independent of pipeline state.
//
// Protocol 表示项目的质量协议配置。
// 存为 .forge/protocol.yml —— 定义质量 standards 与 session 行为，
// 适用于每个 Claude Code session，与 pipeline 状态无关。
type Protocol struct {
	Version      string                      `yaml:"version"      json:"version"`
	Standards    []Standard                  `yaml:"standards"    json:"standards"`
	SessionRules []SessionRule               `yaml:"session_rules" json:"session_rules"`
	Scoring      *scoringtypes.ScoringConfig `yaml:"scoring,omitempty" json:"scoring,omitempty"`
	// CrossRepoImpact tunes the task-verify cross-repo-impact gate
	// (docs/design/multi-repo-workspace.md): "" or "advisory" (default) only
	// reminds when a multi-repo-workspace task never declared its impact;
	// "required" hard-blocks the gate until `forge task impact` records one.
	//
	// CrossRepoImpact 调节 task-verify 的 cross-repo-impact 门禁
	// （docs/design/multi-repo-workspace.md）："" 或 "advisory"（默认）只在
	// 多仓 workspace 任务未声明影响时提醒；"required" 则硬阻断，直到
	// `forge task impact` 记录声明。
	CrossRepoImpact string `yaml:"cross_repo_impact,omitempty" json:"cross_repo_impact,omitempty"`
}

// Standard is a named quality standard with enforcement configuration.
//
// Standard 是具名质量 standard，带 enforcement 配置。
type Standard struct {
	ID          string `yaml:"id"           json:"id"`
	Name        string `yaml:"name"         json:"name"`
	Description string `yaml:"description"  json:"description"`
	EnforceHook string `yaml:"enforce_hook,omitempty" json:"enforce_hook,omitempty"`
	Severity    string `yaml:"severity"     json:"severity"` // error, warning, info
	Enabled     bool   `yaml:"enabled"      json:"enabled"`
}

// SessionRule is a per-session behavior rule for an AI agent.
//
// SessionRule 是 AI agent 每个 session 的行为规则。
type SessionRule struct {
	ID          string `yaml:"id"          json:"id"`
	Trigger     string `yaml:"trigger"     json:"trigger"` // always, on_edit, on_commit
	Instruction string `yaml:"instruction" json:"instruction"`
	Mandatory   bool   `yaml:"mandatory"   json:"mandatory"`
}

// ErrorSeverityStandards returns standards whose severity is error.
//
// ErrorSeverityStandards 返回 severity 为 error 的 standards。
func (p *Protocol) ErrorSeverityStandards() []Standard {
	var result []Standard
	for _, s := range p.Standards {
		if s.Enabled && s.Severity == "error" {
			result = append(result, s)
		}
	}
	return result
}

// MandatoryRules returns session rules that are mandatory.
//
// MandatoryRules 返回 mandatory 的 session 规则。
func (p *Protocol) MandatoryRules() []SessionRule {
	var result []SessionRule
	for _, r := range p.SessionRules {
		if r.Mandatory {
			result = append(result, r)
		}
	}
	return result
}
