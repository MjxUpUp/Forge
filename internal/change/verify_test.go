package change

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseAcceptance_CommandsAndManualItems(t *testing.T) {
	doc := "# 验收\n\n" +
		"```bash\ngo build ./...\n```\n\n" +
		"```sh\necho ok\n```\n\n" +
		"```yaml\nnot: a command\n```\n\n" +
		"- [ ] 人工 A\n" +
		"- [x] 已做 B\n" +
		"-   [ ] 宽松 C\n"
	acc := ParseAcceptance([]byte(doc))
	if len(acc.Commands) != 2 {
		t.Fatalf("期望 2 条命令，got %d: %v", len(acc.Commands), acc.Commands)
	}
	if acc.Commands[0].Body != "go build ./..." || acc.Commands[1].Body != "echo ok" {
		t.Errorf("命令提取错误：%v", acc.Commands)
	}
	// 壳按围栏语言给：bash 围栏就该在 bash 里跑，sh 围栏不该被 bash 顶替。
	if acc.Commands[0].Shell != "bash" || acc.Commands[1].Shell != "sh" {
		t.Errorf("围栏语言与壳不一致：%v", acc.Commands)
	}
	if len(acc.Manual) != 2 || acc.Manual[0] != "人工 A" || acc.Manual[1] != "宽松 C" {
		t.Errorf("人工项提取错误：%v", acc.Manual)
	}
	// 勾选过的人工项既不进判据，也不该从报告里消失。
	if len(acc.ManualDone) != 1 || acc.ManualDone[0] != "已做 B" {
		t.Errorf("已勾选人工项提取错误：%v", acc.ManualDone)
	}
}

func TestParseAcceptance_MultilineCommand(t *testing.T) {
	doc := "```bash\ncd sub\nmake test\n```\n"
	acc := ParseAcceptance([]byte(doc))
	if len(acc.Commands) != 1 || acc.Commands[0].Body != "cd sub\nmake test" {
		t.Fatalf("多行命令应整体保留，got %#v", acc.Commands)
	}
}

func TestVerify_SealsAndPersistsEvidence(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeFile(t, filepath.Join(dir, FileAcceptance),
		"# 验收\n\n```bash\necho hello\n```\n\n```bash\ntrue\n```\n\n- [ ] 人看一眼\n")

	res, err := Verify(context.Background(), root, "demo", VerifyOptions{})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.Summary.Sealed || !res.Summary.Passed {
		t.Fatalf("全部通过应封印，got %+v", res.Summary)
	}
	if len(res.Summary.Checks) != 2 {
		t.Fatalf("期望 2 条检查，got %d", len(res.Summary.Checks))
	}
	if len(res.Manual) != 1 {
		t.Errorf("应带出人工检查项，got %v", res.Manual)
	}

	out, err := os.ReadFile(filepath.Join(res.EvidenceDir, "01.stdout"))
	if err != nil {
		t.Fatalf("应落 stdout 证据: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Errorf("stdout 证据内容不对：%q", out)
	}
	sum, err := loadSummary(res.EvidenceDir)
	if err != nil {
		t.Fatalf("应能读回摘要: %v", err)
	}
	if !sum.Sealed || sum.AcceptanceSHA256 == "" {
		t.Errorf("摘要缺封印或判据哈希：%+v", sum)
	}

	// 状态从文件推导：刚落完证据的 change 就是 verified。
	c, _, err := Load(root, "demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Stage != StageVerified || !c.Sealed {
		t.Errorf("期望 verified/sealed，got %s/sealed=%v", c.Stage, c.Sealed)
	}
}

func TestVerify_StopsAtFirstFailure(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeFile(t, filepath.Join(dir, FileAcceptance),
		"```bash\nexit 3\n```\n\n```bash\necho never\n```\n")

	res, err := Verify(context.Background(), root, "demo", VerifyOptions{})
	if err != nil {
		t.Fatalf("Verify 应返回结果（失败不是调用错误）: %v", err)
	}
	if res.Summary.Sealed || res.Summary.Passed {
		t.Error("有命令失败时不应封印")
	}
	if res.Summary.Checks[0].Exit != 3 {
		t.Errorf("第一条应记录 exit 3，got %d", res.Summary.Checks[0].Exit)
	}
	if !res.Summary.Checks[1].Skipped {
		t.Error("首条失败后，后续命令应标记 skipped（不在一个坏地基上继续跑）")
	}
	// 失败证据同样要落盘：失败现场比"没跑"有用。
	if _, err := os.Stat(filepath.Join(res.EvidenceDir, SummaryFile)); err != nil {
		t.Errorf("失败也应写摘要: %v", err)
	}
}

// 一个围栏里允许多行（换目录、装依赖、再断言）。中间一步失败、最后一步成功，
// 不能因此判过——否则 verify 会替 agent 出假绿。
func TestVerify_MultilineFenceMidFailureNotSealed(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeFile(t, filepath.Join(dir, FileAcceptance),
		"```bash\nfalse\necho done\n```\n")

	res, err := Verify(context.Background(), root, "demo", VerifyOptions{})
	if err != nil {
		t.Fatalf("Verify 应返回结果: %v", err)
	}
	if res.Summary.Sealed || res.Summary.Passed {
		t.Error("围栏中段失败不应被末行成功吞掉")
	}
	if res.Summary.Checks[0].Exit == 0 {
		t.Errorf("应记录非零退出码，got %d", res.Summary.Checks[0].Exit)
	}
}

func TestVerify_RejectsManualOnly(t *testing.T) {
	root, dir := repoWithChange(t, "demo")
	writeFile(t, filepath.Join(dir, FileAcceptance), "# 验收\n\n- [ ] 看起来对\n")
	_, err := Verify(context.Background(), root, "demo", VerifyOptions{})
	if err == nil {
		t.Fatal("只有人工清单时不应算可验证")
	}
	if !strings.Contains(err.Error(), "bash") {
		t.Errorf("错误应指出缺可执行命令，got %v", err)
	}
}

func TestVerify_Timeout(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\nsleep 5\n```\n")

	res, err := Verify(context.Background(), root, "demo", VerifyOptions{Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	chk := res.Summary.Checks[0]
	if !chk.TimedOut {
		t.Errorf("应标记超时，got %+v", chk)
	}
	if chk.Exit == 0 || res.Summary.Sealed {
		t.Error("超时不能算通过")
	}
}

func TestWorktreeFingerprint_TracksUntrackedContent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用")
	}
	root := newGitRepo(t)
	writeFile(t, filepath.Join(root, "a.txt"), "one\n")
	run(t, root, "git", "add", "a.txt")
	run(t, root, "git", "commit", "-q", "-m", "a")

	fp1 := WorktreeFingerprint(root)
	if !fp1.OK || fp1.Head == "" || fp1.DiffSHA256 == "" {
		t.Fatalf("期望可用指纹，got %+v", fp1)
	}
	if fp2 := WorktreeFingerprint(root); fp2.DiffSHA256 != fp1.DiffSHA256 {
		t.Error("工作区没变时指纹应稳定")
	}

	writeFile(t, filepath.Join(root, "new.txt"), "untracked\n")
	fp3 := WorktreeFingerprint(root)
	if fp3.DiffSHA256 == fp1.DiffSHA256 {
		t.Error("新增未跟踪文件后指纹应变化")
	}
	if fp3.Head != fp1.Head {
		t.Error("未提交时 HEAD 不应变化")
	}

	run(t, root, "git", "add", "new.txt")
	run(t, root, "git", "commit", "-q", "-m", "b")
	if fp4 := WorktreeFingerprint(root); fp4.Head == fp1.Head {
		t.Error("提交后 HEAD 应变化")
	}
}

func TestList_MarksStaleWhenWorktreeChanges(t *testing.T) {
	requireBash(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用")
	}
	root := newGitRepo(t)
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	fillScaffold(t, dir)
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\ntrue\n```\n")
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "spec")

	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	items, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || !items[0].Sealed {
		t.Fatalf("期望一条 sealed 证据，got %+v", items)
	}

	// 同一 commit 上改代码 → 证据过期（封印的是"这份工作区"，不是"这个仓库"）。
	writeFile(t, filepath.Join(root, "code.go"), "package x\n")
	items, err = List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !items[0].Stale || items[0].Sealed {
		t.Errorf("工作区变化后应 Stale 且取消 Sealed，got %+v", items[0])
	}
}

// ---------- 测试脚手架 ----------

func requireBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 不可用，跳过实跑验收测试")
	}
}

// repoWithChange 造一个已声明契约、且有一个空 change 的临时仓库（非 git）。
func repoWithChange(t *testing.T, id string) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, id, "")
	if err != nil {
		t.Fatal(err)
	}
	fillScaffold(t, dir)
	return root, dir
}

// fillScaffold 模拟"人已经把模板填完"：删掉 forge:fill 标记。
// 真实流程里人要填内容；测试只关心标记消失后 verify 是否放行。
func fillScaffold(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{FileProposal, FileDesign, FileTasks, FileAcceptance} {
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		cleaned := strings.ReplaceAll(string(raw), scaffoldMarker, "")
		if err := os.WriteFile(path, []byte(cleaned), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run(t, root, "git", "init", "-q")
	run(t, root, "git", "config", "user.email", "test@example.com")
	run(t, root, "git", "config", "user.name", "test")
	run(t, root, "git", "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(root, "README.md"), "init\n")
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "init")
	return root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v 失败: %v\n%s", name, args, err, out)
	}
}

func TestVerify_RejectsTraversalID(t *testing.T) {
	root, _ := repoWithChange(t, "demo")
	if _, err := Verify(context.Background(), root, "../etc", VerifyOptions{}); err == nil {
		t.Fatal("非法 id 不应进入执行路径（防仓库外 acceptance 被执行、证据写到仓库外）")
	}
}

// 同一秒内连跑两次是常态（失败→改→重跑）：两次必须各留一份证据，
// 共用目录会把上一次的失败现场悄悄覆盖成功的结果。
func TestVerify_SameSecondWritesSeparateEvidence(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\ntrue\n```\n")
	now := time.Date(2026, 11, 11, 10, 42, 22, 0, time.UTC)

	first, err := Verify(context.Background(), root, "demo", VerifyOptions{Now: now})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := Verify(context.Background(), root, "demo", VerifyOptions{Now: now})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.EvidenceDir == second.EvidenceDir {
		t.Fatalf("同一秒两次验证必须各写一份证据，got %s", first.EvidenceDir)
	}
}

func TestLoad_StaleAfterAcceptanceDeleted(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\ntrue\n```\n")
	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	c, _, err := Load(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Sealed {
		t.Fatalf("验证通过后应封印，got %+v", c)
	}
	if err := os.Remove(filepath.Join(dir, FileAcceptance)); err != nil {
		t.Fatal(err)
	}
	c, _, err = Load(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if c.Sealed || !c.Stale {
		t.Errorf("判据被删后旧证据应失效，got sealed=%v stale=%v", c.Sealed, c.Stale)
	}
}

// 判据存在但读不出来（权限、IO 错误）时必须判失效，不能保留封印。
// 读失败等于"无法证明判据没漂移"，fail-open 会让人拿一份旧证据继续自称已验证。
func TestLoad_StaleWhenAcceptanceUnreadable(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	accPath := filepath.Join(dir, FileAcceptance)
	writeFile(t, accPath, "```bash\ntrue\n```\n")
	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(accPath); err != nil {
		t.Fatal(err)
	}
	// 用同名目录制造"存在但 ReadFile 失败"（EISDIR）的情形。
	if err := os.Mkdir(accPath, 0o755); err != nil {
		t.Fatal(err)
	}
	c, _, err := Load(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if c.Sealed || !c.Stale {
		t.Errorf("判据读不出来时旧证据应失效，got sealed=%v stale=%v", c.Sealed, c.Stale)
	}
}

// 证据是验证的产物，不是被验证的内容：写证据不能改变工作区指纹，
// 否则每跑一次 verify，上一次的证据就把自己判成 stale。
func TestWorktreeFingerprint_IgnoresEvidence(t *testing.T) {
	root := newGitRepo(t)
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	before := WorktreeFingerprint(root)
	evDir := filepath.Join(root, "spec", "changes", "demo", "evidence", "20261111T104222Z")
	writeFile(t, filepath.Join(evDir, "summary.json"), "{}\n")

	after := WorktreeFingerprint(root)
	if !before.OK || !after.OK {
		t.Fatal("git 仓库应算得出指纹")
	}
	if before.DiffSHA256 != after.DiffSHA256 {
		t.Error("证据文件不应计入工作区指纹")
	}
}

// 工作区有未提交改动时 verify，证据绑的是文件内容：提交把这些改动落进
// commit 并不改变内容，证据继续有效；之后又改被验证的文件才失效。
func TestList_NotStaleAfterCommit(t *testing.T) {
	requireBash(t)
	root := newGitRepo(t)
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "demo", "demo")
	if err != nil {
		t.Fatal(err)
	}
	fillScaffold(t, dir)
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\ntrue\n```\n")
	writeFile(t, filepath.Join(root, "code.go"), "package demo\n")
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "spec")

	// verify 时工作区有未提交改动 → 证据绑文件内容。
	writeFile(t, filepath.Join(root, "code.go"), "package demo // v2\n")
	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "land")

	list, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("期望 1 个 change，got %d", len(list))
	}
	if !list[0].Sealed || list[0].Stale {
		t.Errorf("提交后改动已落地、内容未变，证据应仍有效，got sealed=%v stale=%v", list[0].Sealed, list[0].Stale)
	}
}

// 干净工作区 verify 没有文件级指纹，证据只代表那一个 commit；HEAD 一动就
// 不能再宣称"当前代码已验过"——哪怕新提交只碰了无关文件，也让人重跑一次。
func TestList_StaleWhenHeadMovesAfterCleanVerify(t *testing.T) {
	requireBash(t)
	root := newGitRepo(t)
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "demo", "demo")
	if err != nil {
		t.Fatal(err)
	}
	fillScaffold(t, dir)
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\ntrue\n```\n")
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "spec")

	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	list, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if !list[0].Sealed || list[0].Stale {
		t.Fatalf("干净工作区 verify 后应封印，got sealed=%v stale=%v", list[0].Sealed, list[0].Stale)
	}

	writeFile(t, filepath.Join(root, "README.md"), "changed\n")
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "move head")

	list, err = List(root)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Sealed || !list[0].Stale {
		t.Errorf("HEAD 移动后干净工作区的旧证据应失效，got sealed=%v stale=%v", list[0].Sealed, list[0].Stale)
	}
}

// 归档记录的是历史，仓库之后怎么变都不该让归档项显示成"待重跑"。
func TestList_ArchivedUnaffectedByLaterCommits(t *testing.T) {
	requireBash(t)
	root := newGitRepo(t)
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "demo", "demo")
	if err != nil {
		t.Fatal(err)
	}
	fillScaffold(t, dir)
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\ntrue\n```\n")
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "spec")
	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "seal")
	archiveDir := filepath.Join(ChangesDir(root), ArchiveDirName, "20261111-demo")
	if err := os.MkdirAll(filepath.Dir(archiveDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, archiveDir); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "README.md"), "changed\n")
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "move head")

	list, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].Archive {
		t.Fatalf("期望 1 个归档项，got %+v", list)
	}
	if list[0].Stale || list[0].Stage != StageArchived {
		t.Errorf("归档项不应因后续提交判失效，got stale=%v stage=%v", list[0].Stale, list[0].Stage)
	}
}

// 四份产物缺一份就不算填完：verify 要拒绝，而不是拿三份+一个判据封印。
func TestVerify_RefusesMissingDocs(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\ntrue\n```\n")
	if err := os.Remove(filepath.Join(dir, FileDesign)); err != nil {
		t.Fatal(err)
	}
	_, err := Verify(context.Background(), root, "demo", VerifyOptions{})
	if err == nil {
		t.Fatal("缺少 design.md 时 verify 应拒绝")
	}
	if !strings.Contains(err.Error(), FileDesign) {
		t.Errorf("错误应点名缺失的文件，got %v", err)
	}
}

// 模板骨架不是判据：init+new 之后直接 verify 必须被拒绝，
// 而不是拿模板里的示例命令把 change 封印成 verified。
func TestVerify_RefusesUnfilledScaffold(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, "demo", ""); err != nil {
		t.Fatal(err)
	}
	_, err := Verify(context.Background(), root, "demo", VerifyOptions{})
	if err == nil {
		t.Fatal("未填写的模板不应通过 verify")
	}
	if !strings.Contains(err.Error(), scaffoldMarker) {
		t.Fatalf("错误应指出骨架标记，got %v", err)
	}
}

// 管道被解析期拒绝：sh 环境没有 pipefail，`false | true` 会直接重演成假绿，
// 与其靠运行时兜，不如让这种写法根本进不了执行。
func TestVerify_RejectsPipe(t *testing.T) {
	root, dir := repoWithChange(t, "demo")
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\nfalse | true\n```\n")
	_, err := Verify(context.Background(), root, "demo", VerifyOptions{})
	if err == nil || !strings.Contains(err.Error(), "管道") {
		t.Fatalf("管道应被拒绝，got %v", err)
	}
}

// 提交之后又改被验证的文件：内容变了，证据必须失效。
func TestLoad_StaleAfterEditPostCommit(t *testing.T) {
	requireBash(t)
	root := newGitRepo(t)
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	fillScaffold(t, dir)
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\ntrue\n```\n")
	writeFile(t, filepath.Join(root, "code.go"), "package x\n")
	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "land")

	items, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || !items[0].Sealed || items[0].Stale {
		t.Fatalf("提交后内容未变，证据应仍有效，got %+v", items)
	}

	writeFile(t, filepath.Join(root, "code.go"), "package y\n")
	items, err = List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if items[0].Sealed || !items[0].Stale {
		t.Errorf("提交后又改了被验证文件，证据应失效，got %+v", items[0])
	}
}

// 注释不是判据：一条围栏里只有注释/空行时执行必然退出 0，不能拿来封印。
func TestVerify_RejectsCommentOnlyFence(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\n# TODO 以后补\n\n```\n")
	_, err := Verify(context.Background(), root, "demo", VerifyOptions{})
	if err == nil || !strings.Contains(err.Error(), "注释") {
		t.Fatalf("只有注释的围栏应被拒绝，got %v", err)
	}
}

// 证据是命令跑完后这个工作区的快照：验收命令自己生成的文件不该让证据
// 刚封印就被 list 判成失效。
func TestVerify_FingerprintTakenAfterCommands(t *testing.T) {
	requireBash(t)
	root := newGitRepo(t)
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "demo", "demo")
	if err != nil {
		t.Fatal(err)
	}
	fillScaffold(t, dir)
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\nprintf 'x\\n' > artifact.txt\n```\n")
	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	c, _, err := Load(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Sealed || c.Stale {
		t.Errorf("命令产物应计入封印后的工作区，got sealed=%v stale=%v", c.Sealed, c.Stale)
	}
}

// 一份声称通过、却没有实跑记录的 summary（手写或残缺）不能冒充封印。
func TestLoad_RejectsSummaryWithoutChecks(t *testing.T) {
	root, dir := repoWithChange(t, "demo")
	ev := filepath.Join(dir, EvidenceDirName, "20260101T000000Z")
	if err := os.MkdirAll(ev, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(ev, SummaryFile),
		`{"change":"demo","sealed":true,"diff_sha256":"abc","acceptance_sha256":""}`)
	c, _, err := Load(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if c.Sealed {
		t.Errorf("没有 checks 的摘要不应被当成封印，got %+v", c)
	}
}

// 归档目录里的证据同样是证据，不能算进被验证内容。
func TestWorktreeFingerprint_IgnoresArchivedEvidence(t *testing.T) {
	root := newGitRepo(t)
	archived := filepath.Join(ChangesDir(root), ArchiveDirName, "20260101-demo", EvidenceDirName, "20260101T000000Z")
	writeFile(t, filepath.Join(archived, "01.stdout"), "ok\n")
	fp := WorktreeFingerprint(root)
	if !fp.OK {
		t.Fatalf("git 仓库应能取指纹：%+v", fp)
	}
	for _, f := range fp.Files {
		if strings.Contains(filepath.ToSlash(f.Path), "/evidence/") {
			t.Errorf("归档证据不应计入指纹：%s", f.Path)
		}
	}
}

// 模板里写着"禁止用 || / set +e / 嵌套 shell"，verify 必须真的拒绝，
// 否则这句禁令只是劝告。
func TestVerify_RejectsForbiddenShellForms(t *testing.T) {
	for _, cmd := range []string{
		"false || true",
		"false &",
		"set +e; false; true",
		"bash -c 'exit 0'",
		"sh -c 'false | true'",
	} {
		root, dir := repoWithChange(t, "demo")
		writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\n"+cmd+"\n```\n")
		_, err := Verify(context.Background(), root, "demo", VerifyOptions{})
		if err == nil || !strings.Contains(err.Error(), "判据里不能") {
			t.Errorf("%q 应被判据检查拒绝，got %v", cmd, err)
		}
	}
}

// 引号里的 || 是字面量，不是操作符，不能误杀。
func TestVerify_AllowsQuotedOperatorLiteral(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\necho \"a||b\"\n```\n")
	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{}); err != nil {
		t.Fatalf("引号内的 || 不该被拒绝：%v", err)
	}
}

// 注释以反斜杠结尾会吞掉下一行；看起来有语句，实际永远不会执行。
func TestVerify_RejectsCommentContinuation(t *testing.T) {
	root, dir := repoWithChange(t, "demo")
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\n# TODO \\\ntrue\n```\n")
	_, err := Verify(context.Background(), root, "demo", VerifyOptions{})
	if err == nil || !strings.Contains(err.Error(), "注释") {
		t.Fatalf("反斜杠续行的注释不是判据，got %v", err)
	}
}

// 协议第 5 步是"提交"：干净工作区 verify 后提交的如果只有证据，
// 不能把刚封印的证据判成失效，否则人永远收不了尾。
func TestList_NotStaleAfterCommittingEvidence(t *testing.T) {
	requireBash(t)
	root := newGitRepo(t)
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "demo", "demo")
	if err != nil {
		t.Fatal(err)
	}
	fillScaffold(t, dir)
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\ntrue\n```\n")
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "spec")

	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "evidence")

	list, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("期望 1 个 change，got %d", len(list))
	}
	if !list[0].Sealed || list[0].Stale {
		t.Errorf("只提交证据不该判失效，got sealed=%v stale=%v", list[0].Sealed, list[0].Stale)
	}
	if list[0].BoundHead == "" {
		t.Error("封印应记录绑定的 commit")
	}
}

// 封印之后新出现的文件同样要判失效：内容比对只认记录过的路径，
// "verify 时带改动 → 提交 → 再新建文件"能同时绕过内容比对和 HEAD 比对。
func TestList_StaleWhenNewFileAppearsAfterCommit(t *testing.T) {
	requireBash(t)
	root := newGitRepo(t)
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "demo", "demo")
	if err != nil {
		t.Fatal(err)
	}
	fillScaffold(t, dir)
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\ntrue\n```\n")
	writeFile(t, filepath.Join(root, "code.go"), "package code\n")
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "spec")

	// 工作区带一个未提交改动时验证：摘要只记录 code.go。
	writeFile(t, filepath.Join(root, "code.go"), "package code\n\nvar A = 1\n")
	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "code")

	// 提交后再新建一个当初不存在、也没被记录过的文件。
	writeFile(t, filepath.Join(root, "new.go"), "package code\n\nvar B = 2\n")
	list, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("期望 1 个 change，got %d", len(list))
	}
	if !list[0].Stale || list[0].Sealed {
		t.Errorf("新出现的文件应让证据失效，got sealed=%v stale=%v", list[0].Sealed, list[0].Stale)
	}
}

// 注释和转义引号里的关键词不是代码：`#` 后写"不要用 set +e"不该被判违规，
// 双引号里的 \" 也不该提前结束字面量。
func TestVerify_IgnoresOperatorsInCommentsAndEscapes(t *testing.T) {
	requireBash(t)
	root, dir := repoWithChange(t, "demo")
	body := "```bash\n# 不要用 set +e，也别用 ||\necho \"a\\\"||b\"\n```\n"
	writeFile(t, filepath.Join(dir, FileAcceptance), body)
	res, err := Verify(context.Background(), root, "demo", VerifyOptions{})
	if err != nil {
		t.Fatalf("注释/转义里的关键词不该被拒绝：%v", err)
	}
	if !res.Summary.Sealed {
		t.Error("这条判据应封印")
	}
}

// 注释只在本行有效：注释行之后写 `false || true` 仍然是违规写法，
// 不能因为上一行是注释就被整段抹掉（否则判据检查形同虚设）。
func TestVerify_RejectsOperatorsAfterCommentLine(t *testing.T) {
	root, dir := repoWithChange(t, "demo")
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\n# 说明：下面这行是判据\nfalse || true\n```\n")
	_, err := Verify(context.Background(), root, "demo", VerifyOptions{})
	if err == nil || !strings.Contains(err.Error(), "||") {
		t.Fatalf("注释之后的 || 仍应被拒绝，got %v", err)
	}
}

// 干净工作区 verify → 只提交证据 → 再新建文件：证据不能继续宣称已验证。
func TestList_StaleWhenNewFileAfterCleanVerifyCommit(t *testing.T) {
	requireBash(t)
	root := newGitRepo(t)
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "demo", "demo")
	if err != nil {
		t.Fatal(err)
	}
	fillScaffold(t, dir)
	writeFile(t, filepath.Join(dir, FileAcceptance), "```bash\ntrue\n```\n")
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "spec")

	// 干净树验证：摘要里没有文件级摘要，只能绑 HEAD。
	if _, err := Verify(context.Background(), root, "demo", VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	run(t, root, "git", "add", "-A")
	run(t, root, "git", "commit", "-q", "-m", "evidence")

	writeFile(t, filepath.Join(root, "later.go"), "package later\n")
	list, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("期望 1 个 change，got %d", len(list))
	}
	if !list[0].Stale || list[0].Sealed {
		t.Errorf("干净验证后又出现新文件应判失效，got sealed=%v stale=%v", list[0].Sealed, list[0].Stale)
	}
}
