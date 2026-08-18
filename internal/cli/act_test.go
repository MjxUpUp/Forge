package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// TestAppendConclusion_WritesAndDirectives pins the wiring boundary in task.go: appendConclusion
// must persist the conclusion to DataDir/act/conclusions.jsonl, and Directive must reflect evidence strength. This is the glue-layer coverage of TaskState→disk persistence,
// beyond the act-package unit tests (BuildConclusion/Append are already fully tested).
//
// TestAppendConclusion_WritesAndDirectives 钉住 task.go 的接线边界：appendConclusion
// 必须把结论落盘到 DataDir/act/conclusions.jsonl，且 Directive 反映证据强度。这是 act 包
// 单测（BuildConclusion/Append 已全测）之外、TaskState→落盘 的胶水层覆盖。
func TestAppendConclusion_WritesAndDirectives(t *testing.T) {
	root, p := forgedatatest.RealProject(t)

	// Bare state: no acceptance, no score, no evidence → NoData strength → no nudge → Directive empty. The conclusion should still be persisted.
	//
	// 裸 state：无验收、无评分、无证据 → NoData 强度 → 不 nudge → Directive 空。结论仍应落盘。
	state := &taskpipeline.TaskState{TaskRef: `feat/wire`, SessionID: `sess-wire`}
	d, ok := appendConclusion(root, state)
	if !ok {
		t.Fatal(`appendConclusion 应成功落盘`)
	}
	if d != `` {
		t.Errorf(`bare state Directive=%q want 空（NoData 不 nudge）`, d)
	}
	c, err := act.Latest(p)
	if err != nil {
		t.Fatalf(`Latest: %v`, err)
	}
	if c == nil {
		t.Fatal(`结论未落盘（appendConclusion 没写 DataDir/act/conclusions.jsonl）`)
	}
	if c.TaskRef != `feat/wire` {
		t.Errorf(`TaskRef=%q want feat/wire`, c.TaskRef)
	}
	if c.RetrospectiveNudge {
		t.Error(`NoData+nil score 不应 RetrospectiveNudge`)
	}
}

func TestAppendConclusion_AcceptanceCounted(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	// Acceptance 1/2 passed: the wiring must pass pass/total accurately to the conclusion (prevent silent zero from a missing field).
	//
	// 验收 1/2 通过：接线应把 pass/total 准确传给结论（防漏传字段静默归零）。
	state := &taskpipeline.TaskState{
		TaskRef: `feat/acc`,
		Acceptance: []taskpipeline.AcceptanceCriterion{
			{Run: `echo ok`, Expected: `ok`, Passed: true},
			{Run: `false`, Passed: false},
		},
	}
	appendConclusion(root, state)
	c, err := act.Latest(p)
	if err != nil || c == nil {
		t.Fatalf(`Latest: %v / nil`, err)
	}
	if c.AcceptancePass != 1 || c.AcceptanceTotal != 2 {
		t.Errorf(`Acceptance=%d/%d want 1/2（接线漏传 pass/total）`, c.AcceptancePass, c.AcceptanceTotal)
	}
}

// TestSedimentReminder pins the deterministic sediment line at task complete: an
// Act directive (Nudge path) passes through verbatim — it already carries the
// sediment action entry; an empty directive (clean completion, no Nudge) gets the
// soft reminder pointing at session-retrospective. Before this, the sediment
// evaluation of CLEAN tasks depended entirely on the user remembering to ask
// (the 2026-08-18 case-split/CI-sweep sessions: A-scored yet multi-lesson).
//
// TestSedimentReminder 钉住 task complete 的确定性沉淀提醒：Act directive
// （Nudge 路径）原样透传——它已带沉淀行动入口；空 directive（干净完成、无
// Nudge）得到指向 session-retrospective 的轻提醒。此前干净任务的沉淀评估
// 全靠用户记得问（2026-08-18 case-split/CI 清扫：A 分但多条教训）。
func TestSedimentReminder(t *testing.T) {
	directive := `→ session-retrospective: 任务评分 65 (D)。回顾根因并按载体决策树沉淀（防再犯）。`
	if got := sedimentReminder(directive); got != directive {
		t.Errorf(`有 directive 应原样透传：got=%q want=%q`, got, directive)
	}
	got := sedimentReminder(``)
	if !strings.Contains(got, `session-retrospective`) {
		t.Errorf(`空 directive 的提醒应指向 session-retrospective：got=%q`, got)
	}
	if !strings.Contains(got, `不沉淀`) {
		t.Errorf(`提醒应带噪声边界（不沉淀清单）：got=%q`, got)
	}
}

// TestActNudge pins the session-end hook contract of `forge act nudge`: when RetrospectiveNudge is set it
// outputs a single-line Directive (task-verify uses this to surface at session end); clean completions / no conclusions stay silent.
// This is the last mile of the Act feedback arm — nudge must be visible at the session-end checkpoint, not printed only once at task complete
// (which is easily drowned out by subsequent work).
//
// TestActNudge 钉住 forge act nudge 的会话结束 hook 契约：有 RetrospectiveNudge 时
// 输出一行 Directive（task-verify 据此 surface 到会话结束），干净完成/无结论时静默。
// 这是 Act 反馈臂最后一公里——nudge 必须在会话结束检查点可见，不能只在 task complete
// 打印一次（易被后续工作淹没）。
func TestActNudge(t *testing.T) {
	t.Run(`nudge_present_prints_directive`, func(t *testing.T) {
		tmpDir, p := forgedatatest.RealProject(t)
		if out, _, code := runForge(t, tmpDir, `init`, `--mode`, `medium`); code != 0 {
			t.Fatalf(`init: %s`, out)
		}
		// High score but Unverified (zero real-run evidence) = LLM-judge blind spot → RetrospectiveNudge → Directive non-empty.
		//
		// 高分但 Unverified（零实跑证据）= LLM-judge 盲区 → RetrospectiveNudge → Directive 非空。
		c := act.Conclusion{
			TaskRef: `feat/blind`, Grade: `A`, Strength: `Unverified`, Score: 95,
			RetrospectiveNudge: true, CompletedAt: time.Now(),
		}
		if err := act.Append(p, &c); err != nil {
			t.Fatalf(`seed conclusion: %v`, err)
		}
		out, _, code := runForge(t, tmpDir, `act`, `nudge`)
		if code != 0 {
			t.Fatalf(`forge act nudge exit %d: %s`, code, out)
		}
		// Directive anchors on strength (exposing the blind spot) + the session-retrospective action entry.
		//
		// Directive 锚定 strength（暴露盲区）+ session-retrospective 行动入口
		if !strings.Contains(out, `session-retrospective`) || !strings.Contains(out, `Unverified`) {
			t.Errorf(`nudge 输出缺 Directive 入口或 Strength; got: %s`, out)
		}
	})

	t.Run(`clean_strong_silent`, func(t *testing.T) {
		tmpDir, p := forgedatatest.RealProject(t)
		runForge(t, tmpDir, `init`, `--mode`, `medium`)
		// Strong + high score + no low dimensions = clean completion → Directive empty → silent (no noise).
		//
		// Strong + 高分 + 无低分维度 = 干净完成 → Directive 空 → 静默（不发噪声）。
		c := act.Conclusion{
			TaskRef: `feat/clean`, Grade: `A`, Strength: `Strong`, Score: 95,
			RetrospectiveNudge: false, CompletedAt: time.Now(),
		}
		if err := act.Append(p, &c); err != nil {
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
		tmpDir, _ := forgedatatest.RealProject(t)
		runForge(t, tmpDir, `init`, `--mode`, `medium`)
		// No completed conclusion yet: legitimate empty state, silent (not an error) — distinguished from `act show`'s "no conclusions yet" prompt.
		//
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

// TestPrintSkillReach pins the skill-reach profile injected into `act show`: when toollog has Skill invocations for the task,
// an extra Skills line is printed; with no invocations it stays silent (no empty Skills line). Uses LoadForTaskAll across archives,
// so historical task Skills remain visible after task-start archival (verified by has_skill_calls_archived below).
//
// TestPrintSkillReach 钉住 act show 注入的 skill 触达画像：toollog 有该 task 的 Skill
// 调用时多打印一行 Skills，无调用时静默（不留空 Skills 行）。用 LoadForTaskAll 跨归档，
// task start 归档后查历史 task 的 Skills 仍可见（下面 has_skill_calls_archived 验证）。
func TestPrintSkillReach(t *testing.T) {
	t.Run(`has_skill_calls_prints`, func(t *testing.T) {
		root, _ := forgedatatest.RealProject(t)
		for _, s := range []string{`foo`, `bar`} {
			if err := toolusage.Record(root, &toolusage.ToolCall{
				ToolName: `Skill`, ToolInput: `{"skill":"` + s + `"}`, TaskRef: `feat/reach`,
			}); err != nil {
				t.Fatalf(`seed Skill 调用: %v`, err)
			}
		}
		out := captureStdout(t, func() { printSkillReach(root, `feat/reach`) })
		if !strings.Contains(out, `Skills:`) || !strings.Contains(out, `foo`) || !strings.Contains(out, `bar`) {
			t.Errorf(`应有 Skills: foo, bar，got: %q`, out)
		}
	})

	t.Run(`no_calls_silent`, func(t *testing.T) {
		root, _ := forgedatatest.RealProject(t)
		out := captureStdout(t, func() { printSkillReach(root, `feat/none`) })
		if strings.TrimSpace(out) != `` {
			t.Errorf(`无 Skill 调用应静默（不留空 Skills 行），got: %q`, out)
		}
	})

	// P1 core verification: toolusage.Clear simulates the archival performed by `forge task start`. After archival, querying this task's Skills
	// must still be visible (LoadForTaskAll across archives), otherwise completed tasks would never show their Skills in `forge act show`.
	//
	// P1 核心验证：toolusage.Clear 模拟 forge task start 归档。归档后查该 task 的 Skills
	// 必须仍可见（LoadForTaskAll 跨归档），否则完成的任务再 forge act show 永远看不到 Skills。
	t.Run(`has_skill_calls_archived`, func(t *testing.T) {
		root, _ := forgedatatest.RealProject(t)
		if err := toolusage.Record(root, &toolusage.ToolCall{
			ToolName: `Skill`, ToolInput: `{"skill":"archived-skill"}`, TaskRef: `feat/old`,
		}); err != nil {
			t.Fatalf(`seed: %v`, err)
		}
		if err := toolusage.Clear(root); err != nil {
			t.Fatalf(`Clear（模拟 task start 归档）: %v`, err)
		}
		out := captureStdout(t, func() { printSkillReach(root, `feat/old`) })
		if !strings.Contains(out, `archived-skill`) {
			t.Errorf(`归档后仍应读到该 task 的 Skills（LoadForTaskAll 跨归档），got: %q`, out)
		}
	})
}
