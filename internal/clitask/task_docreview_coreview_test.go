package clitask

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// task_docreview_coreview_test.go — output-readability-gates-v2.md P1-A/P1-B 的
// CLI 端到端测试：doc-review 双评字段成对落档（gate 不消费，机器只管可观测）、
// --critical 的 tag: 前缀打标、finding --tag/--stats 的进化判据聚合。

// TestDocReview_CoReviewPersisted 双评成对给出时落档 SecondReviewer/SecondScore。
func TestDocReview_CoReviewPersisted(t *testing.T) {
	dir := setupDelegateProject(t)
	seedTaskState(t, dir, `feat/coreview`, nil)

	out, stderr, code := runForge(t, dir, `task`, `doc-review`,
		`--ref`, `feat/coreview`, `--passed`, `pass`, `--score`, `88`,
		`--reviewer`, `judge-a`, `--co-reviewer`, `judge-b`, `--co-score`, `71`)
	if code != 0 {
		t.Fatalf(`doc-review exit %d: %s / %s`, code, out, stderr)
	}
	if !strings.Contains(out, `co-reviewer judge-b（71）`) {
		t.Errorf(`stdout 缺双评摘要, got: %s`, out)
	}
	state, err := taskpipeline.LoadTaskState(dir, `feat/coreview`)
	if err != nil {
		t.Fatal(err)
	}
	if state.DocReview == nil || state.DocReview.SecondReviewer != `judge-b` || state.DocReview.SecondScore != 71 {
		t.Fatalf(`双评字段未落档: %+v`, state.DocReview)
	}
}

// TestDocReview_CoReviewPairingRequired 半份双评（只有 id 或只有分数）拒绝——
// 残缺行会污染分歧率统计。
func TestDocReview_CoReviewPairingRequired(t *testing.T) {
	dir := setupDelegateProject(t)
	seedTaskState(t, dir, `feat/coreview-pair`, nil)

	if _, stderr, code := runForge(t, dir, `task`, `doc-review`,
		`--ref`, `feat/coreview-pair`, `--passed`, `pass`, `--score`, `75`,
		`--co-reviewer`, `judge-b`); code == 0 {
		t.Fatalf(`只有 co-reviewer 应被拒: %s`, stderr)
	}
	if _, stderr, code := runForge(t, dir, `task`, `doc-review`,
		`--ref`, `feat/coreview-pair`, `--passed`, `pass`, `--score`, `75`,
		`--co-score`, `70`); code == 0 {
		t.Fatalf(`只有 co-score 应被拒: %s`, stderr)
	}
}

// TestDocReview_CriticalTagPrefix --critical 支持「tag: 内容」前缀打标；未知
// 前缀/散文冒号原样作为内容。
func TestDocReview_CriticalTagPrefix(t *testing.T) {
	dir := setupDelegateProject(t)
	seedTaskState(t, dir, `feat/coreview-tag`, nil)

	if out, stderr, code := runForge(t, dir, `task`, `doc-review`,
		`--ref`, `feat/coreview-tag`, `--passed`, `fail`, `--score`, `60`,
		`--critical`, `style:评审冗长，缺 delete-list`,
		`--critical`, `注: 这里有个冒号但不是枚举前缀`); code != 0 {
		t.Fatalf(`doc-review exit %d: %s / %s`, code, out, stderr)
	}
	state, err := taskpipeline.LoadTaskState(dir, `feat/coreview-tag`)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Findings) != 2 {
		t.Fatalf(`应落 2 条 critical findings, got %d`, len(state.Findings))
	}
	if state.Findings[0].Tag != `style` || state.Findings[0].Content != `评审冗长，缺 delete-list` {
		t.Errorf(`tag 前缀未正确拆分: %+v`, state.Findings[0])
	}
	if state.Findings[1].Tag != `` || state.Findings[1].Content != `注: 这里有个冒号但不是枚举前缀` {
		t.Errorf(`未知前缀应原样保留: %+v`, state.Findings[1])
	}
}

// TestFinding_TagValidatedAndStats 跨任务 Tag×Round 聚合——进化判据「同类 ≥3 次」
// 的机器来源；非法 tag 拒绝。
func TestFinding_TagValidatedAndStats(t *testing.T) {
	dir := setupDelegateProject(t)
	seedTaskState(t, dir, `feat/stats-a`, func(s *taskpipeline.TaskState) {
		for i, tag := range []string{`padding`, `padding`, `padding`, `evidence`, ``} {
			s.Findings = append(s.Findings, taskpipeline.Finding{
				ID:      string(rune('a'+i)) + `-1`,
				Content: `finding`,
				Source:  taskpipeline.DocReviewSource,
				Status:  `open`,
				Tag:     tag,
				Round:   i%2 + 1,
			})
		}
	})
	seedTaskState(t, dir, `feat/stats-b`, func(s *taskpipeline.TaskState) {
		s.Findings = append(s.Findings, taskpipeline.Finding{
			ID: `b-1`, Content: `other`, Source: `other-tool`, Status: `open`, Tag: `padding`, Round: 1,
		})
	})

	out, stderr, code := runForge(t, dir, `task`, `finding`, `--stats`, `--source`, `doc-review`)
	if code != 0 {
		t.Fatalf(`stats exit %d: %s / %s`, code, out, stderr)
	}
	if !strings.Contains(out, `padding`) || !strings.Contains(out, `total 3`) {
		t.Errorf("stats 缺 padding total 3 行, got:\n%s", out)
	}
	// source 过滤后 stats-b（other-tool）无匹配发现，不计入任务数。
	if !strings.Contains(out, `1 任务`) {
		t.Errorf("stats 应报 1 个有匹配发现的任务, got:\n%s", out)
	}
	if strings.Contains(out, `other-tool`) {
		t.Errorf("source 过滤失效, got:\n%s", out)
	}

	// 非法 tag 拒绝。
	if _, _, code := runForge(t, dir, `task`, `finding`, `--ref`, `feat/stats-a`,
		`--content`, `x`, `--tag`, `not-a-tag`); code == 0 {
		t.Fatal(`非法 tag 应被拒`)
	}

	// --tag 合法值落档。
	if out, stderr, code := runForge(t, dir, `task`, `finding`, `--ref`, `feat/stats-a`,
		`--content`, `新发现`, `--tag`, `conclusion`); code != 0 {
		t.Fatalf(`finding add exit %d: %s / %s`, code, out, stderr)
	}
	state, err := taskpipeline.LoadTaskState(dir, `feat/stats-a`)
	if err != nil {
		t.Fatal(err)
	}
	last := state.Findings[len(state.Findings)-1]
	if last.Tag != `conclusion` {
		t.Errorf(`--tag 未落档: %+v`, last)
	}
}
