package skillgen

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/protocol"
)

// TestQualityDocGateMentionsResume pins that the generated forge-quality
// skill's doc-gate paragraph tells the agent that re-review after a doc-review
// round reuses the first-round reviewer for small revisions instead of
// dispatching a fresh subagent every round.
//
// TestQualityDocGateMentionsResume 钉住：生成的 forge-quality skill 的文档回检
// 段告诉 agent，doc-review 打回后的复审对小修订续用首轮评审者，不每轮新派子代理。
func TestQualityDocGateMentionsResume(t *testing.T) {
	proto := &protocol.Protocol{Version: "1"}
	content := buildQualitySkillContent(t.TempDir(), proto)
	for _, want := range []string{"续用首轮评审者", "不每轮新派", "doc-review skill"} {
		if !strings.Contains(content, want) {
			t.Errorf("forge-quality skill 文档回检段缺少 %q", want)
		}
	}
}
