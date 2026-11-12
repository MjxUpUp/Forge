package change

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 这几个用例来自两位只读评审的 P1：停止条件不能给出可执行下一步、封印优先于
// 节点历史失败、勾选不能清掉 blocked、归档命令在任何 cwd 下都能跑。

func TestStatus_BlockedNextStepsHaveNoCommand(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeAcceptance(t, dir, "false")

	for i := 0; i < MaxSameCauseAttempts; i++ {
		res, err := Verify(context.Background(), root, "demo", VerifyOptions{Timeout: 10 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if res.Summary.Passed {
			t.Fatalf("第 %d 次 verify 应该失败", i+1)
		}
	}
	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if st.Verdict != "blocked" {
		t.Fatalf("连续同因失败应该 blocked，实际 %q", st.Verdict)
	}
	for _, step := range st.NextSteps {
		if strings.Contains(step, "forge change ") {
			t.Fatalf("blocked 的下一步里不该出现可执行命令，实际：%q", step)
		}
	}
	if !strings.Contains(st.Handoff, "forge change verify demo") {
		t.Fatalf("恢复命令应放在 Handoff 里，实际：%q", st.Handoff)
	}
}

func TestStatus_SealedOverridesNodeBlocked(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeAcceptance(t, dir, "false")
	writeTasks(t, dir, "- [ ] 第一步（判据：acceptance #1）")

	for i := 0; i < MaxSameCauseAttempts; i++ {
		res, err := GateTask(context.Background(), root, "demo", 1, GateOptions{Timeout: 10 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if res.Summary.Passed {
			t.Fatalf("第 %d 次 gate 应该失败", i+1)
		}
	}
	if st, _ := StatusOf(root, "demo"); st.Verdict != "blocked" {
		t.Fatalf("节点连续失败应该 blocked，实际 %q", st.Verdict)
	}

	// 修好判据后整条 verify 通过：封印是终态，节点上的历史失败只作记录。
	writeAcceptance(t, dir, "echo ok")
	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{Timeout: 10 * time.Second}); err != nil {
		t.Fatal(err)
	}
	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if st.Verdict != "clean" || st.Blocked != "" {
		t.Fatalf("整条封印后应该是 clean 且不 blocked，实际 verdict=%q blocked=%q", st.Verdict, st.Blocked)
	}
	if len(st.NextSteps) == 0 || !strings.Contains(st.NextSteps[0], "git mv spec/changes/demo ") {
		t.Fatalf("封印后的下一步应是可执行的归档命令，实际：%v", st.NextSteps)
	}
}

func TestStatus_DoneDoesNotClearBlocked(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeAcceptance(t, dir, "false")
	writeTasks(t, dir, "- [ ] 第一步（判据：acceptance #1）")

	for i := 0; i < MaxSameCauseAttempts; i++ {
		res, err := GateTask(context.Background(), root, "demo", 1, GateOptions{Timeout: 10 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if res.Summary.Passed {
			t.Fatalf("第 %d 次 gate 应该失败", i+1)
		}
	}
	writeTasks(t, dir, "- [x] 第一步（判据：acceptance #1）")

	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if st.Verdict != "blocked" {
		t.Fatalf("勾选只是进度记录，不该清掉 blocked，实际 %q", st.Verdict)
	}
	if len(st.Nodes) != 1 || st.Nodes[0].State == NodePassed {
		t.Fatalf("没有证据的任务不该显示 passed：%+v", st.Nodes)
	}
}

func TestParseTasks_SkipsFencedExamplesAndNeedsAcceptance(t *testing.T) {
	raw := []byte(`# 任务

` + "```" + `md
- [ ] 这是文档里的示例（判据：acceptance #9）
` + "```" + `

- [ ] 真任务（判据：acceptance #1）
- [ ] 只提了判据 #3 的文档修改
`)
	tasks := ParseTasks(raw)
	if len(tasks) != 2 {
		t.Fatalf("围栏里的示例不该算任务，实际 %d 条：%+v", len(tasks), tasks)
	}
	if len(tasks[0].Criteria) != 1 || tasks[0].Criteria[0] != 1 {
		t.Fatalf("第 1 条应引用 #1：%+v", tasks[0])
	}
	if tasks[1].Criteria != nil {
		t.Fatalf("没有 acceptance 字样的 #3 不该被当成判据：%+v", tasks[1])
	}
}

func TestStatusJSON_UsesSnakeCase(t *testing.T) {
	root, dir := repoWithChange(t, "demo")
	writeTasks(t, dir, "- [ ] 第一步（判据：acceptance #1）")
	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"next_steps"`, `"index"`, `"criteria"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("status JSON 缺 %s：%s", want, raw)
		}
	}
}

// 最新一份整条摘要损坏时不能回溯到更早的封印：那是拿旧证据冒充已验证。
func TestLoad_CorruptNewestSummaryDoesNotResurrectSeal(t *testing.T) {
	root, dir := repoWithChange(t, "demo")
	writeAcceptance(t, dir, "echo ok")
	writeSealedSummary(t, dir, mustReadFile(t, filepath.Join(dir, FileAcceptance)))

	newest := filepath.Join(dir, EvidenceDirName, "20260102T000000Z")
	if err := os.MkdirAll(newest, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(newest, SummaryFile), "{ 截断的 json")

	c, _, err := Load(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if c.Sealed {
		t.Fatal("最新摘要损坏时不该回溯旧封印")
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
