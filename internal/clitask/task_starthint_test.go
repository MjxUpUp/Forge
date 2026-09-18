package clitask

// task_starthint_test.go —— discipline-first P3 的 task start 提示渲染守卫。探针
// 语义（ArtifactChainExpectation）由 taskpipeline 侧测试钉住，此处钉渲染层：
// 缺节点时产出单行提示（链序 → 连接、指引在尾）；链完整/逃生时静默（空串）。

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// TestTaskStart_ArtifactHint 钉住开工前提示的渲染契约（命名对齐 spec P3 验收
// 围栏 `-run TestTaskStart`——复审 P3-2：TestArtifactStartHint 匹配不到围栏，
// 假绿）。
func TestTaskStart_ArtifactHint(t *testing.T) {
	root := t.TempDir() // 无 schema.yaml → 默认链 proposal→spec→design→plan

	st := &taskpipeline.TaskState{TaskRef: "feat/hint"}
	line := artifactStartHint(root, st)
	if line == "" {
		t.Fatal("fresh task on default chain must produce a hint line")
	}
	for _, want := range []string{"proposal→spec", "先产出", "implement gate 将按档执法"} {
		if !strings.Contains(line, want) {
			t.Errorf("hint must contain %q, got: %q", want, line)
		}
	}

	st.SpecArtifacts = map[string]tasktypes.ArtifactRef{"proposal": {}, "spec": {}, "design": {}, "plan": {}}
	if line := artifactStartHint(root, st); line != "" {
		t.Errorf("fully registered chain → silent (empty), got: %q", line)
	}
}
