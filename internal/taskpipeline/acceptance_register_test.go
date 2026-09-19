package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// acceptance_register_test.go — oracle-pipeline L1 的行为钉：层级盖章的一次性、
// complete 登记门的阻断/豁免/逃生三态、conventions 兜底的档案消费、
// RegisterAcceptance 的锁内合并与去重、HeldoutRegistered 探针。

// TestStampAcceptanceSource_WriteOnce: stamping fills empty Sources only — an
// existing tier is never relabeled (write-once; relabeling manual→start would
// launder the weakest tier into the strongest).
//
// TestStampAcceptanceSource_WriteOnce：盖章只填空 Source——已有层级绝不改写
// （一次盖章；把 manual 改标 start 等于把最弱层洗成最强层）。
func TestStampAcceptanceSource_WriteOnce(t *testing.T) {
	cs := []AcceptanceCriterion{
		{Run: "a"}, // 空 → 盖
		{Run: "b", Source: AcceptanceSourceManual}, // 已盖 manual → 保留
	}
	StampAcceptanceSource(cs, AcceptanceSourceStart)
	if cs[0].Source != AcceptanceSourceStart {
		t.Errorf(`空 Source 应盖成 start，got %q`, cs[0].Source)
	}
	if cs[1].Source != AcceptanceSourceManual {
		t.Errorf(`已有 manual 层不得被改写为 start（洗层），got %q`, cs[1].Source)
	}
}

// TestCheckAcceptanceRegistered_BlockOnZero: non-generic task with zero criteria
// → blocked with the exam-missing reason. This is the 2026-09-07 gap (ratio 0.08
// completion) being closed — the core assertion of L1.
//
// TestCheckAcceptanceRegistered_BlockOnZero：非 generic 任务零标准 → 阻断并给出
// 考卷缺位理由。这是 2026-09-07 缺口（ratio 0.08 完成）的关闭断言——L1 的核心。
func TestCheckAcceptanceRegistered_BlockOnZero(t *testing.T) {
	dir := t.TempDir()
	state := &TaskState{TaskRef: "zero-acc"}
	ok, reasons := CheckAcceptanceRegistered(dir, state)
	if ok {
		t.Fatal(`非 generic 任务零验收标准应被阻断（complete 硬前置）`)
	}
	if len(reasons) == 0 {
		t.Fatal(`阻断须带 reasons（BLOCKED 文案消费）`)
	}
}

// TestCheckAcceptanceRegistered_PassCases: generic tasks and tasks with ≥1
// criterion pass; nil state is a defensive pass.
//
// TestCheckAcceptanceRegistered_PassCases：generic 任务与有标准任务放行；nil
// state 防御性放行。
func TestCheckAcceptanceRegistered_PassCases(t *testing.T) {
	dir := t.TempDir()
	if ok, _ := CheckAcceptanceRegistered(dir, nil); !ok {
		t.Error(`nil state 应防御性放行`)
	}
	generic := &TaskState{TaskRef: "g", Kind: TaskKindGeneric}
	if ok, reasons := CheckAcceptanceRegistered(dir, generic); !ok {
		t.Errorf(`generic 任务豁免（不走代码门禁），got blocked: %v`, reasons)
	}
	with := &TaskState{TaskRef: "w", Acceptance: []AcceptanceCriterion{{Run: "go build ./..."}}}
	if ok, reasons := CheckAcceptanceRegistered(dir, with); !ok {
		t.Errorf(`有 ≥1 条标准应放行，got blocked: %v`, reasons)
	}
}

// TestCheckAcceptanceRegistered_EscapeAudited: the acceptance-gate escape
// (per-task override) passes the registration gate but leaves the audited
// escape-hatch row — bypassing is never silent.
//
// TestCheckAcceptanceRegistered_EscapeAudited：acceptance-gate 逃生（per-task
// override）放行登记门但落逃生舱审计行——绕过绝不静默。
func TestCheckAcceptanceRegistered_EscapeAudited(t *testing.T) {
	dir := t.TempDir()
	state := &TaskState{TaskRef: "esc-acc"}
	state.Overrides.AcceptanceGate = "disable"
	ok, reasons := CheckAcceptanceRegistered(dir, state)
	if !ok {
		t.Fatalf(`逃生应放行, reasons: %v`, reasons)
	}
	entries, err := checklog.LoadForTask(dir, state.TaskRef)
	if err != nil {
		t.Fatalf(`LoadForTask: %v`, err)
	}
	found := false
	for _, e := range entries {
		if e.Check == checklog.CheckEscapeHatch && checklog.EscapeGateOf(&e) == "acceptance-gate" {
			found = true
		}
	}
	if !found {
		t.Fatal(`逃生放行须落 acceptance-gate 的 CheckEscapeHatch 审计行`)
	}
}

// TestConventionsDefaultAcceptance_FromProfile: a conventions profile with
// build/test/lint commands yields exactly those criteria, stamped conventions
// (Expected empty = exit-code-0 checks). No profile → nil (caller keeps old
// zero-criteria behavior).
//
// TestConventionsDefaultAcceptance_FromProfile：档案含 build/test/lint 命令 →
// 恰好产出这三条标准，盖 conventions 层（Expected 空 = 只看退出码 0）。无档案
// → nil（调用方保持旧的零标准行为）。
func TestConventionsDefaultAcceptance_FromProfile(t *testing.T) {
	dir := t.TempDir()
	if got, err := ConventionsDefaultAcceptance(dir); got != nil || err != nil {
		t.Fatalf(`无档案应返回 (nil, nil)，got (%v, %v)`, got, err)
	}
	profDir := filepath.Join(forgedata.DataDirFor(dir), "conventions")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatalf(`mkdir: %v`, err)
	}
	body := `{"version":1,"stack":"go","lint":"gofmt -l .","test":"go test ./...","build":"go build ./..."}`
	if err := os.WriteFile(filepath.Join(profDir, "profile.json"), []byte(body), 0o644); err != nil {
		t.Fatalf(`write profile: %v`, err)
	}
	got, err := ConventionsDefaultAcceptance(dir)
	if err != nil {
		t.Fatalf(`有效档案不应报错: %v`, err)
	}
	if len(got) != 3 {
		t.Fatalf(`应产出 build/test/lint 三条，got %d 条: %v`, len(got), got)
	}
	for i, want := range []string{"go build ./...", "go test ./...", "gofmt -l ."} {
		if got[i].Run != want {
			t.Errorf(`第 %d 条应为 %q（build→test→lint 顺序），got %q`, i+1, want, got[i].Run)
		}
		if got[i].Source != AcceptanceSourceConventions {
			t.Errorf(`第 %d 条应盖 conventions 层，got %q`, i+1, got[i].Source)
		}
		if got[i].Expected != "" {
			t.Errorf(`兜底套件 Expected 应为空（退出码 0 判定），got %q`, got[i].Expected)
		}
	}
	// 档案在但损坏 → (nil, err)：调用方不得把读失败说成「无档案」（审查 P2-1）。
	if err := os.WriteFile(filepath.Join(profDir, "profile.json"), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ConventionsDefaultAcceptance(dir); got != nil || err == nil {
		t.Errorf(`损坏档案应返回 (nil, err)，got (%v, %v)`, got, err)
	}
}

// TestRegisterAcceptance_MergeAndTier: registering under the lock merges by-Run
// dedup; a re-registered existing command keeps its ORIGINAL tier (write-once
// at the first registration point).
//
// TestRegisterAcceptance_MergeAndTier：锁内登记按 Run 去重合并；重复登记已有
// 命令保留其原层级（首次登记点一次盖章）。
func TestRegisterAcceptance_MergeAndTier(t *testing.T) {
	dir := t.TempDir()
	state := &TaskState{TaskRef: "reg-acc", Branch: "feat/x"}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatalf(`SaveTaskState: %v`, err)
	}
	added, err := RegisterAcceptance(dir, state.TaskRef,
		[]AcceptanceCriterion{{Run: "go build ./..."}, {Run: "go vet ./..."}},
		AcceptanceSourceStart)
	if err != nil {
		t.Fatalf(`RegisterAcceptance: %v`, err)
	}
	if len(added) != 2 {
		t.Fatalf(`首次登记应新增 2 条，got %d`, len(added))
	}
	// 同命令以 manual 层重登：去重（0 新增），原 start 层保留。
	added2, err := RegisterAcceptance(dir, state.TaskRef,
		[]AcceptanceCriterion{{Run: "go build ./...", Source: AcceptanceSourceManual}},
		AcceptanceSourceManual)
	if err != nil {
		t.Fatalf(`RegisterAcceptance(2): %v`, err)
	}
	if len(added2) != 0 {
		t.Errorf(`重复命令应去重（0 新增），got %d`, len(added2))
	}
	reloaded, err := LoadTaskState(dir, state.TaskRef)
	if err != nil {
		t.Fatalf(`LoadTaskState: %v`, err)
	}
	for _, c := range reloaded.Acceptance {
		if c.Run == "go build ./..." && c.Source != AcceptanceSourceStart {
			t.Errorf(`原 start 层不得被 manual 重登洗层，got %q`, c.Source)
		}
	}
}

// TestAcceptanceTierHelpers: FormatAcceptanceTier maps legacy empty to start;
// tier counts aggregate; manual-tier presence is detected.
//
// TestAcceptanceTierHelpers：FormatAcceptanceTier 把存量空层映射为 start；
// 层级计数聚合；manual 层存在可探测。
func TestAcceptanceTierHelpers(t *testing.T) {
	if got := FormatAcceptanceTier(""); got != AcceptanceSourceStart {
		t.Errorf(`空 Source（存量）应渲染为 start，got %q`, got)
	}
	cs := []AcceptanceCriterion{
		{Run: "a", Source: AcceptanceSourceStart},
		{Run: "b"},
		{Run: "c", Source: AcceptanceSourceManual},
	}
	counts := AcceptanceTierCounts(cs)
	if counts[AcceptanceSourceStart] != 2 || counts[AcceptanceSourceManual] != 1 {
		t.Errorf(`层级计数应 start=2（含存量空层）、manual=1，got %v`, counts)
	}
	if !HasManualTierAcceptance(cs) {
		t.Error(`含 manual 层标准应被探测到（验收单披露触发器）`)
	}
	if HasManualTierAcceptance(cs[:2]) {
		t.Error(`无 manual 层时不应误报`)
	}
}

// TestHeldoutRegistered_Probe: SaveHeldout makes the sidecar visible to the
// read-only probe (report disclosure); absent sidecar reports false.
//
// TestHeldoutRegistered_Probe：SaveHeldout 后只读探针可见（验收单披露）；
// 无侧车报 false。
func TestHeldoutRegistered_Probe(t *testing.T) {
	dir := t.TempDir()
	if held, err := HeldoutRegistered(dir, "probe-ref"); held || err != nil {
		t.Fatalf(`未登记 held-out 时探针应报 (false, nil)，got (%v, %v)`, held, err)
	}
	if err := SaveHeldout(dir, "probe-ref", []AcceptanceCriterion{{Run: "go version"}}); err != nil {
		t.Fatalf(`SaveHeldout: %v`, err)
	}
	if held, err := HeldoutRegistered(dir, "probe-ref"); !held || err != nil {
		t.Fatalf(`登记后探针应报 (true, nil)，got (%v, %v)`, held, err)
	}
}

// TestStripForeignGateSignals_NormalizesAcceptanceTier (review P1-1): foreign
// tier claims are untrusted input — imported criteria are relabeled manual so
// the delivery report discloses them as post-hoc, and a forged spec-extract/
// start label cannot launder exam independence; a forged omission cannot
// suppress the manual-tier disclosure trigger.
//
// TestStripForeignGateSignals_NormalizesAcceptanceTier（审查 P1-1）：外来 tier
// 声明是不可信输入——导入标准一律改标 manual，验收单按事后补登披露；伪造的
// spec-extract/start 标签洗不了考卷独立性，匿去 manual 也压不住披露触发器。
func TestStripForeignGateSignals_NormalizesAcceptanceTier(t *testing.T) {
	s := &TaskState{TaskRef: "imp", Acceptance: []AcceptanceCriterion{
		{Run: "a", Source: AcceptanceSourceSpecExtract, Passed: true, AcceptedHeadCommit: "x"},
		{Run: "b"},
	}}
	StripForeignGateSignals(s)
	for i, c := range s.Acceptance {
		if c.Source != AcceptanceSourceManual {
			t.Errorf(`第 %d 条外来标准应归一为 manual 层，got %q`, i+1, c.Source)
		}
	}
	if s.Acceptance[0].Passed || s.Acceptance[0].AcceptedHeadCommit != "" {
		t.Error(`结果字段照旧剥离（层级归一不放松既有清洗）`)
	}
	if !HasManualTierAcceptance(s.Acceptance) {
		t.Error(`归一后披露触发器必须命中（验收单的 ⚠ manual 层告警）`)
	}
	if !s.AcceptanceForeign {
		t.Error(`外来标记应随非空验收集置位（首跑 --trust-foreign 门）`)
	}
}

// TestRegisterAcceptance_RejectsCompleted (review P1-2): the exam is finalized
// at delivery — the lock closure refuses late registration on completed tasks
// (kills the TOCTOU window between an outside check and the merge).
//
// TestRegisterAcceptance_RejectsCompleted（审查 P1-2）：考卷在交付时定稿——
// 锁内闭包拒绝已完成任务的补登（封掉外部检查与合并之间的竞态窗口）。
func TestRegisterAcceptance_RejectsCompleted(t *testing.T) {
	dir := t.TempDir()
	state := &TaskState{TaskRef: "done-acc", Branch: "feat/x"}
	state.MarkComplete()
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatalf(`SaveTaskState: %v`, err)
	}
	added, err := RegisterAcceptance(dir, state.TaskRef,
		[]AcceptanceCriterion{{Run: "go version"}}, AcceptanceSourceManual)
	if err == nil || !strings.Contains(err.Error(), "已完成") {
		t.Fatalf(`已完成任务的补登应在锁内被拒, got (%v, %v)`, added, err)
	}
	reloaded, lerr := LoadTaskState(dir, state.TaskRef)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(reloaded.Acceptance) != 0 {
		t.Errorf(`被拒的登记不得落盘，got %d 条`, len(reloaded.Acceptance))
	}
}
