package taskpipeline

// mirror_test.go — 镜像计划层的纯函数测试：缺映射创建 / 终态关闭 / no-op 差集 /
// 标题截断 / 映射往返。gh 执行层由 fake gh（FORGE_GH_BIN）在 clitask 侧覆盖。

import (
	"strings"
	"testing"
	"time"
)

func mirrorState(ref, status string, completed bool) *TaskState {
	s := &TaskState{TaskRef: ref, Summary: "做某事", StartedAt: time.Now()}
	if status != "" {
		s.Assignment = &Assignment{Agent: "claude-code", Status: status}
	}
	if completed {
		now := time.Now()
		s.CompletedAt = &now
	}
	return s
}

func TestBuildMirrorPlan(t *testing.T) {
	states := []*TaskState{
		mirrorState("t/offer", AssignOffered, false),
		mirrorState("t/done", AssignDelivered, true),
		mirrorState("t/plain", "", false), // 无分派：不镜像
	}
	mapping := map[string]int{"t/done": 42}
	plan := BuildMirrorPlan(states, mapping)
	if len(plan) != 2 {
		t.Fatalf("应有 2 条动作（创建+关闭）: %+v", plan)
	}
	var create, close *MirrorAction
	for i := range plan {
		switch plan[i].TaskRef {
		case "t/offer":
			create = &plan[i]
		case "t/done":
			close = &plan[i]
		}
	}
	if create == nil || close == nil {
		t.Fatalf("缺动作: %+v", plan)
	}
	if create.Issue != 0 || len(create.LabelAdd) != 1 || create.LabelAdd[0] != "forge:offered" {
		t.Fatalf("创建动作不符: %+v", create)
	}
	if !strings.Contains(create.Title, "t/offer") {
		t.Fatalf("标题应含 ref: %q", create.Title)
	}
	if close.Issue != 42 || !close.Close {
		t.Fatalf("关闭动作不符: %+v", close)
	}
}

func TestBuildMirrorPlan_NoOp(t *testing.T) {
	// 活跃非终态 + 已有映射 → 无动作（v1 不读远端 label，无增量需求）。
	states := []*TaskState{mirrorState("t/live", AssignClaimed, false)}
	plan := BuildMirrorPlan(states, map[string]int{"t/live": 7})
	if len(plan) != 0 {
		t.Fatalf("应 no-op: %+v", plan)
	}
}

func TestBuildMirrorPlan_FailedGetsLabel(t *testing.T) {
	states := []*TaskState{mirrorState("t/f", AssignFailed, false)}
	plan := BuildMirrorPlan(states, map[string]int{"t/f": 9})
	if len(plan) != 1 || !plan[0].Close || len(plan[0].LabelAdd) != 1 || plan[0].LabelAdd[0] != "forge:failed" {
		t.Fatalf("失败终态应打 label+关闭: %+v", plan)
	}
}

func TestMirrorTitleTruncation(t *testing.T) {
	s := mirrorState("t/long", AssignOffered, false)
	s.Summary = makeLongString(200) // 远超 60 rune
	title := mirrorTitle(s)
	if len([]rune(title)) > 80 { // 60 + 前后缀余量
		t.Fatalf("标题未截断: %d runes", len([]rune(title)))
	}
}

func makeLongString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = '字'
	}
	return string(b)
}

func TestMirrorMappingRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveMirrorMapping(dir, map[string]int{"t/a": 1, "t/b": 2}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMirrorMapping(dir)
	if err != nil || got["t/a"] != 1 || got["t/b"] != 2 {
		t.Fatalf("映射往返失败: %+v %v", got, err)
	}
	// 未初始化 → 空映射非错误
	empty, err := LoadMirrorMapping(t.TempDir())
	if err != nil || len(empty) != 0 {
		t.Fatalf("空映射应无错: %+v %v", empty, err)
	}
}
