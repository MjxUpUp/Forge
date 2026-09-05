package taskpipeline

// pushgate_test.go — 推送边界门禁的表驱动测试：临时 git 仓库四态（干净通过 /
// cheat 命中阻断 / 未消解任务阻断 / 逃生舱跳过）+ 范围扫描与钩子脚本的契约。

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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

// TestRunPushGate_FullyQualifiedRef 归一化回归（对抗审查 blocker）：pre-push 钩子
// 传入 refs/heads/<branch>，TaskState.Branch 存裸名——不剥前缀则任务检查静默失效。
func TestRunPushGate_FullyQualifiedRef(t *testing.T) {
	dir := newPushRepo(t)
	gitBranch(t, dir, "feat/fqr")
	commitFile(t, dir, "ok.go", "package main\n", "ok")
	// 带 refs/heads/ 前缀调用——Ref 字段必须归一化为裸分支名。
	res := RunPushGate(dir, "refs/heads/feat/fqr")
	if res.Skipped {
		t.Fatalf("不应 skipped: %+v", res)
	}
	if res.Ref != "feat/fqr" {
		t.Fatalf("ref 应归一化为裸分支名，实际 %q（全限定 ref 会让 blockedTasksOnBranch 永不命中——对抗审查 blocker）", res.Ref)
	}
}

// TestBlockedTasksOnBranch 未消解 BLOCKED 行的分支任务应被列出（回归覆盖：新增源码
// 分支此前零覆盖）。latest-per-check 语义：同一 check 后续 pass 行消解早先 blocked。
func TestBlockedTasksOnBranch(t *testing.T) {
	dir := t.TempDir()
	// 直接种一份带 blocked 行的任务状态 + checklog（不经完整 task start 编排）。
	s := &TaskState{TaskRef: "t/b1", Branch: "feat/bt"}
	s.Checklist = []ChecklistItem{{ID: 1, Desc: "x", Done: true}}
	if err := SaveTaskState(dir, s); err != nil {
		t.Fatal(err)
	}
	checklog.Record(dir, &checklog.Entry{
		Check: checklog.CheckTaskGuard, Passed: false, Checked: true,
		Level: checklog.LevelBlocked, TaskRef: "t/b1",
		Detail: "BLOCKED: test",
	})
	got := blockedTasksOnBranch(dir, "feat/bt")
	if len(got) != 1 || got[0] != "t/b1" {
		t.Fatalf("应列出未消解任务: %v", got)
	}
	// 后续同 check pass 行消解。
	checklog.Record(dir, &checklog.Entry{
		Check: checklog.CheckTaskGuard, Passed: true, Checked: true,
		Level: checklog.LevelPass, TaskRef: "t/b1", Detail: "pass",
	})
	got = blockedTasksOnBranch(dir, "feat/bt")
	if len(got) != 0 {
		t.Fatalf("消解后不应列出: %v", got)
	}
}

// TestProducersOnBranch 生产者声明聚合（P2-2）：OriginTool + 台账 NodeID 去重。
func TestProducersOnBranch(t *testing.T) {
	dir := newPushRepo(t) // 需 seed commit 才能建分支
	gitBranch(t, dir, "feat/pp")
	// 两个未完成任务：不同 OriginTool；一个任务带 NodeID。
	s1 := &TaskState{TaskRef: "t/p1", Branch: "feat/pp", OriginTool: "zcode"}
	SaveTaskState(dir, s1)
	s2 := &TaskState{TaskRef: "t/p2", Branch: "feat/pp", OriginTool: "claude-code"}
	SaveTaskState(dir, s2)
	s3 := &TaskState{TaskRef: "t/done", Branch: "feat/pp", OriginTool: "old-tool"}
	now := time.Now()
	s3.CompletedAt = &now // 完成任务不计
	SaveTaskState(dir, s3)
	// NodeID 经内嵌 nodestamp.Stamp 提升——Record 落盘 node_id 字段。
	seed := checklog.Entry{Check: checklog.CheckTaskVerify, Passed: true, TaskRef: "t/p1"}
	seed.Stamp.NodeID = "fnode_aa"
	seed.RecordedAt = time.Now()
	checklog.Record(dir, &seed)
	seed2 := seed
	seed2.RecordedAt = time.Now().Add(time.Second) // 同 node 去重
	checklog.Record(dir, &seed2)
	got := producersOnBranch(dir, "feat/pp")
	want := []string{"node:fnode_aa", "tool:claude-code", "tool:zcode"}
	if len(got) != len(want) {
		t.Fatalf("聚合不符: %v（期望 %v）", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 项不符: %v", i, got)
		}
	}
}
