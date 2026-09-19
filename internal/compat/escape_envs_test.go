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

// TestUnusedScanEscapeEnvRegistered 钉住 unused-gate 逃生舱进 EscapeEnvs 清单
// （与 FORGE_ARTIFACT_CHAIN 同款契约）：漏登记 = env 逃生生效但不进快照承诺面。
func TestUnusedScanEscapeEnvRegistered(t *testing.T) {
	found := false
	for _, e := range EscapeEnvs {
		if e == "FORGE_UNUSED_SCAN" {
			found = true
		}
	}
	if !found {
		t.Fatal("EscapeEnvs 缺 FORGE_UNUSED_SCAN——compat.go 清单未同步")
	}
}

// TestMutationTimeoutEscapeEnvRegistered (oracle-pipeline L2a)：FORGE_MUTATION_
// TIMEOUT 在 EscapeEnvs roster——新增逃生舱必须同步（守卫的回归钉形态）。
func TestMutationTimeoutEscapeEnvRegistered(t *testing.T) {
	for _, env := range EscapeEnvs {
		if env == "FORGE_MUTATION_TIMEOUT" {
			return
		}
	}
	t.Fatal(`FORGE_MUTATION_TIMEOUT 应在 EscapeEnvs roster（compat 快照 escapes 面）`)
}
