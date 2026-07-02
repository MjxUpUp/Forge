package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// renderStatusWithEvidence 在临时项目里建一个活动任务并写入指定的证据条目，返回
// `forge review status` 的渲染输出。det/claim 分别通过 CheckAutoCompile(deterministic)
// 与 CheckTaskVerify(agent-claim) 各记 N 条实现——SourceForCheck 自动分桶。
func renderStatusWithEvidence(t *testing.T, det, claim int) string {
	t.Helper()
	dir := t.TempDir()
	const sid = `test-session-rs`
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, sid)
	const taskRef = `feat/evidence-task`

	if err := taskpipeline.SetActiveTaskRef(dir, sid, taskRef); err != nil {
		t.Fatal(err)
	}
	state := &taskpipeline.TaskState{TaskRef: taskRef, SessionID: sid, Branch: `feat/x`, StartedAt: time.Now()}
	if err := taskpipeline.SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < det; i++ {
		checklog.Record(dir, &checklog.Entry{Check: checklog.CheckAutoCompile, Passed: true, TaskRef: taskRef})
	}
	for i := 0; i < claim; i++ {
		checklog.Record(dir, &checklog.Entry{Check: checklog.CheckTaskVerify, Passed: true, TaskRef: taskRef})
	}
	return captureStdout(t, func() { _ = renderReviewStatus(dir) })
}

// TestRenderReviewStatus_WeakDirective 钉住 #2 核心：Weak 证据（ratio<0.5）时 review
// status 必须输出校准指令，让 ratio 驱动 reviewer 去核验声称的验证是否真跑过——对冲
// agent 跳过前置就声明完成的盲区。1 deterministic + 3 agent-claim → ratio 0.25 → Weak。
// 这是把 ratio 从"仅可观测"升级为"驱动 review"的接入点：ForTask/Strength/指令任一断裂
// 即被抓。
func TestRenderReviewStatus_WeakDirective(t *testing.T) {
	got := renderStatusWithEvidence(t, 1, 3)
	if !strings.Contains(got, `证据强度`) {
		t.Errorf(`expected "证据强度" line; output:\n%s`, got)
	}
	if !strings.Contains(got, `Weak`) {
		t.Errorf(`expected Weak band (ratio 0.25); output:\n%s`, got)
	}
	if !strings.Contains(got, `核验声称的验证是否真跑过`) {
		t.Errorf(`expected Weak directive "核验声称的验证是否真跑过" — ratio must drive review; output:\n%s`, got)
	}
	// Weak 专属短语（Unverified 用的是"无 deterministic 证据"）：用它能抓 Weak→Unverified
	// 这类分档回归，而不会被两条指令共有的"核验声称的验证是否真跑过"掩盖。
	if !strings.Contains(got, `deterministic 占比低`) {
		t.Errorf(`expected Weak-specific phrase "deterministic 占比低"; output:\n%s`, got)
	}
}

// TestRenderReviewStatus_UnverifiedDirective 钉住 Unverified 档（零 deterministic）：
// 必须输出 Unverified 档位 + "无 deterministic 证据" 指令。0 det + 3 claim → Unverified。
// 用 Unverified 专属短语，确保 Weak↔Unverified 分类回归能被抓。
func TestRenderReviewStatus_UnverifiedDirective(t *testing.T) {
	got := renderStatusWithEvidence(t, 0, 3)
	if !strings.Contains(got, `Unverified`) {
		t.Errorf(`expected Unverified band (zero deterministic); output:\n%s`, got)
	}
	if !strings.Contains(got, `无 deterministic 证据`) {
		t.Errorf(`expected Unverified directive "无 deterministic 证据"; output:\n%s`, got)
	}
}

// TestRenderReviewStatus_NoDataSilent 钉住 NoData（无 checklog）静默契约：不打印证据强度
// 行、不发任何指令——避免对尚无可验证证据的新任务误发噪声。
func TestRenderReviewStatus_NoDataSilent(t *testing.T) {
	got := renderStatusWithEvidence(t, 0, 0) // 无任何证据条目 → Total()==0
	if strings.Contains(got, `证据强度`) {
		t.Errorf(`NoData must not print 证据强度 line; output:\n%s`, got)
	}
	if strings.Contains(got, `核验`) {
		t.Errorf(`NoData must not emit any directive; output:\n%s`, got)
	}
}

// TestRenderReviewStatus_StrongSilent 确认 Strong 证据（ratio>=0.5）只报数字、不发
// 校准指令——避免噪声，也避免对 deterministic 充分的任务误发警告。4 det + 1 claim → 0.8。
func TestRenderReviewStatus_StrongSilent(t *testing.T) {
	got := renderStatusWithEvidence(t, 4, 1)
	if !strings.Contains(got, `Strong`) {
		t.Errorf(`expected Strong band (ratio 0.8); output:\n%s`, got)
	}
	if strings.Contains(got, `核验声称的验证是否真跑过`) {
		t.Errorf(`Strong must NOT emit the Weak directive; output:\n%s`, got)
	}
}
