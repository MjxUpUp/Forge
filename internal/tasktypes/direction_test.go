package tasktypes

import (
	"os/exec"
	"strings"
	"testing"
)

// TestDependencyDirectionIsLeaf pins the architectural contract of this package:
// tasktypes must never depend on taskpipeline (the executor). The model sank
// below the executor precisely so feature packages can consume it without
// dragging the HARD-gate machinery (2026-09 census A3); a reverse import would
// close the cycle and undo the split.
//
// TestDependencyDirectionIsLeaf 钉住本包的架构契约：tasktypes 永不依赖
// taskpipeline（执行器）。模型下沉到执行器之下，正是为了让功能包消费模型时
// 不拖动 HARD 门禁机器（2026-09 普查 A3）；反向 import 会闭合依赖环、推翻
// 整个拆分。
func TestDependencyDirectionIsLeaf(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable in test environment: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasSuffix(dep, "/internal/taskpipeline") {
			t.Errorf("tasktypes must not depend on taskpipeline, but go list -deps contains %s", dep)
		}
	}
}
