package taskpipeline

// loopconfig.go —— protocol.yml `loop:` 段的加载与默认值（loop-contract B1-3）。
// 与 designtier.go 的 loadTierConfig 同一做法：protocol 缺失/不可读/未配置即内置
// 默认，fail-open；预算只影响 advisory 的循环判词，不阻断任何门禁。
//
// loopconfig.go — loading and defaults of the protocol.yml `loop:` section
// (loop-contract B1-3). Same shape as designtier.go loadTierConfig: a missing,
// unreadable or absent section means built-in defaults (fail-open); budgets
// only feed the advisory loop verdict, none blocks a gate.

import (
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// LoopConfig is the resolved loop-contract configuration: every field is the
// effective value after defaults (unlike protocol.LoopConfig, whose zero
// values mean "default"/"unlimited").
//
// LoopConfig 是解析后的 loop 契约配置：每个字段都是套用默认值之后的生效值
// （protocol.LoopConfig 的零值表示"默认"/"不限"，这里不再有歧义）。
type LoopConfig struct {
	// RecurrenceThreshold ≤ 0 disables the same-cause recurrence verdict.
	//
	// RecurrenceThreshold ≤ 0 关闭同因复发判定。
	RecurrenceThreshold int
	// MaxFailedVerifies 0 = unlimited.
	MaxFailedVerifies int
	// MaxChangedFiles 0 = unlimited.
	MaxChangedFiles int
	// TokenBudget is always > 0 (defaults to toolusage.DefaultTaskTokenBudget).
	//
	// TokenBudget 恒 > 0（默认 toolusage.DefaultTaskTokenBudget）。
	TokenBudget int
	// MaxVerifiesWithoutProgress 0 = not judged.
	MaxVerifiesWithoutProgress int
}

// DefaultRecurrenceThreshold is the built-in same-cause recurrence threshold.
//
// DefaultRecurrenceThreshold 是内置的同因复发阈值。
const DefaultRecurrenceThreshold = 2

// DefaultLoopConfig returns the built-in loop configuration.
//
// DefaultLoopConfig 返回内置的 loop 配置。
func DefaultLoopConfig() LoopConfig {
	return LoopConfig{
		RecurrenceThreshold: DefaultRecurrenceThreshold,
		TokenBudget:         toolusage.DefaultTaskTokenBudget,
	}
}

// ResolveLoopConfig applies defaults to a protocol.LoopConfig (nil = defaults).
// Pure; LoadLoopConfig is the disk-backed wrapper.
//
// ResolveLoopConfig 给 protocol.LoopConfig 套默认值（nil = 全默认）。纯函数；
// LoadLoopConfig 是读盘的包装。
func ResolveLoopConfig(p *protocol.LoopConfig) LoopConfig {
	cfg := DefaultLoopConfig()
	if p == nil {
		return cfg
	}
	switch {
	case p.RecurrenceThreshold < 0:
		cfg.RecurrenceThreshold = 0
	case p.RecurrenceThreshold > 0:
		cfg.RecurrenceThreshold = p.RecurrenceThreshold
	}
	if p.MaxFailedVerifies > 0 {
		cfg.MaxFailedVerifies = p.MaxFailedVerifies
	}
	if p.MaxChangedFiles > 0 {
		cfg.MaxChangedFiles = p.MaxChangedFiles
	}
	if p.TokenBudget > 0 {
		cfg.TokenBudget = p.TokenBudget
	}
	if p.MaxVerifiesWithoutProgress > 0 {
		cfg.MaxVerifiesWithoutProgress = p.MaxVerifiesWithoutProgress
	}
	return cfg
}

// LoadLoopConfig reads protocol.yml and resolves the `loop:` section; any read
// failure yields the defaults (fail-open, like loadTierConfig).
//
// LoadLoopConfig 读 protocol.yml 并解析 `loop:` 段；读取失败一律回内置默认
// （fail-open，与 loadTierConfig 同）。
func LoadLoopConfig(root string) LoopConfig {
	proto, err := protocol.Load(root)
	if err != nil || proto == nil {
		return DefaultLoopConfig()
	}
	return ResolveLoopConfig(proto.Loop)
}
