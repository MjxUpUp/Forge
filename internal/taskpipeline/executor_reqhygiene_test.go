package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// executor_reqhygiene_test.go — 钉住 RunReqHygiene 的 verify 接线（63a9457 落地特性
// 后入口函数零调用方——deadcode 实锤的 BUG-1 形态，本测试防接线再次断裂：接线一断
// 测试即红，不再依赖 deadcode canary）。

// findReqHygieneEntry 在 checklog 里找 CheckNameReqHygiene 条目（指针，便于读字段）。
func findReqHygieneEntry(t *testing.T, dir string) *checklog.Entry {
	t.Helper()
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	for i := range entries {
		if entries[i].Check == CheckNameReqHygiene {
			return &entries[i]
		}
	}
	return nil
}

// seedSpecArtifact 把 spec 文件写到 DataDir 下并返回其 ArtifactRef（Path 相对 DataDir，
// 与 artifactAbsPath 的解析规则一致）。
func seedSpecArtifact(t *testing.T, dir, rel, body string) ArtifactRef {
	t.Helper()
	abs := filepath.Join(forgedata.DataDirFor(dir), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(abs), err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	return ArtifactRef{Path: rel, Hash: "deadbeefdeadbeef", UpdatedAt: time.Now()}
}

// TestExecuteTaskGate_ReqHygiene_RecordsAdvisory core contract: a registered spec artifact
// containing ambiguity markers → task-verify records a CheckNameReqHygiene entry at advisory
// level (Passed=true, never blocks), and the gate itself still PASSES.
//
// TestExecuteTaskGate_ReqHygiene_RecordsAdvisory 核心契约：登记的 spec 产物含歧义标记 →
// task-verify 记一条 CheckNameReqHygiene（Level=advisory、Passed=true——advisory 永不
// 阻断），且 gate 照常 PASS。接线断则本测试红（RunReqHygiene 零调用的 BUG-1 回归）。
func TestExecuteTaskGate_ReqHygiene_RecordsAdvisory(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"prod.go": "package main\n\nfunc main() {}\n",
	}, "add prod")

	state := newVerifyState(t, dir, "reqhyg-adv")
	state.SpecArtifacts = map[string]ArtifactRef{}
	state.SpecArtifacts["spec"] = seedSpecArtifact(t, dir, "specs/reqhyg/spec.md",
		"# spec\n\n登录行为待定，可能支持若干种方式。\n")

	captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
			t.Fatalf(`task-verify 应 PASS（req-hygiene 是 advisory）: %v`, err)
		}
	})

	rec := findReqHygieneEntry(t, dir)
	if rec == nil {
		t.Fatal(`CheckNameReqHygiene 条目未记录——RunReqHygiene 未接线进 task-verify`)
	}
	if rec.Level != checklog.LevelAdvisory {
		t.Errorf(`含歧义标记应 Level=advisory, got %s (Detail=%q)`, rec.Level, rec.Detail)
	}
	if !rec.Passed {
		t.Errorf(`advisory 语义应 Passed=true（不阻断）`)
	}
	if !strings.Contains(rec.Detail, "需求卫生") || !strings.Contains(rec.Detail, "歧义标记") {
		t.Errorf(`Detail 应含扫描结论摘要: %q`, rec.Detail)
	}
}

// TestExecuteTaskGate_ReqHygiene_CleanSpecPass: spec without markers + acceptance criteria
// present → advisory row still recorded, at pass level (scanned-and-clean is traceable).
//
// TestExecuteTaskGate_ReqHygiene_CleanSpecPass：无歧义标记的 spec + 已有验收标准 →
// 仍记条目但 Level=pass（「扫过、干净」可在 trace 追溯）。
func TestExecuteTaskGate_ReqHygiene_CleanSpecPass(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"prod.go": "package main\n\nfunc main() {}\n",
	}, "add prod")

	state := newVerifyState(t, dir, "reqhyg-clean")
	state.SpecArtifacts = map[string]ArtifactRef{}
	state.SpecArtifacts["spec"] = seedSpecArtifact(t, dir, "specs/clean/spec.md",
		"# spec\n\n登录页支持手机号验证码登录，错误提示保留 3 秒。\n")
	state.Acceptance = []AcceptanceCriterion{{Run: "打开登录页", Expected: "可输入手机号"}}

	captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
			t.Fatalf(`task-verify 应 PASS: %v`, err)
		}
	})

	rec := findReqHygieneEntry(t, dir)
	if rec == nil {
		t.Fatal(`有 spec 产物就应记 CheckNameReqHygiene（扫过可见）`)
	}
	if rec.Level != checklog.LevelPass {
		t.Errorf(`干净 spec 应 Level=pass, got %s (Detail=%q)`, rec.Level, rec.Detail)
	}
}

// TestExecuteTaskGate_ReqHygiene_NoSpecSilent: tasks without a registered spec artifact
// record NO req-hygiene row — silence-when-inapplicable follows the conventions-lint
// precedent (checklog signal density over completeness noise).
//
// TestExecuteTaskGate_ReqHygiene_NoSpecSilent：未登记 spec 产物的任务不记 req-hygiene
// 条目——检查对象不存在时保持静默，沿 conventions-lint 先例（信号密度优先于完整性噪音）。
func TestExecuteTaskGate_ReqHygiene_NoSpecSilent(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"prod.go": "package main\n\nfunc main() {}\n",
	}, "add prod")

	state := newVerifyState(t, dir, "reqhyg-silent")
	captureStderr(t, func() {
		if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
			t.Fatalf(`task-verify 应 PASS: %v`, err)
		}
	})

	if rec := findReqHygieneEntry(t, dir); rec != nil {
		t.Errorf(`无 spec 产物不应记 CheckNameReqHygiene: %+v`, rec)
	}
}

// TestReqCleanComments_NoDebtSelfHit pins the self-scan fix: reqclean.go's own comments
// must not spell debt-marker words verbatim — cheat-scan scans this repo's own source at
// task-verify, and the pattern-listing comments previously self-hit comment-as-debt twice
// (2026-09-14 req-hygiene-gate task: cheat-scan fail was scanner noise, not real debt).
//
// TestReqCleanComments_NoDebtSelfHit 钉住自噬修复：reqclean.go 自身注释不得连写债务
// 标记词——cheat-scan 在 task-verify 扫本仓源码，模式表注释此前自噬 comment-as-debt
// 两次（2026-09-14 req-hygiene-gate 任务的 cheat-scan fail 是扫描器噪音而非真债务）。
func TestReqCleanComments_NoDebtSelfHit(t *testing.T) {
	body, err := os.ReadFile("reqclean.go")
	if err != nil {
		t.Fatalf(`read reqclean.go: %v`, err)
	}
	for i, line := range strings.Split(string(body), "\n") {
		if !isCommentOrBlank(line) {
			continue
		}
		if commentDebtRe.MatchString(line) {
			t.Errorf(`reqclean.go:%d 注释连写了债务标记词（自噬误报源）: %s`, i+1, line)
		}
	}
}
