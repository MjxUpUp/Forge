package review

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// Git integration tests use real temporary repos (t.TempDir + git init). The review package is a diff/stamp
// state machine; mocks cannot verify assertions like 'git diff really excludes .forge' or 'pure docs really do
// not trigger' — git must run end-to-end. Requires git available (CI and local).
//
// git 集成测试用真实临时仓库（t.TempDir + git init）。review 包的核心是 diff/stamp
// 状态机，单靠 mock 验证不了「git diff 真的排除了 .forge」「纯文档真不触发」这些断言——
// 必须端到端跑 git。环境要求 git 可用（CI 与本地均有）。

// gitEnv provides a GPG-free, fixed-identity git environment to avoid commit failures in a brand-new repo.
//
// gitEnv 提供无 GPG、固定身份的 git 环境，避免 commit 在全新仓库失败。
var gitEnv = append(os.Environ(),
	"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
	"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // isolate DataDir from real ~/.forge (refactor-data-home)
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	// Windows defaults to master; no need to force-rename the branch
	//
	// Windows 默认 master，无需强改分支名
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "init")
	return dir
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestIsSourceCode uses table-driven proof for the extension whitelist + generated-file exclusion — the basis for false-trigger protection.
//
// TestIsSourceCode 表驱动证明扩展名白名单 + 生成物排除——误触发防护的判定基础。
func TestIsSourceCode(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"main.go", true},
		{"src/app.ts", true},
		{"lib.py", true},
		{"cmd/run.rs", true},
		{"scripts/build.sh", true},
		{"README.md", false}, // 文档不审
		{"docs/guide.md", false},
		{".forge/pipeline.yml", false}, // .forge 自身（yml 也非源码）
		{"config.json", false},         // 配置不审
		{"Cargo.toml", false},
		{"foo.gen.go", false},            // 生成物：扩展是 go 但路径含 .gen.
		{"bar_generated_test.go", false}, // 生成物：_generated
		{"baz.pb.go", false},             // protobuf 生成
		{"vendor/lib.go", false},
		{"node_modules/x.js", false},
		{"image.png", false},
		{"style.css", false},
		{"Makefile", false}, // 无扩展名不在白名单
	}
	for _, tc := range cases {
		if got := isSourceCode(tc.path); got != tc.want {
			t.Errorf("isSourceCode(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestEvaluate_NoSourceChanges_PureDocs false-trigger protection #2: pure-doc changes do not trigger review.
// Sessions that edit README or write memory should not be forced into code review.
//
// TestEvaluate_NoSourceChanges_PureDocs 误触发防护 #2：纯文档变更不触发审查。
// 改 README/写 memory 这种会话不该被逼去审代码。
func TestEvaluate_NoSourceChanges_PureDocs(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "README.md", "# 改了文档\n")
	write(t, dir, "docs/notes.md", "笔记\n")

	dec, reason, err := Evaluate(dir)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec != DecisionPass {
		t.Fatalf("纯文档变更应 Pass（无需审），实际 %v（%s）——误触发", dec, reason)
	}
}

// TestEvaluate_NoSourceChanges_Generated false-trigger protection #3: generated-file changes do not trigger review.
// Naming convention note: the generated-file blacklist is .gen./_generated/.pb. (standard markers).
// A bare _gen (e.g. model_gen.go) does not count as generated and is reviewed as source — this is intended (prevents fuzzy names from escaping review),
// so this test only uses the standard marker .pb.go to verify exclusion works.
//
// TestEvaluate_NoSourceChanges_Generated 误触发防护 #3：生成物变更不触发审查。
// 命名约定说明：生成物黑名单是 .gen./_generated/.pb.（标准标记）。
// 单个 _gen（如 model_gen.go）不算生成物会被当源码审——这是预期（防用模糊命名逃审），
// 故本测试只用标准标记 .pb.go 验证排除生效。
func TestEvaluate_NoSourceChanges_Generated(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "api.pb.go", "// generated\n")
	write(t, dir, "real.pb.go", "// x\n")
	dec, _, err := Evaluate(dir)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec != DecisionPass {
		t.Fatalf("生成物(.pb.go)变更应 Pass，实际 %v", dec)
	}
}

// TestEvaluate_SourceChangeTriggersReview source changes (untracked new files) trigger review.
//
// TestEvaluate_SourceChangeTriggersReview 源码变更（untracked 新文件）触发审查。
func TestEvaluate_SourceChangeTriggersReview(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "main.go", "package main\n")

	dec, reason, err := Evaluate(dir)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec != DecisionNeedReview {
		t.Fatalf("源码变更应 NeedReview，实际 %v（%s）", dec, reason)
	}
}

// TestEvaluate_TrackedSourceChange modifying a committed source file (tracked diff) also triggers.
//
// TestEvaluate_TrackedSourceChange 修改已提交的源码文件（tracked diff）也触发。
func TestEvaluate_TrackedSourceChange(t *testing.T) {
	dir := initGitRepo(t)
	// First commit a source file
	//
	// 先提交一个源码文件
	write(t, dir, "svc.go", "package svc\n")
	must := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	must("add", "-A")
	must("commit", "-q", "-m", "add svc")

	// Modify it → tracked diff
	//
	// 修改它 → tracked diff
	write(t, dir, "svc.go", "package svc\n\nfunc New() {}\n")
	dec, _, err := Evaluate(dir)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec != DecisionNeedReview {
		t.Fatalf("tracked 源码修改应 NeedReview，实际 %v", dec)
	}
}

// TestEvaluate_PassThenSameDiffPasses review loop: after MarkPassed the same diff → Pass.
//
// TestEvaluate_PassThenSameDiffPasses 审查闭环：MarkPassed 后同一 diff → Pass。
func TestEvaluate_PassThenSameDiffPasses(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "a.go", "package a\n")

	if dec, _, _ := Evaluate(dir); dec != DecisionNeedReview {
		t.Fatalf("首次应 NeedReview，实际 %v", dec)
	}
	if err := MarkPassed(dir); err != nil {
		t.Fatalf("MarkPassed: %v", err)
	}
	dec, reason, err := Evaluate(dir)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec != DecisionPass {
		t.Fatalf("pass 后同 diff 应 Pass，实际 %v（%s）", dec, reason)
	}
}

// TestMarkPassedWithNote pins the non-task-mode half of `forge review pass --note`:
// the reviewer conclusion is persisted on the branch stamp (the task-mode counterpart
// is ReviewRound.Note); plain MarkPassed leaves it empty (backward compatible).
//
// TestMarkPassedWithNote 钉住 `forge review pass --note` 的非 task 模式半边：审查结论
// 持久化进分支 stamp（task 模式对应物是 ReviewRound.Note）；裸 MarkPassed 保持为空
// （向后兼容）。
func TestMarkPassedWithNote(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "a.go", "package a\n")

	if err := MarkPassedWithNote(dir, "审查结论：无发现"); err != nil {
		t.Fatalf("MarkPassedWithNote: %v", err)
	}
	if got := loadStamp(dir).Note; got != "审查结论：无发现" {
		t.Errorf("stamp.Note 未持久化, got %q", got)
	}

	// Plain MarkPassed keeps the note empty (legacy shape).
	//
	// 裸 MarkPassed 保持 note 为空（旧形状）。
	if err := MarkPassed(dir); err != nil {
		t.Fatalf("MarkPassed: %v", err)
	}
	if got := loadStamp(dir).Note; got != "" {
		t.Errorf("裸 MarkPassed 后 stamp.Note 应为空, got %q", got)
	}
}

// TestEvaluate_NewDiffReTriggers a new source diff (hash changed) re-triggers review — prevents 'review once then keep changing without re-review'.
//
// TestEvaluate_NewDiffReTriggers 新的源码 diff（hash 变）重新触发审查——防「审完继续改不重审」。
func TestEvaluate_NewDiffReTriggers(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "a.go", "package a\n")
	if err := MarkPassed(dir); err != nil {
		t.Fatal(err)
	}
	// Write new content → new hash
	//
	// 改出新内容 → 新 hash
	write(t, dir, "a.go", "package a\n\nfunc F() {}\n")
	write(t, dir, "b.go", "package a\n")
	dec, _, err := Evaluate(dir)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec != DecisionNeedReview {
		t.Fatalf("新 diff 应重新 NeedReview，实际 %v", dec)
	}
}

// TestEvaluate_CrossBranchSameHashPasses scope-portability: a diff reviewed & stamped on one branch
// must still pass on another branch when the content (hence diff hash) is identical — the fast-forward
// merge + checkout case (block 4 in the 2026-08-06 cooking session). Before the fix the stamp was
// branch-scoped, so master re-blocked byte-identical already-reviewed code: a false positive. Safe
// because identical content shares a hash; differing content has a differing hash and still needs review.
//
// TestEvaluate_CrossBranchSameHashPasses scope 可移植性：在一个分支审查并打过戳的 diff，切到另一
// 分支内容（因而 diff hash）一致时仍应放行——ff-merge + checkout 场景（2026-08-06 cooking 会话的
// 拦截4）。修复前戳按分支存，master 会重新 block 字节级一致的已审代码：假阳性。安全是因为内容
// 一致则 hash 一致；内容不同则 hash 不同，仍需审。
func TestEvaluate_CrossBranchSameHashPasses(t *testing.T) {
	dir := initGitRepo(t)
	defaultBranch := currentGitBranch(t, dir)
	write(t, dir, "a.go", "package a\n")

	// Create a feature branch (same HEAD, a.go untracked) and mark the diff reviewed there.
	//
	// 建特性分支（同一 HEAD，a.go 仍 untracked）并在其上标记审查通过。
	gitCheckout(t, dir, "checkout", "-b", "feat/x")
	if err := MarkPassed(dir); err != nil {
		t.Fatalf("MarkPassed: %v", err)
	}

	// Switch back to the default branch (same HEAD, a.go still untracked → identical hash).
	// No stamp exists for the default branch; the pass must come from feat/x's stamp.
	//
	// 切回默认分支（同一 HEAD，a.go 仍 untracked → 同一 hash）。默认分支无戳，
	// 放行必须来自 feat/x 的戳。
	gitCheckout(t, dir, "checkout", defaultBranch)
	dec, reason, err := Evaluate(dir)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec != DecisionPass {
		t.Fatalf("跨分支同 hash 应 Pass（已在他分支审查），实际 %v（%s）——ff-merge 后误拦", dec, reason)
	}
}

// TestEvaluate_CrossBranchDifferentHashStillNeedsReview guardrail: cross-branch portability must not
// mask a genuinely different diff. A reviewed stamp exists for content X on feat/x, but the default
// branch holds different content (hash Y) → still NeedReview (no false pass).
//
// TestEvaluate_CrossBranchDifferentHashStillNeedsReview 护栏：跨分支可移植不能掩盖真正不同的 diff。
// feat/x 上有内容 X 的已审戳，但默认分支是不同内容（hash Y）→ 仍 NeedReview（不假放行）。
func TestEvaluate_CrossBranchDifferentHashStillNeedsReview(t *testing.T) {
	dir := initGitRepo(t)
	defaultBranch := currentGitBranch(t, dir)
	write(t, dir, "a.go", "package a\n")
	gitCheckout(t, dir, "checkout", "-b", "feat/x")
	if err := MarkPassed(dir); err != nil {
		t.Fatalf("MarkPassed: %v", err)
	}
	gitCheckout(t, dir, "checkout", defaultBranch)
	// Different content on the default branch → different hash, no reviewed stamp matches → NeedReview.
	//
	// 默认分支上内容不同 → hash 不同，无已审戳命中 → NeedReview。
	write(t, dir, "a.go", "package a\nfunc F() {}\n")
	dec, _, err := Evaluate(dir)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec != DecisionNeedReview {
		t.Fatalf("跨分支不同 hash 应 NeedReview，实际 %v——跨分支放行掩盖了新内容", dec)
	}
}

// TestEvaluate_OwnBranchBlockingButCrossBranchReviewedRescues pins the subtle rescue: the own-branch
// stamp exists for this hash but Reviewed=false (a prior Evaluate seeded BlockCount=1), AND a peer branch
// reviewed the identical content. Evaluate must Pass via the cross-branch scan rather than increment the
// own-branch block counter — the content has been reviewed, so the pending own-branch block is moot.
//
// TestEvaluate_OwnBranchBlockingButCrossBranchReviewedRescues 钉住微妙的 rescue：own 分支有该 hash 的
// 戳但 Reviewed=false（之前 Evaluate 种了 BlockCount=1），且兄弟分支审过同一内容。Evaluate 必须
// 经跨分支扫描放行，而不是累加 own 分支 block 计数——内容已审，own 分支的待 block 无意义。
func TestEvaluate_OwnBranchBlockingButCrossBranchReviewedRescues(t *testing.T) {
	dir := initGitRepo(t)
	defaultBranch := currentGitBranch(t, dir)
	write(t, dir, "a.go", "package a\n")

	// Seed a non-reviewed stamp on the default branch (Evaluate blocks → writes BlockCount=1).
	//
	// 在默认分支种一个未审戳（Evaluate block → 写 BlockCount=1）。
	if dec, _, _ := Evaluate(dir); dec != DecisionNeedReview {
		t.Fatalf("首次应 NeedReview，实际 %v", dec)
	}

	// Peer branch, identical content → mark reviewed there.
	//
	// 兄弟分支，同一内容 → 在其上标记已审。
	gitCheckout(t, dir, "checkout", "-b", "feat/x")
	if err := MarkPassed(dir); err != nil {
		t.Fatalf("MarkPassed: %v", err)
	}

	// Back on default: own-branch stamp is Reviewed=false for the SAME hash, but feat/x reviewed it.
	//
	// 切回默认分支：own 分支戳对该 hash 是 Reviewed=false，但 feat/x 已审过。
	gitCheckout(t, dir, "checkout", defaultBranch)
	dec, reason, err := Evaluate(dir)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec != DecisionPass {
		t.Fatalf("own 分支 Reviewed=false 但他分支已审同 hash 应 Pass（跨分支 rescue），实际 %v（%s）", dec, reason)
	}
}

// TestEvaluate_MaxRoundsAdvisory fallback: when the agent never calls forge review pass,
// the Stop hook repeatedly blocking the same diff will advisory-pass after MaxReviewRounds (prevents dead loop).
//
// TestEvaluate_MaxRoundsAdvisory 兜底：agent 不调 forge review pass 时，
// Stop hook 反复 block 同 diff 会在 MaxReviewRounds 后 advisory 放行（防死循环）。
func TestEvaluate_MaxRoundsAdvisory(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "a.go", "package a\n")

	var last Decision
	var iters int
	for i := 0; i < MaxReviewRounds+2; i++ {
		iters++
		last, _, _ = Evaluate(dir)
		if last != DecisionNeedReview {
			break
		}
	}
	if last != DecisionPassAdvisory {
		t.Fatalf("撞 MaxReviewRounds 应 PassAdvisory，实际 %v（迭代 %d 次）", last, iters)
	}
	if iters != MaxReviewRounds+1 {
		t.Fatalf("应在第 %d 次放行，实际第 %d 次", MaxReviewRounds+1, iters)
	}
}

// TestEvaluate_StampExcludesForge writing the stamp does not pollute the diff hash — the core anti-dead-loop assertion.
// If the stamp counted toward the diff, writing it would change the hash → forever NeedReview. This proves that
// re-Evaluating right after pass (with the stamp already written) still returns Pass, confirming the .forge exclusion works.
//
// TestEvaluate_StampExcludesForge 写 stamp 不污染 diff hash——防死循环核心断言。
// 如果 stamp 计入 diff，写 stamp 会改 hash → 永远 NeedReview。这里证明 pass 后
// 立即再 Evaluate（此时 stamp 已写）仍 Pass，说明 .forge 排除生效。
func TestEvaluate_StampExcludesForge(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "a.go", "package a\n")
	if err := MarkPassed(dir); err != nil {
		t.Fatal(err)
	}
	// The stamp lands in DataDir/stamps/ (refactor-data-home: user-level for git projects)
	//
	// stamp 落盘在 DataDir/stamps/（refactor-data-home：git 项目用户级）
	if _, err := os.Stat(filepath.Join(forgedata.DataDirFor(dir), "stamps")); err != nil {
		t.Fatalf("stamp 目录未创建: %v", err)
	}
	// Re-Evaluate: if the stamp counted toward the diff, the hash would change → NeedReview (wrong)
	//
	// 再 Evaluate：若 stamp 计入 diff 则 hash 变 → NeedReview（错误）
	dec, _, _ := Evaluate(dir)
	if dec != DecisionPass {
		t.Fatalf("写 stamp 后再 Evaluate 应仍 Pass（.forge 排除生效），实际 %v——stamp 污染了 diff", dec)
	}
}

// TestCurrentState_Runs smoke test: status output does not crash and contains key fields.
//
// TestCurrentState_Runs smoke test：status 输出不崩、含关键字段。
func TestCurrentState_Runs(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "a.go", "package a\n")
	out, err := CurrentState(dir)
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if out == "" {
		t.Fatal("CurrentState 输出为空")
	}
}

// gitCommit commits all changes in the temp repo (helper, reuses gitEnv; test-level, errors are fatal).
//
// gitCommit 在临时仓库提交全部变更（helper，复用 gitEnv；单测级，错误即 fatal）。
func gitCommit(t *testing.T, dir, msg string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = gitEnv
		if err := cmd.Run(); err != nil {
			t.Fatalf(`git %v failed: %v`, args, err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", msg)
}

// gitHeadShort returns the HEAD short hash, used as baseCommit for SourceChangesSince.
//
// gitHeadShort 返回 HEAD 短 hash，作 SourceChangesSince 的 baseCommit。
func gitHeadShort(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf(`git rev-parse HEAD failed: %v`, err)
	}
	return strings.TrimSpace(string(out))
}

// gitCheckout runs git checkout in the temp repo (helper for cross-branch tests; errors are fatal).
//
// gitCheckout 在临时仓库跑 git checkout（跨分支测试 helper；错误即 fatal）。
func gitCheckout(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = gitEnv
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// currentGitBranch returns the current branch name (default branch differs master/main by OS/git version,
// so cross-branch tests resolve it instead of hard-coding).
//
// currentGitBranch 返回当前分支名（默认分支随 OS/git 版本是 master 或 main，
// 故跨分支测试动态解析而非硬编码）。
func currentGitBranch(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf(`git rev-parse --abbrev-ref HEAD failed: %v`, err)
	}
	return strings.TrimSpace(string(out))
}

// TestSourceChangesSince_EmptyBaseUntracked base=empty degrades to HEAD: untracked source → hasChanges=true.
//
// TestSourceChangesSince_EmptyBaseUntracked base="" 退化成 HEAD：untracked 源码 → hasChanges=true。
func TestSourceChangesSince_EmptyBaseUntracked(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, `a.go`, `package a`)
	hash, has, err := SourceChangesSince(dir, "")
	if err != nil {
		t.Fatalf(`SourceChangesSince: %v`, err)
	}
	if !has || hash == "" {
		t.Fatalf(`base="" 对 untracked 源码应 hasChanges=true 且 hash 非空，got has=%v hash=%q`, has, hash)
	}
}

// TestSourceChangesSince_IncludesCommittedChanges core difference: base..HEAD committed changes are included in the fingerprint.
// The old computeDiffHash only looked at the worktree relative to HEAD — a clean worktree (after commit) returned empty, a false negative. SourceChangesSince(base)
// uses a single-tree git diff <base>; base..HEAD committed + uncommitted worktree changes are captured in one step — the basis for commit-then-review flow decisions.
//
// TestSourceChangesSince_IncludesCommittedChanges 核心差异：base..HEAD 的【已提交】变更纳入指纹。
// 旧 computeDiffHash 只看工作区相对 HEAD——干净工作区（已 commit）返空，假阴性。SourceChangesSince(base)
// 用单树 git diff <base>，base..HEAD 已提交 + 工作区未提交一步算进——commit-then-review 流的判定基础。
func TestSourceChangesSince_IncludesCommittedChanges(t *testing.T) {
	dir := initGitRepo(t) // HEAD = C0
	c0 := gitHeadShort(t, dir)
	write(t, dir, `svc.go`, `package svc`)
	gitCommit(t, dir, "add svc") // HEAD = C1，工作区干净

	hash, has, err := SourceChangesSince(dir, c0)
	if err != nil {
		t.Fatalf(`SourceChangesSince: %v`, err)
	}
	if !has {
		t.Fatalf(`C0..C1 含已提交 svc.go，应 hasChanges=true（hash=%q）——旧 computeDiffHash 在干净工作区会误返空`, hash)
	}
}

// TestSourceChangesSince_BaseUnreachable base unreachable (amend/rebase rewrote history) → returns err for fail-open.
//
// TestSourceChangesSince_BaseUnreachable base 不可达（amend/rebase 改写历史）→ 返 err 供 fail-open。
func TestSourceChangesSince_BaseUnreachable(t *testing.T) {
	dir := initGitRepo(t)
	_, _, err := SourceChangesSince(dir, "deadbeefnotacommit")
	if err == nil {
		t.Fatal(`base 不可达应返回 err，got nil——调用方无法 fail-open`)
	}
}

// TestSourceChangesSince_DocChangeExcluded pure-doc changes are excluded (isSourceCode whitelist).
//
// TestSourceChangesSince_DocChangeExcluded 纯文档变更不纳入（isSourceCode 白名单）。
func TestSourceChangesSince_DocChangeExcluded(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, `README.md`, `# docs`)
	hash, has, err := SourceChangesSince(dir, "")
	if err != nil {
		t.Fatalf(`SourceChangesSince: %v`, err)
	}
	if has || hash != "" {
		t.Fatalf(`纯 README 变更应 hasChanges=false hash=""，got has=%v hash=%q`, has, hash)
	}
}

// TestSourceChangesSince_StableAcrossForgeWrites writing under .forge/ does not change the hash (the :(exclude).forge rule is in effect).
//
// TestSourceChangesSince_StableAcrossForgeWrites 写 .forge/ 不改 hash（:(exclude).forge 生效）。
func TestSourceChangesSince_StableAcrossForgeWrites(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, `a.go`, `package a`)
	h1, _, err := SourceChangesSince(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, `.forge/stamps/x.stamp`, `{"x":1}`)
	h2, _, err := SourceChangesSince(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf(`写 .forge/ 后 hash 应不变（.forge 排除），h1=%q h2=%q`, h1, h2)
	}
}

// TestSourceChangesSince_CommitWorkdirContentStaysEqual after committing the worktree content under review, the fingerprint stays unchanged —
// the core of the review-fix-re-review loop. review pass records (base=C0, hash=worktree diff); after the agent commits the reviewed content
// (without changing anything), SourceChangesSince(C0) still equals the recorded hash (the commit is exactly the reviewed worktree diff) → the gate passes;
// only when new content is added after the commit does it != → re-review triggers.
//
// TestSourceChangesSince_CommitWorkdirContentStaysEqual commit 审查的工作区内容后指纹不变——
// 审查-修复-复审闭环核心。review pass 记 (base=C0, hash=工作区 diff)；agent commit 审查内容
// （不改任何东西）后，SourceChangesSince(C0) 仍 == 记录 hash（commit 的正是审查的工作区 diff）→ 门禁放行；
// commit 后再改【新】内容才 != → 触发复审。
func TestSourceChangesSince_CommitWorkdirContentStaysEqual(t *testing.T) {
	dir := initGitRepo(t)                            // HEAD = C0
	write(t, dir, `a.go`, `package a`)               // 工作区有 a.go（untracked）
	hAtReview, _, err := SourceChangesSince(dir, "") // = 工作区相对 C0 的 diff
	if err != nil {
		t.Fatal(err)
	}
	c0 := gitHeadShort(t, dir)    // 记基线 base=C0
	gitCommit(t, dir, "reviewed") // HEAD = C1，工作区干净

	hAfterCommit, _, err := SourceChangesSince(dir, c0) // C0..C1 含 a.go + 工作区空
	if err != nil {
		t.Fatalf(`SourceChangesSince after commit: %v`, err)
	}
	if hAfterCommit != hAtReview {
		t.Fatalf(`commit 审查的工作区内容后指纹应不变（hAtReview=%q hAfterCommit=%q）——commit-then-review 流会假阳性`, hAtReview, hAfterCommit)
	}

	// Counter-example: changing new content after commit → fingerprint changes → re-review triggers.
	//
	// 反例：commit 后再改新内容 → 指纹变 → 触发复审。
	write(t, dir, `a.go`, "package a\nfunc F() {}")
	hAfterNewChange, _, err := SourceChangesSince(dir, c0)
	if err != nil {
		t.Fatal(err)
	}
	if hAfterNewChange == hAtReview {
		t.Fatalf(`commit 后再改新内容指纹应变（触发复审），但 == 审查时 hash`)
	}
}

// TestLoadStamp pins the honest-signature contract: missing/corrupt stamp files
// degrade to an empty (unreviewed) Stamp — never nil, never an error masquerade —
// and a stamp persisted via MarkPassed round-trips back.
//
// TestLoadStamp 钉住诚实签名契约：缺失/损坏的 stamp 文件降级为空（未审）Stamp
// ——永不返回 nil、永不伪装错误——经 MarkPassed 落盘的 stamp 能完整读回。
func TestLoadStamp(t *testing.T) {
	t.Run("missing file returns empty stamp", func(t *testing.T) {
		root := initGitRepo(t)
		s := loadStamp(root)
		if s == nil {
			t.Fatal("loadStamp must never return nil")
		}
		if s.Reviewed || s.DiffHash != "" {
			t.Errorf("missing stamp should be empty, got %+v", s)
		}
	})

	t.Run("corrupt file returns empty stamp", func(t *testing.T) {
		root := initGitRepo(t)
		p := stampPath(root)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		s := loadStamp(root)
		if s.Reviewed || s.DiffHash != "" {
			t.Errorf("corrupt stamp should degrade to empty, got %+v", s)
		}
	})

	t.Run("persisted stamp round-trips", func(t *testing.T) {
		root := initGitRepo(t)
		write(t, root, "a.go", "package main\n")
		if err := MarkPassed(root); err != nil {
			t.Fatalf("MarkPassed: %v", err)
		}
		s := loadStamp(root)
		if !s.Reviewed {
			t.Error("stamp persisted by MarkPassed should load as Reviewed=true")
		}
		if s.DiffHash == "" {
			t.Error("stamp persisted by MarkPassed should carry a DiffHash")
		}
	})
}
