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

func TestParseTasks_CriteriaConjunctions(t *testing.T) {
	tasks := ParseTasks([]byte(`# 任务：demo

- [ ] 1. 两个判据（判据：acceptance #1 和 #2）
- [ ] 2. 顿号写法（判据：acceptance #3、#4）
`))
	if len(tasks) != 2 {
		t.Fatalf("want 2 tasks, got %d", len(tasks))
	}
	if got := tasks[0].Criteria; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		// 「#1 和 #2」之前只解析出 #1，gate 会放行而第二条判据从没跑过。
		t.Fatalf("task 1 criteria = %v, want [1 2]", got)
	}
	if got := tasks[1].Criteria; len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("task 2 criteria = %v, want [3 4]", got)
	}
}

func TestParseTasks_FenceWidth(t *testing.T) {
	tasks := ParseTasks([]byte("# 任务：demo\n\n" + "````markdown\n" + "```bash\n" +
		"- [ ] 示例任务，不该计入\n" + "```\n" + "````\n" +
		"- [ ] 真正的第一条（判据：acceptance #1）\n"))
	if len(tasks) != 1 {
		// 固定三个反引号翻转时，```` 里的 ``` 提前收尾，示例行会被当成任务，
		// 后续 Index 整体错位。
		t.Fatalf("want exactly 1 task, got %d: %+v", len(tasks), tasks)
	}
	if tasks[0].Index != 1 || !strings.Contains(tasks[0].Text, "真正的第一条") {
		t.Fatalf("task = %+v, want 第一条真任务", tasks[0])
	}
}

func TestTrimTaskNumber_KeepsVersionLikeText(t *testing.T) {
	// 第二条任务正文以「2.5 兼容」开头：点号后是数字，不是自己的编号。
	// 必须真的走到 prefix "2." 的分支，否则删掉 startsWithDigit 守卫测试也绿。
	tasks := ParseTasks([]byte("# 任务\n\n- [ ] 1. 第一条（判据：acceptance #1）\n- [ ] 2.5 兼容范围收窄（判据：acceptance #2）\n"))
	if len(tasks) != 2 {
		t.Fatalf("want 2 tasks, got %d", len(tasks))
	}
	if got := tasks[1].Text; got != "2.5 兼容范围收窄（判据：acceptance #2）" {
		t.Fatalf("text = %q, want 保留 2.5", got)
	}
}

func TestStatus_BlockedDoneTaskHandsOffToVerify(t *testing.T) {
	root, dir := repoWithChange(t, "demo")
	writeAcceptance(t, dir, "false")
	writeTasks(t, dir, "- [ ] 1. 一条（判据：acceptance #1）\n")
	for i := 0; i < MaxSameCauseAttempts; i++ {
		if _, err := GateTask(context.Background(), root, "demo", 1, GateOptions{}); err != nil {
			t.Fatalf("GateTask #%d: %v", i+1, err)
		}
	}
	// 人放弃这条任务，勾上进度。
	writeTasks(t, dir, "- [x] 1. 一条（判据：acceptance #1）\n")
	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatalf("StatusOf: %v", err)
	}
	if st.Verdict != "blocked" {
		t.Fatalf("verdict = %q, want blocked（勾选不清锁定）", st.Verdict)
	}
	if strings.Contains(st.Handoff, "change gate") {
		// gate 会拒绝勾选过的任务：给这条命令等于把人指进死路。
		t.Fatalf("handoff = %q, 不该再让 runner 走 gate", st.Handoff)
	}
	if !strings.Contains(st.Handoff, "forge change verify demo") {
		t.Fatalf("handoff = %q, 应指出整条 verify 或取消勾选", st.Handoff)
	}
}

func TestStatus_SealedWithMissingDocNeedsRepair(t *testing.T) {
	root, dir := repoWithChange(t, "demo")
	writeAcceptance(t, dir, "echo ok")
	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, FileDesign)); err != nil {
		t.Fatalf("remove design: %v", err)
	}
	st, err := StatusOf(root, "demo")
	if err != nil {
		t.Fatalf("StatusOf: %v", err)
	}
	if st.Verdict != "action" {
		t.Fatalf("verdict = %q, want action（产物被删不能直接归档）", st.Verdict)
	}
	if len(st.NextSteps) == 0 || !strings.Contains(st.NextSteps[0], FileDesign) {
		t.Fatalf("next steps = %v, want 指向缺的 design.md", st.NextSteps)
	}
}

func TestInit_CreatesArchiveDir(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	archive := filepath.Join(ChangesDir(root), ArchiveDirName)
	if fi, err := os.Stat(archive); err != nil || !fi.IsDir() {
		// status 的归档建议是 `git mv … archive/<日期>-<id>`，目标目录不在时
		// git 会直接拒绝，新仓库第一次归档就失败。
		t.Fatalf("archive dir 未创建: %v", err)
	}
	if _, err := os.Stat(filepath.Join(archive, ".gitkeep")); err != nil {
		// 空目录不会被 git 跟踪：没有 .gitkeep，克隆下来 archive/ 又会消失。
		t.Fatalf(".gitkeep 未创建: %v", err)
	}
}

func TestParseTasks_CriteriaLooseConnectives(t *testing.T) {
	// 「或 / 以及 / 括号」之前会静默只取第一个号：gate 只跑 #1 就放行。
	for _, line := range []string{
		"- [ ] 1. 或（判据：acceptance #1 或 #2）",
		"- [ ] 1. 以及（判据：acceptance #1 以及 #2）",
		"- [ ] 1. 括号（判据：acceptance #1 和（#2））",
		"- [ ] 1. 分号截断（判据：acceptance #1；另见 #9）",
	} {
		tasks := ParseTasks([]byte("# 任务\n\n" + line + "\n"))
		if len(tasks) != 1 {
			t.Fatalf("%q: want 1 task", line)
		}
		got := tasks[0].Criteria
		want := []int{1, 2}
		if line == "- [ ] 1. 分号截断（判据：acceptance #1；另见 #9）" {
			want = []int{1}
		}
		if len(got) != len(want) {
			t.Fatalf("%q: criteria = %v, want %v", line, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%q: criteria = %v, want %v", line, got, want)
			}
		}
	}
}

func TestParseTasks_FenceVariants(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"tilde", "~~~md\n- [ ] 示例\n~~~\n- [ ] 真任务（判据：acceptance #1）\n", 1},
		{"closing with info", "```\n- [ ] 示例\n```go\n- [ ] 真任务（判据：acceptance #1）\n", 1},
		{"unclosed", "```\n- [ ] 真任务（判据：acceptance #1）\n", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tasks := ParseTasks([]byte("# 任务\n\n" + c.body))
			if len(tasks) != c.want {
				t.Fatalf("want %d tasks, got %d: %+v", c.want, len(tasks), tasks)
			}
		})
	}
}
