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
// local work between sync rounds. The ID pool is SHARED across machines (no
// per-machine tag) so cross-machine same-ID conflicts actually occur — an earlier
// revision tagged IDs per machine and the conflict layer (resolveRecordConflictsSync)
// got ZERO property coverage as a result.
//
// randomTaskOps 对 s 施加 n 个种子化随机变更，模拟一台机器两轮同步之间的本地
// 工作。ID 池跨机共享（不带机位 tag），跨机同 ID 冲突才真会发生——早期版本给
// ID 带机位 tag，冲突层（resolveRecordConflictsSync）因此零 property 覆盖。
func randomTaskOps(r *rand.Rand, s *TaskState, n int, tag string) {
	base := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	gates := []string{`task-implement`, `task-verify`, `task-complete`}
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(r.Intn(10000)) * time.Minute)
		switch r.Intn(14) {
		case 0:
			s.Decisions = append(s.Decisions, Decision{ID: fmt.Sprintf(`d-%d`, r.Intn(6)), Content: tag, DecidedAt: ts})
		case 1:
			s.Findings = append(s.Findings, Finding{ID: fmt.Sprintf(`f-%d`, r.Intn(6)), Content: tag, Source: `t`, Status: `open`, RaisedAt: ts})
		case 2:
			s.Blockers = append(s.Blockers, Blocker{ID: fmt.Sprintf(`b-%d`, r.Intn(6)), Content: tag, Status: `open`, RaisedAt: ts})
		case 3:
			s.NextSteps = append(s.NextSteps, fmt.Sprintf(`step-%d`, r.Intn(6)))
		case 4:
			s.Artifacts = append(s.Artifacts, Artifact{Path: fmt.Sprintf(`p-%d`, r.Intn(6)), Kind: `file`, Note: tag})
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
			s.SessionLinks = append(s.SessionLinks, SessionLink{SessionID: fmt.Sprintf(`s-%d`, r.Intn(4)), JoinedAt: ts, Imported: r.Intn(2) == 0})
		case 9: // review anchors set between task-verify and task-complete
			s.ReviewPassed = r.Intn(2) == 0
			s.ReviewedHeadCommit = fmt.Sprintf(`h%d`, r.Intn(3))
			s.ReviewedChangeHash = fmt.Sprintf(`c%d`, r.Intn(3))
		case 10: // identity scalars (pathological dual-create edits)
			s.Summary = fmt.Sprintf(`summary-%d`, r.Intn(3))
			s.Goal = fmt.Sprintf(`goal-%d`, r.Intn(3))
		case 11: // assignment lifecycle
			s.Assignment = &Assignment{Agent: fmt.Sprintf(`agent-%d`, r.Intn(2)), Status: []string{`offered`, `claimed`}[r.Intn(2)]}
		case 12: // external origin / misc scalars
			s.ExternalOrigin = ExternalOrigin{Tracker: `github`, Identifier: fmt.Sprintf(`org/repo#%d`, r.Intn(3))}
			s.ResumeStale = r.Intn(2) == 0
		case 13: // same-ID status UPDATE (finding open → fixed, RaisedAt kept)
			id := fmt.Sprintf(`f-%d`, r.Intn(6))
			for j := range s.Findings {
				if s.Findings[j].ID == id {
					s.Findings[j].Status = `fixed`
				}
			}
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

		// stepwise convergence (the real sync-loop shape): merging the CONVERGED
		// product into the other side must equal the converged product —
		// merge(b, merge(a,b)) == merge(a,b). cpTask round-trips through JSON so the
		// converged product arrives in wire shape (nil/empty slice normalization
		// included) — this is the case a representation-sensitive tiebreak breaks.
		//
		// 轮次收敛（真实同步循环形态）：把收敛产物合进另一侧必须等于收敛产物——
		// merge(b, merge(a,b)) == merge(a,b)。cpTask 经 JSON 往返，收敛产物以
		// 线上形态到达（含 nil/空切片归一）——表示敏感的决胜键正是在此破功。
		step := cpTask(t, b)
		MergeTaskStateSync(step, cpTask(t, ab))
		if taskJSON(t, step) != taskJSON(t, ab) {
			t.Fatalf("seed %d: stepwise merge diverged:\nab=%s\nstep=%s", seed, taskJSON(t, ab), taskJSON(t, step))
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

// TestMergeTaskStateSync_GateHistoryContentUnion: gate history unions by FULL
// CONTENT — a Failed entry and its later Passed retry BOTH survive (rework
// provenance for ReworkRounds/scoring/feed), order is chronological, and the result
// is byte-identical in both merge directions.
//
// TestMergeTaskStateSync_GateHistoryContentUnion：门禁 history 按全内容并集——
// Failed 条目与其后的 Passed 重跑都存活（ReworkRounds/评分/feed 的返工
// provenance），按时间排序，两个合并方向字节一致。
func TestMergeTaskStateSync_GateHistoryContentUnion(t *testing.T) {
	fail := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	pass := time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC)
	mk := func(entries ...TaskGateResult) *TaskState {
		return &TaskState{TaskRef: `feat/g`, History: entries}
	}
	failed := TaskGateResult{Gate: `task-verify`, Passed: false, CompletedAt: fail, HeadCommit: `h1`}
	passed := TaskGateResult{Gate: `task-verify`, Passed: true, CompletedAt: pass, HeadCommit: `h2`}

	ab := mk(failed)
	MergeTaskStateSync(ab, mk(passed))
	ba := mk(passed)
	MergeTaskStateSync(ba, mk(failed))

	if len(ab.History) != 2 {
		t.Fatalf("retry provenance must survive: got %+v", ab.History)
	}
	if !ab.History[0].Passed && ab.History[1].Passed {
		// chronological: Failed@10:00 then Passed@11:00
	} else {
		t.Fatalf("history not chronological: %+v", ab.History)
	}
	if taskJSON(t, ab) != taskJSON(t, ba) {
		t.Fatalf("gate union not direction-independent:\nab=%s\nba=%s", taskJSON(t, ab), taskJSON(t, ba))
	}
	// gatePassed semantics: the Passed entry heals the gate (NextGate advances).
	if ab.NextGate() == `task-verify` {
		t.Fatalf("peer Passed did not heal task-verify: %+v", ab.History)
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
