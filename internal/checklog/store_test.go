package checklog

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

// isolateDataHome points the forge global home at a temp dir so DataDirFor
// resolves under the test sandbox and stores never touch the real ~/.forge.
// Idempotent within a test: helpers that wrap multiple writes (writeEntry)
// call it per write, so a second call must not re-point the home elsewhere.
//
// isolateDataHome 把 forge 全局 home 指向临时目录，DataDirFor 解析进测试沙盒，
// store 绝不触碰真实 ~/.forge。测试内幂等：包装多次写入的 helper（writeEntry）
// 每次写入都调它，第二次调用不得把 home 重新指向别处。
func isolateDataHome(t *testing.T) {
	t.Helper()
	if os.Getenv("FORGE_DATA_HOME") != "" {
		return
	}
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
}

func TestRecordAndLoadAll(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)

	entry1 := &Entry{
		Check:   CheckAutoCompile,
		Passed:  true,
		Checked: true,
		Detail:  "All builds passed",
	}
	entry2 := &Entry{
		Check:   CheckAssertion,
		Passed:  false,
		Checked: true,
		Detail:  "t.Fatal removed",
	}

	if err := Record(dir, entry1); err != nil {
		t.Fatalf("Record entry1: %v", err)
	}
	if err := Record(dir, entry2); err != nil {
		t.Fatalf("Record entry2: %v", err)
	}

	entries, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Check != CheckAutoCompile {
		t.Errorf("entry[0].Check = %q, want %q", entries[0].Check, CheckAutoCompile)
	}
	if !entries[0].Passed {
		t.Errorf("entry[0].Passed = false, want true")
	}
	if entries[1].Check != CheckAssertion {
		t.Errorf("entry[1].Check = %q, want %q", entries[1].Check, CheckAssertion)
	}
	if entries[1].Passed {
		t.Errorf("entry[1].Passed = true, want false")
	}
	// RecordedAt should be set
	if entries[0].RecordedAt.IsZero() {
		t.Error("entry[0].RecordedAt is zero")
	}
}

func TestLoadAll_NoFile(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)
	entries, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll on missing file: %v", err)
	}
	if entries != nil {
		t.Fatalf("expected nil entries, got %v", entries)
	}
}

func TestLatestByCheckForSession_LatestWins(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)

	// Record two entries for auto-compile: one fail, then one pass
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: false, Detail: "failed"})
	time.Sleep(10 * time.Millisecond) // ensure ordering
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, Detail: "passed"})
	Record(dir, &Entry{Check: CheckAssertion, Passed: true, Detail: "ok"})

	latest, err := LatestByCheckForSession(dir, "")
	if err != nil {
		t.Fatalf("LatestByCheckForSession: %v", err)
	}
	if len(latest) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(latest))
	}
	// Latest auto-compile should be the passing one
	if ac, ok := latest[CheckAutoCompile]; !ok {
		t.Fatal("auto-compile not in results")
	} else if !ac.Passed {
		t.Error("latest auto-compile should be passed")
	}
	if as, ok := latest[CheckAssertion]; !ok {
		t.Fatal("assertion-check not in results")
	} else if !as.Passed {
		t.Error("assertion-check should be passed")
	}
}

func TestClear(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)
	logPath := filepath.Join(forgedata.DataDirFor(dir), "checklog.jsonl")

	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, Detail: "ok"})

	// File should exist
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Fatal("checklog.jsonl should exist after Record")
	}

	if err := Clear(dir); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	// File should be gone
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatal("checklog.jsonl should be removed after Clear")
	}

	// Clear on nonexistent file should not error
	if err := Clear(dir); err != nil {
		t.Fatalf("Clear on nonexistent: %v", err)
	}
}

func TestClear_RotatesArchive(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)
	dataDir := forgedata.DataDirFor(dir)

	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, Detail: "ok"})

	if err := Clear(dir); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	// Original file should be gone
	if _, err := os.Stat(filepath.Join(dataDir, "checklog.jsonl")); !os.IsNotExist(err) {
		t.Fatal("checklog.jsonl should not exist after Clear")
	}

	// Timestamped archive should exist
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "checklog-") && strings.HasSuffix(e.Name(), ".jsonl") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no timestamped archive found in DataDir")
	}

	// Clear on nonexistent should be idempotent
	if err := Clear(dir); err != nil {
		t.Fatalf("Clear on nonexistent: %v", err)
	}
}

func TestRecord_SetsRecordedAt(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)

	before := time.Now()
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true})
	after := time.Now()

	entries, _ := LoadAll(dir)
	if entries[0].RecordedAt.Before(before) || entries[0].RecordedAt.After(after) {
		t.Errorf("RecordedAt %v not between %v and %v", entries[0].RecordedAt, before, after)
	}
}

func TestLoadForTask(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)

	// Active checklog: two task refs interleaved.
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, TaskRef: "feat/x", Detail: "active-auto"})
	Record(dir, &Entry{Check: CheckAssertion, Passed: false, TaskRef: "feat/y", Detail: "other-task"})
	Record(dir, &Entry{Check: CheckTaskVerify, Passed: true, TaskRef: "feat/x", Detail: "active-exp"})

	// Archived checklog (rotated by Clear on a previous task start) — feat/x
	// history that LoadAll would miss. This is the gap LoadForTask closes.
	archivePath := filepath.Join(forgedata.DataDirFor(dir), "checklog-20260101000000.jsonl")
	archived := []byte(`{"check":"auto-compile","passed":true,"checked":true,"task_ref":"feat/x","detail":"archived","recorded_at":"2026-01-01T00:00:00Z"}` + "\n")
	if err := os.WriteFile(archivePath, archived, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadForTask(dir, "feat/x")
	if err != nil {
		t.Fatalf("LoadForTask: %v", err)
	}
	// 2 active (auto-compile, security) + 1 archived.
	if len(got) != 3 {
		t.Fatalf("expected 3 entries for feat/x, got %d: %+v", len(got), got)
	}
	for _, e := range got {
		if e.TaskRef != "feat/x" {
			t.Errorf("entry TaskRef = %q, want feat/x", e.TaskRef)
		}
	}
	// Sorted ascending by RecordedAt — the archived entry (2026-01-01) is earliest.
	if got[0].Detail != "archived" {
		t.Errorf("first entry should be the archived (earliest ts), got Detail=%q", got[0].Detail)
	}
}

func TestLoadForTask_NoMatch(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, TaskRef: "feat/x"})

	got, err := LoadForTask(dir, "nonexistent-ref")
	if err != nil {
		t.Fatalf("LoadForTask no match: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 entries for nonexistent ref, got %d", len(got))
	}
}

// TestLoadAllAll pins the cross-archive counterpart to LoadAll: it must read the active checklog.jsonl AND
// every archived checklog-*.jsonl (chronological), so cross-task consumers (skillseval usage reading
// CheckSkillTrigger across project history) do not see only the current task after forge task start archives.
//
// TestLoadAllAll 钉死 LoadAll 的跨归档对应：必须读 active checklog.jsonl 与所有归档 checklog-*.jsonl
// （时间序），让跨任务消费者（skillseval usage 跨项目历史读 CheckSkillTrigger）在 forge task start
// 归档后不至于只看到当前任务。
func TestLoadAllAll(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)

	// Active checklog: one auto-compile entry for the current task.
	//
	// active checklog：当前 task 的一条 auto-compile。
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, TaskRef: "t-now", Detail: "active"})

	// Archived checklog (what forge task start rotates away): a skill-trigger entry from an older task.
	// This is exactly the line LoadAll (active-only) would miss and LoadAllAll must surface — the whole
	// reason skillseval usage needs the cross-archive read.
	//
	// 归档 checklog（forge task start 轮转走的）：一条来自旧 task 的 skill-trigger 条目。
	// 这正是 LoadAll（仅 active）会漏、LoadAllAll 必须暴露的行——skillseval usage 需要跨归档读的全部理由。
	archivePath := filepath.Join(forgedata.DataDirFor(dir), "checklog-20260101000000.jsonl")
	archived := []byte(`{"check":"skill-trigger","passed":true,"checked":true,"task_ref":"t-old","detail":"skill-trigger: tdd-cycle hit (event=Stop test_keyword)","recorded_at":"2026-01-01T00:00:00Z"}` + "\n")
	if err := os.WriteFile(archivePath, archived, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadAllAll(dir)
	if err != nil {
		t.Fatalf("LoadAllAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries (active + archive), got %d: %+v", len(got), got)
	}
	// Sorted ascending by RecordedAt — the archived entry (2026-01-01) is earliest.
	//
	// 按 RecordedAt 升序——归档条目（2026-01-01）最早。
	if got[0].Check != CheckSkillTrigger {
		t.Errorf("first entry should be the archived skill-trigger, got Check=%q", got[0].Check)
	}
	if got[1].TaskRef != "t-now" {
		t.Errorf("second entry should be the active one, got TaskRef=%q", got[1].TaskRef)
	}
}

// TestRecordAndClear_ConcurrentNoDeadlock guards the C2 fix: checklog Clear
// holds the same mutex as Record. archiveLocked (split out of the since-removed
// Archive export) lets Clear archive-then-remove under one lock WITHOUT re-entering
// the non-reentrant mutex (a re-entry would deadlock; the timeout surfaces it).
func TestRecordAndClear_ConcurrentNoDeadlock(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_ = Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, TaskRef: "t"})
			}
		}()
		go func() {
			defer wg.Done()
			_ = Clear(dir)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	// 30s：Windows FS + race 下 250 次 Record（含 fsync）+ 50 次 Clear（archive
	// rename 风暴）合法地远超 5s（首个 Windows CI run 误报死锁）；真死锁永不完成，
	// 30s 仍必然拦截，且受 go test 包超时兜底。
	case <-time.After(30 * time.Second):
		t.Fatal("Record/Clear deadlocked (Clear→archiveLocked mutex re-entry?)")
	}
	if _, err := LoadAll(dir); err != nil {
		t.Fatalf("LoadAll after concurrent Record/Clear: %v", err)
	}
}

// TestClear_NanosecondNaming guards the C3 fix: archive names carry nanosecond
// precision so two same-second rotations don't collide.
func TestClear_NanosecondNaming(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)
	if err := Record(dir, &Entry{Check: CheckAutoCompile, Passed: true}); err != nil {
		t.Fatal(err)
	}
	if err := Clear(dir); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	entries, err := os.ReadDir(forgedata.DataDirFor(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "checklog-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		stamp := strings.TrimSuffix(strings.TrimPrefix(name, "checklog-"), ".jsonl")
		if !strings.Contains(stamp, ".") {
			t.Errorf("archive name %q lacks nanosecond precision (C3 regression)", name)
		}
		return
	}
	t.Fatal("no checklog-* archive produced by Clear")
}

// TestClear_PrunesOldArchives: Clear prunes expired archives by FORGE_LOG_RETENTION_DAYS after rotation,
// keeping recent archives and the active-clear semantics.
//
// TestClear_PrunesOldArchives：Clear 在轮转后按 FORGE_LOG_RETENTION_DAYS 清超期归档，
// 保留近期归档与 active 清空语义。
func TestClear_PrunesOldArchives(t *testing.T) {
	t.Setenv("FORGE_LOG_RETENTION_DAYS", "30")
	dir := t.TempDir()
	isolateDataHome(t)
	forgeDir := forgedata.DataDirFor(dir)
	os.MkdirAll(forgeDir, 0755)
	// Old archive (year 2000, definitely older than 30 days) → deleted.
	//
	// 老归档（2000 年，必然超 30 天）→ 删
	os.WriteFile(filepath.Join(forgeDir, "checklog-20000101000000.jsonl"), []byte("old"), 0644)
	// New archive (today's timestamp) → kept.
	//
	// 新归档（今天时间戳）→ 保留
	today := time.Now().Format("20060102150405.000000000")
	os.WriteFile(filepath.Join(forgeDir, "checklog-"+today+".jsonl"), []byte("new"), 0644)
	// active
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true})

	if err := Clear(dir); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := os.Stat(filepath.Join(forgeDir, "checklog-20000101000000.jsonl")); !os.IsNotExist(err) {
		t.Error("old archive should be pruned after Clear")
	}
	if _, err := os.Stat(filepath.Join(forgeDir, "checklog-"+today+".jsonl")); err != nil {
		t.Error("recent archive should be kept")
	}
	if _, err := os.Stat(filepath.Join(forgeDir, "checklog.jsonl")); !os.IsNotExist(err) {
		t.Error("active should be removed after Clear")
	}
}

// TestClear_DisabledRetention: FORGE_LOG_RETENTION_DAYS=0 disables pruning, old archives are kept.
//
// TestClear_DisabledRetention：FORGE_LOG_RETENTION_DAYS=0 禁用清理，老归档保留。
func TestClear_DisabledRetention(t *testing.T) {
	t.Setenv("FORGE_LOG_RETENTION_DAYS", "0")
	dir := t.TempDir()
	isolateDataHome(t)
	forgeDir := forgedata.DataDirFor(dir)
	os.MkdirAll(forgeDir, 0755)
	os.WriteFile(filepath.Join(forgeDir, "checklog-20000101000000.jsonl"), []byte("old"), 0644)
	Record(dir, &Entry{Check: CheckAutoCompile, Passed: true})

	if err := Clear(dir); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := os.Stat(filepath.Join(forgeDir, "checklog-20000101000000.jsonl")); err != nil {
		t.Error("with retention disabled, old archive should be kept")
	}
}

// TestRecord_WritesToDataDir_GitProject guards the refactor-data-home migration:
// for a real git project, checklog must land in the user-level DataDir
// (~/.forge/projects/<key>/), NOT the legacy project-level <root>/.forge/.
// Non-git tmp-dir tests above exercise the fallback path; this one exercises
// the DataDir path through a real ProjectFor so the migration is actually
// covered (the fallback tests would pass even if DataDir resolution were dead).
func TestRecord_WritesToDataDir_GitProject(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	if err := Record(root, &Entry{Check: CheckAutoCompile, Passed: true, TaskRef: "t-data"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// checklog must NOT be in the legacy ConfigDir.
	if _, err := os.Stat(filepath.Join(root, ".forge", "checklog.jsonl")); err == nil {
		t.Fatal("checklog should NOT be in legacy ConfigDir <root>/.forge/ for a git project")
	}
	// checklog must be in the DataDir.
	checklogPath := filepath.Join(p.DataDir, "checklog.jsonl")
	if _, err := os.Stat(checklogPath); err != nil {
		t.Errorf("checklog should be in DataDir %s: %v", checklogPath, err)
	}
	// LoadAll reads back from the DataDir.
	entries, err := LoadAll(root)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry from DataDir, got %d", len(entries))
	}
	if entries[0].TaskRef != "t-data" {
		t.Errorf("TaskRef = %q, want t-data", entries[0].TaskRef)
	}
}

// TestLoadAll_LongLineOver64KB pins the 1MB scanner buffer: a single entry line larger
// than bufio.Scanner's default 64KB cap (long Detail payload) must load whole, not fail
// scoring/trace wholesale with ErrTooLong.
//
// TestLoadAll_LongLineOver64KB 钉死 1MB scanner buffer：单条 entry 行超过
// bufio.Scanner 默认 64KB 上限（长 Detail 载荷）必须完整读出，不能让
// scoring/trace 全链路因 ErrTooLong 失败。
func TestLoadAll_LongLineOver64KB(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)

	long := strings.Repeat("x", 200*1024) // 200KB > 64KB default cap, < 1MB new cap
	if err := Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, Detail: long}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := Record(dir, &Entry{Check: CheckAssertion, Passed: true, Detail: "short"}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll with >64KB line: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Detail != long {
		t.Errorf("long Detail truncated/corrupted: len=%d, want %d", len(entries[0].Detail), len(long))
	}
	if entries[1].Detail != "short" {
		t.Errorf("entry after long line: Detail=%q, want short", entries[1].Detail)
	}
}

// TestLoadForTask_LongLineOver64KB pins the same 1MB cap for the archived-history path
// (LoadForTask globs checklog*.jsonl), and that scanner errors surface instead of being
// silently truncated.
//
// TestLoadForTask_LongLineOver64KB 为归档历史路径（LoadForTask glob
// checklog*.jsonl）钉同样的 1MB 上限，并保证 scanner 错误显式上抛而非静默截断。
func TestLoadForTask_LongLineOver64KB(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)

	long := strings.Repeat("y", 200*1024)
	if err := Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, TaskRef: "feat/long", Detail: long}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// A long line in an archived file must load too.
	//
	// 归档文件里的长行同样必须能读出。
	longEntry := `{"check":"auto-compile","passed":true,"checked":true,"task_ref":"feat/long","detail":"` + long + `","recorded_at":"2026-01-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(forgedata.DataDirFor(dir), "checklog-20260101000000.jsonl"), []byte(longEntry), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := LoadForTask(dir, "feat/long")
	if err != nil {
		t.Fatalf("LoadForTask with >64KB lines: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (active + archived), got %d", len(entries))
	}
	for _, e := range entries {
		if e.Detail != long {
			t.Errorf("long Detail truncated: len=%d, want %d", len(e.Detail), len(long))
		}
	}
}

// TestRecord_DerivesLevelFallback pins the Record-time Level fallback: entries
// whose caller leaves Level empty are classified from Passed + Detail prefixes
// (BLOCKED: / ADVISORY:), mirroring the Source fallback; an explicit Level
// always wins. The persisted JSON line carries the level field.
//
// TestRecord_DerivesLevelFallback 钉死 Record 时的 Level 兜底：调用方留空
// Level 的条目按 Passed + Detail 前缀（BLOCKED: / ADVISORY:）分级，与 Source
// 兜底同款；显式 Level 恒优先。落盘的 JSON 行带 level 字段。
func TestRecord_DerivesLevelFallback(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)

	entries := []*Entry{
		{Check: CheckAutoCompile, Passed: true, Detail: "all good"},                            // → pass
		{Check: CheckAutoCompile, Passed: false, Detail: "compile broke"},                      // → fail
		{Check: CheckTaskGuard, Passed: false, Detail: "BLOCKED: unread source edit"},          // → blocked
		{Check: CheckScopeDrift, Passed: true, Detail: "ADVISORY: drift beyond PlanScope"},     // → advisory
		{Check: CheckEscapeHatch, Passed: true, Level: LevelWarn, Detail: "escape-hatch: x"},   // explicit wins → warn
		{Check: CheckAutoCompile, Passed: false, Level: LevelWarn, Detail: "INFRA: spawn err"}, // explicit wins over derive
	}
	for _, e := range entries {
		if err := Record(dir, e); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	got, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	want := []Level{LevelPass, LevelFail, LevelBlocked, LevelAdvisory, LevelWarn, LevelWarn}
	if len(got) != len(want) {
		t.Fatalf("expected %d entries, got %d", len(want), len(got))
	}
	for i, w := range want {
		if got[i].Level != w {
			t.Errorf("entry[%d] (%q) Level = %q, want %q", i, got[i].Detail, got[i].Level, w)
		}
	}

	// The JSON line persists the derived level (structured consumers must not
	// need to re-derive for newly written entries).
	raw, err := os.ReadFile(filepath.Join(forgedata.DataDirFor(dir), "checklog.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"level":"blocked"`) {
		t.Errorf("persisted JSON must carry the derived level field, got:\n%s", raw)
	}
}

// TestEffectiveLevel_OldLinesDerive pins the read-side derive fallback: lines
// written before the level field existed (no "level" key — history is NOT
// rewritten) still classify correctly via EffectiveLevel, while Entry.Level
// stays empty on load (no mutation of the archived data in memory either).
//
// TestEffectiveLevel_OldLinesDerive 钉死读取侧 derive 兜底：level 字段引入前
// 写入的行（无 "level" 键——历史不改写）经 EffectiveLevel 仍正确分级，且
// 加载后的 Entry.Level 保持为空（内存里也不篡改归档数据）。
func TestEffectiveLevel_OldLinesDerive(t *testing.T) {
	dir := t.TempDir()
	isolateDataHome(t)

	old := `{"check":"auto-compile","passed":false,"checked":true,"detail":"BLOCKED: legacy hard stop","recorded_at":"2026-01-01T00:00:00Z"}` + "\n" +
		`{"check":"auto-compile","passed":true,"checked":true,"detail":"legacy pass","recorded_at":"2026-01-01T00:00:01Z"}` + "\n" +
		`{"check":"scope-drift","passed":false,"checked":true,"detail":"ADVISORY: legacy drift","recorded_at":"2026-01-01T00:00:02Z"}` + "\n"
	path := filepath.Join(forgedata.DataDirFor(dir), "checklog.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	want := []Level{LevelBlocked, LevelPass, LevelAdvisory}
	for i, w := range want {
		if entries[i].Level != "" {
			t.Errorf("entry[%d] Level must stay empty on load (history not rewritten), got %q", i, entries[i].Level)
		}
		if got := entries[i].EffectiveLevel(); got != w {
			t.Errorf("entry[%d] EffectiveLevel() = %q, want %q", i, got, w)
		}
	}
}

// --- feat/checklog-janitor: active-log size rotation ---

// writeActiveLog overwrites the active checklog.jsonl with raw JSONL lines — test
// fixture builder for oversized-active scenarios, where growing the file past the
// tiny test threshold via Record itself would rotate mid-setup.
//
// writeActiveLog 用原始 JSONL 行直接覆写 active checklog.jsonl——超阈值 active 场景的
// 测试夹具构造器：若用 Record 自己长过（测试用的极小）阈值，会在预置中途就轮转。
func writeActiveLog(t *testing.T, root, content string) {
	t.Helper()
	path := filepath.Join(forgedata.DataDirFor(root), "checklog.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestRecord_RotatesOversizedActive pins feat/checklog-janitor: when the active
// checklog.jsonl exceeds FORGE_CHECKLOG_ROTATE_BYTES, the next Record rotates it
// into a checklog-*.jsonl archive (the naming pruneArchives globs and
// archiveTimestamp parses, and loadAllArchives reads), opens a fresh active, and
// subsequent Records land in the fresh active. Without rotation the active file
// grew unbounded (15946 lines observed in review evidence) — Clear has no
// production caller and Prune only globs archives, so nothing else bounds it.
//
// TestRecord_RotatesOversizedActive 钉死 feat/checklog-janitor：active
// checklog.jsonl 超过 FORGE_CHECKLOG_ROTATE_BYTES 时，下一次 Record 把它轮转成
// checklog-*.jsonl 归档（命名同时被 pruneArchives glob/archiveTimestamp 解析、被
// loadAllArchives 读取），新开 active，后续 Record 落新 active。没有轮转时 active
// 无限增长（审查实证 15946 行）——Clear 无生产调用方、Prune 只 glob 归档，再无他者
// 约束它。
func TestRecord_RotatesOversizedActive(t *testing.T) {
	t.Setenv("FORGE_CHECKLOG_ROTATE_BYTES", "1024")
	dir := t.TempDir()
	isolateDataHome(t)
	dataDir := forgedata.DataDirFor(dir)

	// Pre-grow the active file past the threshold with ONE valid pre-rotation entry.
	//
	// 用一条合法的轮转前条目把 active 预置到超阈值。
	pre := `{"check":"auto-compile","passed":true,"checked":true,"task_ref":"t-old","detail":"` + strings.Repeat("x", 2900) + `","recorded_at":"2026-01-01T00:00:00Z"}` + "\n"
	writeActiveLog(t, dir, pre)
	info, err := os.Stat(filepath.Join(dataDir, "checklog.jsonl"))
	if err != nil || info.Size() <= 1024 {
		t.Fatalf("预置 active 未超阈值: size=%v err=%v", info, err)
	}

	// The rotation-triggering Record: oversized active is rotated away BEFORE the
	// append, so this entry lands in the fresh active.
	//
	// 触发轮转的 Record：超阈值的 active 在追加前被轮走，本条落进新开的 active。
	if err := Record(dir, &Entry{Check: CheckAssertion, Passed: true, TaskRef: "t-new", Detail: "fresh-1"}); err != nil {
		t.Fatalf("Record(触发轮转): %v", err)
	}

	// Exactly one archive exists, carrying the pre-rotation history.
	//
	// 恰好一个归档，承载轮转前历史。
	archives, err := filepath.Glob(filepath.Join(dataDir, "checklog-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("轮转应产生恰好 1 个归档, got %d: %v", len(archives), archives)
	}
	// The archive name keeps the nanosecond convention (parseable by pruneArchives'
	// archiveTimestamp → filename-timestamp retention works on rotation-produced archives).
	//
	// 归档名保持纳秒约定（pruneArchives 的 archiveTimestamp 可解析 → 按文件名时间戳的
	// retention 对轮转产物有效）。
	name := filepath.Base(archives[0])
	if !strings.Contains(strings.TrimSuffix(strings.TrimPrefix(name, "checklog-"), ".jsonl"), ".") {
		t.Errorf("归档名 %q 缺纳秒精度（archiveTimestamp 约定）", name)
	}

	// Fresh active contains ONLY the new entry.
	//
	// 新开的 active 只含新条目。
	active, err := LoadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].TaskRef != "t-new" || active[0].Detail != "fresh-1" {
		t.Fatalf("轮转后 active 应只含新条目, got %+v", active)
	}

	// Subsequent Records keep landing in the fresh active (no re-rotation below threshold).
	//
	// 后续 Record 持续落新 active（阈值以下不再轮转）。
	if err := Record(dir, &Entry{Check: CheckTaskVerify, Passed: true, TaskRef: "t-new", Detail: "fresh-2"}); err != nil {
		t.Fatal(err)
	}
	active, err = LoadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 {
		t.Fatalf("后续 Record 应落新 active（共 2 条）, got %d", len(active))
	}

	// Cross-archive readers keep the rotated history: 1 pre-rotation + 2 new.
	//
	// 跨归档读者仍见轮转走的历史：1 条轮转前 + 2 条新。
	all, err := LoadAllAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("LoadAllAll 应见全部 3 条（active+归档）, got %d", len(all))
	}
}

// TestRecord_NoRotationBelowThreshold: below the threshold Record appends in place —
// no archive appears (rotation must not fire on normal-sized logs).
//
// TestRecord_NoRotationBelowThreshold：阈值以下 Record 原地追加——不产生归档
// （正常尺寸的日志不得触发轮转）。
func TestRecord_NoRotationBelowThreshold(t *testing.T) {
	// 8MB threshold: two small entries can never cross it.
	//
	// 8MB 阈值：两条小条目无论如何越不过。
	t.Setenv("FORGE_CHECKLOG_ROTATE_BYTES", "8388608")
	dir := t.TempDir()
	isolateDataHome(t)
	dataDir := forgedata.DataDirFor(dir)

	for i := 0; i < 2; i++ {
		if err := Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, Detail: "normal"}); err != nil {
			t.Fatal(err)
		}
	}
	archives, err := filepath.Glob(filepath.Join(dataDir, "checklog-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 0 {
		t.Fatalf("阈值以下不得轮转, got archives: %v", archives)
	}
	entries, err := LoadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("两条都应在 active, got %d", len(entries))
	}
}

// TestRecord_RotationInvalidEnvUsesDefault: a non-numeric FORGE_CHECKLOG_ROTATE_BYTES
// falls back to the 5MB default (mirrors util.RetentionDays' invalid→default rule) —
// normal writes never rotate.
//
// TestRecord_RotationInvalidEnvUsesDefault：非法数字的 FORGE_CHECKLOG_ROTATE_BYTES
// 回落到 5MB 默认（镜像 util.RetentionDays 的非法→默认规则）——常规写入不轮转。
func TestRecord_RotationInvalidEnvUsesDefault(t *testing.T) {
	t.Setenv("FORGE_CHECKLOG_ROTATE_BYTES", "not-a-number")
	dir := t.TempDir()
	isolateDataHome(t)
	dataDir := forgedata.DataDirFor(dir)

	if err := Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, Detail: "normal"}); err != nil {
		t.Fatal(err)
	}
	archives, err := filepath.Glob(filepath.Join(dataDir, "checklog-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 0 {
		t.Fatalf("非法 env 应回落 5MB 默认（不轮转）, got archives: %v", archives)
	}
}

// TestRecord_RotationDisabled: FORGE_CHECKLOG_ROTATE_BYTES=0 disables rotation — an
// oversized active is appended to in place (explicit escape hatch back to the legacy
// unbounded behavior, never data loss).
//
// TestRecord_RotationDisabled：FORGE_CHECKLOG_ROTATE_BYTES=0 禁用轮转——超阈值
// active 原地继续追加（显式逃生阀，回到旧的无限增长行为，绝不丢数据）。
func TestRecord_RotationDisabled(t *testing.T) {
	t.Setenv("FORGE_CHECKLOG_ROTATE_BYTES", "0")
	dir := t.TempDir()
	isolateDataHome(t)
	dataDir := forgedata.DataDirFor(dir)

	pre := `{"check":"auto-compile","passed":true,"checked":true,"task_ref":"t-old","detail":"` + strings.Repeat("x", 2900) + `","recorded_at":"2026-01-01T00:00:00Z"}` + "\n"
	writeActiveLog(t, dir, pre)
	if err := Record(dir, &Entry{Check: CheckAssertion, Passed: true, TaskRef: "t-new"}); err != nil {
		t.Fatal(err)
	}

	archives, err := filepath.Glob(filepath.Join(dataDir, "checklog-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 0 {
		t.Fatalf("禁用轮转时不得产生归档, got %v", archives)
	}
	entries, err := LoadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("禁用轮转时两条都留在 active, got %d", len(entries))
	}
}

// TestAppendEntries_RotatesOversizedActive: the import path (AppendEntries) carries
// the same rotation guard — one cross-machine bundle can push the active file past
// the threshold in a single call.
//
// TestAppendEntries_RotatesOversizedActive：import 路径（AppendEntries）带同样的
// 轮转守卫——一次跨机器 bundle 就能把 active 顶过阈值。
func TestAppendEntries_RotatesOversizedActive(t *testing.T) {
	t.Setenv("FORGE_CHECKLOG_ROTATE_BYTES", "1024")
	dir := t.TempDir()
	isolateDataHome(t)
	dataDir := forgedata.DataDirFor(dir)

	pre := `{"check":"auto-compile","passed":true,"checked":true,"task_ref":"t-old","detail":"` + strings.Repeat("x", 2900) + `","recorded_at":"2026-01-01T00:00:00Z"}` + "\n"
	writeActiveLog(t, dir, pre)

	if err := AppendEntries(dir, []Entry{{Check: CheckTaskVerify, Passed: true, TaskRef: "t-import", Detail: "imported"}}); err != nil {
		t.Fatalf("AppendEntries(触发轮转): %v", err)
	}
	archives, err := filepath.Glob(filepath.Join(dataDir, "checklog-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("import 路径也应轮转出 1 个归档, got %d: %v", len(archives), archives)
	}
	active, err := LoadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].TaskRef != "t-import" {
		t.Fatalf("轮转后 active 应只含导入条目, got %+v", active)
	}
}

// TestRecord_ConcurrentRotation: rotation is serialized with Record under the same
// mutex — concurrent writers at a tiny threshold must not lose entries or error;
// every Record survives in active-or-archive (LoadAllAll counts them all).
//
// TestRecord_ConcurrentRotation：轮转与 Record 同锁串行——极小阈值下的并发写者不得
// 丢条目、不得报错；每条 Record 都存活在 active 或归档里（LoadAllAll 全数可见）。
func TestRecord_ConcurrentRotation(t *testing.T) {
	t.Setenv("FORGE_CHECKLOG_ROTATE_BYTES", "512")
	dir := t.TempDir()
	isolateDataHome(t)

	const goroutines = 8
	const per = 25
	errCh := make(chan error, goroutines*per)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				if err := Record(dir, &Entry{Check: CheckAutoCompile, Passed: true, TaskRef: "t-race", Detail: strings.Repeat("d", 60)}); err != nil {
					errCh <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("并发 Record(轮转): %v", err)
	}
	all, err := LoadAllAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != goroutines*per {
		t.Fatalf("轮转并发下不得丢条目: got %d want %d", len(all), goroutines*per)
	}
}
