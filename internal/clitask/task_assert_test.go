package clitask

// task_assert_test.go — v2 断言 CLI 面（L2 P1）的 E2E：--assert 绑定与声明期拒绝、
// --accept-file YAML 入库、verify-acceptance 的逐断言 acceptance-assert 证据行、
// task accept --assert 补登。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// setupAssertTask 建带断言的活动任务（setupAcceptanceTask 的断言版：直接构造
// AcceptanceCriterion，不经 CLI——本文件要测的正是 CLI 的入口绑定）。
func setupAssertTask(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	const sid = `test-session-assert`
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, sid)
	const taskRef = `feat/assert-e2e`
	if err := taskpipeline.SetActiveTaskRef(dir, sid, taskRef); err != nil {
		t.Fatal(err)
	}
	state := &taskpipeline.TaskState{
		TaskRef:   taskRef,
		SessionID: sid,
		Branch:    `feat/assert-e2e`,
		StartedAt: time.Now(),
		Acceptance: []taskpipeline.AcceptanceCriterion{
			{
				Run:      `go version`,
				Expected: `go version`,
				Assertions: []taskpipeline.Assertion{
					{Type: `exit`, Expected: `0`},
					{Type: `contains`, Expected: `go version`},
				},
			},
			{
				Run: `go version`,
				Assertions: []taskpipeline.Assertion{
					{Type: `not-contains`, Expected: `NONEXISTENT-MARKER`},
				},
			},
		},
	}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	return dir, taskRef
}

// TestRunTaskVerifyAcceptanceAt_RecordsAssertionRows 钉住逐断言证据行：verify 后
// checklog 出现 acceptance-assert 行（每断言一行、deterministic source），聚合行
// acceptance 保留——兼容既有强度评级。
func TestRunTaskVerifyAcceptanceAt_RecordsAssertionRows(t *testing.T) {
	dir, taskRef := setupAssertTask(t)
	if err := runTaskVerifyAcceptanceAt(dir, taskRef, false); err != nil {
		t.Fatalf(`全部断言应通过（本测试验证证据行，不是失败路径）: %v`, err)
	}

	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	assertRows, aggregate := 0, 0
	for i := range entries {
		switch entries[i].Check {
		case taskpipeline.CheckNameAcceptanceAssert:
			assertRows++
			if entries[i].Source != checklog.EvidenceDeterministic {
				t.Errorf(`acceptance-assert 行应为 deterministic，got %q`, entries[i].Source)
			}
			if entries[i].TaskRef != taskRef {
				t.Errorf(`行 TaskRef = %q, want %q`, entries[i].TaskRef, taskRef)
			}
		case taskpipeline.CheckNameAcceptance:
			aggregate++
		}
	}
	if assertRows != 3 {
		t.Errorf(`应记 3 条 acceptance-assert 行（2+1 断言），got %d`, assertRows)
	}
	if aggregate != 1 {
		t.Errorf(`聚合行 acceptance 应恰好 1 条（兼容既有评级），got %d`, aggregate)
	}
}

// TestTaskStart_AssertBinding 钉住 --assert 绑定规则与声明期拒绝：preceding --accept
// 挂载、file-* 独立成条、叙述性 glob 被拒（exit 非 0）。
func TestTaskStart_AssertBinding(t *testing.T) {
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `assert-bind`)
	dir := t.TempDir()
	if out, _, code := runForge(t, dir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`forge init failed: %s`, out)
	}
	out, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/assert-bind`,
		`--accept`, `go version :: go version`,
		`--assert`, `exit: :: 0`,
		`--assert`, `not-contains: :: NONEXISTENT`,
		`--assert`, `file-untouched:internal/freeze/**`)
	if code != 0 {
		t.Fatalf(`task start --assert failed: %s`, out)
	}
	st, err := taskpipeline.LoadTaskState(dir, `feat/assert-bind`)
	if err != nil {
		t.Fatal(err)
	}
	// 就近绑定：exit/not-contains 与 file-untouched 都挂 preceding --accept 的条目
	//（file-* 的可独立性体现在无 preceding 时自建——见 --accept-file 测试的 Run-less 条目）。
	if len(st.Acceptance) != 1 {
		t.Fatalf(`应为 1 条带 3 断言，got %d (%v)`, len(st.Acceptance), st.Acceptance)
	}
	if len(st.Acceptance[0].Assertions) != 3 {
		t.Errorf(`exit/not-contains/file-untouched 应挂 preceding --accept 条目，got %d 断言 (%v)`, len(st.Acceptance[0].Assertions), st.Acceptance[0].Assertions)
	}
	seenFileUntouched := false
	for _, a := range st.Acceptance[0].Assertions {
		if a.Type == `file-untouched` && a.Arg == `internal/freeze/**` {
			seenFileUntouched = true
		}
	}
	if !seenFileUntouched {
		t.Errorf(`file-untouched:internal/freeze/** 应在断言集中, got %v`, st.Acceptance[0].Assertions)
	}
	if st.Acceptance[0].Source != taskpipeline.AcceptanceSourceStart {
		t.Errorf(`start 登记应盖 start 层，got %q`, st.Acceptance[0].Source)
	}

	// 声明期拒绝：叙述性 glob（CJK 主导）——降级发生在声明时而非跑时。
	if _, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/assert-narrative`,
		`--assert`, `file-changed:把所有源码文件都改一遍`); code == 0 {
		t.Error(`叙述性 glob 应被声明期拒绝（exit 非 0）`)
	}
	// 声明期拒绝：未知类型（regex 宪法——意见不走 hard）。
	if _, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/assert-regex`,
		`--accept`, `go version ::`,
		`--assert`, `regex:^ok$ :: ok`); code == 0 {
		t.Error(`regex 断言应被声明期拒绝`)
	}
}

// TestTaskStart_AcceptFile 钉住 --accept-file YAML 批量声明入库。
func TestTaskStart_AcceptFile(t *testing.T) {
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `accept-file`)
	dir := t.TempDir()
	if out, _, code := runForge(t, dir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`forge init failed: %s`, out)
	}
	yamlPath := filepath.Join(dir, `acceptance.yml`)
	doc := `criteria:
  - run: go version
    expected: go version
    assertions:
      - type: exit
        expected: "0"
  - assertions:
      - type: file-untouched
        arg: internal/freeze/**
`
	if err := os.WriteFile(yamlPath, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/accept-file`,
		`--accept-file`, yamlPath)
	if code != 0 {
		t.Fatalf(`task start --accept-file failed: %s`, out)
	}
	st, err := taskpipeline.LoadTaskState(dir, `feat/accept-file`)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Acceptance) != 2 {
		t.Fatalf(`--accept-file 应入库 2 条，got %d (%v)`, len(st.Acceptance), st.Acceptance)
	}
	if st.Acceptance[0].Run != `go version` || len(st.Acceptance[0].Assertions) != 1 || st.Acceptance[0].Assertions[0].Type != `exit` {
		t.Errorf(`[0] = %+v`, st.Acceptance[0])
	}
	if st.Acceptance[1].Run != `` || st.Acceptance[1].Assertions[0].Arg != `internal/freeze/**` {
		t.Errorf(`[1] = %+v`, st.Acceptance[1])
	}
	// 坏 YAML：明确报错而非静默空入库。
	if err := os.WriteFile(yamlPath, []byte("criteria: [}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/accept-file-bad`,
		`--accept-file`, yamlPath); code == 0 {
		t.Error(`坏 YAML 应拒绝启动`)
	}
}

// TestTaskAccept_Assert 钉住补登通道的 --assert：挂到本命令最后一条标准。
func TestTaskAccept_Assert(t *testing.T) {
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `accept-assert`)
	dir := t.TempDir()
	if out, _, code := runForge(t, dir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`forge init failed: %s`, out)
	}
	if out, _, code := runForge(t, dir, `task`, `start`, `--ref`, `feat/accept-assert`); code != 0 {
		t.Fatalf(`task start failed: %s`, out)
	}
	out, _, code := runForge(t, dir, `task`, `accept`,
		`go version :: go version`,
		`--assert`, `exit: :: 0`)
	if code != 0 {
		t.Fatalf(`task accept --assert failed: %s`, out)
	}
	st, err := taskpipeline.LoadTaskState(dir, `feat/accept-assert`)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Acceptance) != 1 || len(st.Acceptance[0].Assertions) != 1 || st.Acceptance[0].Assertions[0].Type != `exit` {
		t.Fatalf(`--assert 应挂到补登的最后一条标准，got %+v`, st.Acceptance)
	}
	if st.Acceptance[0].Source != taskpipeline.AcceptanceSourceManual {
		t.Errorf(`补登应盖 manual 层，got %q`, st.Acceptance[0].Source)
	}
	if !strings.Contains(out, `manual`) {
		t.Errorf(`输出应披露 manual 层，got: %s`, out)
	}
}
