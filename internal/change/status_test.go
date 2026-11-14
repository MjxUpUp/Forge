package change

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseTasks(t *testing.T) {
	raw := []byte(`# 任务

- [ ] 写判据（判据：acceptance #1）
- [x] 改实现（判据：acceptance #2、#2）
- [ ] 没写判据的一条
随便一行不是任务
`)
	tasks := ParseTasks(raw)
	if len(tasks) != 3 {
		t.Fatalf("想解析出 3 条任务，实际 %d 条：%+v", len(tasks), tasks)
	}
	if tasks[0].Index != 1 || tasks[0].Done || len(tasks[0].Criteria) != 1 || tasks[0].Criteria[0] != 1 {
		t.Fatalf("第 1 条解析不对：%+v", tasks[0])
	}
	if !tasks[1].Done || len(tasks[1].Criteria) != 1 || tasks[1].Criteria[0] != 2 {
		t.Fatalf("第 2 条应完成且判据去重为 [2]：%+v", tasks[1])
	}
	if tasks[2].Criteria != nil {
		t.Fatalf("没写判据的任务不该凭空得到判据：%+v", tasks[2])
	}
}

// writeAcceptance 用给定围栏正文覆盖 acceptance.md（去掉骨架标记）。
func writeAcceptance(t *testing.T, dir string, fences ...string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# 验收判据\n\n## 可执行判据\n\n")
	for _, f := range fences {
		b.WriteString("```bash\n" + f + "\n```\n\n")
	}
	writeFile(t, filepath.Join(dir, FileAcceptance), b.String())
}

func writeTasks(t *testing.T, dir string, lines ...string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, FileTasks), "# 任务\n\n"+strings.Join(lines, "\n")+"\n")
}

func TestGateTask_RunsOnlyReferencedFences(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	fillScaffold(t, dir)
	writeAcceptance(t, dir, "echo one > one.txt", "echo two > two.txt")
	writeTasks(t, dir, "- [ ] 第一步（判据：acceptance #1）", "- [ ] 第二步（判据：acceptance #2）")

	res, err := GateTask(context.Background(), root, "demo", 1, GateOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Summary.Passed {
		t.Fatalf("节点 1 应该通过：%+v", res.Summary.Checks)
	}
	if len(res.Summary.Checks) != 1 || res.Summary.Checks[0].Index != 1 {
		t.Fatalf("只该跑判据 #1，且序号保留原文：%+v", res.Summary.Checks)
	}
	if res.Summary.Task != 1 {
		t.Fatalf("摘要要记下是哪条任务的 gate：%+v", res.Summary)
	}
	if res.Summary.Sealed {
		t.Fatal("节点证据不能封印整条 change")
	}
	if !exists(filepath.Join(root, "one.txt")) {
		t.Fatal("判据 #1 的副作用应发生在仓库根")
	}
	if exists(filepath.Join(root, "two.txt")) {
		t.Fatal("没被引用的判据 #2 不该被执行")
	}

	c, _, err := Load(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if c.Sealed {
		t.Fatal("跑过节点 gate 之后整条 change 仍不算封印")
	}
}

func TestGateTask_RejectsBadTasks(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	fillScaffold(t, dir)
	writeAcceptance(t, dir, "echo ok")
	writeTasks(t, dir, "- [x] 已经完成（判据：acceptance #1）", "- [ ] 没写判据")

	if _, err := GateTask(context.Background(), root, "demo", 1, GateOptions{}); err == nil {
		t.Fatal("已完成的任务不该再 gate")
	}
	if _, err := GateTask(context.Background(), root, "demo", 2, GateOptions{}); err == nil {
		t.Fatal("没写判据的任务 gate 不了")
	}
	if _, err := GateTask(context.Background(), root, "demo", 9, GateOptions{}); err == nil {
		t.Fatal("越界的任务序号应报错")
	}
}

func TestGateTask_FailureKeepsEvidence(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	fillScaffold(t, dir)
	writeAcceptance(t, dir, "exit 3")
	writeTasks(t, dir, "- [ ] 一定会失败（判据：acceptance #1）")

	res, err := GateTask(context.Background(), root, "demo", 1, GateOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Passed {
		t.Fatal("exit 3 不该算通过")
	}
	if res.Summary.Sealed {
		t.Fatal("失败的节点更不能封印")
	}
	if !exists(filepath.Join(res.EvidenceDir, SummaryFile)) {
		t.Fatal("失败也要落证据")
	}
}

func TestStatus_UnfilledPointsAtMissingDocs(t *testing.T) {
	// 这里刻意不填模板：骨架未清时状态要指向"补齐产物"，而不是让人去跑判据。
	root := t.TempDir()
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, "demo", ""); err != nil {
		t.Fatal(err)
	}
	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if st.Verdict == "clean" {
		t.Fatalf("模板骨架不该是 clean：%+v", st)
	}
	joined := strings.Join(st.NextSteps, "\n")
	if !strings.Contains(joined, "forge:fill") {
		t.Fatalf("下一步要提醒删掉骨架标记：%v", st.NextSteps)
	}
}

func TestStatus_BlockedAfterRepeatedSameCause(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	fillScaffold(t, dir)
	writeAcceptance(t, dir, "false")
	writeTasks(t, dir, "- [ ] 第一步（判据：acceptance #1）")

	for i := 0; i < MaxSameCauseAttempts; i++ {
		if _, err := Verify(context.Background(), root, "demo", VerifyOptions{Timeout: 30 * time.Second}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if st.Verdict != "blocked" {
		t.Fatalf("同因失败 %d 次后应停下交给人：%+v", MaxSameCauseAttempts, st)
	}
	if st.Blocked == "" || !strings.Contains(strings.Join(st.NextSteps, "\n"), "停下") {
		t.Fatalf("blocked 要说明原因并明确停下：%+v", st)
	}
}

func TestStatus_SealedSuggestsArchive(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	fillScaffold(t, dir)
	writeAcceptance(t, dir, "echo ok")
	writeTasks(t, dir, "- [ ] 第一步（判据：acceptance #1）")

	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{Timeout: 30 * time.Second}); err != nil {
		t.Fatal(err)
	}
	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if st.LastVerdict != "sealed" || st.Verdict == "blocked" {
		t.Fatalf("验证通过后应是 sealed：%+v", st)
	}
	joined := strings.Join(st.NextSteps, "\n")
	if !strings.Contains(joined, "archive") {
		t.Fatalf("封印之后下一步应是归档：%v", st.NextSteps)
	}
}

func TestFastChange_OnlyNeedsProposalAndAcceptance(t *testing.T) {
	requireBash(t)
	root := t.TempDir()
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := NewFast(root, "tiny", "小改动")
	if err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(dir, FastFile)) {
		t.Fatal("fast change 应落 FAST 标记")
	}
	if exists(filepath.Join(dir, FileDesign)) || exists(filepath.Join(dir, FileTasks)) {
		t.Fatal("fast change 只写 proposal 与 acceptance 两份模板")
	}
	// 只补齐两份就够了。
	for _, name := range []string{FileGoal, FileProposal, FileAcceptance} {
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, strings.ReplaceAll(string(raw), scaffoldMarker, ""))
	}
	writeAcceptance(t, dir, "echo ok")

	res, err := Verify(context.Background(), root, "tiny", VerifyOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("fast change 不该因为缺 design/tasks 被拦：%v", err)
	}
	if !res.Summary.Sealed || !res.Summary.Fast {
		t.Fatalf("fast change 的封印要标记 fast：%+v", res.Summary)
	}
	c, _, err := Load(root, "tiny")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Fast || !c.Sealed {
		t.Fatalf("列表视图应能看出 fast 且已封印：%+v", c)
	}
}

func TestLoad_IgnoresNodeEvidenceForSealing(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	fillScaffold(t, dir)
	writeAcceptance(t, dir, "echo ok")
	writeTasks(t, dir, "- [ ] 第一步（判据：acceptance #1）")

	// 先整条验证通过（封印）。
	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{Timeout: 30 * time.Second}); err != nil {
		t.Fatal(err)
	}
	// 再跑一次节点 gate：它更新了"最新证据"，但不该把封印冲掉。
	if _, err := GateTask(context.Background(), root, "demo", 1, GateOptions{Timeout: 30 * time.Second}); err != nil {
		t.Fatal(err)
	}
	c, _, err := Load(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Sealed {
		t.Fatalf("节点证据顶替了整条 change 的封印：%+v", c)
	}
}

func TestStatus_BlockedWhenNodeExhausted(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	fillScaffold(t, dir)
	writeAcceptance(t, dir, "exit 4")
	writeTasks(t, dir, "- [ ] 卡住的一步（判据：acceptance #1）")

	for i := 0; i < MaxSameCauseAttempts; i++ {
		if _, err := GateTask(context.Background(), root, "demo", 1, GateOptions{Timeout: 30 * time.Second}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if st.Verdict != "blocked" {
		t.Fatalf("节点同因失败 %d 次后整条循环应停下：%+v", MaxSameCauseAttempts, st)
	}
	if len(st.Nodes) != 1 || st.Nodes[0].State != NodeBlocked {
		t.Fatalf("节点应标记为 blocked：%+v", st.Nodes)
	}
	if !strings.Contains(st.Blocked, "任务 1") {
		t.Fatalf("停下时要指明卡在哪条任务：%q", st.Blocked)
	}
}

func TestParseTasks_StripsOwnNumbering(t *testing.T) {
	tasks := ParseTasks([]byte(`# 任务：demo

- [ ] 1. 第一条（判据：acceptance #1）
- [ ] 2、第二条（判据：acceptance #2）
- [ ] 2.0 兼容的正文不该被剪
`))
	if got := tasks[0].Text; got != "第一条（判据：acceptance #1）" {
		t.Fatalf("Task 1 text = %q, want 去掉自己的编号", got)
	}
	if got := tasks[1].Text; got != "第二条（判据：acceptance #2）" {
		t.Fatalf("Task 2 text = %q, want 去掉「2、」", got)
	}
	if got := tasks[2].Text; got != "2.0 兼容的正文不该被剪" {
		t.Fatalf("Task 3 text = %q, 序号不同不该剪", got)
	}
}

// writeAttemptSummary 造一份指定结论的证据（不实跑命令）。
func writeAttemptSummary(t *testing.T, changeDir, ts string, sum Summary) {
	t.Helper()
	ev := filepath.Join(changeDir, EvidenceDirName, ts)
	if err := os.MkdirAll(ev, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(sum)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ev, SummaryFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// PriorAttempts 是给返工的人看的死路记忆：失败的尝试按新→旧列出失败命令、
// 退出码与输出文件名，已封印后不再出现。
func TestStatus_PriorAttemptsListFailingHistory(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "demo", "demo")
	if err != nil {
		t.Fatal(err)
	}
	writeAttemptSummary(t, dir, "20260101T000000Z", Summary{
		Change: "demo", Passed: false,
		Checks: []Check{{Index: 1, Command: "go test ./a", Exit: 1, Shell: "bash", StderrFile: "stderr.txt"}},
	})
	writeAttemptSummary(t, dir, "20260101T000100Z", Summary{
		Change: "demo", Passed: false, Task: 2,
		Checks: []Check{{Index: 1, Command: "go test ./b", Exit: 2, Shell: "sh", StdoutFile: "stdout.txt"}},
	})

	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.PriorAttempts) != 2 {
		t.Fatalf("应列出 2 条失败尝试，got %+v", st.PriorAttempts)
	}
	newest := st.PriorAttempts[0]
	if newest.Evidence != "20260101T000100Z" || newest.Command != "go test ./b" || newest.Exit != 2 || newest.Task != 2 {
		t.Errorf("最新一条应排在最前且带命令/退出码/任务号，got %+v", newest)
	}
	if newest.StdoutFile != "stdout.txt" {
		t.Errorf("应带输出文件名，got %+v", newest)
	}
	if st.PriorAttempts[1].Command != "go test ./a" {
		t.Errorf("第二条应是更早的失败，got %+v", st.PriorAttempts[1])
	}

	// 封印后是终态：历史失败只作记录，不再作为"返工先读"的清单。
	acceptance := []byte("# acceptance\n\n```bash\n判据：\ntest -d spec\n```\n")
	goalBody := "# 目标契约\n\n## Objective\n\n做点事。\n\n## Why\n\n因为要做。\n\n## Constraints\n\n- 无。\n\n## Out of Scope\n\n- 无。\n\n## Check\n\n- acceptance #1 通过。\n"
	for name, body := range map[string][]byte{
		"goal.md":       []byte(goalBody),
		"acceptance.md": acceptance,
		"proposal.md":   []byte("# 提案\n\n做点事。\n"),
		"design.md":     []byte("# 设计\n\n就按提案做。\n"),
		"tasks.md":      []byte("# 任务\n\n- [ ] 跑判据（判据：acceptance #1）\n"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeAttemptSummary(t, dir, "20260101T000200Z", Summary{
		Change: "demo", Passed: true, Sealed: true,
		AcceptanceSHA256: hashBytes(acceptance),
		Checks:           []Check{{Index: 1, Command: "test -d spec", Exit: 0}},
	})
	st, err = StatusOf(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if st.Verdict != "clean" || len(st.PriorAttempts) != 0 {
		t.Fatalf("封印后应为 clean 且不列 prior attempts，got verdict=%s prior=%+v", st.Verdict, st.PriorAttempts)
	}
}

// 没有失败命令可定位时（checks 为空），理由要说明是证据不完整，而不是编造
// 一条不存在的命令。
func TestStatus_PriorAttemptsExplainMissingCommand(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "demo", "demo")
	if err != nil {
		t.Fatal(err)
	}
	writeAttemptSummary(t, dir, "20260101T000000Z", Summary{Change: "demo", Passed: false})

	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.PriorAttempts) != 1 || st.PriorAttempts[0].Reason == "" {
		t.Fatalf("缺命令时应写理由，got %+v", st.PriorAttempts)
	}
}

func TestPriorAttempts_SkipsPassingAndCapsAtTen(t *testing.T) {
	atts := []Attempt{{Dir: "evidence/20260101T000000Z", Passed: true}}
	for i := 0; i < 12; i++ {
		atts = append(atts, Attempt{
			Dir:          fmt.Sprintf("evidence/20260101T%06dZ", i),
			FirstFailure: &Check{Command: "go test ./...", Exit: 1},
		})
	}
	got := priorAttempts(atts, 10)
	if len(got) != 10 {
		t.Fatalf("最多 10 条，got %d", len(got))
	}
	for _, pa := range got {
		if pa.Command == "" {
			t.Fatalf("通过的记录不该进死路记忆：%+v", pa)
		}
	}
	if got[0].Evidence != "20260101T000000Z" || got[9].Evidence != "20260101T000009Z" {
		t.Fatalf("输入是新→旧，输出应保持：首条 %q 末条 %q", got[0].Evidence, got[9].Evidence)
	}
}

func TestPriorAttempts_MarkTimeoutInsteadOfExit(t *testing.T) {
	atts := []Attempt{{
		Dir:          "evidence/20260101T000000Z",
		FirstFailure: &Check{Command: "sleep 30", Exit: 1, Shell: "bash", TimedOut: true},
	}}
	got := priorAttempts(atts, 10)
	if len(got) != 1 || !got[0].TimedOut {
		t.Fatalf("超时要标出来，got %+v", got)
	}
	b, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"evidence"`, `"failed_command"`, `"timed_out":true`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("JSON 缺 %s：%s", key, b)
		}
	}
	if strings.Contains(string(b), `"exit"`) {
		t.Fatalf("exit 是 0 且超时，不该输出 exit：%s", b)
	}
}

func TestStatus_CapsPriorAttemptsAtTenThroughStatusOf(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, "demo", "demo"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(ChangesDir(root), "demo")
	for name, body := range map[string]string{
		"goal.md":       "# 目标契约\n\n## Objective\n\n做点事。\n\n## Why\n\n因为要做。\n\n## Constraints\n\n- 无。\n\n## Out of Scope\n\n- 无。\n\n## Check\n\n- acceptance #1 通过。\n",
		"proposal.md":   "# 提案\n\nx\n",
		"design.md":     "# 设计\n\nx\n",
		"tasks.md":      "# 任务\n\n- [ ] 1. 跑\n\n判据：acceptance #1\n",
		"acceptance.md": "# 验收\n\n```bash\ntest -f missing.txt\n```\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 12; i++ {
		// 判据失败时 Verify 的 err 是 nil，失败体现在 Summary.Passed。
		res, err := Verify(context.Background(), root, "demo", VerifyOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if res.Summary.Passed {
			t.Fatalf("第 %d 次应当失败", i+1)
		}
	}
	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.PriorAttempts) != 10 {
		t.Fatalf("prior_attempts 应封顶 10 条，got %d", len(st.PriorAttempts))
	}
	// 同一秒内会有 -1..-12 的序号，字符串比较 "-12" < "-3" 会误判；按（时间戳, 序号）比较。
	parse := func(s string) (string, int) {
		i := strings.LastIndexByte(s, '-')
		if i < 0 {
			return s, 0
		}
		n, err := strconv.Atoi(s[i+1:])
		if err != nil {
			return s, 0
		}
		return s[:i], n
	}
	baseFirst, seqFirst := parse(st.PriorAttempts[0].Evidence)
	baseLast, seqLast := parse(st.PriorAttempts[9].Evidence)
	if baseFirst < baseLast || (baseFirst == baseLast && seqFirst <= seqLast) {
		t.Fatalf("应按新→旧（时间戳, 序号）：first=%s last=%s", st.PriorAttempts[0].Evidence, st.PriorAttempts[9].Evidence)
	}
}

func TestNodeStatuses_CountsOnlyOwnAttempts(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	fillScaffold(t, dir)
	writeAcceptance(t, dir, "echo ok", "exit 3")
	writeTasks(t, dir,
		"- [ ] 第一条（判据：acceptance #1）",
		"- [ ] 第二条（判据：acceptance #2）",
	)
	if res, err := GateTask(context.Background(), root, "demo", 1, GateOptions{Timeout: 30 * time.Second}); err != nil || !res.Summary.Passed {
		t.Fatalf("任务 1 应当通过：%v", err)
	}
	if res, err := GateTask(context.Background(), root, "demo", 2, GateOptions{Timeout: 30 * time.Second}); err != nil || res.Summary.Passed {
		t.Fatalf("任务 2 应当失败：%v", err)
	}
	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Nodes) != 2 {
		t.Fatalf("节点数 = %d", len(st.Nodes))
	}
	first, second := st.Nodes[0], st.Nodes[1]
	if first.Attempts != 1 || first.State != NodePassed {
		t.Fatalf("任务 1 应只算自己的 1 次并通过，got attempts=%d state=%s", first.Attempts, first.State)
	}
	if second.Attempts != 1 || second.State != NodeFailed {
		t.Fatalf("任务 2 应只算自己的 1 次并未通过，got attempts=%d state=%s", second.Attempts, second.State)
	}
}

func TestNodeStatuses_LastEvidenceIsNewestAttempt(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	fillScaffold(t, dir)
	writeAcceptance(t, dir, "exit 3")
	writeTasks(t, dir, "- [ ] 一定失败（判据：acceptance #1）")
	for i := 0; i < 2; i++ {
		if _, err := GateTask(context.Background(), root, "demo", 1, GateOptions{Timeout: 30 * time.Second}); err != nil {
			t.Fatal(err)
		}
	}
	c, _, err := Load(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	atts := Attempts(c)
	if len(atts) < 2 {
		t.Fatalf("应有两份证据，got %d", len(atts))
	}
	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if st.Nodes[0].LastEvidence != atts[0].Dir {
		t.Fatalf("LastEvidence 应指向最新一次（%s），got %s", atts[0].Dir, st.Nodes[0].LastEvidence)
	}
	if st.Nodes[0].LastEvidence == atts[len(atts)-1].Dir {
		t.Fatal("LastEvidence 不该指向最早一次")
	}
}
