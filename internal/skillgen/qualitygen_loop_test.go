package skillgen

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/protocol"
)

// TestQualitySkillLoopMode 钉住 forge-quality skill 的 Loop 模式节（loop 契约 B1-5）：
// 六步规程、judgment 三值、死路记录命令、"不设逃生 env / 不自己执行需人工命令"的
// 红线、protocol loop 段与 status --json 的机器读入口、/loop prompt 模板。
func TestQualitySkillLoopMode(t *testing.T) {
	content := buildQualitySkillContent(t.TempDir(), &protocol.Protocol{Version: "1"})
	for _, want := range []string{
		"## Loop 模式",
		"循环判词",
		"escalate / done / continue",
		"【已证伪方案】",
		"forge task decide --rejected",
		"不设任何 `FORGE_*` 逃生 env",
		"需人工",
		"forge task verify-acceptance`，读末行 `loop:`",
		"acceptance-recurrence",
		"gate-repeat-blocked",
		"`next:`",
		"forge task status --json` 的 `loop_verdict`",
		"recurrence_threshold",
		"max_verifies_without_progress",
		"`/loop` prompt 模板",
		"同因失败两次就停",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("Loop 模式节缺 %q", want)
		}
	}
	// 收尾序列的顺序必须与 NextDecision 的真实门禁链一致。
	seq := []string{"git commit", "forge task gate task-implement", "forge task gate task-verify", "forge review pass", "forge task gate task-complete", "forge task complete"}
	last := -1
	for _, s := range seq {
		i := strings.Index(content[strings.Index(content, "## Loop 模式"):], s)
		if i < 0 || i < last {
			t.Fatalf("收尾序列顺序错或缺项: %q", s)
		}
		last = i
	}
}
