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

func hasKindTask(argv []string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--kind" && argv[i+1] == "task" {
			return true
		}
	}
	return false
}

// 声明了 spec 的仓库里，任务是一等入口但要用 --kind task 显式声明「小修」；
// 演练脚本若漏了这个 flag，eval 会在入口守卫处直接失败（不是测试意图）。
// 这两个断言把「脚本与守卫同步」钉成可回归的事实。
func TestWedgeScript_TaskStartDeclaresKindTask(t *testing.T) {
	steps := taskStartSteps(wedgeScript("forge"))
	if len(steps) == 0 {
		t.Fatal("wedgeScript 里没有 task start 步骤")
	}
	for _, st := range steps {
		if !hasKindTask(st.argv) {
			t.Errorf("wedgeScript 的 %q 缺少 --kind task：%v", st.name, st.argv)
		}
	}
}

func TestArtifactScript_TaskStartDeclaresKindTask(t *testing.T) {
	steps := taskStartSteps(artifactScript("forge", t.TempDir()))
	if len(steps) == 0 {
		t.Fatal("artifactScript 里没有 task start 步骤")
	}
	for _, st := range steps {
		if !hasKindTask(st.argv) {
			t.Errorf("artifactScript 的 %q 缺少 --kind task：%v", st.name, st.argv)
		}
	}
}
