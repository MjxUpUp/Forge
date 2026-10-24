package checklog

import "testing"

// TestArtifactChainChecksInRoster 钉住产物链两 CheckName 进 roster（compat 面 2
// 单一真相源）：漏登记 = checklog 条目落盘但 compat/看板看不到——面缩水。
// eval-artifact-drill（演练执行记录）与 artifact-drift（漂移观测）是两个概念
// 最易漏登。
func TestArtifactChainChecksInRoster(t *testing.T) {
	want := map[CheckName]string{
		CheckArtifactChain: "artifact-chain",
		CheckArtifactDrift: "artifact-drift",
		CheckArtifactDrill: "eval-artifact-drill",
	}
	for name, lit := range want {
		if string(name) != lit {
			t.Errorf("常量值漂移: %q != %q", name, lit)
		}
	}
	roster := AllCheckNames()
	seen := make(map[string]bool, len(roster))
	for _, n := range roster {
		seen[n] = true
	}
	for _, lit := range want {
		if !seen[lit] {
			t.Errorf("AllCheckNames 缺 %q——escape.go allCheckNames 未同步", lit)
		}
	}
}

// TestSelfcheckChecksInRoster 钉住 selfcheck 两 CheckName 进 roster（discipline-first
// P2）：漏登记 = selfcheck 条目落盘但 compat 面/看板看不到——镜像自检的落痕对分析
// 面不可见。
func TestSelfcheckChecksInRoster(t *testing.T) {
	want := map[CheckName]string{
		CheckSelfcheckPairing: "selfcheck-pairing",
		CheckSelfcheckScope:   "selfcheck-scope",
	}
	for name, lit := range want {
		if string(name) != lit {
			t.Errorf("常量值漂移: %q != %q", name, lit)
		}
	}
	roster := AllCheckNames()
	seen := make(map[string]bool, len(roster))
	for _, n := range roster {
		seen[n] = true
	}
	for _, lit := range want {
		if !seen[lit] {
			t.Errorf("AllCheckNames 缺 %q——escape.go allCheckNames 未同步", lit)
		}
	}
}

// TestTreeGuardCheckInRoster 钉住错树守护的 check 名进 roster（compat 面 2
// 单一真相源）：漏登记 = 门禁/写入层写了 tree-guard 行，但 compat 快照与
// forge trace 的清单里看不到它——「留痕存在但承诺面不可见」。
func TestTreeGuardCheckInRoster(t *testing.T) {
	if string(CheckTreeGuard) != "tree-guard" {
		t.Errorf("常量值漂移: %q != tree-guard", CheckTreeGuard)
	}
	seen := false
	for _, n := range AllCheckNames() {
		if n == "tree-guard" {
			seen = true
			break
		}
	}
	if !seen {
		t.Fatal("AllCheckNames 缺 tree-guard——escape.go allCheckNames 未同步")
	}
}

// TestTreeGuardIsObservationOnly 钉住 tree-guard 只作观察证据：它记录
// 「会话站在错树」，不是验证动作，绝不能喂进 evidence strength 桶——
// 否则关一次守护就等于给自己加了几分确定性证据。
func TestTreeGuardIsObservationOnly(t *testing.T) {
	ec := BuildEvidenceChain([]Entry{
		{Check: CheckTreeGuard, Passed: false, Checked: true, Source: EvidenceDeterministic},
	}, "feat/x")
	if ec.Deterministic != 0 || ec.AgentClaim != 0 {
		t.Fatalf("tree-guard 不得进证据桶，got det=%d claim=%d", ec.Deterministic, ec.AgentClaim)
	}
	if len(ec.Entries) != 1 {
		t.Fatalf("tree-guard 行必须保留在 Entries（forge trace 可见），got %d", len(ec.Entries))
	}
}
