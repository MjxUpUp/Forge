package evalkit

import "testing"

// taskStartSteps 返回脚本里所有 task start 步骤。
func taskStartSteps(steps []wedgeStep) []wedgeStep {
	var out []wedgeStep
	for _, st := range steps {
		if len(st.argv) < 4 || st.argv[1] != "task" || st.argv[2] != "start" {
			continue
		}
		out = append(out, st)
	}
	return out
}

func hasFlag(argv []string, flag string) bool {
	for _, a := range argv {
		if a == flag {
			return true
		}
	}
	return false
}

// 任务是一等入口：声明了 spec 的仓库里不带 --change 也能 task start。
// 演练脚本若还要求 --kind task，会把「小修标记」误当成开工前提（第一版模型）；
// 这两个断言把反向重构钉成回归：脚本里的 task start 一律不带 --kind。
func TestWedgeScript_TaskStartDoesNotRequireKind(t *testing.T) {
	steps := taskStartSteps(wedgeScript("forge"))
	if len(steps) == 0 {
		t.Fatal("wedgeScript 里没有 task start 步骤")
	}
	for _, st := range steps {
		if hasFlag(st.argv, "--kind") {
			t.Errorf("wedgeScript 的 %q 不该带 --kind（任务是一等入口）：%v", st.name, st.argv)
		}
	}
}
