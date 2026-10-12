package protocol

import "github.com/MjxUpUp/Forge/internal/scoringtypes"

// Protocol represents the project's quality-protocol configuration.
//
// Protocol 表示项目的质量协议配置。
// 存为 .forge/protocol.yml —— 定义质量 standards 与 session 行为，
// 适用于每个 Claude Code session，与 pipeline 状态无关。
type Protocol struct {
	Version      string                      `yaml:"version"      json:"version"`
	Standards    []Standard                  `yaml:"standards"    json:"standards"`
	SessionRules []SessionRule               `yaml:"session_rules" json:"session_rules"`
	Scoring      *scoringtypes.ScoringConfig `yaml:"scoring,omitempty" json:"scoring,omitempty"`
	// CrossRepoImpact tunes the task-verify cross-repo-impact gate (docs/design/multi-repo-workspace.md): "" or "advisory" (default) only reminds when a multi-repo-workspace task never declared its impact; "required" hard-blocks the gate until `forge task impact` records one.
	//
	// CrossRepoImpact 调节 task-verify 的 cross-repo-impact 门禁
	// （docs/design/multi-repo-workspace.md）："" 或 "advisory"（默认）只在
	// 多仓 workspace 任务未声明影响时提醒；"required" 则硬阻断，直到
	// `forge task impact` 记录声明。
	CrossRepoImpact string `yaml:"cross_repo_impact,omitempty" json:"cross_repo_impact,omitempty"`
	// DesignTiers tunes design-tier routing (docs/plans/design-stage-hardening-2026-10-proposal.md
	// W4, D3): extra or overriding hard-trigger rules and standard-change patterns on top of
	// the built-in table. nil = built-in table only.
	//
	// DesignTiers 调节方案分档路由（docs/plans/design-stage-hardening-2026-10-proposal.md
	// W4、D3）：在内置规则表之上追加或覆盖硬触发器规则与标准变更模式。nil = 只用内置表。
	DesignTiers *DesignTiersConfig `yaml:"design_tiers,omitempty" json:"design_tiers,omitempty"`
}

// DesignTiersConfig is the project-level override of the design-tier rule table.
//
// DesignTiersConfig 是方案分档规则表的项目级覆盖。
type DesignTiersConfig struct {
	// Rules are appended to the built-in rules; a rule whose Name equals a built-in
	// rule replaces it.
	//
	// Rules 追加到内置规则之后；Name 与内置规则同名者替换该内置规则。
	Rules []DesignTierRule `yaml:"rules,omitempty" json:"rules,omitempty"`
	// StandardChanges are extra standard-change patterns (pre-approved routine
	// changes that never trigger a tier).
	//
	// StandardChanges 是额外的标准变更模式（事先认可的例行改动，永不触发档位）。
	StandardChanges []string `yaml:"standard_changes,omitempty" json:"standard_changes,omitempty"`
	// ReplaceDefaults drops the built-in rules and standard changes entirely.
	//
	// ReplaceDefaults 完全弃用内置规则与标准变更。
	ReplaceDefaults bool `yaml:"replace_defaults,omitempty" json:"replace_defaults,omitempty"`
}

// DesignTierRule maps file patterns to the minimum design tier they demand.
// Pattern forms: "seg:<name>" = any path segment equals name; a pattern with "/"
// = repo path glob or directory prefix; otherwise = base-name glob.
//
// DesignTierRule 把文件模式映射到它要求的最低方案档位。模式写法："seg:<名>" =
// 任一路径段等于该名；含 "/" = 仓库路径 glob 或目录前缀；其余 = 文件名 glob。
type DesignTierRule struct {
	Name  string   `yaml:"name" json:"name"`
	Tier  string   `yaml:"tier" json:"tier"` // mini | full
	Paths []string `yaml:"paths" json:"paths"`
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
