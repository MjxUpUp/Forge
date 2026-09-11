package clitask

import (
	"os/exec"
	"strings"
	"testing"
)

// C.3 stderr 兜底测试（docs/design/harness-fixes-a-g-2026-09.md 设计 C 验收项：
// `forge task gate … | tail -1` 场景下 stderr 可见 BLOCKED）。runForge 以子进程跑
// 编译好的 forge，stdout 是管道——正是「stdout 非 TTY」的目标形态，无需注入。

// TestTaskGate_BlockedMirrorsToStderrWhenPiped pins the design-C fallback: with stdout
// piped (the dominant two-machine audit form `gate … | tail -N`), the BLOCKED verdict
// line AND the recovery next line must reach stderr — the exit-code contract's text face
// can no longer be swallowed by a truncating pipe. The passed path stays stdout-only.
//
// TestTaskGate_BlockedMirrorsToStderrWhenPiped 钉住设计 C 兜底：stdout 接管道时
// （两机审计主形态 `gate … | tail -N`），BLOCKED 判定行与恢复用 next 行必须落 stderr
// ——退出码契约的文本面不再被截断管道吞掉。passed 路径保持只写 stdout。
func TestTaskGate_BlockedMirrorsToStderrWhenPiped(t *testing.T) {
	root := setupNextProject(t)
	stdout, stderr, code := runForgeStreams(t, root, "task", "gate", "task-implement", "--ref", "feat/next-line")
	if code == 0 {
		t.Fatalf("empty-branch implement gate must BLOCK (exit != 0), got 0:\n%s", stdout)
	}
	if !strings.Contains(stderr, "BLOCKED") {
		t.Fatalf("piped stdout must mirror the BLOCKED verdict to stderr (design C.3):\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	if !strings.Contains(stderr, "→ next:") {
		t.Fatalf("BLOCKED exit must also mirror the recovery next line to stderr (design B risk note):\nstderr: %s", stderr)
	}
}

// TestTaskGate_PassedDoesNotMirrorToStderr pins the no-noise half: on the passed path
// (exit 0 — the contract is already complete), stderr must not carry the duplicated
// verdict/next lines.
//
// TestTaskGate_PassedDoesNotMirrorToStderr 钉住无噪音的一半：passed 路径（退出码 0
// ——契约本身已完整）不得在 stderr 重复判定/next 行。
func TestTaskGate_PassedDoesNotMirrorToStderr(t *testing.T) {
	root := setupNextProject(t)
	// 先让 implement 可过：给分支补一个提交（task-implement 要求新提交或脏工作区）。
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("commit", "--allow-empty", "-m", "impl")
	stdout, stderr, code := runForgeStreams(t, root, "task", "gate", "task-implement", "--ref", "feat/next-line")
	if code != 0 {
		t.Fatalf("implement gate with a fresh commit should pass, exit %d:\n%s", code, stderr)
	}
	// 正向断言先把行为钉成 stdout-only（而非「哪都不出现」——verdict 行被整体删除时负向
	// 断言不报警，评审漏测清单第 3 条）。
	if !strings.Contains(stdout, "— passed") || !strings.Contains(stdout, "→ next:") {
		t.Fatalf("passed path must print verdict and next line to stdout:\n%s", stdout)
	}
	if strings.Contains(stderr, "— passed") || strings.Contains(stderr, "→ next:") {
		t.Fatalf("passed path must stay stdout-only (no stderr mirror):\nstderr: %s", stderr)
	}
}

// TestTaskGate_SilentBlockedNoMirrorOutput pins the silent path: --silent is the hook
// exit-code contract (text-free by design) — a BLOCKED exit must not print the verdict
// line or mirror anything to stderr.
//
// TestTaskGate_SilentBlockedNoMirrorOutput 钉住 silent 路径：--silent 是 hook 的退出码
// 契约（设计上无文本面）——BLOCKED 出口不得打印判定行，也不得镜像到 stderr。
func TestTaskGate_SilentBlockedNoMirrorOutput(t *testing.T) {
	root := setupNextProject(t)
	stdout, stderr, code := runForgeStreams(t, root, "task", "gate", "task-implement", "--ref", "feat/next-line", "--silent")
	if code == 0 {
		t.Fatalf("empty-branch implement gate must BLOCK (exit != 0), got 0:\n%s", stdout)
	}
	for _, s := range []string{"❌", "→ next:", "BLOCKED"} {
		if strings.Contains(stdout, s) {
			t.Fatalf("silent mode must not print %q to stdout:\n%s", s, stdout)
		}
		if strings.Contains(stderr, s) {
			t.Fatalf("silent mode must not mirror %q to stderr:\n%s", s, stderr)
		}
	}
}

// TestMirrorBlockedToStderrDecision pins the C.3 decision table (pure-function anchor —
// the TTY side cannot be exercised by the piped subprocess e2e above).
//
// TestMirrorBlockedToStderrDecision 钉住 C.3 判定表（纯函数锚点——TTY 一侧无法由上面
// 管道子进程 e2e 走到）。
func TestMirrorBlockedToStderrDecision(t *testing.T) {
	cases := []struct {
		name                string
		passed, silent, tty bool
		want                bool
	}{
		{"piped blocked mirrors", false, false, false, true},
		{"tty blocked does not mirror (noise guard)", false, false, true, false},
		{"piped passed does not mirror (contract complete)", true, false, false, false},
		{"silent blocked does not mirror (hook contract is text-free)", false, true, false, false},
	}
	for _, c := range cases {
		if got := mirrorBlockedToStderr(c.passed, c.silent, c.tty); got != c.want {
			t.Errorf("%s: mirrorBlockedToStderr(%v,%v,%v) = %v, want %v", c.name, c.passed, c.silent, c.tty, got, c.want)
		}
	}
}

// TestBlockedResultLineFormat pins the single formatting source shared by the stdout and
// stderr mirror sides (pure-function anchor — the two writers cannot drift apart).
//
// TestBlockedResultLineFormat 钉住 stdout 与 stderr 镜像两侧共用的单一格式化源
// （纯函数锚点——两处写入不会漂移）。
func TestBlockedResultLineFormat(t *testing.T) {
	got := blockedResultLine("task-implement", "insufficient work activity")
	want := "  ❌ task-implement — BLOCKED: insufficient work activity\n"
	if got != want {
		t.Fatalf("blockedResultLine drift:\ngot:  %q\nwant: %q", got, want)
	}
	if got := nextHintLine("forge task gate task-verify --ref x", "implement 已过"); !strings.HasPrefix(got, "  → next: forge task gate task-verify --ref x（") {
		t.Fatalf("nextHintLine drift: %q", got)
	}
}
