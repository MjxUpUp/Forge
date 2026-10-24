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
	// 两种情形分行标 (a)/(b)，避免与 ReReviewGuidance 内部自带的「否则」同级混读。
	const fresh = "(a) 首审、新会话接手、或手里没有首轮 reviewer 的句柄：按本步新派"
	const resume = "(b) 本会话内已对本分支派过 reviewer"
	dispatch := strings.Index(gateGuidance, "派【只读】子 agent 审查当前 diff")
	a := strings.Index(gateGuidance, fresh)
	b := strings.Index(gateGuidance, resume)
	tier := strings.Index(gateGuidance, review.ReReviewGuidance)
	mark := strings.Index(gateGuidance, "3. 审查通过后运行 `forge review pass`")
	if a < 0 || b < 0 || !(dispatch >= 0 && dispatch < a && a < b && b < tier && tier < mark) {
		t.Errorf("须依次为：派发步 → (a) 首审/新会话新派 → (b) 复审条件前缀 → 分档指引 → 标记步，index dispatch=%d a=%d b=%d tier=%d mark=%d", dispatch, a, b, tier, mark)
	}
	// 情形 (a) 里「修复后句柄丢失、新派复审」同样会被裸标记拒绝：--note 要求不限情形。
	if !strings.Contains(gateGuidance, "任一情形都须带 `--note") {
		t.Errorf("第 3 步须写明有源码变更时任一情形都带 --note，got:\n%s", gateGuidance)
	}
}
