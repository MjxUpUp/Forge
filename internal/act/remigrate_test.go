package act

import (
	"reflect"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// remAt 给 remigrate 测试一个稳定完成时刻。
var remAt = time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)

// TestRemigrateConclusion 钉住 2026-08 证据缩放逃生舱 cap 的历史结论就地迁移：按旧
// 平价规则落盘的结论可从已存字段精确重推导——Strength==Weak && Ratio>=0.5 是逃生
// cap 的唯一指纹（旧规则下 ratio>=0.5 却 Weak 只有 cap 一条路），重建
// EvidenceChain{UsedEscapeHatch:true} 再跑 Strength() 即得新规则值，零信息损失。
// RetrospectiveNudge 按 BuildConclusion 同一判据重算。
func TestRemigrateConclusion(t *testing.T) {
	cases := []struct {
		name string
		in   Conclusion
		want Conclusion
	}{
		{
			// 旧规则 escape-cap 的重证据任务（ratio 0.98, det 125）：新规则边际豁免 → Strong，nudge 重算为 false
			name: `escape-cap重证据→Strong且不nudge`,
			in:   Conclusion{TaskRef: `a`, Score: 99.5, Grade: `A`, Strength: `Weak`, Ratio: 0.98, Deterministic: 125, AgentClaim: 2, RetrospectiveNudge: true, CompletedAt: remAt},
			want: Conclusion{TaskRef: `a`, Score: 99.5, Grade: `A`, Strength: `Strong`, Ratio: 0.98, Deterministic: 125, AgentClaim: 2, RetrospectiveNudge: false, CompletedAt: remAt},
		},
		{
			// escape-cap 但证据非压倒性（ratio 0.76, det 16）：cap 维持 → 原样
			name: `escape-cap轻证据→维持Weak且nudge`,
			in:   Conclusion{TaskRef: `b`, Score: 89, Grade: `B`, Strength: `Weak`, Ratio: 0.76, Deterministic: 16, AgentClaim: 5, RetrospectiveNudge: true, CompletedAt: remAt},
			want: Conclusion{TaskRef: `b`, Score: 89, Grade: `B`, Strength: `Weak`, Ratio: 0.76, Deterministic: 16, AgentClaim: 5, RetrospectiveNudge: true, CompletedAt: remAt},
		},
		{
			// 真 Weak（ratio 0.25 < 0.5）：非 cap 产物 → 原样
			name: `真Weak→原样`,
			in:   Conclusion{TaskRef: `c`, Score: 88, Grade: `A`, Strength: `Weak`, Ratio: 0.25, Deterministic: 1, AgentClaim: 3, RetrospectiveNudge: true, CompletedAt: remAt},
			want: Conclusion{TaskRef: `c`, Score: 88, Grade: `A`, Strength: `Weak`, Ratio: 0.25, Deterministic: 1, AgentClaim: 3, RetrospectiveNudge: true, CompletedAt: remAt},
		},
		{
			// Strong：无 cap 参与 → 原样
			name: `Strong→原样`,
			in:   Conclusion{TaskRef: `d`, Score: 95, Grade: `A`, Strength: `Strong`, Ratio: 0.9, Deterministic: 90, AgentClaim: 10, RetrospectiveNudge: false, CompletedAt: remAt},
			want: Conclusion{TaskRef: `d`, Score: 95, Grade: `A`, Strength: `Strong`, Ratio: 0.9, Deterministic: 90, AgentClaim: 10, RetrospectiveNudge: false, CompletedAt: remAt},
		},
		{
			// flip 到 Strong 但低分（<70）：nudge 按低分判据保留
			name: `escape-cap重证据但低分→Strong仍nudge（分数判据）`,
			in:   Conclusion{TaskRef: `e`, Score: 65, Grade: `D`, Strength: `Weak`, Ratio: 0.98, Deterministic: 125, AgentClaim: 2, RetrospectiveNudge: true, CompletedAt: remAt},
			want: Conclusion{TaskRef: `e`, Score: 65, Grade: `D`, Strength: `Strong`, Ratio: 0.98, Deterministic: 125, AgentClaim: 2, RetrospectiveNudge: true, CompletedAt: remAt},
		},
		{
			// Unverified：与 cap 无关 → 原样
			name: `Unverified→原样`,
			in:   Conclusion{TaskRef: `f`, Score: 95, Grade: `A`, Strength: `Unverified`, Ratio: 0, Deterministic: 0, AgentClaim: 2, RetrospectiveNudge: true, CompletedAt: remAt},
			want: Conclusion{TaskRef: `f`, Score: 95, Grade: `A`, Strength: `Unverified`, Ratio: 0, Deterministic: 0, AgentClaim: 2, RetrospectiveNudge: true, CompletedAt: remAt},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RemigrateConclusion(tc.in)
			if got.Strength != tc.want.Strength {
				t.Errorf(`Strength=%q want %q`, got.Strength, tc.want.Strength)
			}
			if got.RetrospectiveNudge != tc.want.RetrospectiveNudge {
				t.Errorf(`RetrospectiveNudge=%v want %v`, got.RetrospectiveNudge, tc.want.RetrospectiveNudge)
			}
			// 无关字段必须逐字保留（迁移不得碰历史事实）
			if got.TaskRef != tc.in.TaskRef || got.Score != tc.in.Score || got.Grade != tc.in.Grade ||
				got.Ratio != tc.in.Ratio || got.Deterministic != tc.in.Deterministic ||
				got.AgentClaim != tc.in.AgentClaim || !got.CompletedAt.Equal(tc.in.CompletedAt) {
				t.Errorf(`迁移改动了无关字段: in=%+v got=%+v`, tc.in, got)
			}
		})
	}
}

// TestRemigrateConclusion_Idempotent 钉住重复运行安全：迁移过的结论第二次是 no-op
// （Strength 不再带 escape-cap 指纹）。
func TestRemigrateConclusion_Idempotent(t *testing.T) {
	c := Conclusion{TaskRef: `a`, Score: 99.5, Grade: `A`, Strength: `Weak`, Ratio: 0.98, Deterministic: 125, AgentClaim: 2, RetrospectiveNudge: true, CompletedAt: remAt}
	once := RemigrateConclusion(c)
	twice := RemigrateConclusion(once)
	if once.Strength != `Strong` {
		t.Fatalf(`第一次迁移应翻 Strong，got %q`, once.Strength)
	}
	if !reflect.DeepEqual(twice, once) {
		t.Errorf(`第二次迁移应为 no-op: once=%+v twice=%+v`, once, twice)
	}
}

// TestRemigrateConclusion_FingerprintGuards 钉住指纹边界：Weak 且 ratio>=0.5 走
// escape-cap 迁移路径；ratio<0.5 的 Weak 不得隐式置 UsedEscapeHatch（它从未被 cap——
// 真弱证据）。
func TestRemigrateConclusion_FingerprintGuards(t *testing.T) {
	heavy := checklog.EvidenceChain{Deterministic: 125, AgentClaim: 2, UsedEscapeHatch: true}
	if heavy.Strength() != checklog.Strong {
		t.Fatalf(`前置校验失败：新规则下重证据+逃生应 Strong（got %s），测试前提不成立`, heavy.Strength())
	}
	light := checklog.EvidenceChain{Deterministic: 16, AgentClaim: 5, UsedEscapeHatch: true}
	if light.Strength() != checklog.Weak {
		t.Fatalf(`前置校验失败：新规则下轻证据+逃生应 Weak（got %s），测试前提不成立`, light.Strength())
	}
}
