package taskpipeline

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStderr 临时重定向 os.Stderr 捕获 ExecuteTaskGate 等 stderr advisory 输出。
// 与 cli 包的 captureStdout 同构，但 taskpipeline 包内此前无 stderr 捕获——#3 的
// acceptance advisory 写 os.Stderr，需要本 helper 钉住。
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// TestExecuteTaskGate_TaskVerifyAcceptanceAdvisory 钉住 #3 的 task-verify advisory：
// 任务登记了验收标准但未全部通过（Passed 全 false = 还没跑 verify-acceptance）时，gate 必须
// stderr 提醒跑 'forge task verify-acceptance'。FORGE_WORK_ACTIVITY=disable 绕过 activity
// 硬失败（executor_test.go:809 同款 escape hatch），让控制流走到 acceptance advisory 块。
// 关键契约：advisory 只读 state 提醒，不记 CheckNameAcceptance 条目（该条目专属于真实实跑）。
func TestExecuteTaskGate_TaskVerifyAcceptanceAdvisory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(`FORGE_WORK_ACTIVITY`, `disable`)

	state := &TaskState{TaskRef: `feat/spec`, Branch: `feat/spec`}
	state.RecordGateResult(`task-implement`, true, ``)
	// 未实跑：ParseAcceptance 后 Passed 全 false → AllAcceptancePassed=false → 应触发 advisory
	state.Acceptance = ParseAcceptance([]string{`go version :: go version`})

	stderr := captureStderr(t, func() { _, _ = ExecuteTaskGate(dir, `task-verify`, state) })

	if !strings.Contains(stderr, `forge task verify-acceptance`) {
		t.Errorf(`task-verify 应提醒跑 verify-acceptance, stderr: %s`, stderr)
	}
	if !strings.Contains(stderr, `spec-as-gate`) {
		t.Errorf(`advisory 缺 spec-as-gate 锚词: %s`, stderr)
	}
}

// TestExecuteTaskGate_TaskVerifyAcceptanceSilentWhenAllPassed 钉住静默契约：验收标准已全部
// 通过（AllAcceptancePassed=true）时，task-verify 不应再发 acceptance advisory——避免对已回扣
// 完毕的任务重复噪声。
func TestExecuteTaskGate_TaskVerifyAcceptanceSilentWhenAllPassed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(`FORGE_WORK_ACTIVITY`, `disable`)

	state := &TaskState{TaskRef: `feat/spec`, Branch: `feat/spec`}
	state.RecordGateResult(`task-implement`, true, ``)
	state.Acceptance = ParseAcceptance([]string{`go version :: go version`})
	// 模拟 verify-acceptance 已跑过：Passed=true + Output 回填（status 据此区分「没跑」与「跑过」）
	state.Acceptance[0].Passed = true
	state.Acceptance[0].Output = `go version go1.x`

	stderr := captureStderr(t, func() { _, _ = ExecuteTaskGate(dir, `task-verify`, state) })

	if strings.Contains(stderr, `verify-acceptance`) {
		t.Errorf(`全部通过时不应发 acceptance advisory: %s`, stderr)
	}
}

// TestExecuteTaskGate_TaskVerifyNoAcceptanceAdvises 钉住：门禁任务未登记验收标准时，
// task-verify 发零验收 advisory（oracle-pipeline L1 起职责翻转——complete 已被
// CheckAcceptanceRegistered 硬拦，advisory 是提前路标：现在就补考卷，而不是到
// complete 才发现被拦；2026-09-07 ratio 0.08 事故缺口由此关闭）。
func TestExecuteTaskGate_TaskVerifyNoAcceptanceAdvises(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(`FORGE_WORK_ACTIVITY`, `disable`)

	state := &TaskState{TaskRef: `feat/no-spec`, Branch: `feat/no-spec`}
	state.RecordGateResult(`task-implement`, true, ``)
	// 无 Acceptance

	stderr := captureStderr(t, func() { _, _ = ExecuteTaskGate(dir, `task-verify`, state) })

	if !strings.Contains(stderr, `考卷缺位`) {
		t.Errorf(`零验收门禁任务应发考卷缺位 advisory（complete 登记门将硬拦）：got %s`, stderr)
	}
	if !strings.Contains(stderr, `accept`) {
		t.Errorf(`advisory 应给登记出口（spec 提取/accept 补登/conventions 兜底）：got %s`, stderr)
	}
}
