package compat

import "testing"

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

// TestHardeningEscapeEnvsRegistered（墙价硬批次）：mutation 检查已在 1.88 删除，
// roster 不得再留它的逃生舱。
func TestHardeningEscapeEnvsRegistered(t *testing.T) {
	for _, env := range []string{"FORGE_MUTATION_GATE"} {
		found := false
		for _, e := range EscapeEnvs {
			if e == env {
				found = true
			}
		}
		if found {
			t.Errorf("%s 已随 mutation 检查删除，不应再在 EscapeEnvs roster", env)
		}
	}
}

// TestTaskTreeGuardEscapeEnvRegistered 钉住错树守护的逃生 env 进 EscapeEnvs
// 清单：漏登记 = 关掉守护能生效但不进快照承诺面（用户无法从 compat 面
// 发现「有种环境变量能关掉错树保护」）。
func TestTaskTreeGuardEscapeEnvRegistered(t *testing.T) {
	for _, e := range EscapeEnvs {
		if e == "FORGE_TASK_TREE_GUARD" {
			return
		}
	}
	t.Fatal("EscapeEnvs 缺 FORGE_TASK_TREE_GUARD——compat.go 清单未同步")
}
