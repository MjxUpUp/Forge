package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// TestAppendConclusion_WritesAndDirectives 钉住 task.go 的接线边界：appendConclusion
// 必须把结论落盘到 .forge/act/conclusions.jsonl，且 Directive 反映证据强度。这是 act 包
// 单测（BuildConclusion/Append 已全测）之外、TaskState→落盘 的胶水层覆盖。
func TestAppendConclusion_WritesAndDirectives(t *testing.T) {
	root := t.TempDir()

	// 裸 state：无验收、无评分、无证据 → NoData 强度 → 不 nudge → Directive 空。结论仍应落盘。
	state := &taskpipeline.TaskState{TaskRef: `feat/wire`, SessionID: `sess-wire`}
	d := appendConclusion(root, state)
	if d != `` {
		t.Errorf(`bare state Directive=%q want 空（NoData 不 nudge）`, d)
	}
	c, err := act.Latest(root)
	if err != nil {
		t.Fatalf(`Latest: %v`, err)
	}
	if c == nil {
		t.Fatal(`结论未落盘（appendConclusion 没写 .forge/act/conclusions.jsonl）`)
	}
	if c.TaskRef != `feat/wire` {
		t.Errorf(`TaskRef=%q want feat/wire`, c.TaskRef)
	}
	if c.RetrospectiveNudge {
		t.Error(`NoData+nil score 不应 RetrospectiveNudge`)
	}
}

func TestAppendConclusion_AcceptanceCounted(t *testing.T) {
	root := t.TempDir()
	// 验收 1/2 通过：接线应把 pass/total 准确传给结论（防漏传字段静默归零）。
	state := &taskpipeline.TaskState{
		TaskRef: `feat/acc`,
		Acceptance: []taskpipeline.AcceptanceCriterion{
			{Run: `echo ok`, Expected: `ok`, Passed: true},
			{Run: `false`, Passed: false},
		},
	}
	appendConclusion(root, state)
	c, err := act.Latest(root)
	if err != nil || c == nil {
		t.Fatalf(`Latest: %v / nil`, err)
	}
	if c.AcceptancePass != 1 || c.AcceptanceTotal != 2 {
		t.Errorf(`Acceptance=%d/%d want 1/2（接线漏传 pass/total）`, c.AcceptancePass, c.AcceptanceTotal)
	}
}

// TestActNudge 钉住 forge act nudge 的会话结束 hook 契约：有 RetrospectiveNudge 时
// 输出一行 Directive（task-verify 据此 surface 到会话结束），干净完成/无结论时静默。
// 这是 Act 反馈臂最后一公里——nudge 必须在会话结束检查点可见，不能只在 task complete
// 打印一次（易被后续工作淹没）。
func TestActNudge(t *testing.T) {
	t.Run(`nudge_present_prints_directive`, func(t *testing.T) {
		tmpDir := t.TempDir()
		if out, _, code := runForge(t, tmpDir, `init`, `--mode`, `medium`); code != 0 {
			t.Fatalf(`init: %s`, out)
		}
		// 高分但 Unverified（零实跑证据）= LLM-judge 盲区 → RetrospectiveNudge → Directive 非空。
		c := act.Conclusion{
			TaskRef: `feat/blind`, Grade: `A`, Strength: `Unverified`, Score: 95,
			RetrospectiveNudge: true, CompletedAt: time.Now(),
		}
		if err := act.Append(tmpDir, &c); err != nil {
			t.Fatalf(`seed conclusion: %v`, err)
		}
		out, _, code := runForge(t, tmpDir, `act`, `nudge`)
		if code != 0 {
			t.Fatalf(`forge act nudge exit %d: %s`, code, out)
		}
		// Directive 锚定 strength（暴露盲区）+ session-retrospective 行动入口
		if !strings.Contains(out, `session-retrospective`) || !strings.Contains(out, `Unverified`) {
			t.Errorf(`nudge 输出缺 Directive 入口或 Strength; got: %s`, out)
		}
	})

	t.Run(`clean_strong_silent`, func(t *testing.T) {
		tmpDir := t.TempDir()
		runForge(t, tmpDir, `init`, `--mode`, `medium`)
		// Strong + 高分 + 无低分维度 = 干净完成 → Directive 空 → 静默（不发噪声）。
		c := act.Conclusion{
			TaskRef: `feat/clean`, Grade: `A`, Strength: `Strong`, Score: 95,
			RetrospectiveNudge: false, CompletedAt: time.Now(),
		}
		if err := act.Append(tmpDir, &c); err != nil {
			t.Fatalf(`seed: %v`, err)
		}
		out, _, code := runForge(t, tmpDir, `act`, `nudge`)
		if code != 0 {
			t.Fatalf(`exit %d: %s`, code, out)
		}
		if strings.TrimSpace(out) != `` {
			t.Errorf(`Strong+高分应静默（无盲区），got: %q`, out)
		}
	})

	t.Run(`no_conclusions_silent`, func(t *testing.T) {
		tmpDir := t.TempDir()
		runForge(t, tmpDir, `init`, `--mode`, `medium`)
		// 尚无完成结论：合法空状态，静默（非错误）——与 act show 的"尚无结论"提示区分。
		out, _, code := runForge(t, tmpDir, `act`, `nudge`)
		if code != 0 {
			t.Fatalf(`exit %d: %s`, code, out)
		}
		if strings.TrimSpace(out) != `` {
			t.Errorf(`无结论应静默，got: %q`, out)
		}
	})
}
