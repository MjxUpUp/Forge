package taskpipeline

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// deliveredPtr 造 Delivered 章指针（nil=false 语义在读取侧，指针本身表达「已盖章」）。
func deliveredPtr(b bool) *bool { return &b }

// lastCoverageEntry 取该 task 最新一条 test-coverage-gate 审计（测试里每次只记一条）。
func lastCoverageEntry(t *testing.T, root, taskRef string) checklog.Entry {
	t.Helper()
	entries, err := checklog.LoadForTask(root, taskRef)
	if err != nil {
		t.Fatalf("LoadForTask: %v", err)
	}
	var last checklog.Entry
	for _, e := range entries {
		if e.Check == CheckNameTestCoverage {
			last = e
		}
	}
	if last.Check == "" {
		t.Fatalf("no test-coverage-gate entry recorded for %s", taskRef)
	}
	return last
}

// TestNudgeDeliveredForTask pins the first-line-signal lookup feeding outcome
// classification: only a DELIVERED test-nudge counts (nil/false = never reached
// the model's context — counting it would launder a dead advisory channel into
// "confirmation"); entries of other checks never count.
//
// TestNudgeDeliveredForTask 钉住 outcome 分类的第一防线信号查询：只有**已送达**的
// test-nudge 才算数（nil/false=从未到达模型上下文——把它计入 confirmation 会把死
// advisory 通道洗白成「已告知」）；其他 check 的条目永不计数。
func TestNudgeDeliveredForTask(t *testing.T) {
	root := t.TempDir()
	const ref = "feat/outcome-lookup"

	if nudgeDeliveredForTask(root, ref) {
		t.Fatal("empty task log → false (no first-line signal)")
	}

	// 未送达（nil）与送达失败（false）都不算：信号没进上下文。
	if err := checklog.Record(root, &checklog.Entry{Check: checklog.CheckTestNudge, TaskRef: ref, Passed: true}); err != nil {
		t.Fatal(err)
	}
	if err := checklog.Record(root, &checklog.Entry{Check: checklog.CheckTestNudge, TaskRef: ref, Passed: true, Delivered: deliveredPtr(false)}); err != nil {
		t.Fatal(err)
	}
	if nudgeDeliveredForTask(root, ref) {
		t.Fatal("undelivered nudges (nil/false) must not count as first-line delivery")
	}

	if err := checklog.Record(root, &checklog.Entry{Check: checklog.CheckTestNudge, TaskRef: ref, Passed: true, Delivered: deliveredPtr(true)}); err != nil {
		t.Fatal(err)
	}
	if !nudgeDeliveredForTask(root, ref) {
		t.Fatal("delivered nudge → true")
	}

	// 同 root 其他 task 的 nudge 不串号（LoadForTask 按 TaskRef 过滤——outcome
	// 判定依赖的真轴；跨 root 隔离由 store 层路径分离保证，不在此重复钉）。
	if err := checklog.Record(root, &checklog.Entry{Check: checklog.CheckTestNudge, TaskRef: "feat/other-task", Passed: true, Delivered: deliveredPtr(true)}); err != nil {
		t.Fatal(err)
	}
	if nudgeDeliveredForTask(root, "feat/third-task") {
		t.Fatal("another task's delivered nudge in the same root must not leak into this task's lookup")
	}
}

// TestCheckVerifyTestCoverage_OutcomeStamp pins the discipline-first outcome
// classification on the failing test-coverage gate: discovery when no prior
// delivered nudge exists in the task (the gate is the first disclosure);
// confirmation when one exists (agent was told and did not act); passing
// entries stay unclassified.
//
// TestCheckVerifyTestCoverage_OutcomeStamp 钉住失败态 test-coverage 门禁的
// 纪律优先 outcome 分类：task 内无已送达 nudge → discovery（门禁是第一披露点）；
// 有 → confirmation（agent 被告知过但未行动）；通过条目不分类。
func TestCheckVerifyTestCoverage_OutcomeStamp(t *testing.T) {
	missing := []string{"internal/audit/audit.go", "internal/cli/cmd_export.go"}

	t.Run("no_prior_nudge_marks_discovery", func(t *testing.T) {
		root := t.TempDir()
		st := &TaskState{TaskRef: "feat/outcome-disc"}
		if err := checkVerifyTestCoverage(root, st, missing); err != nil {
			t.Fatalf("checkVerifyTestCoverage: %v", err)
		}
		e := lastCoverageEntry(t, root, st.TaskRef)
		if e.Passed {
			t.Fatal("changed source without tests must fail the pairing check")
		}
		if e.Outcome != checklog.OutcomeDiscovery {
			t.Errorf("no prior delivered nudge → outcome=discovery, got %q", e.Outcome)
		}
		// D2 度量数据源：missing 计数/清单必须结构化落 Meta——回测脚本不得
		// 解析无契约的 Detail 散文（doc-review L2 Major-1）。
		if e.Meta["missing_files"] != "2" {
			t.Errorf("missing_files meta = %q, want 2", e.Meta["missing_files"])
		}
		if !strings.Contains(e.Meta["missing_list"], "internal/audit/audit.go") {
			t.Errorf("missing_list meta must carry the file paths, got: %q", e.Meta["missing_list"])
		}
	})

	t.Run("delivered_nudge_marks_confirmation", func(t *testing.T) {
		root := t.TempDir()
		st := &TaskState{TaskRef: "feat/outcome-conf"}
		if err := checklog.Record(root, &checklog.Entry{
			Check: checklog.CheckTestNudge, TaskRef: st.TaskRef, Passed: true,
			Delivered: deliveredPtr(true),
		}); err != nil {
			t.Fatal(err)
		}
		if err := checkVerifyTestCoverage(root, st, missing); err != nil {
			t.Fatalf("checkVerifyTestCoverage: %v", err)
		}
		e := lastCoverageEntry(t, root, st.TaskRef)
		if e.Outcome != checklog.OutcomeConfirmation {
			t.Errorf("prior delivered nudge → outcome=confirmation, got %q", e.Outcome)
		}
	})

	t.Run("passing_entry_stays_unclassified", func(t *testing.T) {
		root := t.TempDir()
		st := &TaskState{TaskRef: "feat/outcome-pass"}
		paired := []string{"internal/audit/audit.go", "internal/audit/audit_test.go"}
		if err := checkVerifyTestCoverage(root, st, paired); err != nil {
			t.Fatalf("checkVerifyTestCoverage: %v", err)
		}
		e := lastCoverageEntry(t, root, st.TaskRef)
		if !e.Passed {
			t.Fatal("paired source+test must pass")
		}
		if e.Outcome != "" {
			t.Errorf("passing entry must stay unclassified, got %q", e.Outcome)
		}
	})
}

// TestCheckVerifyScopeDrift_OutcomeAlwaysDiscovery pins the honest-gap rule: no
// first-line scope signal exists today, so a failing scope-drift entry is always
// a discovery — the metric then shows exactly which gates still lack upstream
// coverage (the case for P3's forward move).
//
// TestCheckVerifyScopeDrift_OutcomeAlwaysDiscovery 钉住如实缺口规则：今日不存在
// scope 的第一防线信号，失败的 scope-drift 条目恒为 discovery——度量因此能直接
// 指出哪些门禁还缺上游覆盖（正是 P3 前移的依据）。
func TestCheckVerifyScopeDrift_OutcomeAlwaysDiscovery(t *testing.T) {
	root := t.TempDir()
	st := &TaskState{TaskRef: "feat/outcome-scope", PlanScope: []string{"internal/audit/"}}
	if err := checkVerifyScopeDrift(root, st, []string{"internal/audit/audit.go", "internal/cli/cmd_export.go"}); err != nil {
		t.Fatalf("checkVerifyScopeDrift: %v", err)
	}
	entries, err := checklog.LoadForTask(root, st.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	var drift checklog.Entry
	for _, e := range entries {
		if e.Check == checklog.CheckScopeDrift {
			drift = e
		}
	}
	if drift.Check == "" {
		t.Fatal("no scope-drift entry recorded")
	}
	if drift.Passed {
		t.Fatal("out-of-scope change must fail the drift check")
	}
	if drift.Outcome != checklog.OutcomeDiscovery {
		t.Errorf("scope-drift failure is always discovery (no upstream signal today), got %q", drift.Outcome)
	}
}
