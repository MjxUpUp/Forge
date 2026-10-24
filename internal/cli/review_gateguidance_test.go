package cli

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/review"
)

// TestGateGuidanceCarriesReReviewTier pins that the non-task Stop-hook guidance
// embeds the tiered re-review instruction. The stamp is content-addressed and
// cannot tell a first review from a post-fix re-review, so without the embedded
// tier every Stop block steers the agent to a fresh cold-start reviewer.
//
// TestGateGuidanceCarriesReReviewTier 钉住：非 task 模式 Stop hook 指引内嵌分档
// 复审指令。stamp 内容寻址、分不出首审与修复后复审，缺了内嵌分档，每次 Stop
// block 都把 agent 引向新派冷启动 reviewer。
func TestGateGuidanceCarriesReReviewTier(t *testing.T) {
	if !strings.Contains(gateGuidance, review.ReReviewGuidance) {
		t.Errorf("gateGuidance 应内嵌 review.ReReviewGuidance（单一真相源），got:\n%s", gateGuidance)
	}
	// 首审路径不被复审指引取代：派只读子 agent 与 forge review pass 步骤仍在。
	for _, want := range []string{"派【只读】子 agent 审查当前 diff", "forge review pass"} {
		if !strings.Contains(gateGuidance, want) {
			t.Errorf("gateGuidance 丢了首审步骤 %q，got:\n%s", want, gateGuidance)
		}
	}
}
