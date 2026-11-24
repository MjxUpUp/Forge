package checklog

import "testing"

// TestGoalContractCheckInRoster 钉住 goal-contract 进 roster（task-goal-contract
// Change A）：漏登记 = implement 门禁的契约观测在 compat 面/看板不可见。
func TestGoalContractCheckInRoster(t *testing.T) {
	if string(CheckGoalContract) != "goal-contract" {
		t.Fatalf("常量值漂移: %q != goal-contract", CheckGoalContract)
	}
	for _, lit := range AllCheckNames() {
		if lit == "goal-contract" {
			return
		}
	}
	t.Fatal("AllCheckNames 缺 goal-contract——escape.go allCheckNames 未同步")
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
