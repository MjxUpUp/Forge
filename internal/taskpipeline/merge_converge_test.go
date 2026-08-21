package taskpipeline

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// merge_converge_test.go — the convergence property anchor for MergeTaskStateSync
// (docs/design/sync-convergence.md §2): merge must be COMMUTATIVE (merge(A,B) and
// merge(B,A) yield byte-identical results) and IDEMPOTENT (re-merge is a no-op).
// Without commutativity a two-machine sync loop flip-flops forever on ORDER alone.
//
// merge_converge_test.go —— MergeTaskStateSync 的收敛性质锚
// （docs/design/sync-convergence.md §2）：合并必须满足交换律（merge(A,B) 与
// merge(B,A) 字节一致）与幂等（重复合并是 no-op）。没有交换律，双机同步循环
// 会仅凭顺序永远来回翻转。

// cpTask deep-copies a TaskState via JSON round-trip (strips monotonic clock and
// pointer sharing, matching what a sync boundary actually transports).
//
// cpTask 经 JSON 往返深拷贝 TaskState（剥掉单调时钟与指针共享，与同步边界真实
// 传输的内容一致）。
func cpTask(t *testing.T, s *TaskState) *TaskState {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out TaskState
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &out
}

func taskJSON(t *testing.T, s *TaskState) string {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

// randomTaskOps applies n seeded random mutations to s, simulating one machine's
// local work between sync rounds.
//
// randomTaskOps 对 s 施加 n 个种子化随机变更，模拟一台机器两轮同步之间的本地工作。
func randomTaskOps(r *rand.Rand, s *TaskState, n int, tag string) {
	base := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	gates := []string{`task-implement`, `task-verify`, `task-complete`}
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(r.Intn(10000)) * time.Minute)
		switch r.Intn(9) {
		case 0:
			s.Decisions = append(s.Decisions, Decision{ID: fmt.Sprintf(`d-%s-%d`, tag, r.Intn(6)), Content: `c`, DecidedAt: ts})
		case 1:
			s.Findings = append(s.Findings, Finding{ID: fmt.Sprintf(`f-%s-%d`, tag, r.Intn(6)), Content: `c`, Source: `t`, Status: `open`, RaisedAt: ts})
		case 2:
			s.Blockers = append(s.Blockers, Blocker{ID: fmt.Sprintf(`b-%s-%d`, tag, r.Intn(6)), Content: `c`, Status: `open`, RaisedAt: ts})
		case 3:
			s.NextSteps = append(s.NextSteps, fmt.Sprintf(`step-%s-%d`, tag, r.Intn(6)))
		case 4:
			s.Artifacts = append(s.Artifacts, Artifact{Path: fmt.Sprintf(`p-%s-%d`, tag, r.Intn(6)), Kind: `file`})
		case 5:
			g := gates[r.Intn(len(gates))]
			s.History = append(s.History, TaskGateResult{Gate: g, Passed: r.Intn(4) != 0, CompletedAt: ts, HeadCommit: fmt.Sprintf(`h%d`, r.Intn(3))})
		case 6:
			s.ReviewRounds = append(s.ReviewRounds, ReviewRound{HeadCommit: fmt.Sprintf(`h%d`, r.Intn(3)), ReviewedAt: ts})
		case 7:
			done := ts
			s.CompletedAt = &done
			s.Score = &scoringtypes.ScoreResult{Grade: `A`, Overall: float64(80 + r.Intn(20))}
		case 8:
			s.SessionLinks = append(s.SessionLinks, SessionLink{SessionID: fmt.Sprintf(`s-%s-%d`, tag, r.Intn(4)), JoinedAt: ts, Imported: true})
		}
	}
}

// TestMergeTaskStateSync_ConvergenceProperty runs seeded random dual-machine
// interleavings and asserts full commutativity + idempotency of the sync merge.
//
// TestMergeTaskStateSync_ConvergenceProperty 跑种子化随机双机交错序列，断言同步
// 合并的完全交换律 + 幂等。
func TestMergeTaskStateSync_ConvergenceProperty(t *testing.T) {
	for seed := int64(0); seed < 40; seed++ {
		r := rand.New(rand.NewSource(seed))
		a := &TaskState{TaskRef: `feat/x`, Branch: `feat/x`, StartedAt: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)}
		b := cpTask(t, a)
		randomTaskOps(r, a, 8, `a`)
		randomTaskOps(r, b, 8, `b`)

		// commutativity: merge(a,b) == merge(b,a), byte-identical.
		ab := cpTask(t, a)
		MergeTaskStateSync(ab, cpTask(t, b))
		ba := cpTask(t, b)
		MergeTaskStateSync(ba, cpTask(t, a))
		if taskJSON(t, ab) != taskJSON(t, ba) {
			t.Fatalf("seed %d: merge not commutative:\nab=%s\nba=%s", seed, taskJSON(t, ab), taskJSON(t, ba))
		}

		// idempotency: merging b into the converged result again is a no-op.
		before := taskJSON(t, ab)
		MergeTaskStateSync(ab, cpTask(t, b))
		MergeTaskStateSync(ab, cpTask(t, ba))
		if taskJSON(t, ab) != before {
			t.Fatalf("seed %d: re-merge not idempotent:\nbefore=%s\nafter=%s", seed, before, taskJSON(t, ab))
		}
	}
}

// TestMergeTaskStateSync_CanonicalOrdering pins the direction-independent ordering:
// record sets are canonically sorted, so arrival order (which side merged first)
// cannot leak into the bytes.
//
// TestMergeTaskStateSync_CanonicalOrdering 钉死方向无关排序：记录集合按规范序
// 排序，到达顺序（哪侧先合并）无法渗进字节。
func TestMergeTaskStateSync_CanonicalOrdering(t *testing.T) {
	ts := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	mk := func(ids ...string) *TaskState {
		s := &TaskState{TaskRef: `feat/ord`}
		for _, id := range ids {
			s.Decisions = append(s.Decisions, Decision{ID: id, Content: `c`, DecidedAt: ts})
		}
		return s
	}
	ab := mk(`d-3`, `d-1`)
	MergeTaskStateSync(ab, mk(`d-2`, `d-1`))
	ba := mk(`d-2`, `d-1`)
	MergeTaskStateSync(ba, mk(`d-3`, `d-1`))
	got := []string{ab.Decisions[0].ID, ab.Decisions[1].ID, ab.Decisions[2].ID}
	want := []string{`d-1`, `d-2`, `d-3`}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("canonical order = %v, want %v", got, want)
		}
	}
	if taskJSON(t, ab) != taskJSON(t, ba) {
		t.Fatalf("direction leaked into bytes:\nab=%s\nba=%s", taskJSON(t, ab), taskJSON(t, ba))
	}
}

// TestMergeTaskStateSync_DeterministicGateConflict: same gate + same result on both
// sides (differing timestamps) must resolve to ONE winner regardless of direction —
// earlier CompletedAt (first attainment is the fact; later ones are re-runs).
//
// TestMergeTaskStateSync_DeterministicGateConflict：两侧同门禁同结论（时间戳不同）
// 必须不分方向收敛到同一胜者——更早的 CompletedAt（首次达成是事实，后者是重跑）。
func TestMergeTaskStateSync_DeterministicGateConflict(t *testing.T) {
	early := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	late := time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC)
	mk := func(at time.Time, head string) *TaskState {
		return &TaskState{TaskRef: `feat/g`, History: []TaskGateResult{{Gate: `task-verify`, Passed: true, CompletedAt: at, HeadCommit: head}}}
	}
	ab := mk(early, `h-early`)
	MergeTaskStateSync(ab, mk(late, `h-late`))
	ba := mk(late, `h-late`)
	MergeTaskStateSync(ba, mk(early, `h-early`))
	if len(ab.History) != 1 || !ab.History[0].CompletedAt.Equal(early) {
		t.Fatalf("gate conflict winner = %+v, want earlier CompletedAt", ab.History)
	}
	if taskJSON(t, ab) != taskJSON(t, ba) {
		t.Fatalf("gate conflict not direction-independent:\nab=%s\nba=%s", taskJSON(t, ab), taskJSON(t, ba))
	}
}

// TestMergeTaskStateSync_BothCompleteFirstFinisherWins: both sides completed with
// different snapshots → the EARLIER completion block wins, direction-independently
// (the first finish is the real one; later completions are re-runs of finished work).
//
// TestMergeTaskStateSync_BothCompleteFirstFinisherWins：双完成且快照不同 → 更早的
// 完成块胜，方向无关（先完成者是真实完成；后完成者是对已完成工作的重跑）。
func TestMergeTaskStateSync_BothCompleteFirstFinisherWins(t *testing.T) {
	early := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	late := time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC)
	mk := func(at time.Time, grade string) *TaskState {
		return &TaskState{TaskRef: `feat/c`, CompletedAt: &at, Score: &scoringtypes.ScoreResult{Grade: grade}}
	}
	ab := mk(early, `A`)
	MergeTaskStateSync(ab, mk(late, `B`))
	ba := mk(late, `B`)
	MergeTaskStateSync(ba, mk(early, `A`))
	if ab.Score.Grade != `A` || !ab.CompletedAt.Equal(early) {
		t.Fatalf("both-complete winner = %+v, want earlier completion (grade A)", ab.Score)
	}
	if taskJSON(t, ab) != taskJSON(t, ba) {
		t.Fatalf("both-complete not direction-independent:\nab=%s\nba=%s", taskJSON(t, ab), taskJSON(t, ba))
	}
}
