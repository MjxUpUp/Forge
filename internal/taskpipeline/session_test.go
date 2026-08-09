package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
)

func TestEnsureSession_CreatesNewSession(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	session, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if session == nil {
		t.Fatal("session is nil")
	}
	if session.SessionID == "" {
		t.Error("SessionID is empty")
	}
	if session.StartedAt.IsZero() {
		t.Error("StartedAt is zero")
	}
	if session.AgentType != "claude-code" {
		t.Errorf("AgentType = %q, want claude-code", session.AgentType)
	}

	// Verify session file was created (user-level DataDir)
	if _, err := os.Stat(filepath.Join(forgedata.DataDirFor(dir), "session.json")); os.IsNotExist(err) {
		t.Error("session.json was not created")
	}

	// Verify sessions log was created
	if _, err := os.Stat(filepath.Join(forgedata.DataDirFor(dir), "sessions.jsonl")); os.IsNotExist(err) {
		t.Error("sessions.jsonl was not created")
	}
}

// TestEnsureSession_WritesStartedEpoch guards the integer started_epoch field.
// The session-health bash hook reads this field directly (via extract_num) to
// avoid parsing the RFC3339Nano started_at string with the cross-platform-fragile
// date command (GNU vs BSD). Both construction paths — the legacy global
// session.json and the session-scoped per-id file — must populate it, and it must
// be the exact Unix-seconds form of StartedAt so the Go→bash contract is an
// integer, not a format bash has to reverse.
func TestEnsureSession_WritesStartedEpoch(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	// Legacy path (empty sessionID → global session.json).
	legacy, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("EnsureSession legacy: %v", err)
	}
	if legacy.StartedEpoch != legacy.StartedAt.Unix() {
		t.Errorf("legacy StartedEpoch=%d, want StartedAt.Unix()=%d (hook reads this int directly)",
			legacy.StartedEpoch, legacy.StartedAt.Unix())
	}
	if legacy.StartedEpoch == 0 {
		t.Error("legacy StartedEpoch is zero — hook would fall back to the fragile date parse")
	}

	// Scoped path (non-empty sessionID → per-id file under DataDir/sessions/).
	const scopedID = "scoped-abc-123"
	scoped, err := EnsureSession(dir, scopedID)
	if err != nil {
		t.Fatalf("EnsureSession scoped: %v", err)
	}
	if scoped.StartedEpoch != scoped.StartedAt.Unix() {
		t.Errorf("scoped StartedEpoch=%d, want StartedAt.Unix()=%d",
			scoped.StartedEpoch, scoped.StartedAt.Unix())
	}

	// The on-disk global session.json (user-level DataDir) must marshal the field
	// by its json tag so the bash hook's extract_num started_epoch finds it.
	raw, err := os.ReadFile(filepath.Join(forgedata.DataDirFor(dir), "session.json"))
	if err != nil {
		t.Fatalf("read session.json: %v", err)
	}
	if !strings.Contains(string(raw), `"started_epoch":`) {
		t.Errorf("session.json missing started_epoch field; bash hook would not find it.\ngot: %s", raw)
	}
}

func TestEnsureSession_ReusesExistingWithinMaxIdle(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	first, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("first EnsureSession: %v", err)
	}

	// Second call within maxIdle should return the same session
	second, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("second EnsureSession: %v", err)
	}

	if second.SessionID != first.SessionID {
		t.Errorf("SessionID changed: %q -> %q (should stay the same)", first.SessionID, second.SessionID)
	}
}

func TestEnsureSession_RotatesAfterMaxIdle(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	first, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("first EnsureSession: %v", err)
	}

	// Manually set the session start time far in the past to simulate expiration
	oldSession := &SessionRecord{
		SessionID: first.SessionID,
		StartedAt: time.Now().Add(-3 * time.Hour),
		AgentType: first.AgentType,
	}
	if err := saveSession(dir, oldSession); err != nil {
		t.Fatalf("saveSession: %v", err)
	}

	// Next call should create a new session
	second, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("second EnsureSession: %v", err)
	}

	if second.SessionID == first.SessionID {
		t.Errorf("SessionID should have rotated, but got same: %q", second.SessionID)
	}
}

func TestLoadSessions_IncludesCurrent(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	// Create a session
	session, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

	sessions, err := LoadSessions(dir)
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("LoadSessions count = %d, want 1", len(sessions))
	}
	if sessions[0].SessionID != session.SessionID {
		t.Errorf("SessionID = %q, want %q", sessions[0].SessionID, session.SessionID)
	}
}

func TestLoadSessions_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	sessions, err := LoadSessions(dir)
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	if sessions != nil {
		t.Fatalf("LoadSessions should return nil for empty dir, got %d entries", len(sessions))
	}
}

func TestSessionAgentTypeDetection(t *testing.T) {
	dir := t.TempDir()
	if got := detectAgentType(dir); got != "" {
		t.Errorf("detectAgentType on empty dir = %q, want empty", got)
	}

	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)
	if got := detectAgentType(dir); got != "claude-code" {
		t.Errorf("detectAgentType on .claude dir = %q, want claude-code", got)
	}
}

// TestDetectAgentType_DeterministicPriority pins the fixed-priority fix: with several
// agent dirs present (.claude + .cursor), the result must be deterministic (first hit
// in the ordered list wins). The old map-range version returned a random pick per
// process start, mis-attributing OriginTool.
//
// TestDetectAgentType_DeterministicPriority 钉死固定优先级修复：同时存在多个 agent
// 目录（.claude + .cursor）时结果必须确定（有序列表首个命中）。旧 map 遍历版本
// 每次进程启动随机取，致 OriginTool 归属错误。
func TestDetectAgentType_DeterministicPriority(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)
	os.MkdirAll(filepath.Join(dir, ".cursor"), 0755)

	// Repeated calls: a map-iteration regression would show up as a flip.
	//
	// 重复调用：map 遍历回归会表现为结果翻转。
	for i := 0; i < 20; i++ {
		if got := detectAgentType(dir); got != "claude-code" {
			t.Fatalf("detectAgentType with .claude+.cursor = %q, want claude-code (deterministic first-hit wins)", got)
		}
	}
}

func TestNewTaskState_HasSessionID(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)
	os.MkdirAll(filepath.Join(dir, ".forge", "tasks"), 0755)

	session, err := EnsureSession(dir, "")
	if err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}

	ctx := &taskcontext.Context{
		Source:     "explicit",
		TaskRef:    "PROJ-456",
		Branch:     "feature/PROJ-456",
		DetectedAt: time.Now(),
	}
	state := NewTaskState(ctx)
	state.SessionID = session.SessionID

	// Save and reload to verify persistence
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}

	loaded, err := LoadTaskState(dir, "PROJ-456")
	if err != nil {
		t.Fatalf("LoadTaskState: %v", err)
	}
	if loaded.SessionID != session.SessionID {
		t.Errorf("TaskState.SessionID = %q, want %q", loaded.SessionID, session.SessionID)
	}
}

// TestDetectAgentType_RecognizesAllNineAgents pins the marker-table delegation at the
// taskpipeline layer: every supported agent resolves via detectAgentType. Before the
// agentsignals refactor only claude/cursor/copilot/windsurf did; reasonix/kimi/codex/
// opencode/cline were invisible to session attribution — the "53% agent_type missing"
// root cause. This validates the full path runHook→EnsureSession→detectAgentType→
// agentsignals.ProjectAgentMarker end to end.
//
// TestDetectAgentType_RecognizesAllNineAgents 在 taskpipeline 层钉死标记表委托：每个
// 支持的 agent 都能经 detectAgentType 解析。agentsignals 重构前只有 claude/cursor/
// copilot/windsurf 行；reasonix/kimi/codex/opencode/cline 对会话归因不可见——"53%
// agent_type 缺失"根因。本测试端到端验证 runHook→EnsureSession→detectAgentType→
// agentsignals.ProjectAgentMarker 全链路。
func TestDetectAgentType_RecognizesAllNineAgents(t *testing.T) {
	cases := []struct {
		name  string
		setup func(dir string)
		want  string
	}{
		{`claude`, func(d string) { os.MkdirAll(filepath.Join(d, `.claude`), 0755) }, `claude-code`},
		{`cursor`, func(d string) { os.MkdirAll(filepath.Join(d, `.cursor`), 0755) }, `cursor`},
		{`copilot`, func(d string) { os.MkdirAll(filepath.Join(d, `.github`, `instructions`), 0755) }, `copilot`},
		{`windsurf`, func(d string) { os.WriteFile(filepath.Join(d, `.windsurfrules`), []byte(`x`), 0644) }, `windsurf`},
		{`codex`, func(d string) { os.MkdirAll(filepath.Join(d, `.codex`), 0755) }, `codex`},
		{`opencode`, func(d string) { os.MkdirAll(filepath.Join(d, `.opencode`), 0755) }, `opencode`},
		{`cline`, func(d string) { os.MkdirAll(filepath.Join(d, `.cline`), 0755) }, `cline`},
		{`clinerules`, func(d string) { os.MkdirAll(filepath.Join(d, `.clinerules`), 0755) }, `cline`},
		{`kimi`, func(d string) { os.MkdirAll(filepath.Join(d, `.kimi-code`), 0755) }, `kimi`},
		{`reasonix`, func(d string) { os.MkdirAll(filepath.Join(d, `.reasonix`), 0755) }, `reasonix`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(dir)
			if got := detectAgentType(dir); got != tc.want {
				t.Errorf("detectAgentType(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestStampSessionAgent_FillsEmptyScoped: a marker-absent scoped session (empty
// agent_type — the kimi/reasonix/codex-without-project-marker case) gets stamped by the
// hook's authoritative agent on the first Pre/PostToolUse after the record exists.
//
// TestStampSessionAgent_FillsEmptyScoped：无标记的 scoped session（空 agent_type——
// 无项目标记的 kimi/reasonix/codex 场景）在记录存在后的首个 Pre/PostToolUse 被 hook
// 的权威 agent 盖上。
func TestStampSessionAgent_FillsEmptyScoped(t *testing.T) {
	dir := t.TempDir() // no marker → detectAgentType empty
	const sid = `scoped-stamp-1`
	if _, err := EnsureSession(dir, sid); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	StampSessionAgent(dir, sid, `reasonix`)
	reloaded, err := ensureScopedSession(dir, sid)
	if err != nil {
		t.Fatalf("ensureScopedSession reload: %v", err)
	}
	if reloaded.AgentType != `reasonix` {
		t.Errorf("scoped AgentType after stamp = %q, want reasonix", reloaded.AgentType)
	}
}

// TestStampSessionAgent_FillsEmptyLegacy: the same fill-empty contract on the legacy
// global session.json path (empty sessionID).
//
// TestStampSessionAgent_FillsEmptyLegacy：在 legacy 全局 session.json 路径（空
// sessionID）上同样的"只填空"契约。
func TestStampSessionAgent_FillsEmptyLegacy(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureSession(dir, ""); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	StampSessionAgent(dir, "", `kimi`)
	loaded, err := loadSession(dir)
	if err != nil || loaded == nil {
		t.Fatalf("loadSession: %v (%v)", err, loaded)
	}
	if loaded.AgentType != `kimi` {
		t.Errorf("legacy AgentType after stamp = %q, want kimi", loaded.AgentType)
	}
}

// TestStampSessionAgent_DoesNotOverwrite: a session already attributed by a project
// marker must NOT be clobbered by a later hook stamp — that would regress a CORRECT
// marker-based attribution. This is the contract's load-bearing guard.
//
// TestStampSessionAgent_DoesNotOverwrite：已被项目标记正确归因的 session 绝不能被后续
// hook 盖戳覆盖——否则会回退"正确的标记归因"。这是契约的核心护栏。
func TestStampSessionAgent_DoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, `.claude`), 0755) // marker → claude-code
	const sid = `scoped-stamp-2`
	if _, err := EnsureSession(dir, sid); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	StampSessionAgent(dir, sid, `reasonix`) // must NOT overwrite claude-code
	reloaded, err := ensureScopedSession(dir, sid)
	if err != nil {
		t.Fatalf("ensureScopedSession reload: %v", err)
	}
	if reloaded.AgentType != `claude-code` {
		t.Errorf("AgentType = %q, want claude-code (stamp must not overwrite marker attribution)", reloaded.AgentType)
	}
}

// TestStampSessionAgent_DoesNotCreateIfAbsent: stamping before the session record exists
// must be a no-op — it must never synthesize a session file (EnsureSession owns creation;
// a stamp before existence is meaningless).
//
// TestStampSessionAgent_DoesNotCreateIfAbsent：session 记录尚不存在时盖戳必须是 no-op——
// 绝不能凭空造出 session 文件（创建归 EnsureSession；存在前盖戳无意义）。
func TestStampSessionAgent_DoesNotCreateIfAbsent(t *testing.T) {
	dir := t.TempDir()
	const sid = `scoped-stamp-3`
	StampSessionAgent(dir, sid, `codex`)
	if _, err := os.Stat(sessionScopedFilePath(dir, sid)); !os.IsNotExist(err) {
		t.Errorf("stamp created a session file (must be no-op when record absent): %v", err)
	}
}

// TestStampSessionAgent_EmptyAgentNoop: an empty agent string must never stamp (guards
// the early return that keeps the hook's best-effort stamp from no-oping harmlessly but
// pointlessly when no agent resolved).
//
// TestStampSessionAgent_EmptyAgentNoop：空 agent 串绝不能盖戳（守护早返回，使 hook 在未
// 解析出 agent 时的尽力盖戳无害且不徒劳）。
func TestStampSessionAgent_EmptyAgentNoop(t *testing.T) {
	dir := t.TempDir()
	const sid = `scoped-stamp-4`
	if _, err := EnsureSession(dir, sid); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	StampSessionAgent(dir, sid, "") // empty → no-op
	reloaded, err := ensureScopedSession(dir, sid)
	if err != nil {
		t.Fatalf("ensureScopedSession reload: %v", err)
	}
	if reloaded.AgentType != "" {
		t.Errorf("AgentType = %q, want empty (empty agent must not stamp)", reloaded.AgentType)
	}
}

// TestStampSessionAgent_UpdatesSessionsLog is the MEDIUM-2 fix verification: a stamp must
// make the agent visible in sessions.jsonl (the file LoadSessions / attribution read), not
// just in the scoped/legacy file. Before the jsonl upsert, the stamp's contribution to the
// "53% missing agent_type" metric was exactly zero for scoped sessions — appendSessionLog
// wrote the line at creation with an empty AgentType, and LoadSessions reads the jsonl, not
// the scoped file, so the stamped value was invisible. Both the scoped and legacy paths are
// covered (legacy carries the real SessionID via the loaded record). Also asserts the jsonl
// is touched at most once (a second stamp is a no-op) and an already-attributed line is not
// rewritten.
//
// TestStampSessionAgent_UpdatesSessionsLog 是 MEDIUM-2 修复验证：盖戳必须让 agent 在
// sessions.jsonl（LoadSessions / 归因读取的文件）里可见，而不只是 scoped/legacy 文件里。
// 加 jsonl upsert 前，盖戳对 scoped session 在"53% agent_type 缺失"指标上的贡献恰好为零——
// appendSessionLog 创建时写了一行（空 AgentType），而 LoadSessions 读 jsonl、不读 scoped
// 文件，故盖戳值不可见。scoped 与 legacy 两路径都覆盖（legacy 经加载记录带真实 SessionID）。
// 另断言 jsonl 至多被碰一次（第二次盖戳 no-op），且已归因行不被重写。
func TestStampSessionAgent_UpdatesSessionsLog(t *testing.T) {
	t.Run(`scoped`, func(t *testing.T) {
		dir := t.TempDir() // no marker → detectAgentType empty
		const sid = `scoped-log-1`
		if _, err := EnsureSession(dir, sid); err != nil {
			t.Fatalf("EnsureSession: %v", err)
		}

		// Precondition: the jsonl line exists with an empty agent_type (creation wrote it).
		before, err := LoadSessions(dir)
		if err != nil {
			t.Fatalf("LoadSessions before stamp: %v", err)
		}
		var found bool
		for _, s := range before {
			if s.SessionID == sid {
				found = true
				if s.AgentType != "" {
					t.Fatalf("precondition: jsonl agent_type = %q, want empty before stamp", s.AgentType)
				}
			}
		}
		if !found {
			t.Fatal("precondition: session not in sessions.jsonl before stamp")
		}

		StampSessionAgent(dir, sid, `reasonix`)

		// LoadSessions (which reads the jsonl, not the scoped file) must now see reasonix.
		after, err := LoadSessions(dir)
		if err != nil {
			t.Fatalf("LoadSessions after stamp: %v", err)
		}
		for _, s := range after {
			if s.SessionID == sid && s.AgentType == `reasonix` {
				return // pass
			}
		}
		t.Errorf("after stamp, LoadSessions did not show reasonix for %s; got %v", sid, after)
	})

	t.Run(`legacy`, func(t *testing.T) {
		dir := t.TempDir()
		sess, err := EnsureSession(dir, "")
		if err != nil {
			t.Fatalf("EnsureSession: %v", err)
		}

		StampSessionAgent(dir, "", `kimi`)

		after, err := LoadSessions(dir)
		if err != nil {
			t.Fatalf("LoadSessions after stamp: %v", err)
		}
		for _, s := range after {
			if s.SessionID == sess.SessionID && s.AgentType == `kimi` {
				return // pass
			}
		}
		t.Errorf("after stamp, LoadSessions did not show kimi for legacy %s; got %v", sess.SessionID, after)
	})

	t.Run(`stamp-is-idempotent-and-non-overwriting`, func(t *testing.T) {
		dir := t.TempDir()
		const sid = `scoped-log-2`
		if _, err := EnsureSession(dir, sid); err != nil {
			t.Fatalf("EnsureSession: %v", err)
		}

		// First stamp fills it; second stamp must not rewrite the line (already attributed).
		StampSessionAgent(dir, sid, `kimi`)
		rawAfterFirst, _ := os.ReadFile(sessionsLogPath(dir))
		StampSessionAgent(dir, sid, `reasonix`) // different agent — must NOT overwrite
		rawAfterSecond, _ := os.ReadFile(sessionsLogPath(dir))

		after, err := LoadSessions(dir)
		if err != nil {
			t.Fatalf("LoadSessions: %v", err)
		}
		for _, s := range after {
			if s.SessionID == sid && s.AgentType != `kimi` {
				t.Errorf("agent_type = %q, want kimi (second stamp must not overwrite)", s.AgentType)
			}
		}
		// The jsonl bytes must be unchanged after the second (no-op) stamp — proves it is
		// not rewritten when the value is already set.
		if string(rawAfterFirst) != string(rawAfterSecond) {
			t.Error("sessions.jsonl was rewritten by a no-op second stamp (must be untouched)")
		}
	})
}

// TestUpsertSessionAgentInLog_PreservesByteStructure is the golden-bytes guard for the jsonl
// round-trip (the critical correctness property — corrupting sessions.jsonl is a serious bug,
// and LoadSessions tolerates a dropped trailing newline so it cannot catch this). bytes.Split
// on "\n" then bytes.Join back must preserve: the trailing newline, the no-trailing-newline
// case (no spurious newline added), the empty file (no write), and a target line in any
// position. A future refactor to bufio.Scanner (which drops the trailing newline) would pass
// the LoadSessions-based tests but corrupt the on-disk file for raw-byte consumers — this
// test fails it.
//
// TestUpsertSessionAgentInLog_PreservesByteStructure 是 jsonl 往返的金字节守卫（关键正确性
// 属性——损坏 sessions.jsonl 是严重 Bug，而 LoadSessions 容忍丢失尾随换行故抓不到）。
// bytes.Split("\n") 后 bytes.Join 回去必须保留：尾随换行、无尾随换行（不添多余换行）、
// 空文件（不写）、以及目标行在任意位置。未来若有人改用 bufio.Scanner（会丢尾随换行），
// 会通过基于 LoadSessions 的测试却损坏盘上文件——本测试让它失败。
func TestUpsertSessionAgentInLog_PreservesByteStructure(t *testing.T) {
	// A second, already-attributed line that must pass through byte-for-byte unchanged.
	other := `{"session_id":"other","started_at":"2026-01-01T00:00:00Z","agent_type":"codex"}`

	mkTarget := func(sid string) string {
		return `{"session_id":"` + sid + `","started_at":"2026-01-01T00:00:00Z"}`
	}
	targetFilled := func(sid string) string {
		return `{"session_id":"` + sid + `","started_at":"2026-01-01T00:00:00Z","agent_type":"kimi"}`
	}

	t.Run(`trailing-newline-preserved`, func(t *testing.T) {
		dir := t.TempDir()
		const sid = `T1`
		// target is the LAST line, file ends with \n (the normal appendSessionLog shape).
		writeSessionsLog(t, dir, mkTarget(sid)+"\n"+other+"\n")
		upsertSessionAgentInLog(dir, sid, `kimi`)
		after, _ := os.ReadFile(sessionsLogPath(dir))
		want := targetFilled(sid) + "\n" + other + "\n"
		if string(after) != want {
			t.Errorf("trailing newline dropped/structure changed.\nwant: %q\ngot:  %q", want, string(after))
		}
	})

	t.Run(`no-trailing-newline-not-added`, func(t *testing.T) {
		dir := t.TempDir()
		const sid = `T2`
		// File does NOT end with \n — the upsert must not synthesize one.
		writeSessionsLog(t, dir, other+"\n"+mkTarget(sid)) // target last, no trailing \n
		upsertSessionAgentInLog(dir, sid, `kimi`)
		after, _ := os.ReadFile(sessionsLogPath(dir))
		want := other + "\n" + targetFilled(sid)
		if string(after) != want {
			t.Errorf("spurious trailing newline added or structure changed.\nwant: %q\ngot:  %q", want, string(after))
		}
	})

	t.Run(`empty-file-not-written`, func(t *testing.T) {
		dir := t.TempDir()
		writeSessionsLog(t, dir, "")
		upsertSessionAgentInLog(dir, `T3`, `kimi`)
		after, _ := os.ReadFile(sessionsLogPath(dir))
		if len(after) != 0 {
			t.Errorf("empty jsonl was written to (%d bytes); must stay empty", len(after))
		}
	})

	t.Run(`absent-file-no-error`, func(t *testing.T) {
		dir := t.TempDir() // no sessions.jsonl at all
		upsertSessionAgentInLog(dir, `T4`, `kimi`) // must not panic / not create
		if _, err := os.Stat(sessionsLogPath(dir)); !os.IsNotExist(err) {
			t.Errorf("upsert created a jsonl for an absent file: %v", err)
		}
	})

	t.Run(`target-not-last`, func(t *testing.T) {
		dir := t.TempDir()
		const sid = `T5`
		// target in the MIDDLE, with a trailing newline.
		writeSessionsLog(t, dir, other+"\n"+mkTarget(sid)+"\n"+other+"\n")
		upsertSessionAgentInLog(dir, sid, `kimi`)
		after, _ := os.ReadFile(sessionsLogPath(dir))
		want := other + "\n" + targetFilled(sid) + "\n" + other + "\n"
		if string(after) != want {
			t.Errorf("middle-line rewrite broke structure.\nwant: %q\ngot:  %q", want, string(after))
		}
	})
}

// TestUpsertSessionAgentInLog_UpdatesAllDuplicateLines pins the duplicate-line behavior that
// the corrected ensureScopedSession comment now warns against "simplifying" away.
// appendSessionLog can append the same session multiple times (no dedup at append), and
// LoadSessions preserves duplicates — so the upsert must update EVERY matching line, or
// duplicate copies would keep an empty agent_type and leak into the metric. A naive
// "break after first match" optimization would silently reintroduce the MEDIUM-2 gap.
//
// TestUpsertSessionAgentInLog_UpdatesAllDuplicateLines 钉死重复行行为——纠正后的
// ensureScopedSession 注释现在警告别把它「简化」掉。appendSessionLog 可能多次追加同一
// session（追加时不去重），而 LoadSessions 保留重复——故 upsert 必须更新每一条匹配行，
// 否则重复副本会保持空 agent_type 并泄漏到指标。「首次命中即 break」的朴素优化会静默
// 重新引入 MEDIUM-2 缺口。
func TestUpsertSessionAgentInLog_UpdatesAllDuplicateLines(t *testing.T) {
	dir := t.TempDir()
	const sid = `DUP1`
	emptyLine := `{"session_id":"` + sid + `","started_at":"2026-01-01T00:00:00Z"}`
	filledLine := `{"session_id":"` + sid + `","started_at":"2026-01-01T00:00:00Z","agent_type":"kimi"}`
	// Three identical empty lines for the same session id.
	writeSessionsLog(t, dir, emptyLine+"\n"+emptyLine+"\n"+emptyLine+"\n")

	upsertSessionAgentInLog(dir, sid, `kimi`)

	after, _ := os.ReadFile(sessionsLogPath(dir))
	want := filledLine + "\n" + filledLine + "\n" + filledLine + "\n"
	if string(after) != want {
		t.Errorf("not all duplicate lines updated (the upsert must touch every match).\nwant: %q\ngot:  %q", want, string(after))
	}

	// LoadSessions must report agent_type on every copy (no empty-agent duplicate survives).
	loaded, err := LoadSessions(dir)
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	for i, s := range loaded {
		if s.SessionID == sid && s.AgentType != `kimi` {
			t.Errorf("duplicate line %d has agent_type %q, want kimi (all copies must be filled)", i, s.AgentType)
		}
	}
}

// writeSessionsLog writes content to dir's sessions.jsonl, creating the data-home directory
// tree first. sessionsLogPath lives under the USER-LEVEL DataDir (a hashed projects/<hash>/
// path), NOT under dir itself — so os.WriteFile directly fails with "path not found" unless
// the parent is created first (this is what appendSessionLog's MkdirAll does in production).
//
// writeSessionsLog 把 content 写到 dir 的 sessions.jsonl，先建好 data-home 目录树。
// sessionsLogPath 在用户级 DataDir 下（哈希的 projects/<hash>/ 路径），不在 dir 本身下——
// 故直接 os.WriteFile 会因「找不到路径」失败，除非先建父目录（正是 appendSessionLog 在
// 生产里做的 MkdirAll）。
func writeSessionsLog(t *testing.T, dir, content string) {
	t.Helper()
	path := sessionsLogPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll data home: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write sessions.jsonl: %v", err)
	}
}
