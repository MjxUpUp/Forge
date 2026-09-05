package taskpipeline

// pushgate_test.go — 推送边界门禁的表驱动测试：临时 git 仓库四态（干净通过 /
// cheat 命中阻断 / 未消解任务阻断 / 逃生舱跳过）+ 范围扫描与钩子脚本的契约。

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// newPushRepo 构造带一次初始提交的临时 git 仓库。
func newPushRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "seed")
	return dir
}

// gitBranch 建并切换分支（夹具：cheat 场景的真实推送形态）。
func gitBranch(t *testing.T, dir, name string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "checkout", "-b", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b: %v\n%s", err, out)
	}
}

func commitFile(t *testing.T, dir, name, body, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmds := [][]string{{"add", "."}, {"commit", "-m", msg}}
	for _, args := range cmds {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestRunPushGate_CleanPass(t *testing.T) {
	dir := newPushRepo(t)
	res := RunPushGate(dir, "")
	if res.Skipped {
		t.Fatalf("不应 skipped: %+v", res)
	}
	if res.Blocked() {
		t.Fatalf("干净仓库不应阻断: %+v", res)
	}
	// 证据快照应落盘
	snap := filepath.Join(dataHome(dir), "pushes")
	entries, err := os.ReadDir(snap)
	if err != nil || len(entries) == 0 {
		t.Fatalf("推送快照缺失: %v", err)
	}
	// checklog 行
	rows, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range rows {
		if e.Check == checklog.CheckGatePush && e.EffectiveLevel() == checklog.LevelPass {
			found = true
		}
	}
	if !found {
		t.Fatalf("应落 gate-push pass 行: %+v", rows)
	}
}

func TestRunPushGate_CheatBlocks(t *testing.T) {
	dir := newPushRepo(t)
	// 真实推送形态：特性分支上作恶——merge-base(HEAD, main) 是 seed，范围含 cheat 提交。
	gitBranch(t, dir, "feat/x")
	// error-swallow 形态：空 catch 体（errorSwallowRe 的跨语言高置信模式）。
	commitFile(t, dir, "app.js", "function f() {\n  try { g(); } catch (e) {}\n}\n", "cheat")
	res := RunPushGate(dir, "feat/x")
	if !res.Blocked() || len(res.Findings) == 0 {
		t.Fatalf("cheat 命中应阻断: %+v", res)
	}
}

func TestRunPushGate_EscapeSkips(t *testing.T) {
	dir := newPushRepo(t)
	commitFile(t, dir, "main.go", "package main\nfunc main() {\n\t_ = recover()\n}\n", "cheat")
	t.Setenv("FORGE_GATE_PUSH", "disable")
	res := RunPushGate(dir, "")
	if !res.Skipped || res.Blocked() {
		t.Fatalf("逃生舱应跳过: %+v", res)
	}
}

func TestRunPushGate_DirtyWarns(t *testing.T) {
	dir := newPushRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "uncommitted.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := RunPushGate(dir, "")
	if res.Blocked() {
		t.Fatalf("dirty 只 warn 不阻断: %+v", res)
	}
	if !res.Dirty {
		t.Fatalf("应检出 dirty: %+v", res)
	}
}

func TestScanCheatPatternsRange_RangeOnly(t *testing.T) {
	dir := newPushRepo(t)
	// 第一笔：干净；第二笔：cheat。范围扫描只看 base...HEAD——干净提交不产 finding。
	commitFile(t, dir, "a.go", "package main\n", "clean")
	res := ScanCheatPatternsRange(dir, "HEAD")
	if len(res) != 0 {
		t.Fatalf("无新增源码行应零 finding: %+v", res)
	}
}
