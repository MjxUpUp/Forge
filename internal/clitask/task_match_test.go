package clitask

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// task_match_test.go —— 按需拉取方案的模糊匹配（REQ-8，纯函数层）与 resume --match 端到端
// （REQ-4/INV-2，子进程层）。

// fuzzyFixture is a fixed task set for matcher tests; StartedAt orders ties deterministically.
//
// fuzzyFixture 是匹配器测试的固定任务集；StartedAt 让同分时的排序确定。
func fuzzyFixture() []projectTasks {
	base := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	done := base.Add(time.Hour)
	return []projectTasks{{Project: "p1", Root: "/r/p1", States: []*taskpipeline.TaskState{
		{TaskRef: "feat/resume-pull", Branch: "feat/resume-pull", Summary: "任务接续改为按需拉取", Goal: "降低会话启动噪音", StartedAt: base.Add(3 * time.Minute)},
		{TaskRef: "fix/hazard-exit-ux", Branch: "fix/hazard-exit-ux", Summary: "hazard 退出体验", StartedAt: base.Add(2 * time.Minute)},
		{TaskRef: "feat/alpha", Branch: "feat/alpha", Summary: "alpha 功能", StartedAt: base.Add(1 * time.Minute)},
		{TaskRef: "feat/resume-done", Branch: "feat/resume-done", Summary: "已完成的接续任务", StartedAt: base, CompletedAt: &done},
	}}}
}

// TestFuzzyMatch covers every tier of the design's fuzzy-match table plus the noise floor and completed-task exclusion.
//
// TestFuzzyMatch 覆盖方案「模糊匹配」表的各档（精确/全词/错拼/中文覆盖率/子序列）、噪音
// 下限（单字符、无关查询零候选）与已完成任务排除。逐例断言首个候选的 ref、来源字段与分数。
func TestFuzzyMatch(t *testing.T) {
	cases := []struct {
		name, query string
		wantRef     string // 首个候选；"" = 期望零候选
		wantField   string
		wantScore   int
	}{
		{name: "exact ref", query: "fix/hazard-exit-ux", wantRef: "fix/hazard-exit-ux", wantField: "ref", wantScore: 100},
		{name: "case-insensitive substring", query: "HAZARD-EXIT", wantRef: "fix/hazard-exit-ux", wantField: "ref", wantScore: 80},
		{name: "words out of order", query: "pull resume", wantRef: "feat/resume-pull", wantField: "ref", wantScore: 70},
		// 单词带标点：剥掉标点后整词在 ref 里，走全词档而不是掉到子序列档。
		{name: "punctuated single word", query: "resume.", wantRef: "feat/resume-pull", wantField: "ref", wantScore: 70},
		{name: "typo transposition", query: "reusme-pull", wantRef: "feat/resume-pull", wantField: "ref", wantScore: 60},
		{name: "chinese fragment", query: "接续拉取", wantRef: "feat/resume-pull", wantField: "summary", wantScore: 51},
		{name: "abbreviation subsequence", query: "rsmpl", wantRef: "feat/resume-pull", wantField: "ref", wantScore: 40},
		{name: "single char is noise", query: "a"},
		// 「务」「拉」都在 feat/resume-pull 的标题里，但不相邻——汉字段整体成词，散落的字凑不齐全词档。
		{name: "scattered han chars are noise", query: "务拉"},
		{name: "unrelated query", query: "zzzz"},
		{name: "completed task excluded", query: "resume-done"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hits := matchTasks(c.query, fuzzyFixture())
			if c.wantRef == "" {
				if len(hits) != 0 {
					t.Fatalf("%q 期望零候选，实得 %+v", c.query, hits)
				}
				return
			}
			if len(hits) == 0 {
				t.Fatalf("%q 期望命中 %s，实得零候选", c.query, c.wantRef)
			}
			got := hits[0]
			if got.Ref != c.wantRef || got.MatchedOn != c.wantField || got.Score != c.wantScore {
				t.Fatalf("%q 首个候选 = {%s %s %d}，期望 {%s %s %d}", c.query, got.Ref, got.MatchedOn, got.Score, c.wantRef, c.wantField, c.wantScore)
			}
		})
	}

	t.Run("ties ordered by last activity then ref", func(t *testing.T) {
		// "feat" 是 feat/resume-pull 与 feat/alpha 两个 ref 的共同前缀（同为 90 分）；
		// feat/resume-pull 的 StartedAt 更晚，排在前面。
		hits := matchTasks("feat", fuzzyFixture())
		if len(hits) != 2 || hits[0].Ref != "feat/resume-pull" || hits[1].Ref != "feat/alpha" {
			t.Fatalf("同分应按最后活动时间倒序，实得 %+v", hits)
		}
	})

	t.Run("query validation", func(t *testing.T) {
		if _, err := validateMatchQuery("   "); err == nil {
			t.Fatal("空白查询应报错")
		}
		if _, err := validateMatchQuery(strings.Repeat("长", matchQueryMaxRunes+1)); err == nil {
			t.Fatal("超长查询应报错")
		}
		if q, err := validateMatchQuery("  pull  "); err != nil || q != "pull" {
			t.Fatalf("应去首尾空白，实得 %q, %v", q, err)
		}
	})
}

// TestResumeMatch drives `forge task resume --match` end to end: candidates come back as JSON, nothing is written, --ref conflicts, and zero hits are an empty array with exit 0.
//
// TestResumeMatch 端到端驱动 `forge task resume --match`：JSON 返回候选、不写任何任务状态
// （INV-2：不锚定、不认领）、与 --ref 冲突报错、零命中为空数组且 exit 0、文本输出带确认指引。
func TestResumeMatch(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	initGitProject(t, dir)
	for _, task := range [][2]string{{`feat/resume-pull`, `任务接续改为按需拉取`}, {`fix/hazard-exit-ux`, `hazard 退出体验`}} {
		if out, _, code := runForge(t, dir, `task`, `start`, `--ref`, task[0], `--title`, task[1]); code != 0 {
			t.Fatalf(`task start %s: %s`, task[0], out)
		}
	}
	if out, _, code := runForge(t, dir, `task`, `assign`, `--ref`, `feat/resume-pull`, `--to`, `claude-code`); code != 0 {
		t.Fatalf(`task assign: %s`, out)
	}
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, `sid-match`)
	t.Setenv(`FORGE_AGENT`, `claude-code`)

	before := snapshotTaskFiles(t, dir)
	stdout, stderr, code := runForgeStreams(t, dir, `task`, `resume`, `--match`, `reusme pull`, `--json`)
	if code != 0 {
		t.Fatalf("resume --match exit %d: %s\n%s", code, stdout, stderr)
	}
	var hits []openTaskEntry
	if err := json.Unmarshal([]byte(stdout), &hits); err != nil {
		t.Fatalf("--json 输出应为条目数组: %v\n%s", err, stdout)
	}
	if len(hits) != 1 || hits[0].Ref != `feat/resume-pull` || hits[0].Assignment != `offered` || hits[0].Score == 0 {
		t.Fatalf("应只命中 feat/resume-pull（派活状态 offered、带得分），实得 %+v", hits)
	}
	after := snapshotTaskFiles(t, dir)
	for name, b := range before {
		if after[name] != b {
			t.Errorf("resume --match 改写了任务文件 %s（INV-2：不锚定、不认领）", name)
		}
	}

	if _, _, code := runForgeStreams(t, dir, `task`, `resume`, `--match`, `pull`, `--ref`, `feat/resume-pull`); code == 0 {
		t.Error("--match 与 --ref 同时给出应报错退出")
	}
	if _, _, code := runForgeStreams(t, dir, `task`, `resume`, `--match`, `   `); code == 0 {
		t.Error("空白 --match 应报错退出")
	}
	empty, _, code := runForgeStreams(t, dir, `task`, `resume`, `--match`, `zzzz`, `--json`)
	if code != 0 || strings.TrimSpace(empty) != `[]` {
		t.Fatalf("零命中应输出 [] 且 exit 0，实得 exit %d: %q", code, empty)
	}
	text, _, code := runForgeStreams(t, dir, `task`, `resume`, `--match`, `接续拉取`)
	if code != 0 || !strings.Contains(text, `feat/resume-pull`) || !strings.Contains(text, `forge task resume --ref`) {
		t.Fatalf("文本输出应列候选并给确认指引，实得 exit %d:\n%s", code, text)
	}
}
