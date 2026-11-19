package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/change"
	"github.com/MjxUpUp/Forge/internal/specroot"
)

// 回归（代码评审 P2）：老 change（没有 goal.md）可以走到 ready，但它不能 --change
// （钉契约要五段 goal）。NextRepoContext 必须把它排除在 ReadyChanges 之外，否则
// next / gate / status 会建议一条照抄就报错的命令。
func TestNextRepoContext_ReadyChangeWithoutGoalIsNotRecommended(t *testing.T) {
	root := t.TempDir()
	if _, err := change.Init(root, specroot.ProfileMini); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := change.New(root, "demo", "demo"); err != nil {
		t.Fatalf("New: %v", err)
	}
	dir := filepath.Join(root, "spec", "changes", "demo")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("clarify.md", "## 边界\n\n只改 A 的渲染路径\n\n## 非目标\n\n不动 B\n\n人确认：2026-11-16T00:00:00Z\n")
	write("proposal.md", "## 问题\n\nx\n\n## 提案\n\ny\n")
	write("design.md", "## 方案\n\nz\n")
	write("tasks.md", "- [ ] 改 A（判据：acceptance #1）\n")
	write("acceptance.md", "```bash\n# 1. build\ngo build ./...\n```\n")
	// 退化成老 change：删掉 goal.md 与 .goal-required。
	for _, name := range []string{change.FileGoal, change.GoalRequiredFile, change.FileChange} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatalf("remove %s: %v", name, err)
		}
	}

	phase, _, err := change.ComputePhase(root, "demo")
	if err != nil {
		t.Fatalf("ComputePhase: %v", err)
	}
	if phase != change.PhaseReady {
		t.Fatalf("老 change 应能到 ready，实际 %s", phase)
	}

	ctx := NextRepoContext(root, "main", true, 0)
	for _, id := range ctx.ReadyChanges {
		if id == "demo" {
			t.Fatalf("ready 但无 goal.md 的老 change 不应进 ReadyChanges（--change 会失败）")
		}
	}
	if len(ctx.OpenChanges) != 1 || ctx.OpenChanges[0] != "demo" {
		t.Fatalf("OpenChanges = %v，应仍含 demo", ctx.OpenChanges)
	}
}

// next 不该把用户拖回旧活：ready 但三天没动过的 change 进 StaleChanges，
// 决策给新任务入口（change new），而不是 task start --change <stale>。
// 2026-11-19 实证：一个 2h17m 的会话开头被 next 指向已放弃的 ready change。
func TestNextRepoContext_StaleReadyChangeIsNotRecommended(t *testing.T) {
	root := t.TempDir()
	if _, err := change.Init(root, specroot.ProfileMini); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := change.New(root, "doudou", "doudou"); err != nil {
		t.Fatalf("New: %v", err)
	}
	dir := filepath.Join(root, "spec", "changes", "doudou")
	if err := os.WriteFile(filepath.Join(dir, "clarify.md"), []byte("## 边界\n\nb\n\n## 非目标\n\nn\n\n人确认：2026-11-16T00:00:00Z\n"), 0o644); err != nil {
		t.Fatalf("clarify: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "goal.md"), []byte("# t\n\n## Objective\n\n做 x\n\n## Out of Scope\n\n不做 y\n\n## Check\n\n- 判据\n"), 0o644); err != nil {
		t.Fatalf("goal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "change.md"), []byte("## 改动\n\nx\n\n## 影响\n\ny\n\n## 验证\n\nz\n"), 0o644); err != nil {
		t.Fatalf("change.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "acceptance.md"), []byte("```bash\ngo build ./...\n```\n"), 0o644); err != nil {
		t.Fatalf("acceptance: %v", err)
	}
	old := time.Now().Add(-5 * 24 * time.Hour)
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		return os.Chtimes(path, old, old)
	}); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	ctx := NextRepoContext(root, "main", true, 0)
	if len(ctx.ReadyChanges) != 0 {
		t.Fatalf("陈旧 change 不应进 ReadyChanges: %v", ctx.ReadyChanges)
	}
	if len(ctx.StaleChanges) != 1 || ctx.StaleChanges[0] != "doudou" {
		t.Fatalf("StaleChanges = %v，应含 doudou", ctx.StaleChanges)
	}
	got := NextDecisionCtx(ctx, nil)
	if got.Next != "forge change new <id>" {
		t.Fatalf("陈旧 change 场景应给新任务入口，实际 %q", got.Next)
	}
	if !strings.Contains(got.Reason, "三天") {
		t.Fatalf("理由应说明陈旧，实际 %q", got.Reason)
	}
}

// 新鲜的 ready change 仍按原规则推荐 task start --change。
func TestNextRepoContext_FreshReadyChangeIsRecommended(t *testing.T) {
	root := t.TempDir()
	if _, err := change.Init(root, specroot.ProfileMini); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := change.New(root, "fresh", "fresh"); err != nil {
		t.Fatalf("New: %v", err)
	}
	dir := filepath.Join(root, "spec", "changes", "fresh")
	for name, body := range map[string]string{
		"clarify.md":    "## 边界\n\nb\n\n## 非目标\n\nn\n\n人确认：2026-11-19T00:00:00Z\n",
		"goal.md":       "# t\n\n## Objective\n\n做 x\n\n## Out of Scope\n\n不做 y\n\n## Check\n\n- 判据\n",
		"change.md":     "## 改动\n\nx\n\n## 影响\n\ny\n\n## 验证\n\nz\n",
		"acceptance.md": "```bash\ngo build ./...\n```\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	ctx := NextRepoContext(root, "main", true, 0)
	if len(ctx.ReadyChanges) != 1 || ctx.ReadyChanges[0] != "fresh" {
		t.Fatalf("新鲜 ready change 应进 ReadyChanges: %v", ctx.ReadyChanges)
	}
	if len(ctx.StaleChanges) != 0 {
		t.Fatalf("StaleChanges 应为空: %v", ctx.StaleChanges)
	}
	got := NextDecisionCtx(ctx, nil)
	if !strings.Contains(got.Next, "--change fresh") {
		t.Fatalf("应推荐 task start --change fresh，实际 %q", got.Next)
	}
}

// writeReadyChange 造一个 ready 的 change，并把目录内所有文件的时间回拨 age。
func writeReadyChange(t *testing.T, root, id string, age time.Duration) {
	t.Helper()
	if _, err := change.New(root, id, id); err != nil {
		t.Fatalf("New(%s): %v", id, err)
	}
	dir := filepath.Join(root, "spec", "changes", id)
	for name, body := range map[string]string{
		"clarify.md":    "## 边界\n\nb\n\n## 非目标\n\nn\n\n人确认：2026-11-19T00:00:00Z\n",
		"goal.md":       "# t\n\n## Objective\n\n做 x\n\n## Out of Scope\n\n不做 y\n\n## Check\n\n- 判据\n",
		"change.md":     "## 改动\n\nx\n\n## 影响\n\ny\n\n## 验证\n\nz\n",
		"acceptance.md": "```bash\ngo build ./...\n```\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if age > 0 {
		ts := time.Now().Add(-age)
		if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			return os.Chtimes(path, ts, ts)
		}); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
}

// 72h 边界：71h 仍算新鲜、73h 才算陈旧（评审 P3-e：只有「5 天/刚建」两侧用例，
// 把 StaleAfter 改成 24h 也全绿）。
func TestNextRepoContext_StaleBoundary(t *testing.T) {
	cases := []struct {
		name  string
		age   time.Duration
		stale bool
	}{
		{"71h", 71 * time.Hour, false},
		{"73h", 73 * time.Hour, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if _, err := change.Init(root, specroot.ProfileMini); err != nil {
				t.Fatalf("Init: %v", err)
			}
			writeReadyChange(t, root, "demo", tc.age)
			ctx := NextRepoContext(root, "main", true, 0)
			if tc.stale {
				if len(ctx.StaleChanges) != 1 {
					t.Fatalf("%v 应判陈旧，StaleChanges=%v ReadyChanges=%v", tc.age, ctx.StaleChanges, ctx.ReadyChanges)
				}
				return
			}
			if len(ctx.ReadyChanges) != 1 {
				t.Fatalf("%v 应算新鲜，StaleChanges=%v ReadyChanges=%v", tc.age, ctx.StaleChanges, ctx.ReadyChanges)
			}
		})
	}
}

// 评审 P2-b：stale 分支曾排在 OpenChanges 之前，会把「手上正在填的 change」盖掉。
// 半成品（缺 clarify）+ 陈旧 ready 时，应先推半成品的 phase。
func TestNextRepoContext_OpenChangeBeatsStale(t *testing.T) {
	root := t.TempDir()
	if _, err := change.Init(root, specroot.ProfileMini); err != nil {
		t.Fatalf("Init: %v", err)
	}
	writeReadyChange(t, root, "old", 5*24*time.Hour)
	if _, err := change.New(root, "half", "half"); err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := NextRepoContext(root, "main", true, 0)
	got := NextDecisionCtx(ctx, nil)
	if !strings.Contains(got.Next, "forge change phase half") {
		t.Fatalf("半成品 change 应优先于陈旧 change，实际 %q（原因 %q）", got.Next, got.Reason)
	}
}

// 评审 P3-a：state 里要有陈旧 change 的 id，而不只是一个计数。
func TestNextRepoContext_StaleChangeIDsExposed(t *testing.T) {
	root := t.TempDir()
	if _, err := change.Init(root, specroot.ProfileMini); err != nil {
		t.Fatalf("Init: %v", err)
	}
	writeReadyChange(t, root, "old", 5*24*time.Hour)
	ctx := NextRepoContext(root, "main", true, 0)
	got := NextDecisionCtx(ctx, nil)
	if ctx.StaleChangeIDs == nil || len(ctx.StaleChangeIDs) != 1 || ctx.StaleChangeIDs[0] != "old" {
		t.Fatalf("StaleChangeIDs = %v，应含 old", ctx.StaleChangeIDs)
	}
	if !strings.Contains(got.Next, "change new") {
		t.Fatalf("只有陈旧 change 时应给新任务入口，实际 %q", got.Next)
	}
	if !strings.Contains(got.Reason, "old") {
		t.Fatalf("理由应点名陈旧 change，实际 %q", got.Reason)
	}
}

// TestNextRepoContext_ShallowCloneStaleChangeIsNotRecommended 浅克隆里 next 必须把
// 「无法判定历史」的 ready change 当陈旧消费（评审 P3-e：之前只有 Activities 层测试，
// 把 next.go 的 `ok && (IsZero || …)` 改回 `!last.IsZero()` 时全包测试仍绿）。
func TestNextRepoContext_ShallowCloneStaleChangeIsNotRecommended(t *testing.T) {
	base := t.TempDir()
	gitDo := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = base
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitDo("init", "-q")
	gitDo("config", "user.email", "t@example.com")
	gitDo("config", "user.name", "t")
	if _, err := change.Init(base, specroot.ProfileMini); err != nil {
		t.Fatal(err)
	}
	if _, err := change.New(base, "old", "old"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "spec", "changes", "old")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write(change.FileGoal, "# goal：old\n\n## Objective\n\nold goal\n\n## Out of Scope\n\nnone\n\n## Check\n\nls\n")
	write(change.FileChange, "# change：old\n\n## 改动\n\nx\n\n## 影响\n\nnone\n\n## 验证\n\nacceptance\n")
	write(change.FileAcceptance, "# acceptance：old\n\n```bash\nls .\n```\n")
	write(change.FileClarify, "## 边界\n\nonly this\n\n人确认：2026-11-19T00:00:00Z\n")
	old := time.Now().Add(-5 * 24 * time.Hour).Format(time.RFC3339)
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = base
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "old")
	cmd.Dir = base
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+old, "GIT_COMMITTER_DATE="+old)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(base, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", "-A")
	cmd.Dir = base
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "touch root")
	cmd.Dir = base
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	cloneDir := t.TempDir()
	cmd = exec.Command("git", "clone", "--depth", "1", "file://"+filepath.ToSlash(base), cloneDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("clone --depth 1 不可用: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", cloneDir, "rev-parse", "--is-shallow-repository").Output(); err != nil || strings.TrimSpace(string(out)) != "true" {
		t.Skipf("clone 不是浅克隆，跳过: %v %s", err, out)
	}

	ctx := NextRepoContext(cloneDir, "master", true, 0)
	for _, id := range ctx.ReadyChanges {
		if id == "old" {
			t.Fatalf("浅克隆无法判定历史，old 不应进 ReadyChanges")
		}
	}
	found := false
	for _, id := range ctx.StaleChanges {
		if id == "old" {
			found = true
		}
	}
	if !found {
		t.Fatalf("浅克隆里 old 应作为陈旧出现，StaleChanges=%v", ctx.StaleChanges)
	}
}
