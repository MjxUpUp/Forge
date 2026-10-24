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
	// 复审分档必须带条件前缀并落在「派 reviewer」与「标记」两步之间：删掉前缀会让
	// 首审也被引导去续用不存在的首轮 reviewer；挪出两步之间会错读成独立步骤。
	const condition = "仅当你在本会话内已对本分支派过 reviewer"
	dispatch := strings.Index(gateGuidance, "派【只读】子 agent 审查当前 diff")
	cond := strings.Index(gateGuidance, condition)
	tier := strings.Index(gateGuidance, review.ReReviewGuidance)
	mark := strings.Index(gateGuidance, "3. 审查通过后运行 `forge review pass`")
	if cond < 0 || !(dispatch >= 0 && dispatch < cond && cond < tier && tier < mark) {
		t.Errorf("复审分档须以条件前缀开头并位于派发步与标记步之间，index dispatch=%d cond=%d tier=%d mark=%d", dispatch, cond, tier, mark)
	}
	// 新会话接手、首审都按第 2 步新派：条件句必须写明否则分支。
	if !strings.Contains(gateGuidance, "否则（首审、新会话接手）按本步新派") {
		t.Errorf("gateGuidance 复审条件句缺「否则」分支，got:\n%s", gateGuidance)
	}
}
