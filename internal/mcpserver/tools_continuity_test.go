package mcpserver

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestTaskResume_ReturnsContext：resume 拉回结构化接续上下文（no_attach=true 不改 state）。
// 这是接续真相源的核心读端——agent 冷启动调一次即拿到 goal/decisions/gate 进度。
func TestTaskResume_ReturnsContext(t *testing.T) {
	root := t.TempDir()
	state := &taskpipeline.TaskState{TaskRef: "feat/r", Branch: "feat/r", Goal: "做 X"}
	state.AddDecision(taskpipeline.Decision{Content: "用 PG"})
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	out, err := taskResumeCore(root, taskResumeInput{Ref: "feat/r", NoAttach: true})
	if err != nil {
		t.Fatalf("taskResumeCore: %v", err)
	}
	if out.TaskRef != "feat/r" || out.Goal != "做 X" {
		t.Errorf("TaskRef=%q Goal=%q，期望 feat/r / 做X", out.TaskRef, out.Goal)
	}
	if len(out.Decisions) != 1 || out.Decisions[0].Content != "用 PG" {
		t.Errorf("应拉回 1 条决策，实际 %+v", out.Decisions)
	}
	if len(out.GateProgress) == 0 {
		t.Error("GateProgress 不应为空（应含 3 道门禁步骤）")
	}
}

// TestTaskResume_AnchorsSession：resume 默认锚定当前 session（多向锚定的接手方动作）。
// 设 env 让 detectTool 探测到 claude-code；接手方锚定的 tool 必须是探测值，不能回退创建方
// OriginTool=pi（错误归属会让 attach 失去意义）。已锚定的 session 再 resume 不重复 Anchored。
func TestTaskResume_AnchorsSession(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "cc-env")
	root := t.TempDir()
	state := &taskpipeline.TaskState{TaskRef: "feat/a", Branch: "feat/a", OriginTool: "pi"}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	out, err := taskResumeCore(root, taskResumeInput{Ref: "feat/a", AttachSession: "sess-b"})
	if err != nil {
		t.Fatalf("taskResumeCore: %v", err)
	}
	if !out.Anchored {
		t.Error("新 session 应触发 Anchored=true")
	}
	// 接手方 tool 必须是探测值 claude-code，不能错误回退创建方 OriginTool=pi。
	loaded, _ := taskpipeline.LoadTaskState(root, "feat/a")
	if len(loaded.SessionLinks) != 1 || loaded.SessionLinks[0].Tool != "claude-code" {
		t.Errorf("接手方 tool 应为探测值 claude-code（非 OriginTool 回退 pi），实际 %+v", loaded.SessionLinks)
	}
	// 再 resume 同 session：已锚定，不再 Anchored
	out2, _ := taskResumeCore(root, taskResumeInput{Ref: "feat/a", AttachSession: "sess-b"})
	if out2.Anchored {
		t.Error("已锚定 session 再 resume 不应 Anchored=true")
	}
}

// TestTaskResume_NoToolSkipsAnchor：无 agent env 时 detectTool 返空，resume 跳过锚定
// （不回退 OriginTool 错误归属），但仍成功返回上下文——锚定是附加动作，不能破坏 resume。
func TestTaskResume_NoToolSkipsAnchor(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	root := t.TempDir()
	state := &taskpipeline.TaskState{TaskRef: "feat/n", Branch: "feat/n", OriginTool: "pi"}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	out, err := taskResumeCore(root, taskResumeInput{Ref: "feat/n", AttachSession: "sess-x"})
	if err != nil {
		t.Fatalf("resume 不应因 tool 探测失败而报错（锚定是附加动作）: %v", err)
	}
	if out.Anchored {
		t.Error("无 tool 探测时不应锚定（避免错误归属 OriginTool）")
	}
	loaded, _ := taskpipeline.LoadTaskState(root, "feat/n")
	if len(loaded.SessionLinks) != 0 {
		t.Errorf("无 tool 时不应写入 SessionLinks，实际 %+v", loaded.SessionLinks)
	}
	if out.TaskRef != "feat/n" {
		t.Errorf("resume 应仍返回上下文，TaskRef=%q", out.TaskRef)
	}
}

// TestTaskAttach_RequiresTool：attach 无 tool 探测（无 env + 未传 Tool）必报错——
// attach 的意义是跨工具锚定，tool 空则有错误归属风险，宁可不锚定。
func TestTaskAttach_RequiresTool(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	root := t.TempDir()
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{TaskRef: "feat/at2", Branch: "feat/at2"}); err != nil {
		t.Fatal(err)
	}
	_, err := taskAttachCore(root, taskAttachInput{Ref: "feat/at2", SessionID: "s"})
	if err == nil {
		t.Fatal("无 tool 探测应报错（attach 接手方必须显式工具，避免错误归属）")
	}
}

// TestTaskDecide_Persists：decide 持久化决策到 state 文件（重读验证，非仅返回值）。
func TestTaskDecide_Persists(t *testing.T) {
	root := t.TempDir()
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{TaskRef: "feat/d", Branch: "feat/d"}); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	out, err := taskDecideCore(root, taskDecideInput{Ref: "feat/d", Content: "选方案A", Rationale: "更简单"})
	if err != nil {
		t.Fatalf("taskDecideCore: %v", err)
	}
	if out.DecisionID == "" || out.TotalDecisions != 1 {
		t.Errorf("DecisionID=%q TotalDecisions=%d，期望非空 ID / 1", out.DecisionID, out.TotalDecisions)
	}
	loaded, _ := taskpipeline.LoadTaskState(root, "feat/d")
	if len(loaded.Decisions) != 1 || loaded.Decisions[0].Content != "选方案A" || loaded.Decisions[0].Rationale != "更简单" {
		t.Errorf("state 未持久化决策（含 rationale），实际 %+v", loaded.Decisions)
	}
}

// TestTaskAttach_AnchorsTool：attach 锚定指定 session+工具；重复 attach 不增、标 AlreadyAnchored。
func TestTaskAttach_AnchorsTool(t *testing.T) {
	root := t.TempDir()
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{TaskRef: "feat/at", Branch: "feat/at"}); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}
	out, err := taskAttachCore(root, taskAttachInput{Ref: "feat/at", Tool: "pi", SessionID: "sess-pi"})
	if err != nil {
		t.Fatalf("taskAttachCore: %v", err)
	}
	if out.SessionID != "sess-pi" || out.Tool != "pi" || out.TotalSessions != 1 {
		t.Errorf("锚定结果异常: %+v", out)
	}
	if out.AlreadyAnchored {
		t.Error("首次 attach 不应 AlreadyAnchored=true")
	}
	out2, _ := taskAttachCore(root, taskAttachInput{Ref: "feat/at", Tool: "pi", SessionID: "sess-pi"})
	if !out2.AlreadyAnchored || out2.TotalSessions != 1 {
		t.Errorf("重复锚定应 AlreadyAnchored=true 且 TotalSessions 不增: %+v", out2)
	}
}

// TestTaskAttach_RequiresRef：attach 无 ref 必报错（不静默取 active，因接手方常在 main 分支）。
func TestTaskAttach_RequiresRef(t *testing.T) {
	root := t.TempDir()
	_, err := taskAttachCore(root, taskAttachInput{SessionID: "s"})
	if err == nil {
		t.Fatal("无 ref 应报错")
	}
}
