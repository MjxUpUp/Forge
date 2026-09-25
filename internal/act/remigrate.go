package act

import (
	"github.com/MjxUpUp/Forge/internal/checklog"
)

// RemigrateConclusion re-derives Strength/RetrospectiveNudge of a stored conclusion under current escape-hatch caps.
//
// RemigrateConclusion 就地按当前逃生舱 cap 规则重推导已落盘结论的
// Strength/RetrospectiveNudge（不碰磁盘）。随 2026-08 证据缩放 cap
// （checklog.EscapeDowngradedStrength）一同发布：旧平价规则下写入的结论带着过时的
// "escape-cap" Strength，本函数从已存字段精确恢复新规则值。
//
// 指纹：Strength==Weak && Ratio>=0.5 是旧规则下 escape-cap 的唯一签名——其余到 Weak
// 的路径全都是 ratio<0.5（agent-claim 占多数），Strong/Unverified/NoData 不经过 cap。
// 因此迁移重建 cap 前的 EvidenceChain{Deterministic, AgentClaim, UsedEscapeHatch:true}
// 再跑 Strength()；无需回读原始 checklog，零信息损失。
//
// RetrospectiveNudge 按 BuildConclusion 同一判据重算（Unverified/Weak 或 分数<70）——
// 注意分数判据让翻成 Strong 的低分任务保持 nudge（教训在低分，不在证据）。
//
// 无关字段逐字保留：迁移只重推导判定字段——历史事实（分数/档/占比/计数/时间/戳）
// 不动。
//
// 幂等：迁移后的 Strength==Strong 不再匹配 Weak 指纹，第二次跑是 no-op。
func RemigrateConclusion(c Conclusion) Conclusion {
	// escape-cap 指纹：Weak 且 deterministic 占多数。非 cap 的 Weak 全是 ratio<0.5；
	// Strong/Unverified/NoData 从不经过 cap——按原样返回。
	if c.Strength != checklog.Weak.String() || c.Ratio < 0.5 {
		return c
	}
	ec := checklog.EvidenceChain{
		Deterministic:   c.Deterministic,
		AgentClaim:      c.AgentClaim,
		UsedEscapeHatch: true,
	}
	out := c
	out.Strength = ec.Strength().String()
	// 与 BuildConclusion 同判据：证据弱或低分。翻成 Strong 的低分任务仍 nudge——
	// 教训在低分。
	out.RetrospectiveNudge = ec.Strength() == checklog.Unverified || ec.Strength() == checklog.Weak || c.Score < 70
	return out
}
