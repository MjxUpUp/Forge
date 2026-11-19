package clitask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// TestArtifactStartHint_ChangePinned：钉 change 契约后，开工提示必须走 change 口径，
// 不能再说「产物链预期节点未登记」（与 implement gate 的文案自相矛盾）。
func TestArtifactStartHint_ChangePinned(t *testing.T) {
	st := &taskpipeline.TaskState{GoalContract: &tasktypes.GoalContract{Path: "goal.md", SHA256: "x", Change: "c1"}}
	got := artifactStartHint(t.TempDir(), st)
	if !strings.Contains(got, "已钉 change 目标契约（c1）") {
		t.Fatalf("change 口径提示缺失：%q", got)
	}
	if strings.Contains(got, "未登记") {
		t.Fatalf("已钉 change 后不应再报产物链未登记：%q", got)
	}
}

// TestRepoHasGoTestFiles：无测试文件与有测试文件两种仓库要能区分（report 措辞的事实来源）。
func TestRepoHasGoTestFiles(t *testing.T) {
	empty := t.TempDir()
	if found, known := repoHasGoTestFiles(empty); found || !known {
		t.Fatalf("空目录应 known=true found=false，得到 found=%v known=%v", found, known)
	}
	if err := os.WriteFile(filepath.Join(empty, "x_test.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if found, known := repoHasGoTestFiles(empty); !found || !known {
		t.Fatalf("存在 *_test.go 时应 found=true known=true，得到 found=%v known=%v", found, known)
	}
}

// TestBuildTaskReport_RepoHasGoTestFilesWiring 钉住 buildTaskReport 的接线：
// 只测 repoHasGoTestFiles 不足以防止字段被断线（评审变异 B）。
func TestBuildTaskReport_RepoHasGoTestFilesWiring(t *testing.T) {
	dir := t.TempDir()
	writeLibGo(t, dir, "package demo\n")
	state := &taskpipeline.TaskState{TaskRef: "feat/demo", Acceptance: nil}
	rep, err := buildTaskReport(dir, state)
	if err != nil {
		t.Fatal(err)
	}
	if rep.RepoHasGoTestFiles == nil || *rep.RepoHasGoTestFiles {
		t.Fatalf("无测试文件时字段应为 false，得到 %v", rep.RepoHasGoTestFiles)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib_test.go"), []byte("package demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err = buildTaskReport(dir, state)
	if err != nil {
		t.Fatal(err)
	}
	if rep.RepoHasGoTestFiles == nil || !*rep.RepoHasGoTestFiles {
		t.Fatalf("有测试文件时字段应为 true，得到 %v", rep.RepoHasGoTestFiles)
	}
}

// TestRenderTaskReport_NoTestFilesHonest：没有测试文件时，「未验证面：无」不得断言
// 「改动源码均有配对测试」；扫描未知时退回原句。
func TestRenderTaskReport_NoTestFilesHonest(t *testing.T) {
	rep := &taskReportJSON{Evidence: &tasktypes.TaskEvidence{Criteria: 3, CriteriaPassed: 3}}
	no := false
	rep.RepoHasGoTestFiles = &no
	out := renderTaskReport(rep)
	if !strings.Contains(out, "未检测到 Go 测试文件") {
		t.Fatalf("无测试文件时未给出诚实措辞：\n%s", out)
	}
	if strings.Contains(out, "改动源码均有配对测试") {
		t.Fatalf("无测试文件时不应断言均有配对测试：\n%s", out)
	}
	yes := true
	rep.RepoHasGoTestFiles = &yes
	if out := renderTaskReport(rep); !strings.Contains(out, "改动源码均有配对测试") {
		t.Fatalf("有测试文件时应保留原措辞：\n%s", out)
	}
	rep.RepoHasGoTestFiles = nil
	if out := renderTaskReport(rep); !strings.Contains(out, "改动源码均有配对测试") {
		t.Fatalf("扫描未知时应退回原句：\n%s", out)
	}
}

// writeLibGo 写一个最小 Go 源文件（buildTaskReport 只做扫描，不编译）。
func writeLibGo(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "lib.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBuildTaskReport_PinnedChangeDisclosed：钉了 change 的任务，交付单必须披露
// change id 并说明以 change 产物为准，而不是笼统写「无 spec 产物」。
func TestBuildTaskReport_PinnedChangeDisclosed(t *testing.T) {
	dir := t.TempDir()
	state := &taskpipeline.TaskState{TaskRef: "feat/demo"}
	rep, err := buildTaskReport(dir, state)
	if err != nil {
		t.Fatal(err)
	}
	if rep.PinnedChange != "" {
		t.Fatalf("未钉 change 时不应有 pinned_change，得到 %q", rep.PinnedChange)
	}
	if out := renderTaskReport(rep); !strings.Contains(out, "无 spec 产物") {
		t.Fatalf("未钉 change 时应保留原口径：%q", out)
	}

	state.GoalContract = &tasktypes.GoalContract{Path: "spec/changes/c1/goal.md", SHA256: "x", Change: "c1"}
	rep, err = buildTaskReport(dir, state)
	if err != nil {
		t.Fatal(err)
	}
	if rep.PinnedChange != "c1" {
		t.Fatalf("pinned_change 应为 c1，得到 %q", rep.PinnedChange)
	}
	out := renderTaskReport(rep)
	if !strings.Contains(out, "已钉 change（c1）") {
		t.Fatalf("披露行缺 change id：%q", out)
	}
	if strings.Contains(out, "无 spec 产物") {
		t.Fatalf("钉了 change 后不应再说无 spec 产物：%q", out)
	}
}

// TestBuildTaskReport_PinnedChangeBeatsSpecApproval：pinned 与 spec 审批同时存在时，
// 渲染必须仍出 pinned 行、且不出现「spec 已由 … 审批」——switch 靠 case 顺序保证
// 优先级，这条组合断言防止以后重排静默丢掉披露（代码评审 P2）。
func TestBuildTaskReport_PinnedChangeBeatsSpecApproval(t *testing.T) {
	dir := t.TempDir()
	state := &taskpipeline.TaskState{
		TaskRef:       "feat/demo",
		GoalContract:  &tasktypes.GoalContract{Path: "spec/changes/c1/goal.md", SHA256: "x", Change: "c1"},
		SpecArtifacts: map[string]taskpipeline.ArtifactRef{"spec": {Path: "spec.yaml", Hash: "deadbeef"}},
		ArtifactApprovals: map[string]taskpipeline.ArtifactApproval{
			"spec": {By: "alice", At: time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)},
		},
	}
	rep, err := buildTaskReport(dir, state)
	if err != nil {
		t.Fatal(err)
	}
	if rep.PinnedChange != "c1" || rep.SpecApprovalBy != "alice" {
		t.Fatalf("前置态不对：pinned=%q approval=%q", rep.PinnedChange, rep.SpecApprovalBy)
	}
	out := renderTaskReport(rep)
	if !strings.Contains(out, "已钉 change（c1）") {
		t.Fatalf("pinned 行缺失：%q", out)
	}
	if strings.Contains(out, "spec 已由") {
		t.Fatalf("pinned 时应取代 spec 审批行：%q", out)
	}
}
