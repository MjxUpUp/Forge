package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// enforcement_test.go 钉住 vNext P2 的审计层：聚合（三路数据→报告）、双环/降格
// 信号、随机采样 join（任务×会话遥测）与输出契约。

// seedEnforcementFixture 布置三路数据：
// checklog（task-guard advisory×2 / blocked×1）、markers（sessA ignores=3 升档、
// sessB ignores=1、sessA test-edits=2）、wild（sessA×1、sessC×1）、两个已完成任务
// （t1@sSessA、t2@sSessB）。返回项目根与 DataDir。
func seedEnforcementFixture(t *testing.T) (string, string) {
	t.Helper()
	root := newWildGitRepo(t)
	dataDir := forgedata.DataDirFor(root)

	// markers：无视计数与测试编辑计数（值=字符串数字，与 embed 脚本写法一致）。
	mk := filepath.Join(dataDir, "markers")
	if err := os.MkdirAll(mk, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, val := range map[string]string{
		"forge-taskguard-ignores-sessA": "3",
		"forge-taskguard-ignores-sessB": "1",
		"forge-test-edits-sessA":        "2",
	} {
		if err := os.WriteFile(filepath.Join(mk, name), []byte(val), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// wild 申报：sessA 与 sessC 各一条。
	wd := filepath.Join(dataDir, "wild")
	if err := os.MkdirAll(wd, 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	for _, s := range []string{"sessA", "sessC"} {
		fmt.Fprintf(&buf, `{"ts":"2026-08-31T10:00:00Z","session":%q,"note":"n","branch":"main","head":"abc","task_active":false}`+"\n", s)
	}
	if err := os.WriteFile(filepath.Join(wd, "declarations.jsonl"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	// checklog：task-guard advisory×2（其一挂 t1）+ blocked×1（挂在别的任务上，
	// 专门让 t1 成为"无视升档仍完成且从未被阻断"的强制复盘样本）。
	rec := func(level checklog.Level, taskRef string) {
		t.Helper()
		if err := checklog.Record(root, &checklog.Entry{
			Check:     checklog.CheckName("task-guard"),
			Passed:    level == checklog.LevelAdvisory,
			Checked:   true,
			Level:     level,
			TaskRef:   taskRef,
			SessionID: "sessA",
			Detail:    "[task-guard] Untracked source edit — no active task.",
		}); err != nil {
			t.Fatal(err)
		}
	}
	rec(checklog.LevelAdvisory, "")
	rec(checklog.LevelAdvisory, "feat/t1")
	rec(checklog.LevelBlocked, "feat/other")

	// 两个已完成任务（ListTaskStates 消费 tasks/ 下带签名的状态文件）。
	newTask := func(ref, sid string) {
		t.Helper()
		now := time.Now()
		if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{
			TaskRef:     ref,
			Branch:      "feat/x",
			Source:      "explicit",
			Summary:     "test",
			StartedAt:   now.Add(-time.Hour),
			CompletedAt: &now,
			SessionID:   sid,
		}); err != nil {
			t.Fatal(err)
		}
	}
	newTask("feat/t1", "sessA")
	newTask("feat/t2", "sessB")
	return root, dataDir
}

func TestEnforcementReportAggregates(t *testing.T) {
	root, _ := seedEnforcementFixture(t)
	rep := buildEnforcementReport(root, forgedata.DataDirFor(root))

	if rep.TaskGuard.Advisory != 2 || rep.TaskGuard.Blocked != 1 {
		t.Errorf("task-guard 计数 = advisory %d / blocked %d, want 2/1", rep.TaskGuard.Advisory, rep.TaskGuard.Blocked)
	}
	if rep.EscalatedSessions != 1 || rep.MaxIgnores != 3 {
		t.Errorf("升档会话 %d / 峰值 %d, want 1/3（sessA=3 升档、sessB=1 不升）", rep.EscalatedSessions, rep.MaxIgnores)
	}
	if rep.TestEditTotal != 2 || rep.TestEditSessions != 1 {
		t.Errorf("测试编辑 %d 次/%d 会话, want 2/1", rep.TestEditTotal, rep.TestEditSessions)
	}
	if rep.WildDeclarations != 2 {
		t.Errorf("野外申报 = %d, want 2", rep.WildDeclarations)
	}
	if rep.TasksCompleted != 2 {
		t.Errorf("已完成任务 = %d, want 2", rep.TasksCompleted)
	}
	// 双环：存在升档会话 → 必触发；降格：blocked>0 → 不触发。
	if len(rep.DoubleLoop) == 0 {
		t.Error("存在无视升档会话，双环信号必须触发（审查规则本身）")
	}
	if len(rep.DemotionReview) != 0 {
		t.Errorf("blocked=1>0，不应列降格复审，got %v", rep.DemotionReview)
	}
}

func TestEnforcementDemotionSignal(t *testing.T) {
	root := newWildGitRepo(t)
	rep := buildEnforcementReport(root, forgedata.DataDirFor(root))
	// 全空数据：零阻断零升档 → 提升位的 zombie 复审信号必须出现。
	if len(rep.DemotionReview) == 0 {
		t.Error("零阻断零升档应列降格复审（zombie rule 信号）")
	}
	if len(rep.DoubleLoop) != 0 {
		t.Errorf("无升档会话不应触发双环, got %v", rep.DoubleLoop)
	}
}

func TestEnforcementSampleJoinsSessionTelemetry(t *testing.T) {
	root, dataDir := seedEnforcementFixture(t)
	samples := sampleCompletedTasks(root, dataDir, 5)
	if len(samples) != 2 {
		t.Fatalf("应采到全部 2 个已完成任务, got %d", len(samples))
	}
	byRef := map[string]taskAudit{}
	for _, s := range samples {
		byRef[s.TaskRef] = s
	}
	t1 := byRef["feat/t1"]
	if t1.SessionID != "sessA" || t1.Ignores != 3 || t1.WildCount != 1 || t1.Advisories != 1 || t1.Blocked != 0 {
		t.Errorf("t1 join 结果不符: %+v（want sessA/ignores=3/wild=1/adv=1/blocked=0）", t1)
	}
	if !strings.Contains(t1.Verdict, "无灾≠安全") {
		t.Errorf("t1 是无视升档仍完成且未被阻断的样本，verdict 应为强制复盘, got %q", t1.Verdict)
	}
	t2 := byRef["feat/t2"]
	if t2.Ignores != 1 || t2.WildCount != 0 {
		t.Errorf("t2 join 结果不符: %+v（want ignores=1/wild=0）", t2)
	}
}

func TestEnforcementJSONOutput(t *testing.T) {
	root, _ := seedEnforcementFixture(t)
	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	cmd.Flags().Int("sample", 2, "")
	cmd.Flags().Bool("json", true, "")
	if err := runEnforcement(cmd, nil); err != nil {
		t.Fatalf("runEnforcement: %v", err)
	}
	var rep enforcementReport
	if err := json.Unmarshal(buf.Bytes(), &rep); err != nil {
		t.Fatalf("输出须为 enforcementReport JSON: %v\n%s", err, buf.String())
	}
	if rep.TaskGuard.Advisory != 2 || len(rep.Samples) != 2 {
		t.Errorf("json 契约不符: advisory=%d samples=%d", rep.TaskGuard.Advisory, len(rep.Samples))
	}
}
