package change

import (
	"context"
	"os"
	"path/filepath"
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
	for _, name := range []string{FileProposal, FileAcceptance} {
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
