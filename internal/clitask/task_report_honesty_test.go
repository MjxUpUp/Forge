package clitask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/scoringtypes"
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

// TestRepoHasTestFiles：无测试文件与有测试文件两种仓库要能区分（report 措辞的事实来源）。
func TestRepoHasTestFiles(t *testing.T) {
	empty := t.TempDir()
	if repoHasTestFiles(empty) {
		t.Fatal("空目录不应被认为有测试文件")
	}
	if err := os.WriteFile(filepath.Join(empty, "x_test.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !repoHasTestFiles(empty) {
		t.Fatal("存在 *_test.go 时应识别为有测试文件")
	}
}

// TestRenderTaskReport_NoTestFilesHonest：没有测试文件时，「未验证面：无」不得断言
// 「改动源码均有配对测试」。
func TestRenderTaskReport_NoTestFilesHonest(t *testing.T) {
	rep := &taskReportJSON{Score: &scoringtypes.ScoreResult{Overall: 88, Grade: "B"}, RepoHasTestFiles: false}
	out := renderTaskReport(rep)
	if !strings.Contains(out, "未检测到 *_test.go") {
		t.Fatalf("无测试文件时未给出诚实措辞：\n%s", out)
	}
	if strings.Contains(out, "改动源码均有配对测试") {
		t.Fatalf("无测试文件时不应断言均有配对测试：\n%s", out)
	}
	rep.RepoHasTestFiles = true
	if out := renderTaskReport(rep); !strings.Contains(out, "改动源码均有配对测试") {
		t.Fatalf("有测试文件时应保留原措辞：\n%s", out)
	}
}
