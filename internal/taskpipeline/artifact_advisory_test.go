package taskpipeline

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// TestArtifactAdvisoryDetail_ChangeContract 钉住 1.87.2 dogfood 的修复：已钉
// change goal 的任务，产物链提醒应说「以 change 产物为准」，不再指示逐段产出
// task 级产物（那会诱导 agent 重复造产物）。
func TestArtifactAdvisoryDetail_ChangeContract(t *testing.T) {
	missing := []string{"proposal", "design", "plan"}
	hint := "forge task artifact --set proposal --file <path>"

	plain := artifactAdvisoryDetail(&TaskState{}, missing, hint)
	if !strings.Contains(plain, "产物链节点缺失") || !strings.Contains(plain, hint) {
		t.Fatalf("无契约时应保留原文案，got %q", plain)
	}

	withChange := artifactAdvisoryDetail(&TaskState{GoalContract: &tasktypes.GoalContract{Change: "demo", Path: "spec/changes/demo/goal.md"}}, missing, hint)
	if !strings.Contains(withChange, "以 change 产物为准") {
		t.Fatalf("已钉 change 时应改为以 change 产物为准，got %q", withChange)
	}
	if strings.Contains(withChange, "按 schema.yaml 逐段产出") {
		t.Fatalf("已钉 change 时不应再给逐段产出指令，got %q", withChange)
	}
	if !strings.Contains(withChange, "demo") {
		t.Fatalf("文案应点名 change id，got %q", withChange)
	}

	// --goal-file/--goal 钉了 goal 但没有 change：仍按 task 级产物链提示
	goalOnly := artifactAdvisoryDetail(&TaskState{GoalContract: &tasktypes.GoalContract{Path: "goal.md", SHA256: "x"}}, missing, hint)
	if !strings.Contains(goalOnly, "产物链节点缺失") || !strings.Contains(goalOnly, "按 schema.yaml 逐段产出") {
		t.Fatalf("无 change 的 goal 契约应退回原文案，got %q", goalOnly)
	}
}
