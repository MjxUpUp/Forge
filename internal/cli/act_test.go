package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/MjxUpUp/Forge/internal/util"
)

// TestAppendConclusion_WritesAndDirectives pins the wiring boundary in task.go: appendConclusion must persist the conclusion to DataDir/act/conclusions.jsonl, and Directive must reflect evidence strength.
//
// TestAppendConclusion_WritesAndDirectives 钉住 task.go 的接线边界：appendConclusion
// 必须把结论落盘到 DataDir/act/conclusions.jsonl，且 Directive 反映证据强度。这是 act 包
// 单测（BuildConclusion/Append 已全测）之外、TaskState→落盘 的胶水层覆盖。
func TestAppendConclusion_WritesAndDirectives(t *testing.T) {
	root, p := forgedatatest.RealProject(t)

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

// TestSedimentReminder pins the deterministic sediment line at task complete.
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

// TestActNudge pins the session-end hook contract of `forge act nudge`.
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
		// Directive 锚定 strength（暴露盲区）+ session-retrospective 行动入口
		if !strings.Contains(out, `session-retrospective`) || !strings.Contains(out, `Unverified`) {
			t.Errorf(`nudge 输出缺 Directive 入口或 Strength; got: %s`, out)
		}
	})

	t.Run(`clean_strong_silent`, func(t *testing.T) {
		tmpDir, p := forgedatatest.RealProject(t)
		runForge(t, tmpDir, `init`, `--mode`, `medium`)
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

// TestPrintSkillReach pins the skill-reach profile injected into `act show`: when toollog has Skill invocations for the task, an extra Skills line is printed; with no invocations it stays silent (no empty Skills line).
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

	// P1 核心验证：toollog 归档改为直接 rename 模拟（旧的 toolusage.Clear 助手已作
	// 死代码删除——task start 不再截断 toollog）。归档后查该 task 的 Skills
	// 必须仍可见（LoadForTaskAll 跨归档），否则完成的任务再 forge act show 永远看不到 Skills。
	t.Run(`has_skill_calls_archived`, func(t *testing.T) {
		root, _ := forgedatatest.RealProject(t)
		if err := toolusage.Record(root, &toolusage.ToolCall{
			ToolName: `Skill`, ToolInput: `{"skill":"archived-skill"}`, TaskRef: `feat/old`,
		}); err != nil {
			t.Fatalf(`seed: %v`, err)
		}
		if err := archiveToollogForTest(t, root); err != nil {
			t.Fatalf(`归档模拟（rename active toollog）: %v`, err)
		}
		out := captureStdout(t, func() { printSkillReach(root, `feat/old`) })
		if !strings.Contains(out, `archived-skill`) {
			t.Errorf(`归档后仍应读到该 task 的 Skills（LoadForTaskAll 跨归档），got: %q`, out)
		}
	})
}

// archiveToollogForTest 把 active toollog 重命名为带时间戳的归档——即旧
// toolusage.Clear 为 `forge task start` 归档产出的磁盘形态。读取方必须跨归档
// （LoadForTaskAll / LoadAllAll）。
func archiveToollogForTest(t *testing.T, root string) error {
	t.Helper()
	dir := forgedata.DataDirFor(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.Rename(
		filepath.Join(dir, `toollog.jsonl`),
		util.ArchivedName(dir, `toollog`, time.Now()),
	)
}
