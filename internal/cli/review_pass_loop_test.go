package cli

import (
	"os"
	"strings"
	"testing"
)

// TestReviewPassLoopEdgeWiring 钉住 review pass 源码引用回边钩子（G2 接线）：
// runReviewPassAt 所在源文件必须引用 MarkLoopExhaustedIfDue（轮龄重估入口）
// ——漏接则轮次预算在 review pass 位失效，回环退化回「只累积不耗尽」。
func TestReviewPassLoopEdgeWiring(t *testing.T) {
	b, err := os.ReadFile("review.go")
	if err != nil {
		t.Fatalf("读 review.go: %v", err)
	}
	if !strings.Contains(string(b), "MarkLoopExhaustedIfDue") {
		t.Fatal("review.go 未引用 MarkLoopExhaustedIfDue——回边接线脱落")
	}
}
