package review

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// git 集成测试用真实临时仓库（t.TempDir + git init）。review 包的核心是 diff/stamp
// 状态机，单靠 mock 验证不了"git diff 真的排除了 .forge""纯文档真不触发"这些断言——
// 必须端到端跑 git。环境要求 git 可用（CI 与本地均有）。

// gitEnv 提供无 GPG、固定身份的 git 环境，避免 commit 在全新仓库失败。
var gitEnv = append(os.Environ(),
	"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
	"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
)

func initGitRepo(t *testing.T) string {
	t.Helper()
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
		{"README.md", false},   // 文档不审
		{"docs/guide.md", false},
		{".forge/pipeline.yml", false}, // .forge 自身（yml 也非源码）
		{"config.json", false},  // 配置不审
		{"Cargo.toml", false},
		{"foo.gen.go", false},   // 生成物：扩展是 go 但路径含 .gen.
		{"bar_generated_test.go", false}, // 生成物：_generated
		{"baz.pb.go", false},    // protobuf 生成
		{"vendor/lib.go", false},
		{"node_modules/x.js", false},
		{"image.png", false},
		{"style.css", false},
		{"Makefile", false},     // 无扩展名不在白名单
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

// TestEvaluate_NewDiffReTriggers 新的源码 diff（hash 变）重新触发审查——防"审完继续改不重审"。
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
	// stamp 文件确实落盘在 .forge/stamps/
	if _, err := os.Stat(filepath.Join(dir, ".forge", "stamps")); err != nil {
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
	dir := initGitRepo(t) // HEAD = C0
	write(t, dir, `a.go`, `package a`) // 工作区有 a.go（untracked）
	hAtReview, _, err := SourceChangesSince(dir, "") // = 工作区相对 C0 的 diff
	if err != nil {
		t.Fatal(err)
	}
	c0 := gitHeadShort(t, dir) // 记基线 base=C0
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
