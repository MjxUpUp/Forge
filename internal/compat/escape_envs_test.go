package compat

import "testing"

// TestArtifactChainEscapeEnvRegistered 钉住产物链逃生舱进 EscapeEnvs 清单
// （compat 面 3 单一真相源）：漏登记 = env 逃生生效但不进快照承诺面——
// 「逃生舱 env-disable-able 落审计」宪法面的缺口。
func TestArtifactChainEscapeEnvRegistered(t *testing.T) {
	found := false
	for _, e := range EscapeEnvs {
		if e == "FORGE_ARTIFACT_CHAIN" {
			found = true
		}
	}
	if !found {
		t.Fatal("EscapeEnvs 缺 FORGE_ARTIFACT_CHAIN——compat.go 清单未同步")
	}
}
