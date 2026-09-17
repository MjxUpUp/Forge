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

// TestArtifactStartHint 钉住开工前提示的渲染契约。
func TestArtifactStartHint(t *testing.T) {
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
