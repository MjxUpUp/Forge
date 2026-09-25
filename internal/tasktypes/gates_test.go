package tasktypes

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// TestGateIDsMatchConstants pins DefaultGates to the exported gate-ID constants.
//
// TestGateIDsMatchConstants 钉死 DefaultGates 与导出的门禁 ID 常量一致——常量是
// 全仓消费方（cli 渲染/gate 分发）的单一真相源，DefaultGates 若手写字面量漂移，
// 这里第一时间失败。
func TestGateIDsMatchConstants(t *testing.T) {
	gates := DefaultGates()
	want := []struct {
		id  string
		idx int
	}{{GateImplement, 0}, {GateVerify, 1}, {GateComplete, 2}}
	for _, w := range want {
		if gates[w.idx].ID != w.id {
			t.Errorf("DefaultGates()[%d].ID = %q, want %q", w.idx, gates[w.idx].ID, w.id)
		}
	}
}

// TestCheckLogGateNameInterlock pins the checklog CheckName constants to the gate-ID
// constants — the two sides cannot import each other (taskpipeline → checklog), so the
// mirror contract is enforced here instead.
//
// TestCheckLogGateNameInterlock 把 checklog 的 CheckName 常量与门禁 ID 常量互钉：
// checklog 被 taskpipeline 依赖、不能反向 import，两侧同值只能靠本测试钉住
// （2026-09 代码普查 R2：曾散布 26 处手写字面量）。只钉 Verify/Complete——这两个
// gate 的推进记录有 checklog 生产写入方；task-implement gate 的证据走
// auto-compile/assertion-check 检查名，不产 "task-implement" CheckName（评审裁决：
// 不为对称性造无生产者的常量）。
func TestCheckLogGateNameInterlock(t *testing.T) {
	pairs := []struct {
		check checklog.CheckName
		gate  string
		desc  string
	}{
		{checklog.CheckTaskVerify, GateVerify, "verify"},
		{checklog.CheckTaskComplete, GateComplete, "complete"},
	}
	for _, p := range pairs {
		if string(p.check) != p.gate {
			t.Errorf("checklog %s constant %q != gate ID %q — two sources drifted", p.desc, p.check, p.gate)
		}
	}
}
