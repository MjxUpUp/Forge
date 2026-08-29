package review

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// git 集成测试用真实临时仓库（t.TempDir + git init）。review 包的核心是 diff/stamp
// 状态机，单靠 mock 验证不了「git diff 真的排除了 .forge」「纯文档真不触发」这些断言——
// 必须端到端跑 git。环境要求 git 可用（CI 与本地均有）。

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

// TestEvaluate_TrackedSourceChange 修改已提交的源码文件（tracked diff）也触发。
func TestEvaluate_TrackedSourceChange(t *testing.T) {
	dir := initGitRepo(t)
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

// TestMarkPassedWithNote 钉住 `forge review pass --note` 的非 task 模式半边：审查结论
// 持久化进分支 stamp（task 模式对应物是 ReviewRound.Note）；裸 MarkPassed 保持为空
// （向后兼容）。
func TestMarkPassedWithNote(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "a.go", "package a\n")

	loadStamped := func() *Stamp {
		hash, _, err := computeDiffHash(dir)
		if err != nil {
			t.Fatal(err)
		}
		return loadStamp(dir, hash)
	}
	if err := MarkPassedWithNote(dir, "审查结论：无发现"); err != nil {
		t.Fatalf("MarkPassedWithNote: %v", err)
	}
	if got := loadStamped().Note; got != "审查结论：无发现" {
		t.Errorf("stamp.Note 未持久化, got %q", got)
	}

	// 裸 MarkPassed 保持 note 为空（旧形状）。
	if err := MarkPassed(dir); err != nil {
		t.Fatalf("MarkPassed: %v", err)
	}
	if got := loadStamped().Note; got != "" {
		t.Errorf("裸 MarkPassed 后 stamp.Note 应为空, got %q", got)
	}
}

// TestEvaluate_NewDiffReTriggers 新的源码 diff（hash 变）重新触发审查——防「审完继续改不重审」。
func TestEvaluate_NewDiffReTriggers(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "a.go", "package a\n")
	if err := MarkPassed(dir); err != nil {
		t.Fatal(err)
	}
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

// TestEvaluate_CrossBranchSameHashPasses scope 可移植性：在一个分支审查并打过戳的 diff，切到另一
// 分支内容（因而 diff hash）一致时仍应放行——ff-merge + checkout 场景（2026-08-06 cooking 会话的
// 拦截4）。修复前戳按分支存，master 会重新 block 字节级一致的已审代码：假阳性。安全是因为内容
// 一致则 hash 一致；内容不同则 hash 不同，仍需审。
func TestEvaluate_CrossBranchSameHashPasses(t *testing.T) {
	dir := initGitRepo(t)
	defaultBranch := currentGitBranch(t, dir)
	write(t, dir, "a.go", "package a\n")

	// 建特性分支（同一 HEAD，a.go 仍 untracked）并在其上标记审查通过。
	gitCheckout(t, dir, "checkout", "-b", "feat/x")
	if err := MarkPassed(dir); err != nil {
		t.Fatalf("MarkPassed: %v", err)
	}

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

// TestEvaluate_OwnBranchBlockingButCrossBranchReviewedRescues 钉住微妙的 rescue：own 分支有该 hash 的
// 戳但 Reviewed=false（之前 Evaluate 种了 BlockCount=1），且兄弟分支审过同一内容。Evaluate 必须
// 经跨分支扫描放行，而不是累加 own 分支 block 计数——内容已审，own 分支的待 block 无意义。
func TestEvaluate_OwnBranchBlockingButCrossBranchReviewedRescues(t *testing.T) {
	dir := initGitRepo(t)
	defaultBranch := currentGitBranch(t, dir)
	write(t, dir, "a.go", "package a\n")

	// 在默认分支种一个未审戳（Evaluate block → 写 BlockCount=1）。
	if dec, _, _ := Evaluate(dir); dec != DecisionNeedReview {
		t.Fatalf("首次应 NeedReview，实际 %v", dec)
	}

	// 兄弟分支，同一内容 → 在其上标记已审。
	gitCheckout(t, dir, "checkout", "-b", "feat/x")
	if err := MarkPassed(dir); err != nil {
		t.Fatalf("MarkPassed: %v", err)
	}

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

// TestEvaluate_StampExcludesForge 写 stamp 不污染 diff hash——防死循环核心断言。
// 如果 stamp 计入 diff，写 stamp 会改 hash → 永远 NeedReview。这里证明 pass 后
// 立即再 Evaluate（此时 stamp 已写）仍 Pass，说明 .forge 排除生效。
func TestEvaluate_StampExcludesForge(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "a.go", "package a\n")
	if err := MarkPassed(dir); err != nil {
		t.Fatal(err)
	}
	// stamp 落盘在 DataDir/stamps/（refactor-data-home：git 项目用户级）
	if _, err := os.Stat(filepath.Join(forgedata.DataDirFor(dir), "stamps")); err != nil {
		t.Fatalf("stamp 目录未创建: %v", err)
	}
	// 再 Evaluate：若 stamp 计入 diff 则 hash 变 → NeedReview（错误）
	dec, _, _ := Evaluate(dir)
	if dec != DecisionPass {
		t.Fatalf("写 stamp 后再 Evaluate 应仍 Pass（.forge 排除生效），实际 %v——stamp 污染了 diff", dec)
	}
}

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

// gitHeadShort 返回 HEAD 短 hash，作 SourceChangesSince 的 baseCommit。
func gitHeadShort(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf(`git rev-parse HEAD failed: %v`, err)
	}
	return strings.TrimSpace(string(out))
}

// gitCheckout 在临时仓库跑 git checkout（跨分支测试 helper；错误即 fatal）。
func gitCheckout(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = gitEnv
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

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

// TestSourceChangesSince_BaseUnreachable base 不可达（amend/rebase 改写历史）→ 返 err 供 fail-open。
func TestSourceChangesSince_BaseUnreachable(t *testing.T) {
	dir := initGitRepo(t)
	_, _, err := SourceChangesSince(dir, "deadbeefnotacommit")
	if err == nil {
		t.Fatal(`base 不可达应返回 err，got nil——调用方无法 fail-open`)
	}
}

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

// TestLoadStamp 钉住诚实签名契约：缺失/损坏的 stamp 文件降级为空（未审）Stamp
// ——永不返回 nil、永不伪装错误——经 MarkPassed 落盘的 stamp 能完整读回。
func TestLoadStamp(t *testing.T) {
	t.Run("missing file returns empty stamp", func(t *testing.T) {
		root := initGitRepo(t)
		s := loadStamp(root, "deadbeef")
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
		s := loadStamp(root, "deadbeef")
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
		hash, _, err := computeDiffHash(root)
		if err != nil {
			t.Fatal(err)
		}
		s := loadStamp(root, hash)
		if !s.Reviewed {
			t.Error("stamp persisted by MarkPassed should load as Reviewed=true")
		}
		if s.DiffHash == "" {
			t.Error("stamp persisted by MarkPassed should carry a DiffHash")
		}
	})
}

// TestCurrentBranch 钉住导出的分支访问器（非 task 模式 review-pass 的 checklog
// detail 所用，2026-08 评审可观测性）：git 仓库内返回当前分支名；非 git 目录
// 降级为 ""。
func TestCurrentBranch(t *testing.T) {
	dir := initGitRepo(t)
	want := currentBranch(dir)
	if want == "" {
		t.Fatal("fixture 分支名不能为空（currentBranch 前置失效）")
	}
	if got := CurrentBranch(dir); got != want {
		t.Errorf("CurrentBranch = %q, want %q（应与内部实现一致）", got, want)
	}
	if got := CurrentBranch(t.TempDir()); got != "" {
		t.Errorf("非 git 目录应降级为空串, got %q", got)
	}
}

// TestMarkPassed_ContentAddressed 钉住 multi-task-concurrency §5 的 stamp 键控变更：
// MarkPassed 写内容寻址的 stamps/dh-<diffhash>.stamp，且不再创建旧 <branch>.stamp
// （按分支键控的 stamp 在同分支多 worktree 下碰撞——里面的 hash 是最后保存的那个
// worktree 的）。
func TestMarkPassed_ContentAddressed(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "a.go", "package a\nfunc F() {}\n")
	hash, hasChanges, err := computeDiffHash(dir)
	if err != nil || !hasChanges {
		t.Fatalf("computeDiffHash: err=%v hasChanges=%v", err, hasChanges)
	}
	if err := MarkPassed(dir); err != nil {
		t.Fatalf("MarkPassed: %v", err)
	}
	if _, err := os.Stat(stampContentPath(dir, hash)); err != nil {
		t.Fatalf("内容寻址 stamp 应存在 (%s): %v", stampContentPath(dir, hash), err)
	}
	if _, err := os.Stat(stampPath(dir)); !os.IsNotExist(err) {
		t.Errorf("不应再写旧分支 stamp（%s）——分支键控已退役为只读兼容", stampPath(dir))
	}
}

// TestEvaluate_LegacyBranchStampCompat 钉住迁移前 stamp 的读兼容：旧 <branch>.stamp 带
// 当前 diff hash + Reviewed 时，Evaluate 仍放行、不要求重审（无 dh-<hash>.stamp 时
// loadStamp 回落到分支路径）。
func TestEvaluate_LegacyBranchStampCompat(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "a.go", "package a\nfunc F() {}\n")
	hash, _, err := computeDiffHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	legacy := Stamp{
		DiffHash:   hash,
		Reviewed:   true,
		BlockCount: 0,
		ReviewedAt: time.Now(),
		Branch:     currentBranch(dir),
	}
	p := stampPath(dir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	decision, _, err := Evaluate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if decision != DecisionPass {
		t.Errorf("旧分支 stamp 读兼容失败：同 hash 已审应放行, got %v", decision)
	}
}

// TestSourceChangesSinceExcluded 钉住 T3 排除契约：被排除路径离开指纹；nil map 与
// 旧全树计算逐字节一致。
func TestSourceChangesSinceExcluded(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "b.go", "package b\n")

	whole, hasChanges, err := SourceChangesSince(dir, "")
	if err != nil || !hasChanges {
		t.Fatalf("whole-tree: %v has=%v", err, hasChanges)
	}
	excluded, _, err := SourceChangesSinceExcluded(dir, "", map[string]bool{"b.go": true})
	if err != nil {
		t.Fatal(err)
	}
	if excluded == whole {
		t.Fatal("排除 b.go 后指纹应变化")
	}
	// nil map == legacy
	again, _, _ := SourceChangesSinceExcluded(dir, "", nil)
	if again != whole {
		t.Fatal("nil 排除集必须与旧全树计算一致")
	}
}

// --- feat/checklog-janitor: stale stamp sweep + filename matching ---

// TestKnownReviewed_PrunesStaleStamps 钉住 retention 清扫：超过 FORGE_STAMP_RETENTION_DAYS
// （默认 30）的 dh-*.stamp 在 knownReviewed 入口被删（龄取文件 mtime，由 AtomicWrite 写入
// 时设定），窗口内的戳保留；retention 为 0 禁用清扫。每个新 diff 的 Stop-block 都留下一枚
// dh-<hash>.stamp 且此前无路径删除——清扫让目录与每次 Evaluate 的扫描保持有界。
func TestKnownReviewed_PrunesStaleStamps(t *testing.T) {
	t.Run("sweep removes stale keeps fresh", func(t *testing.T) {
		t.Setenv("FORGE_STAMP_RETENTION_DAYS", "30")
		root := initGitRepo(t) // 同时隔离 FORGE_DATA_HOME
		dir := filepath.Join(forgedata.DataDirFor(root), "stamps")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		put := func(h string) string {
			p := filepath.Join(dir, "dh-"+h+".stamp")
			data, _ := json.MarshalIndent(Stamp{DiffHash: h, Reviewed: true, Branch: "feat/x"}, "", "  ")
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}
		stale := put(strings.Repeat("aa", 32))
		fresh := put(strings.Repeat("bb", 32))
		// 40 天前的 mtime —— 超过 30 天 retention 窗口。
		//
		// mtime 设为 40 天前——超过 30 天 retention 窗口。
		old := time.Now().AddDate(0, 0, -40)
		if err := os.Chtimes(stale, old, old); err != nil {
			t.Fatal(err)
		}

		// 任意 hash 的查找都会先触发入口清扫。
		knownReviewed(root, strings.Repeat("cc", 32))

		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Error("超过 retention 的 dh-*.stamp 应被清扫")
		}
		if _, err := os.Stat(fresh); err != nil {
			t.Errorf("窗口内的 dh-*.stamp 必须保留: %v", err)
		}
	})

	t.Run("retention disabled keeps stale", func(t *testing.T) {
		t.Setenv("FORGE_STAMP_RETENTION_DAYS", "0")
		root := initGitRepo(t)
		dir := filepath.Join(forgedata.DataDirFor(root), "stamps")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		staleHash := strings.Repeat("aa", 32)
		stale := filepath.Join(dir, "dh-"+staleHash+".stamp")
		data, _ := json.MarshalIndent(Stamp{DiffHash: staleHash, Reviewed: true}, "", "  ")
		if err := os.WriteFile(stale, data, 0o644); err != nil {
			t.Fatal(err)
		}
		old := time.Now().AddDate(0, 0, -40)
		if err := os.Chtimes(stale, old, old); err != nil {
			t.Fatal(err)
		}

		knownReviewed(root, strings.Repeat("cc", 32))

		if _, err := os.Stat(stale); err != nil {
			t.Errorf("FORGE_STAMP_RETENTION_DAYS=0 禁用清扫，陈戳应保留: %v", err)
		}
	})
}

// TestKnownReviewed_ContentAddressedFilenameMatch 钉住扫描策略：
//   - 文件名命中的 dh-<hash>.stamp 且 Reviewed=true → 命中（并点名来源分支）；
//   - 名字命中但 Reviewed=false（Evaluate 的 block 路径把计数戳写在同一内容寻址路径）
//     不得命中——只按文件名命中会让仅被 block 的 diff 假放行；
//   - 名字不匹配的 dh- 文件即使【内容】带目标 hash 也不读不命中（saveStamp 用
//     DiffHash 推导路径，名/内容错配非合法状态）——扫描由 O(N) 次读降到至多一次的
//     关键；
//   - legacy <branch>.stamp 仍按【内容】命中（读兼容不得回归）。
func TestKnownReviewed_ContentAddressedFilenameMatch(t *testing.T) {
	root := initGitRepo(t)
	dir := filepath.Join(forgedata.DataDirFor(root), "stamps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := strings.Repeat("ab", 32)
	other := strings.Repeat("cd", 32)
	putStampFile := func(name string, s Stamp) {
		data, _ := json.MarshalIndent(s, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cleanStamps := func() {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}

	// 1) 名字命中 + Reviewed=true → 命中，且能点名来源分支。
	putStampFile("dh-"+target+".stamp", Stamp{DiffHash: target, Reviewed: true, Branch: "feat/x"})
	s, ok := knownReviewed(root, target)
	if !ok {
		t.Fatal("名字命中的已审戳应命中")
	}
	if s.Branch != "feat/x" {
		t.Errorf("命中应带来源分支, got %q", s.Branch)
	}

	// 2) 名字命中但 Reviewed=false（block 戳同路径）→ 不得命中（假放行）。
	putStampFile("dh-"+target+".stamp", Stamp{DiffHash: target, Reviewed: false, BlockCount: 1})
	if _, ok := knownReviewed(root, target); ok {
		t.Fatal("block 戳（Reviewed=false）不得被文件名命中当成已审——假放行")
	}

	// 3) 名字不匹配的 dh- 文件即使内容带目标 hash 也不读不命中。
	cleanStamps()
	putStampFile("dh-"+other+".stamp", Stamp{DiffHash: target, Reviewed: true, Branch: "feat/tamper"})
	if _, ok := knownReviewed(root, target); ok {
		t.Fatal("dh- 命中按文件名：名字不匹配的文件即使内容带目标 hash 也不得命中")
	}

	// 4) legacy <branch>.stamp 仍按内容命中（读兼容不回归）。
	if err := os.Remove(filepath.Join(dir, "dh-"+other+".stamp")); err != nil {
		t.Fatal(err)
	}
	putStampFile("master.stamp", Stamp{DiffHash: target, Reviewed: true, Branch: "master"})
	if _, ok := knownReviewed(root, target); !ok {
		t.Fatal("legacy <branch>.stamp 的内容命中必须保留（读兼容）")
	}
}
