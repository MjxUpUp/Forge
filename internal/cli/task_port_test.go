package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// initGitForgeProject sets up a forge project in dir (git + init + initial commit + feat/port
// branch) WITHOUT touching env — so the caller controls FORGE_DATA_HOME / CLAUDE_CODE_SESSION_ID to
// simulate distinct machines over a shared codebase (export from machine A, import into machine B).
// Mirrors setupDelegateProject minus the env pinning.
//
// initGitForgeProject 在 dir 建好 forge 项目（git + init + 初始提交 + feat/port 分支）但不碰 env——
// 让调用方控制 FORGE_DATA_HOME / CLAUDE_CODE_SESSION_ID 来模拟「共享代码库上的不同机器」
// （从机器 A 导出，导入机器 B）。与 setupDelegateProject 一致，仅去掉 env 钉定。
func initGitForgeProject(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, `init`)
	runGit(t, dir, `config`, `user.email`, `test@test.com`)
	runGit(t, dir, `config`, `user.name`, `Test`)
	if out, _, code := runForge(t, dir, `init`, `--mode`, `medium`); code != 0 {
		t.Fatalf(`forge init failed: %s`, out)
	}
	os.WriteFile(filepath.Join(dir, `main.go`), []byte(`package main

func main() {}
`), 0644)
	runGit(t, dir, `add`, `.`)
	runGit(t, dir, `commit`, `-m`, `initial`)
	runGit(t, dir, `checkout`, `-b`, `feat/delegate`)
}

// switchMachine re-pins the ambient forge env to simulate a second machine: a fresh user-level
// DataDir (so B's task state is isolated from A's) and a distinct session id. State already written
// to A's DataDir stays on disk — switching the env only redirects subsequent commands to B's home.
//
// switchMachine 重新钉定环境以模拟第二台机器：全新的用户级 DataDir（B 的 task state 与 A 隔离）与
// 不同 session id。已写入 A DataDir 的 state 留在盘上——切 env 只是把后续命令重定向到 B 的 home。
func switchMachine(t *testing.T, sessionID string) string {
	t.Helper()
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	t.Setenv(`CLAUDE_CODE_SESSION_ID`, sessionID)
	dir := t.TempDir()
	initGitForgeProject(t, dir)
	return dir
}

// TestTaskExportImport_FreshRoundTrip is the end-to-end cross-machine handoff: machine A starts a
// task, records a decision + an anchored worker session; exports (with checklog). Machine B (fresh
// DataDir) imports. The task lands in B with the decision migrated and — crucially — every session
// link marked Imported (ghost): provenance only, never a local anchor. The ghost invariant is
// asserted directly via HasSession (false for A's session on B).
//
// TestTaskExportImport_FreshRoundTrip 端到端跨机器交接：机器 A 起任务、记决策 + 锚定一个 worker
// session；导出（含 checklog）。机器 B（全新 DataDir）导入。任务落地到 B，决策迁移过来，且——
// 关键——每个 session 链接标记 Imported（幽灵）：仅溯源，永非本机锚点。幽灵不变量经 HasSession
// 直接断言（A 的 session 在 B 上为 false）。
func TestTaskExportImport_FreshRoundTrip(t *testing.T) {
	// Machine A.
	dirA := setupDelegateProject(t)
	if out, _, code := runForge(t, dirA, `task`, `start`, `--ref`, `feat/delegate`, `--title`, `port roundtrip`); code != 0 {
		t.Fatalf(`A task start: %s`, out)
	}
	if out, _, code := runForge(t, dirA, `task`, `decide`, `--ref`, `feat/delegate`, `--content`, `use REST`, `--by`, `claude-code`); code != 0 {
		t.Fatalf(`A decide: %s`, out)
	}
	// Anchor a known worker session so the ghost assertion is over a concrete, present link.
	if out, _, code := runForge(t, dirA, `task`, `attach`, `--ref`, `feat/delegate`, `--tool`, `claude-code`, `--session`, `a-worker-sid`); code != 0 {
		t.Fatalf(`A attach: %s`, out)
	}
	bundlePath := filepath.Join(t.TempDir(), `bundle.json`)
	if out, _, code := runForge(t, dirA, `task`, `export`, `--ref`, `feat/delegate`, `-o`, bundlePath, `--include-checklog`); code != 0 {
		t.Fatalf(`A export: %s`, out)
	}

	// Machine B: fresh DataDir, no feat/delegate task yet.
	dirB := switchMachine(t, `machine-b-sid`)
	if out, _, code := runForge(t, dirB, `task`, `import`, `--file`, bundlePath); code != 0 {
		t.Fatalf(`B import: %s`, out)
	}
	loaded, err := taskpipeline.LoadTaskState(dirB, `feat/delegate`)
	if err != nil {
		t.Fatalf(`B load imported task: %v`, err)
	}
	if loaded.Summary != `port roundtrip` {
		t.Errorf(`Summary = %q, want "port roundtrip"`, loaded.Summary)
	}
	if len(loaded.Decisions) != 1 || loaded.Decisions[0].Content != `use REST` {
		t.Errorf(`决策应迁移 1 条 use REST, got %+v`, loaded.Decisions)
	}
	// Every session link that crossed the wire is a ghost on B.
	anyLink := false
	for i, l := range loaded.SessionLinks {
		anyLink = true
		if !l.Imported {
			t.Errorf(`session link %d (%s) 应 Imported=true（幽灵）`, i, l.SessionID)
		}
	}
	if !anyLink {
		t.Fatal(`前置失败：导入任务竟无 session link，幽灵断言无意义（检查 A 的 attach 是否生效）`)
	}
	// The ghost does not block local attach: A's worker session is NOT a local anchor on B.
	if loaded.HasSession(`a-worker-sid`) {
		t.Error(`A 的 session 在 B 上是幽灵，HasSession 应 false（不阻断 B 本机 attach）`)
	}
	if !loaded.HasAnySession(`a-worker-sid`) {
		t.Error(`HasAnySession 应看到 A 的幽灵 session（溯源完整）`)
	}
}

// TestTaskImport_DefaultRejectsExisting: importing a bundle whose ref already exists locally, with
// no strategy flag, must refuse (safe default — never silently clobber local work).
//
// TestTaskImport_DefaultRejectsExisting：导入 ref 已在本地存在的 bundle 且未带策略 flag，必须拒绝
// （安全默认——绝不静默覆盖本地工作）。
func TestTaskImport_DefaultRejectsExisting(t *testing.T) {
	dirA := setupDelegateProject(t)
	runForge(t, dirA, `task`, `start`, `--ref`, `feat/delegate`, `--title`, `x`)
	bundlePath := filepath.Join(t.TempDir(), `b.json`)
	runForge(t, dirA, `task`, `export`, `--ref`, `feat/delegate`, `-o`, bundlePath)

	dirB := switchMachine(t, `b-sid-2`)
	runForge(t, dirB, `task`, `import`, `--file`, bundlePath) // fresh: ok
	out, _, code := runForge(t, dirB, `task`, `import`, `--file`, bundlePath) // again, default
	if code == 0 {
		t.Fatalf(`重复 import 默认应拒绝（非 0 退出），got exit 0: %s`, out)
	}
	if !strings.Contains(out, `已存在`) {
		t.Errorf(`拒绝信息应含「已存在」, got: %s`, out)
	}
}

// TestTaskImport_ForceOverwrites: --force replaces the local task wholesale (delete + write the
// bundled task). B's locally-diverged decision must be gone after force — the bundle wins.
//
// TestTaskImport_ForceOverwrites：--force 整体替换本地任务（删 + 写 bundled task）。B 本地分叉出的
// 决策在 force 后必须消失——bundle 胜。
func TestTaskImport_ForceOverwrites(t *testing.T) {
	dirA := setupDelegateProject(t)
	runForge(t, dirA, `task`, `start`, `--ref`, `feat/delegate`, `--title`, `from-A`)
	bundlePath := filepath.Join(t.TempDir(), `b.json`)
	runForge(t, dirA, `task`, `export`, `--ref`, `feat/delegate`, `-o`, bundlePath)

	dirB := switchMachine(t, `b-sid-3`)
	runForge(t, dirB, `task`, `import`, `--file`, bundlePath)
	// B diverges: its own decision that A's bundle does not carry.
	runForge(t, dirB, `task`, `decide`, `--ref`, `feat/delegate`, `--content`, `b-local-decision`, `--by`, `claude-code`)
	if out, _, code := runForge(t, dirB, `task`, `import`, `--file`, bundlePath, `--force`); code != 0 {
		t.Fatalf(`--force import: %s`, out)
	}
	loaded, err := taskpipeline.LoadTaskState(dirB, `feat/delegate`)
	if err != nil {
		t.Fatalf(`B reload after force: %v`, err)
	}
	for _, d := range loaded.Decisions {
		if d.Content == `b-local-decision` {
			t.Error(`--force 应覆盖：B 的本地决策应被清除（被 A 的 bundle 替换）`)
		}
	}
	if loaded.Summary != `from-A` {
		t.Errorf(`--force 后 Summary 应回到 A 的 %q, got %q`, `from-A`, loaded.Summary)
	}
}

// TestTaskImport_MergeUnions: --merge keeps the local task's identity/definition and unions the
// collaborative records by ID. Decision IDs are globally unique (newContinuityID: nano + seq + 4
// crypto bytes), so A's and B's decisions never collide — both survive the union.
//
// TestTaskImport_MergeUnions：--merge 保留本地任务身份/定义，按 ID 并集协作记录。决策 ID 全局唯一
// （newContinuityID：nano + seq + 4 字节随机），故 A 与 B 的决策不碰撞——并集后都在。
func TestTaskImport_MergeUnions(t *testing.T) {
	dirA := setupDelegateProject(t)
	runForge(t, dirA, `task`, `start`, `--ref`, `feat/delegate`, `--title`, `merge`)
	runForge(t, dirA, `task`, `decide`, `--ref`, `feat/delegate`, `--content`, `remote-decision`, `--by`, `claude-code`)
	bundlePath := filepath.Join(t.TempDir(), `b.json`)
	runForge(t, dirA, `task`, `export`, `--ref`, `feat/delegate`, `-o`, bundlePath)

	dirB := switchMachine(t, `b-sid-4`)
	runForge(t, dirB, `task`, `import`, `--file`, bundlePath) // fresh: brings remote-decision
	runForge(t, dirB, `task`, `decide`, `--ref`, `feat/delegate`, `--content`, `local-decision`, `--by`, `claude-code`)
	// Re-import the SAME bundle with --merge: remote-decision's ID already present → no dup; union keeps both.
	if out, _, code := runForge(t, dirB, `task`, `import`, `--file`, bundlePath, `--merge`); code != 0 {
		t.Fatalf(`--merge import: %s`, out)
	}
	loaded, err := taskpipeline.LoadTaskState(dirB, `feat/delegate`)
	if err != nil {
		t.Fatalf(`B reload after merge: %v`, err)
	}
	contents := map[string]bool{}
	for _, d := range loaded.Decisions {
		contents[d.Content] = true
	}
	if !contents[`remote-decision`] || !contents[`local-decision`] {
		t.Errorf(`--merge 应并集：remote + local 都在, got %v`, contents)
	}
	if len(loaded.Decisions) != 2 {
		t.Errorf(`并集去重后应恰好 2 条决策（remote 不重复），got %d: %+v`, len(loaded.Decisions), loaded.Decisions)
	}
}

// TestTaskExport_Redacts: --redact strips identifying/evidence fields (issue origin, agent, commit
// SHAs, decision/finding content+evidence) while keeping the task's SHAPE (status, gate IDs, decision
// COUNT). It also verifies the deep-copy guarantee: the on-disk original is NOT mutated by redaction.
//
// TestTaskExport_Redacts：--redact 抹除身份/证据字段（issue 来源/agent/commit SHA/决策与发现的正文+证据），
// 保留任务形状（状态/门禁 ID/决策计数）。另验证深拷贝保证：盘上原件不被脱敏改动。
func TestTaskExport_Redacts(t *testing.T) {
	dirA := setupDelegateProject(t)
	runForge(t, dirA, `task`, `start`, `--ref`, `feat/delegate`, `--title`, `secret`)
	runForge(t, dirA, `task`, `assign`, `--ref`, `feat/delegate`, `--to`, `kimi`, `--role`, `frontend`, `--by`, `claude-code`)
	runForge(t, dirA, `task`, `decide`, `--ref`, `feat/delegate`, `--content`, `internal-api-key-12345`, `--rationale`, `leaks repo layout`, `--by`, `claude-code`)
	runForge(t, dirA, `task`, `finding`, `--ref`, `feat/delegate`, `--content`, `sql injection`, `--evidence`, `main.go:42`, `--source`, `claude-code`)
	// Seed the high-leak free-text / code-path fields the redactor must catch — the start/assign/decide
	// flow above does not populate Goal/Plan/PlanScope/Acceptance.Run/Decisions.Affects. Loaded + saved
	// directly (the redact contract is about export reading whatever is in the state, not how it got there).
	//
	// 播种脱敏器必须抓到的高泄露自由文本/代码路径字段——上面的 start/assign/decide 流不填
	// Goal/Plan/PlanScope/Acceptance.Run/Decisions.Affects。直接加载+保存（脱敏契约关乎 export 读
	// state 里有什么，而非它怎么进来的）。
	seeded, err := taskpipeline.LoadTaskState(dirA, `feat/delegate`)
	if err != nil {
		t.Fatalf(`seed load: %v`, err)
	}
	seeded.Goal = `migrate customer-acme billing endpoint /internal/api`
	seeded.Plan = `touch internal/billing/handler.go and internal/api/routes.go`
	seeded.PlanScope = []string{`internal/billing/*.go`, `internal/api/routes.go`}
	seeded.Branch = `fix/customer-acme-billing`
	seeded.NextSteps = []string{`update internal/billing/handler.go:42`}
	seeded.SessionID = `creator-session-secret`
	if len(seeded.Decisions) > 0 {
		seeded.Decisions[0].Affects = []string{`internal/billing/handler.go`}
	}
	seeded.Acceptance = []taskpipeline.AcceptanceCriterion{
		{Run: `go test ./internal/billing/...`, Expected: `ok`, Passed: true, AcceptedHeadCommit: `deadbeef`, Output: `secret output`},
	}
	if err := taskpipeline.SaveTaskState(dirA, seeded); err != nil {
		t.Fatalf(`seed save: %v`, err)
	}
	bundlePath := filepath.Join(t.TempDir(), `b.json`)
	if out, _, code := runForge(t, dirA, `task`, `export`, `--ref`, `feat/delegate`, `-o`, bundlePath, `--redact`); code != 0 {
		t.Fatalf(`redact export: %s`, out)
	}
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf(`read bundle: %v`, err)
	}
	var bundle taskBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf(`parse bundle: %v`, err)
	}
	if !bundle.Redacted {
		t.Error(`bundle.Redacted 应 true`)
	}
	// Commit SHAs cleared (only meaningful if start set one; cleared either way is the contract).
	if bundle.Task.HeadCommit != `` {
		t.Errorf(`HeadCommit 应脱敏为空, got %q`, bundle.Task.HeadCommit)
	}
	// Assignment identity redacted (kept non-empty so the shape "has an assignee" survives).
	if bundle.Task.Assignment == nil || bundle.Task.Assignment.Agent != `[redacted]` {
		t.Errorf(`Assignment.Agent 应 [redacted], got %+v`, bundle.Task.Assignment)
	}
	if bundle.Task.Assignment != nil && bundle.Task.Assignment.OfferedBy != `[redacted]` {
		t.Errorf(`Assignment.OfferedBy 应 [redacted], got %q`, bundle.Task.Assignment.OfferedBy)
	}
	// Decision content replaced, rationale cleared; finding evidence cleared.
	if len(bundle.Task.Decisions) == 0 || bundle.Task.Decisions[0].Content != `[redacted]` {
		t.Errorf(`bundle 决策内容应 [redacted], got %+v`, bundle.Task.Decisions)
	}
	if len(bundle.Task.Findings) == 0 || bundle.Task.Findings[0].Evidence != `` {
		t.Errorf(`bundle finding evidence 应清空, got %+v`, bundle.Task.Findings)
	}
	// ExternalOrigin cleared entirely.
	if bundle.Task.ExternalOrigin.URL != `` || bundle.Task.ExternalOrigin.Identifier != `` {
		t.Errorf(`ExternalOrigin 应整体清空, got %+v`, bundle.Task.ExternalOrigin)
	}

	// Code-path / free-text redactions (the --redact contract: not leak issue/agent/commit/code-paths).
	if bundle.Task.Goal != `[redacted]` {
		t.Errorf(`Goal 应 [redacted], got %q`, bundle.Task.Goal)
	}
	if bundle.Task.Plan != `[redacted]` {
		t.Errorf(`Plan 应 [redacted], got %q`, bundle.Task.Plan)
	}
	if bundle.Task.Summary != `[redacted]` {
		t.Errorf(`Summary 应 [redacted], got %q`, bundle.Task.Summary)
	}
	if len(bundle.Task.PlanScope) != 0 {
		t.Errorf(`PlanScope 应清空, got %+v`, bundle.Task.PlanScope)
	}
	if bundle.Task.Branch != `` {
		t.Errorf(`Branch 应清空, got %q`, bundle.Task.Branch)
	}
	if len(bundle.Task.NextSteps) != 0 {
		t.Errorf(`NextSteps 应清空, got %+v`, bundle.Task.NextSteps)
	}
	if bundle.Task.SessionID != `` {
		t.Errorf(`SessionID 应清空, got %q`, bundle.Task.SessionID)
	}
	if len(bundle.Task.Decisions) == 0 || len(bundle.Task.Decisions[0].Affects) != 0 {
		t.Errorf(`Decisions Affects 应清空, got %+v`, bundle.Task.Decisions)
	}
	if len(bundle.Task.Acceptance) == 0 ||
		bundle.Task.Acceptance[0].Run != `[redacted]` ||
		bundle.Task.Acceptance[0].Expected != `[redacted]` ||
		bundle.Task.Acceptance[0].AcceptedHeadCommit != `` ||
		bundle.Task.Acceptance[0].Output != `` {
		t.Errorf(`Acceptance Run/Expected 应 [redacted]、AcceptedHeadCommit/Output 应清空, got %+v`, bundle.Task.Acceptance)
	}

	// Deep-copy guarantee: the ORIGINAL on disk is untouched by redaction.
	orig, err := taskpipeline.LoadTaskState(dirA, `feat/delegate`)
	if err != nil {
		t.Fatalf(`reload original: %v`, err)
	}
	if orig.Assignment == nil || orig.Assignment.Agent != `kimi` {
		t.Errorf(`脱敏不得改原件：A 的 Agent 应仍 kimi, got %+v`, orig.Assignment)
	}
	if len(orig.Decisions) == 0 || orig.Decisions[0].Content != `internal-api-key-12345` {
		t.Errorf(`脱敏不得改原件：决策内容应仍在, got %+v`, orig.Decisions)
	}
	if orig.Goal != `migrate customer-acme billing endpoint /internal/api` {
		t.Errorf(`脱敏不得改原件：Goal 应仍在, got %q`, orig.Goal)
	}
	if len(orig.PlanScope) == 0 || orig.PlanScope[0] != `internal/billing/*.go` {
		t.Errorf(`脱敏不得改原件：PlanScope 应仍在, got %+v`, orig.PlanScope)
	}
	if len(orig.Acceptance) == 0 || orig.Acceptance[0].Run != `go test ./internal/billing/...` {
		t.Errorf(`脱敏不得改原件：Acceptance.Run 应仍在, got %+v`, orig.Acceptance)
	}
}

// TestTaskImport_RejectsMalformedSchema: a bundle whose schema_version is absent/zero (a malformed or
// hand-edited doc) must be refused, not silently parsed as v1 — otherwise the forward-compat guard is
// one missing field away from useless. (A genuinely-higher future version is also refused; that path
// is pre-existing and unchanged.)
//
// TestTaskImport_RejectsMalformedSchema：schema_version 缺失/为 0 的 bundle（畸形或手改文档）必须被拒，
// 而非静默当 v1 解析——否则前向兼容守卫离失效只差一个缺字段。（真正更高的未来版本同样被拒；该路径
// 既有未改。）
func TestTaskImport_RejectsMalformedSchema(t *testing.T) {
	dirA := setupDelegateProject(t)
	runForge(t, dirA, `task`, `start`, `--ref`, `feat/delegate`, `--title`, `x`)
	bundlePath := filepath.Join(t.TempDir(), `b.json`)
	runForge(t, dirA, `task`, `export`, `--ref`, `feat/delegate`, `-o`, bundlePath)

	// Hand-edit schema_version 1 → 0 to simulate a malformed bundle.
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf(`read bundle: %v`, err)
	}
	patched := strings.Replace(string(data), `"schema_version": 1,`, `"schema_version": 0,`, 1)
	if patched == string(data) {
		t.Fatal(`前置失败：未在 bundle 中找到 "schema_version": 1 占位`)
	}
	if err := os.WriteFile(bundlePath, []byte(patched), 0644); err != nil {
		t.Fatalf(`write patched bundle: %v`, err)
	}

	dirB := switchMachine(t, `b-sid-schema`)
	out, _, code := runForge(t, dirB, `task`, `import`, `--file`, bundlePath)
	if code == 0 {
		t.Fatalf(`schema_version=0 应被拒（非 0 退出），got exit 0: %s`, out)
	}
	if !strings.Contains(out, `不被支持`) {
		t.Errorf(`拒绝信息应含「不被支持」, got: %s`, out)
	}
}

// TestTaskImport_StripsForeignGateSignals: imported review/acceptance/score signals are FOREIGN evidence
// and must NOT be honored locally — otherwise a hand-edited bundle could satisfy the task-complete gate's
// hard prerequisites without the review sub-agent / verify-acceptance ever running on this machine. The
// import lands the task with these signals cleared; History (gate progression, provenance) is kept.
//
// TestTaskImport_StripsForeignGateSignals：导入的 review/验收/评分信号是外来证据，本机不得采信——否则
// 手改的 bundle 可绕过 task-complete 门禁的硬前置而本机从未跑过 review 子 agent / verify-acceptance。
// import 落地时这些信号被清空；History（门禁进度，溯源）保留。
func TestTaskImport_StripsForeignGateSignals(t *testing.T) {
	dirA := setupDelegateProject(t)
	runForge(t, dirA, `task`, `start`, `--ref`, `feat/delegate`, `--title`, `trusted-on-A`)
	// On A: fake a fully-reviewed + scored + acceptance-passed task (a foreign bundle could carry these).
	src, err := taskpipeline.LoadTaskState(dirA, `feat/delegate`)
	if err != nil {
		t.Fatalf(`load A: %v`, err)
	}
	src.MarkReviewPassed(`aaa111`, `hash-aaa`)
	src.Score = &scoringtypes.ScoreResult{Overall: 91}
	src.Acceptance = []taskpipeline.AcceptanceCriterion{
		{Run: `go test ./...`, Expected: `ok`, Passed: true, AcceptedHeadCommit: `aaa111`, Output: `ok`},
	}
	if err := taskpipeline.SaveTaskState(dirA, src); err != nil {
		t.Fatalf(`save A: %v`, err)
	}
	bundlePath := filepath.Join(t.TempDir(), `b.json`)
	if out, _, code := runForge(t, dirA, `task`, `export`, `--ref`, `feat/delegate`, `-o`, bundlePath); code != 0 {
		t.Fatalf(`A export: %s`, out)
	}

	dirB := switchMachine(t, `b-sid-trust`)
	if out, _, code := runForge(t, dirB, `task`, `import`, `--file`, bundlePath); code != 0 {
		t.Fatalf(`B import: %s`, out)
	}
	got, err := taskpipeline.LoadTaskState(dirB, `feat/delegate`)
	if err != nil {
		t.Fatalf(`B load: %v`, err)
	}
	if got.ReviewPassed {
		t.Error(`导入不得继承外来 ReviewPassed（本机须重跑 review）`)
	}
	if got.ReviewedHeadCommit != `` || got.ReviewedChangeHash != `` {
		t.Errorf(`外来 review 快照应清空, got HeadCommit=%q ChangeHash=%q`, got.ReviewedHeadCommit, got.ReviewedChangeHash)
	}
	if got.Score != nil {
		t.Errorf(`外来 Score 应清空（本机 complete 时重算）, got %+v`, got.Score)
	}
	if len(got.Acceptance) == 0 {
		t.Fatal(`Acceptance 结构应保留（只清信号，不删条目）`)
	}
	if got.Acceptance[0].Passed || got.Acceptance[0].AcceptedHeadCommit != `` || got.Acceptance[0].Output != `` {
		t.Errorf(`Acceptance 信号应清空（Passed/AcceptedHeadCommit/Output），got %+v`, got.Acceptance[0])
	}
	// Run/Expected preserved (they are the spec, not a trust signal) — only the pass-result is foreign.
	if got.Acceptance[0].Run != `go test ./...` {
		t.Errorf(`Acceptance.Run 是 spec 不应清, got %q`, got.Acceptance[0].Run)
	}
}

// TestUnionDecisions_EmptyIDNotCollapsed: a malformed bundle whose decisions carry empty IDs must NOT
// have them collapsed into one by the ID-keyed union (silent data loss). Empty-ID entries are appended
// as-is; non-empty duplicates are still deduped.
//
// TestUnionDecisions_EmptyIDNotCollapsed：决策带空 ID 的畸形 bundle 不能被按 ID 的并集压成一条（静默
// 丢数据）。空 ID 条目原样追加；非空重复仍去重。
func TestUnionDecisions_EmptyIDNotCollapsed(t *testing.T) {
	local := []taskpipeline.Decision{{ID: `d-1`, Content: `local`}}
	incoming := []taskpipeline.Decision{
		{ID: ``, Content: `malformed-A`},
		{ID: ``, Content: `malformed-B`},
		{ID: `d-1`, Content: `dup-of-local`},
	}
	got := unionDecisions(local, incoming)
	if len(got) != 3 {
		t.Fatalf(`空 ID 不应折叠：期望 3 条（local + 2 空 ID），got %d: %+v`, len(got), got)
	}
	contents := map[string]bool{}
	for _, d := range got {
		contents[d.Content] = true
	}
	if !contents[`malformed-A`] || !contents[`malformed-B`] || !contents[`local`] {
		t.Errorf(`空 ID 条目应都保留 + local 保留, got %v`, contents)
	}
	if contents[`dup-of-local`] {
		t.Error(`非空重复 ID（d-1）应去重掉`)
	}
}

// TestFilterImportedChecklog_DropsDuplicates: a repeated --merge import of the same bundle must not
// duplicate checklog evidence lines in `forge trace`. filterImportedChecklog drops entries already on
// disk (keyed on the stable composite identity), keeping only genuinely-new entries.
//
// TestFilterImportedChecklog_DropsDuplicates：重复 --merge import 同一 bundle 不得在 forge trace 重复
// 证据行。filterImportedChecklog 丢弃盘上已有条目（按稳定复合身份去重），只留真正新条目。
func TestFilterImportedChecklog_DropsDuplicates(t *testing.T) {
	root := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	ref := `feat/x`
	base := time.Unix(5000, 0).UTC()
	existing := []checklog.Entry{
		{Check: `compile`, TaskRef: ref, RecordedAt: base, Detail: `dup-line`, SessionID: `s1`, ToolName: `claude-code`},
		{Check: `test`, TaskRef: ref, RecordedAt: base.Add(time.Second), Detail: `local-only`, SessionID: `s1`, ToolName: `claude-code`},
	}
	if err := checklog.AppendEntries(root, existing); err != nil {
		t.Fatalf(`seed existing: %v`, err)
	}
	incoming := []checklog.Entry{
		existing[0], // 与盘上完全一致 → 丢弃
		{Check: `test`, TaskRef: ref, RecordedAt: base.Add(2 * time.Second), Detail: `fresh-remote`, SessionID: `s2`, ToolName: `kimi`},
	}
	got, err := filterImportedChecklog(root, ref, incoming)
	if err != nil {
		t.Fatalf(`filter: %v`, err)
	}
	if len(got) != 1 {
		t.Fatalf(`去重后应 1 条（dup 丢、fresh-remote 留），got %d: %+v`, len(got), got)
	}
	if got[0].Detail != `fresh-remote` {
		t.Errorf(`保留的应是 fresh-remote, got %q`, got[0].Detail)
	}
}
