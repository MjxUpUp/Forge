package cli

import (
	"os"
	"strings"
	"testing"
)

// TestReviewHasNoLLMJudgeArm 钉住「评测时代的 LLM 判官臂不再可见」：
//
//  1. `forge review llm` 子命令必须不在命令树上（core-only 删了 eval 命令组，
//     但 review llm 一直挂在 review 下，是最后一个可达的 eval 入口）；
//  2. review.go 源码不得再 import `internal/evalkit`——那个包整包已删，
//     出现即说明有人把判官臂又接回来了。
//
// 断言命令树与源码文本，不看 help 措辞：措辞可以绕过，形状不行。
func TestReviewHasNoLLMJudgeArm(t *testing.T) {
	for _, c := range reviewCmd.Commands() {
		if c.Name() == "llm" {
			t.Errorf("review llm 应已随 evalkit 一并删除，但仍在 review 的命令树上")
		}
	}
	src, err := os.ReadFile("review.go")
	if err != nil {
		t.Fatalf("读 review.go 失败（守卫需在包目录内运行）: %v", err)
	}
	if strings.Contains(string(src), "internal/evalkit") {
		t.Errorf("review.go 仍引用 internal/evalkit——该包已删，判官臂不应再接回")
	}
}
