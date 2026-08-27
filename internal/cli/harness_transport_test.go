package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHarnessTransport_FirstPushHITL pins T9 (multi-task-concurrency §13): the first push
// is an outbound action — non-TTY (agent context) is REFUSED with the manifest hint; the
// full round (push → clone-fresh pull) works against a local bare remote; trust anchors
// never travel.
//
// TestHarnessTransport_FirstPushHITL 钉住 T9（multi-task-concurrency §13）：首推是外发
// 动作——非 TTY（agent 场景）被拒并附清单指引；完整回路（push → 全新 clone pull）
// 对本地 bare 远端成立；信任锚绝不随行。
func TestHarnessTransport_FirstPushHITL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	if stdout, _, code := runForge(t, t.TempDir(), "harness", "push"); code == 0 || !strings.Contains(stdout, "未建立") {
		t.Fatalf("未 init 的 push 应被拒: %s", stdout)
	}

	if stdout, _, code := runForge(t, t.TempDir(), "harness", "init", "--yes"); code != 0 {
		t.Fatalf("init: %s", stdout)
	}
	// 无远端 → 拒绝并指引。
	if stdout, _, code := runForge(t, t.TempDir(), "harness", "push"); code == 0 || !strings.Contains(stdout, "未配置远端") {
		t.Fatalf("无远端 push 应指引: %s", stdout)
	}

	// 本地 bare 远端 + 配置。
	remote := t.TempDir() + "-remote.git"
	os.MkdirAll(remote, 0o755)
	if out, err := harnessGit(remote, "init", "--bare", "-b", "main"); err != nil {
		t.Fatalf("bare remote: %v\n%s", err, out)
	}
	// git init -b 需要 git>=2.28；失败回落默认分支即可（不判定名字）。
	if out, err := harnessGit(home, "remote", "add", "origin", remote); err != nil {
		t.Fatalf("remote add: %v\n%s", err, out)
	}

	// 首推：非 TTY 且无 --yes → 拒（agent 不得代批外发）。
	if stdout, _, code := runForge(t, t.TempDir(), "harness", "push"); code == 0 || !strings.Contains(stdout, "人在终端确认") {
		t.Fatalf("非 TTY 首推应被拒: %s", stdout)
	}
	// 首推（--yes 走脚本路径验证 git 回路本身）。推前放一个 tracked 过程状态条目，
	// 使「到达性」断言有实料。
	os.MkdirAll(filepath.Join(home, "projects", "k1", "tasks"), 0o755)
	os.WriteFile(filepath.Join(home, "projects", "k1", "tasks", "x.json"), []byte(`{"task_ref":"x"}`), 0o644)
	if stdout, _, code := runForge(t, t.TempDir(), "harness", "push", "--yes"); code != 0 {
		t.Fatalf("首推失败: %s", stdout)
	}

	// 信任锚不随行：远端按【实际推送分支】检索（bare 的 HEAD 可能指向别的默认分支名）。
	branchOut, err := harnessGit(home, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v\n%s", err, branchOut)
	}
	branch := strings.TrimSpace(branchOut)
	out, err := harnessGit(remote, "ls-tree", "-r", "--name-only", branch)
	if err != nil {
		t.Fatalf("ls-tree: %v\n%s", err, out)
	}
	if strings.Contains(out, "stamps/") || strings.Contains(out, "workspaces/") || strings.Contains(out, "hazards/") {
		t.Fatalf("排除清单内容泄漏到远端:\n%s", out)
	}
	if !strings.Contains(out, ".gitignore") {
		t.Fatalf("tracked 内容应至少含 .gitignore:\n%s", out)
	}

	// 全新 clone → pull 回路。
	fresh := t.TempDir() + "-fresh"
	if out, err := harnessGit(t.TempDir(), "clone", remote, fresh); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	t.Setenv("FORGE_DATA_HOME", fresh)
	if stdout, _, code := runForge(t, t.TempDir(), "harness", "pull"); code != 0 {
		t.Fatalf("pull 失败: %s", stdout)
	}
	// cloned 侧可见 tracked 状态条目（到达性）。
	if _, err := os.Stat(filepath.Join(fresh, "projects", "k1", "tasks", "x.json")); err != nil {
		t.Fatalf("clone 后应含推送的 tracked 条目: %v", err)
	}
	t.Setenv("FORGE_DATA_HOME", home)
}
