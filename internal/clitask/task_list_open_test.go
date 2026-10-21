package clitask

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// task_list_open_test.go —— `forge task list --open` 端到端（按需拉取方案 REQ-5）：未完成任务的
// 条目字段、三档范围（当前项目 / --workspace / --all-projects）、flag 互斥，以及不加 --open 时
// 旧输出不变。

// listOpen runs `task list --open` with extra args in dir and decodes the JSON entries.
//
// listOpen 在 dir 下跑 `task list --open --json` 加额外参数，解码出条目。
func listOpen(t *testing.T, dir string, extra ...string) []openTaskEntry {
	t.Helper()
	args := append([]string{`task`, `list`, `--open`, `--json`}, extra...)
	stdout, stderr, code := runForgeStreams(t, dir, args...)
	if code != 0 {
		t.Fatalf("%v exit %d: %s\n%s", args, code, stdout, stderr)
	}
	var entries []openTaskEntry
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		t.Fatalf("%v 输出应为条目数组: %v\n%s", args, err, stdout)
	}
	return entries
}

// entryRefs returns the refs of entries in order.
//
// entryRefs 按顺序返回条目的 ref。
func entryRefs(entries []openTaskEntry) []string {
	var refs []string
	for _, e := range entries {
		refs = append(refs, e.Ref)
	}
	return refs
}

// TestTaskListOpen covers entry fields, completed-task exclusion, the three scopes and flag validation.
//
// TestTaskListOpen 覆盖条目字段、已完成任务排除、三档范围与 flag 校验。
func TestTaskListOpen(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	projA, projB := t.TempDir(), t.TempDir()
	initGitProject(t, projA)
	initGitProject(t, projB)

	for _, task := range [][2]string{{`feat/one`, `第一个`}, {`feat/two`, `第二个`}} {
		if out, _, code := runForge(t, projA, `task`, `start`, `--ref`, task[0], `--title`, task[1]); code != 0 {
			t.Fatalf(`task start %s: %s`, task[0], out)
		}
	}
	if out, _, code := runForge(t, projA, `task`, `next`, `--ref`, `feat/two`, `写测试`); code != 0 {
		t.Fatalf(`task next: %s`, out)
	}
	if out, _, code := runForge(t, projA, `task`, `block`, `--ref`, `feat/two`, `--content`, `等评审`); code != 0 {
		t.Fatalf(`task block: %s`, out)
	}
	if out, _, code := runForge(t, projA, `task`, `assign`, `--ref`, `feat/one`, `--to`, `claude-code`); code != 0 {
		t.Fatalf(`task assign: %s`, out)
	}
	done := time.Now()
	if err := taskpipeline.SaveTaskState(projA, &taskpipeline.TaskState{TaskRef: `feat/done`, Summary: `已完成`, StartedAt: done, CompletedAt: &done}); err != nil {
		t.Fatalf(`seed completed task: %v`, err)
	}
	if out, _, code := runForge(t, projB, `task`, `start`, `--ref`, `feat/in-b`, `--title`, `B 的任务`); code != 0 {
		t.Fatalf(`task start feat/in-b: %s`, out)
	}

	t.Run("current project fields", func(t *testing.T) {
		entries := listOpen(t, projA)
		if len(entries) != 2 {
			t.Fatalf("当前项目应有 2 个未完成任务（已完成的排除、B 项目不在范围），实得 %v", entryRefs(entries))
		}
		byRef := map[string]openTaskEntry{}
		for _, e := range entries {
			byRef[e.Ref] = e
			if e.Gate == `` || e.Project == `` || e.LastActivity.IsZero() {
				t.Errorf("条目缺必填字段: %+v", e)
			}
		}
		two := byRef[`feat/two`]
		if two.Summary != `第二个` || two.NextStep != `写测试` || two.OpenBlockers != 1 || two.Assignment != `` {
			t.Errorf("feat/two 字段不符: %+v", two)
		}
		if one := byRef[`feat/one`]; one.Assignment != `offered` {
			t.Errorf("feat/one 应显示派活状态 offered: %+v", one)
		}
	})

	t.Run("all projects", func(t *testing.T) {
		refs := entryRefs(listOpen(t, projA, `--all-projects`))
		if len(refs) != 3 || !strings.Contains(strings.Join(refs, ","), `feat/in-b`) {
			t.Fatalf("--all-projects 应含两个项目的 3 个未完成任务，实得 %v", refs)
		}
	})

	t.Run("workspace", func(t *testing.T) {
		// 未入任何 workspace：回落当前项目（stderr 提示），不报错。
		_, stderr, code := runForgeStreams(t, projA, `task`, `list`, `--open`, `--workspace`)
		if code != 0 || !strings.Contains(stderr, `不属于任何多仓 forge workspace`) {
			t.Fatalf("无 workspace 时应回落并提示，exit %d stderr %q", code, stderr)
		}
		if out, _, code := runForge(t, projA, `workspace`, `create`, `ws`); code != 0 {
			t.Fatalf(`workspace create: %s`, out)
		}
		for _, d := range []string{projA, projB} {
			if out, _, code := runForge(t, d, `workspace`, `add`, `ws`); code != 0 {
				t.Fatalf(`workspace add: %s`, out)
			}
		}
		refs := entryRefs(listOpen(t, projA, `--workspace`))
		if len(refs) != 3 || !strings.Contains(strings.Join(refs, ","), `feat/in-b`) {
			t.Fatalf("--workspace 应含两个成员仓的 3 个未完成任务，实得 %v", refs)
		}
		text, _, code := runForgeStreams(t, projA, `task`, `list`, `--open`, `--workspace`)
		if code != 0 || !strings.Contains(text, `[`) || !strings.Contains(text, `forge task resume --ref`) {
			t.Fatalf("跨项目文本视图应带项目前缀与接续指引，实得 exit %d:\n%s", code, text)
		}
	})

	t.Run("flag validation and legacy output", func(t *testing.T) {
		for _, args := range [][]string{
			{`task`, `list`, `--workspace`},
			{`task`, `list`, `--open`, `--workspace`, `--all-projects`},
			{`task`, `list`, `--open`, `--timeline`},
		} {
			if _, _, code := runForgeStreams(t, projA, args...); code == 0 {
				t.Errorf("%v 应报错退出", args)
			}
		}
		legacy, _, code := runForgeStreams(t, projA, `task`, `list`)
		if code != 0 || !strings.Contains(legacy, `Tasks:`) || !strings.Contains(legacy, `feat/done`) {
			t.Fatalf("不加 --open 时 task list 输出应不变（含已完成任务），实得 exit %d:\n%s", code, legacy)
		}
	})

	t.Run("no open tasks", func(t *testing.T) {
		if out, _, code := runForge(t, projB, `task`, `abort`, `--ref`, `feat/in-b`); code != 0 {
			t.Fatalf(`task abort feat/in-b: %s`, out)
		}
		stdout, _, code := runForgeStreams(t, projB, `task`, `list`, `--open`, `--json`)
		if code != 0 || strings.TrimSpace(stdout) != `[]` {
			t.Fatalf("零未完成任务应输出 [] 且 exit 0，实得 exit %d: %q", code, stdout)
		}
	})
}
