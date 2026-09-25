package taskpipeline

// selfreport_test.go / segmenter_test.go 合一：自报一致性门禁与监控分段工具的
// 表驱动测试。三态判定（pass/warn/blocked）是门禁契约，分段（预算/重注入/时序）
// 是 judge 消费纪律——两者都钉行为不钉实现。

import (
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

func TestExtractClaimedCommands(t *testing.T) {
	tests, builds := ExtractClaimedCommands([]string{
		"跑通单元测试：`go test ./internal/taskpipeline/...`",
		"集成验证 cd /e/x && go test ./e2e/ -run TestE2E",
		"构建检查 `go build ./...` 无错误",
		"更新文档说明（描述性文字，无命令）",
		"`cargo test --workspace` 全绿",
	})
	if len(tests) != 3 {
		t.Fatalf("测试类声称应 3 条，实际 %v", tests)
	}
	wantTests := map[string]bool{
		"go test ./internal/taskpipeline/...": false,
		"go test ./e2e/":                      false,
		"cargo test --workspace":              false,
	}
	for _, c := range tests {
		if _, ok := wantTests[c]; !ok {
			t.Fatalf("意外的测试类声称 %q", c)
		}
		wantTests[c] = true
	}
	for c, hit := range wantTests {
		if !hit {
			t.Fatalf("缺测试类声称 %q（得 %v）", c, tests)
		}
	}
	if len(builds) != 1 || builds[0] != "go build ./..." {
		t.Fatalf("构建类声称应只有 go build ./...，实际 %v", builds)
	}
}

// TestCheckSelfReport_States 三态契约：blocked（零证据）/ warn（部分差集）/ pass。
func TestCheckSelfReport_States(t *testing.T) {
	dir := t.TempDir()
	// toollog 有数据（ToollogHasData 边界）：先记录一条无关 Bash 调用打底。
	if err := toolusage.Record(dir, &toolusage.ToolCall{
		ToolName: "Bash", ToolInput: "echo hi", TaskRef: "t/sr", Timestamp: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	mk := func(desc string, done bool) *TaskState {
		s := NewTaskStateForTest("t/sr")
		s.Checklist = []ChecklistItem{{ID: 1, Desc: desc, Done: done}}
		return s
	}

	t.Run("blocked_测试声称零证据", func(t *testing.T) {
		res := CheckSelfReport(dir, mk("单测已跑：`go test ./internal/...`", true))
		if !res.Checked || !res.Blocked {
			t.Fatalf("应 blocked，实际 %+v", res)
		}
		if len(res.UnmatchedTests) != 1 {
			t.Fatalf("差集应 1 条: %+v", res)
		}
	})
	t.Run("warn_有部分证据时差集降级", func(t *testing.T) {
		if err := toolusage.Record(dir, &toolusage.ToolCall{
			ToolName: "Bash", ToolInput: "cd x && go test ./other/ -run A", TaskRef: "t/sr", Timestamp: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
		res := CheckSelfReport(dir, mk("单测已跑：`go test ./internal/...`", true))
		if !res.Checked || res.Blocked {
			t.Fatalf("有其他测试证据时应 warn 不 blocked，实际 %+v", res)
		}
		if len(res.UnmatchedTests) != 1 {
			t.Fatalf("差集仍应列出: %+v", res)
		}
	})
	t.Run("pass_声称有据", func(t *testing.T) {
		if err := toolusage.Record(dir, &toolusage.ToolCall{
			ToolName: "Bash", ToolInput: "go test ./internal/taskpipeline/... -count=1", TaskRef: "t/sr", Timestamp: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
		res := CheckSelfReport(dir, mk("单测已跑：`go test ./internal/taskpipeline/...`", true))
		if !res.Checked || res.Blocked || len(res.UnmatchedTests) != 0 {
			t.Fatalf("应 pass，实际 %+v", res)
		}
	})
	t.Run("skip_无toollog遥测", func(t *testing.T) {
		empty := t.TempDir()
		res := CheckSelfReport(empty, mk("单测已跑：`go test ./...`", true))
		if res.Checked {
			t.Fatalf("toollog 缺失应 Checked=false（无法验证≠通过）: %+v", res)
		}
	})
	t.Run("skip_无checklist", func(t *testing.T) {
		res := CheckSelfReport(dir, NewTaskStateForTest("t/sr"))
		if res.Checked {
			t.Fatalf("无 checklist 应跳过: %+v", res)
		}
	})
}

// TestCheckSelfReport_EscapeHatch 逃生舱留痕后放行。
func TestCheckSelfReport_EscapeHatch(t *testing.T) {
	dir := t.TempDir()
	if err := toolusage.Record(dir, &toolusage.ToolCall{
		ToolName: "Bash", ToolInput: "echo hi", TaskRef: "t/sr2", Timestamp: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	s := NewTaskStateForTest("t/sr2")
	s.Checklist = []ChecklistItem{{ID: 1, Desc: "`go test ./...` 全绿", Done: true}}
	t.Setenv("FORGE_SELF_REPORT", "disable")
	res := CheckSelfReport(dir, s)
	if res.Checked {
		t.Fatalf("逃生舱应放行（Checked=false）: %+v", res)
	}
	entries, err := checklog.LoadAll(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("逃生舱应留 checklog 痕: %v %v", entries, err)
	}
	if entries[0].Check != checklog.CheckEscapeHatch {
		t.Fatalf("应为 escape-hatch 行: %+v", entries[0])
	}
}

// TestCheckSelfReport_ChecklogRow 判定落 checklog（fail 形态带差集文案）。
func TestCheckSelfReport_ChecklogRow(t *testing.T) {
	dir := t.TempDir()
	if err := toolusage.Record(dir, &toolusage.ToolCall{
		ToolName: "Bash", ToolInput: "echo hi", TaskRef: "t/sr3", Timestamp: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	s := NewTaskStateForTest("t/sr3")
	s.Checklist = []ChecklistItem{{ID: 1, Desc: "`pytest tests/` 全过", Done: true}}
	res := CheckSelfReport(dir, s)
	if !res.Blocked {
		t.Fatalf("应 blocked")
	}
	entries, _ := checklog.LoadAll(dir)
	found := false
	for _, e := range entries {
		if e.Check == checklog.CheckSelfReport {
			found = true
			if e.EffectiveLevel() != checklog.LevelFail {
				t.Fatalf("blocked 形态应落 fail: %+v", e)
			}
			if !strings.Contains(e.Detail, "pytest tests/") {
				t.Fatalf("detail 应含差集: %s", e.Detail)
			}
		}
	}
	if !found {
		t.Fatal("应落 self-report-consistency 行")
	}
}

// TestSegmentEvents 分段契约：预算切窗 + 每窗头部重注入 + 时序合并稳定。
func TestSegmentEvents(t *testing.T) {
	base := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	var entries []checklog.Entry
	var calls []toolusage.ToolCall
	// 合成 100K+ 字符的"长轨迹"：50 条 check（每条 ~2.2K 字符 detail）+ 20 条 tool。
	for i := 0; i < 50; i++ {
		entries = append(entries, checklog.Entry{
			Check: checklog.CheckCheatScan, Passed: i%7 != 0,
			Detail: strings.Repeat("d", 2200), RecordedAt: base.Add(time.Duration(i) * time.Minute),
			TaskRef: "t/seg",
		})
	}
	for i := 0; i < 20; i++ {
		calls = append(calls, toolusage.ToolCall{
			ToolName: "Bash", ToolInput: strings.Repeat("c", 400), TaskRef: "t/seg",
			Timestamp: base.Add(time.Duration(i*3) * time.Minute),
		})
	}
	windows := SegmentEvents(entries, calls, 8000, "GUARD: 测试头部")
	if len(windows) < 5 {
		t.Fatalf("100K+ 轨迹按 8K 预算应切 ≥5 窗，实际 %d", len(windows))
	}
	for i, w := range windows {
		if w.Header != "GUARD: 测试头部" {
			t.Fatalf("窗 %d 头部应重注入守卫行", i)
		}
		size := 0
		for _, l := range w.Lines {
			size += len(l)
		}
		if size > 8000+2200 { // 单行可能超预算（不拆行），容差一行
			t.Fatalf("窗 %d 超预算: %d", i, size)
		}
	}
	// 时序合并：首行时间 ≤ 末行时间（乱序输入不产生倒窗）。
	first := windows[0].Lines[0]
	last := windows[len(windows)-1].Lines[len(windows[len(windows)-1].Lines)-1]
	if strings.Compare(first[:16], last[:16]) > 0 {
		t.Fatalf("时序倒窗: %q > %q", first[:16], last[:16])
	}
}

// TestSegmentEvents_Degenerate 退化形态：budget<=0 单窗；空输入给一个空窗。
func TestSegmentEvents_Degenerate(t *testing.T) {
	if w := SegmentEvents(nil, nil, 0, ""); len(w) != 1 || len(w[0].Lines) != 0 || w[0].Header == "" {
		t.Fatalf("空输入应给单个带头部空窗: %+v", w)
	}
	e := checklog.Entry{Check: checklog.CheckCheatScan, Detail: "x", RecordedAt: time.Now()}
	if w := SegmentEvents([]checklog.Entry{e}, nil, 0, ""); len(w) != 1 || len(w[0].Lines) != 1 {
		t.Fatalf("budget<=0 应单窗: %+v", w)
	}
}

// NewTaskStateForTest 构造最小 TaskState（自报测试夹具；不落盘——CheckSelfReport
// 只读 state 字段）。
func NewTaskStateForTest(ref string) *TaskState {
	return &TaskState{TaskRef: ref}
}
