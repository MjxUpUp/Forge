package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// failAt 构造一条 FailHistory 记录。
func failAt(fp, hash string, changed map[string]string) tasktypes.AcceptanceFailure {
	return tasktypes.AcceptanceFailure{At: time.Now(), ExitCode: 1, Fingerprint: fp, ChangeHash: hash, Changed: changed}
}

// TestAcceptanceRecurrence 钉住同因复发判定（loop 契约 B1-1 / REQ-1）：
//  1. 真实实跑：同一失败命令带不同耗时噪音、输出超 500 字节，连续两次失败在不同
//     ChangeHash 下指纹相同 → 判为复发，FailHistory 经 MergeAcceptanceResults 追加；
//  2. ChangeHash 相同的重跑不计数；
//  3. 中间夹一次通过 → FailHistory 清空、重新计数；
//  4. 自动写一条死路 Decision（Kind=dead-end，By=forge），重复检出不重复写。
func TestAcceptanceRecurrence(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	// 失败脚本：先吐 >500 字节填充，再按参数吐一段每次不同的耗时噪音，最后固定失败行。
	script := "#!/bin/sh\ni=0\nwhile [ $i -lt 40 ]; do echo \"=== RUN   TestFill line $i: ok\"; i=$((i+1)); done\n" +
		"echo \"--- FAIL: TestFoo ($1s)\"\necho \"    foo_test.go:42: want 3 got 2\"\necho \"FAIL\tpkg/x\t$1s\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "fail.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := LoadLoopConfig(dir) // 未配置 → 阈值默认 2
	if cfg.RecurrenceThreshold != DefaultRecurrenceThreshold {
		t.Fatalf("默认阈值应为 %d, got %d", DefaultRecurrenceThreshold, cfg.RecurrenceThreshold)
	}

	// 1. 两次真实实跑，耗时参数不同；两次之间"改动"一次代码（提交一个源文件改 HeadCommit
	//    之后的内容 → TaskFingerprint 变）。
	state := &TaskState{TaskRef: "rec", Branch: "feat/testcov", HeadCommit: gitHead(t, dir)}
	state.Acceptance = []AcceptanceCriterion{{Run: "sh fail.sh 0.12"}}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	runVerifyAndMerge := func(run string) *TaskState {
		t.Helper()
		s, err := LoadTaskState(dir, state.TaskRef)
		if err != nil {
			t.Fatal(err)
		}
		s.Acceptance[0].Run = run
		VerifyAcceptanceWithVerdicts(dir, s)
		if err := MutateTaskState(dir, s.TaskRef, func(on *TaskState) error {
			on.Acceptance[0].Run = run
			MergeAcceptanceResults(on, s.Acceptance, false)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		on, err := LoadTaskState(dir, state.TaskRef)
		if err != nil {
			t.Fatal(err)
		}
		return on
	}
	writeSrc := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSrc("package x // v1\n")
	on := runVerifyAndMerge("sh fail.sh 0.12")
	if n := len(on.Acceptance[0].FailHistory); n != 1 {
		t.Fatalf("第一次失败应记 1 条 FailHistory, got %d", n)
	}
	if on.Acceptance[0].FailHistory[0].ChangeHash == "" {
		t.Fatal("有 HeadCommit 时跑前 ChangeHash 应非空")
	}
	if recs := DetectAcceptanceRecurrences(cfg, on.Acceptance); len(recs) != 0 {
		t.Fatalf("只有 1 次失败不应判复发: %+v", recs)
	}
	fp1 := on.Acceptance[0].FailHistory[0].Fingerprint

	// 2. 没改代码直接重跑（不同耗时）：ChangeHash 相同 → 覆盖上一条而不追加，不计为复发。
	on = runVerifyAndMerge("sh fail.sh 7.5")
	if n := len(on.Acceptance[0].FailHistory); n != 1 {
		t.Fatalf("同 ChangeHash 的重跑应覆盖上一条（仍 1 条）, got %d", n)
	}
	if got := on.Acceptance[0].FailHistory[0].Fingerprint; got != fp1 {
		t.Fatalf("同一失败带不同耗时应同指纹: %s vs %s", fp1, got)
	}
	if recs := DetectAcceptanceRecurrences(cfg, on.Acceptance); len(recs) != 0 {
		t.Fatalf("ChangeHash 相同的重跑不应判复发: %+v", recs)
	}

	// 3. 改代码后再失败：相邻两条 ChangeHash 不同、指纹相同 → 复发。
	writeSrc("package x // v2 changed\n")
	on = runVerifyAndMerge("sh fail.sh 0.9")
	recs := DetectAcceptanceRecurrences(cfg, on.Acceptance)
	if len(recs) != 1 || recs[0].CriterionIdx != 0 || recs[0].Count != 2 {
		t.Fatalf("改动后同因失败应判 1 次复发（窗口 2）, got %+v", recs)
	}
	if len(recs[0].ChangedBetween) == 0 || recs[0].ChangedBetween[0] != "x.go" {
		t.Fatalf("应点名两次失败之间动过的文件 x.go, got %v", recs[0].ChangedBetween)
	}

	// 4. 自动死路记录：Kind=dead-end、By=forge；再次检出不重复。
	if added := RecordRecurrenceDeadEnds(on, recs); added != 1 {
		t.Fatalf("应新增 1 条死路记录, got %d", added)
	}
	if added := RecordRecurrenceDeadEnds(on, recs); added != 0 {
		t.Fatalf("重复检出不应重复写, got %d", added)
	}
	d := on.Decisions[len(on.Decisions)-1]
	if !d.IsDeadEnd() || d.By != "forge" || !strings.Contains(d.Rationale, "x.go") {
		t.Fatalf("死路记录形态不对: %+v", d)
	}

	// 5. 通过即清空：换成会通过的命令，FailHistory 归零。
	if err := os.WriteFile(filepath.Join(dir, "ok.sh"), []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	on = runVerifyAndMerge("sh ok.sh")
	if len(on.Acceptance[0].FailHistory) != 0 || !on.Acceptance[0].Passed {
		t.Fatalf("通过后 FailHistory 应清空: %+v", on.Acceptance[0])
	}
}

// TestDetectAcceptanceRecurrenceRules 钉住纯判定的边界：阈值 N 要求窗口内每对相邻
// hash 都不同；空 hash 不可度量不计；阈值 ≤0 关闭；已通过的条目不判；
// self-mutating 疑点标注。
func TestDetectAcceptanceRecurrenceRules(t *testing.T) {
	same := map[string]string{"a.go": "1"}
	cfg2 := LoopConfig{RecurrenceThreshold: 2}
	cases := []struct {
		name string
		cfg  LoopConfig
		c    AcceptanceCriterion
		want int
		note string
	}{
		{"两次不同 hash 同指纹", cfg2, AcceptanceCriterion{FailHistory: []tasktypes.AcceptanceFailure{failAt("f", "h1", same), failAt("f", "h2", map[string]string{"a.go": "2"})}}, 1, ""},
		{"指纹不同", cfg2, AcceptanceCriterion{FailHistory: []tasktypes.AcceptanceFailure{failAt("f", "h1", nil), failAt("g", "h2", nil)}}, 0, ""},
		{"hash 为空不可度量", cfg2, AcceptanceCriterion{FailHistory: []tasktypes.AcceptanceFailure{failAt("f", "", nil), failAt("f", "", nil)}}, 0, ""},
		{"阈值 3 中间一对 hash 相同", LoopConfig{RecurrenceThreshold: 3}, AcceptanceCriterion{FailHistory: []tasktypes.AcceptanceFailure{failAt("f", "h1", nil), failAt("f", "h1", nil), failAt("f", "h2", nil)}}, 0, ""},
		{"阈值 3 全不同", LoopConfig{RecurrenceThreshold: 3}, AcceptanceCriterion{FailHistory: []tasktypes.AcceptanceFailure{failAt("f", "h1", nil), failAt("f", "h2", nil), failAt("f", "h3", nil)}}, 1, ""},
		{"关闭", LoopConfig{RecurrenceThreshold: 0}, AcceptanceCriterion{FailHistory: []tasktypes.AcceptanceFailure{failAt("f", "h1", nil), failAt("f", "h2", nil)}}, 0, ""},
		{"已通过不判", cfg2, AcceptanceCriterion{Passed: true, FailHistory: []tasktypes.AcceptanceFailure{failAt("f", "h1", nil), failAt("f", "h2", nil)}}, 0, ""},
		{"hash 变但文件指纹没变 → self-mutating 疑点", cfg2, AcceptanceCriterion{FailHistory: []tasktypes.AcceptanceFailure{failAt("f", "h1", same), failAt("f", "h2", same)}}, 1, "self-mutating"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recs := DetectAcceptanceRecurrences(c.cfg, []AcceptanceCriterion{c.c})
			if len(recs) != c.want {
				t.Fatalf("want %d recurrences, got %+v", c.want, recs)
			}
			if c.note != "" && (len(recs) == 0 || !strings.Contains(recs[0].Note, c.note)) {
				t.Fatalf("应带 %q 标注, got %+v", c.note, recs)
			}
		})
	}
}

// TestMergeAcceptanceResultsFailHistoryCap 钉住 FailHistory 上限与只在匹配条目上追加
// （每次用不同 ChangeHash，否则按"重跑覆盖"规则只会保留 1 条）。
func TestMergeAcceptanceResultsFailHistoryCap(t *testing.T) {
	s := &TaskState{Acceptance: []AcceptanceCriterion{{Run: "a"}, {Run: "b"}}}
	for i := 0; i < tasktypes.MaxAcceptanceFailHistory+3; i++ {
		res := []AcceptanceCriterion{{Run: "a", LastFailure: &tasktypes.AcceptanceFailure{Fingerprint: "f", ChangeHash: "h" + strings.Repeat("x", i)}}}
		MergeAcceptanceResults(s, res, false)
	}
	if n := len(s.Acceptance[0].FailHistory); n != tasktypes.MaxAcceptanceFailHistory {
		t.Fatalf("FailHistory 应封顶 %d, got %d", tasktypes.MaxAcceptanceFailHistory, n)
	}
	if len(s.Acceptance[1].FailHistory) != 0 {
		t.Fatal("未匹配的条目不应被追加")
	}
}

// TestRecurrenceStickyUntilReset 钉住复发的"粘性"与唯一出口（评审 I-1/I-2）：
// 检出复发后，没改代码的空跑重跑只覆盖上一条、不把复发顶出窗口；只有
// --reset-loop 清空 FailHistory，判词才回到 continue。
func TestRecurrenceStickyUntilReset(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	cfg := LoopConfig{RecurrenceThreshold: 2}
	st := &TaskState{TaskRef: "sticky", Branch: "feat/testcov"}
	st.Acceptance = []AcceptanceCriterion{{Run: "sh fail.sh", FailHistory: []tasktypes.AcceptanceFailure{failAt("f", "h1", nil), failAt("f", "h2", nil)}}}
	if err := SaveTaskState(dir, st); err != nil {
		t.Fatal(err)
	}
	if len(DetectAcceptanceRecurrences(cfg, st.Acceptance)) != 1 {
		t.Fatal("前置：应已复发")
	}
	// 空跑重跑三次（同 h2）：覆盖而非追加，仍复发。
	for i := 0; i < 3; i++ {
		MergeAcceptanceResults(st, []AcceptanceCriterion{{Run: "sh fail.sh", LastFailure: &tasktypes.AcceptanceFailure{Fingerprint: "f", ChangeHash: "h2", ExitCode: i + 10}}}, false)
	}
	h := st.Acceptance[0].FailHistory
	if len(h) != 2 || h[1].ExitCode != 12 {
		t.Fatalf("同 ChangeHash 重跑应覆盖上一条（长度 2、末条为最新）, got %+v", h)
	}
	if len(DetectAcceptanceRecurrences(cfg, st.Acceptance)) != 1 {
		t.Fatal("空跑重跑不得清掉复发")
	}
	// 人工重置：FailHistory 清空（盘上与内存都清），不再复发。
	if err := ResetLoopExhaustion(dir, st, "换方向"); err != nil {
		t.Fatal(err)
	}
	if len(st.Acceptance[0].FailHistory) != 0 {
		t.Fatal("reset 后内存 FailHistory 应清空")
	}
	on, err := LoadTaskState(dir, "sticky")
	if err != nil {
		t.Fatal(err)
	}
	if len(on.Acceptance[0].FailHistory) != 0 || len(DetectAcceptanceRecurrences(cfg, on.Acceptance)) != 0 {
		t.Fatalf("reset 后盘上 FailHistory 应清空且不再复发: %+v", on.Acceptance[0].FailHistory)
	}
}

// TestStripTrustDropsFailHistory 钉住外来 state 的 FailHistory 被剥离（评审 I-3）：
// 导入后本机第一次失败不能与他机记录凑成复发窗口。
func TestStripTrustDropsFailHistory(t *testing.T) {
	st := &TaskState{TaskRef: "foreign"}
	st.Acceptance = []AcceptanceCriterion{{Run: "x", Passed: true, FailHistory: []tasktypes.AcceptanceFailure{failAt("f", "h1", nil)}}}
	StripForeignGateSignals(st)
	if len(st.Acceptance[0].FailHistory) != 0 || st.Acceptance[0].Passed {
		t.Fatalf("StripForeignGateSignals 应清 FailHistory 与 Passed: %+v", st.Acceptance[0])
	}
}

// gitHead 读测试仓库 HEAD。
func gitHead(t *testing.T, dir string) string {
	t.Helper()
	h := GetHeadCommit(dir)
	if h == "" {
		t.Fatal("测试仓库应有 HEAD")
	}
	return h
}
