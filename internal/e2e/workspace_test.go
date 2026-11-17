package e2e

// 多仓 workspace 端到端测试（docs/design/multi-repo-workspace.md）：清单
// （~/.forge/workspaces.json）、status 聚合、doctor 的 drift 与跨仓依赖环
// 检出、task-verify 的 cross-repo-impact 门禁（advisory → protocol required
// 阻断 → 声明后放行）、跨仓 DependsOn（key:ref 的 pending 语义）——全部走
// 编译产物 + 共享一个 FORGE_DATA_HOME 的两个真实 git 仓。

import (
	"os"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// wsFreshProject 是去掉 FORGE_DATA_HOME 钉设的 freshProject：workspace 横跨
// 多仓，由调用方统一钉一个共享 home（freshProject 会给每仓各钉一个，清单/
// 注册表/DataDir 会分裂）。含 initial commit（task start --branch 需要干净
// 基线）。
func wsFreshProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "master")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	initGoProject(t, dir)
	forge(t, dir, "init")
	// 门禁类 e2e 需要宿主接线后的 DataDir（protocol.yml/hooks）：Phase C 起
	// init 只写仓库物，接线是 integrations enable 的显式 opt-in。
	forge(t, dir, "registry", "add", ".")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "initial")
	return dir
}

// wsFixture 建两仓 workspace 夹具：同一共享 FORGE_DATA_HOME 下的仓 A、仓 B，
// 创建 workspace "ws" 并加入两仓。返回 (dirA, dirB, keyA, keyB)。
func wsFixture(t *testing.T) (dirA, dirB, keyA, keyB string) {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dirA = wsFreshProject(t)
	dirB = wsFreshProject(t)
	var err error
	if keyA, err = forgedata.Key(dirA); err != nil {
		t.Fatalf("derive keyA: %v", err)
	}
	if keyB, err = forgedata.Key(dirB); err != nil {
		t.Fatalf("derive keyB: %v", err)
	}
	forge(t, dirA, "workspace", "create", "ws")
	forge(t, dirA, "workspace", "add", "ws")
	forge(t, dirA, "workspace", "add", "ws", "--path", dirB)
	return dirA, dirB, keyA, keyB
}

// TestWorkspaceCreateAddListStatus covers the manifest lifecycle: create → add (default cwd + --path) → list shows both member keys → status aggregates the active task of member A across the two repos.
//
// TestWorkspaceCreateAddListStatus 覆盖清单生命周期：create → add（默认当前
// 目录 + --path）→ list 列出两个成员 key → status 跨两仓聚合成员 A 的活跃
// 任务。
func TestWorkspaceCreateAddListStatus(t *testing.T) {
	dirA, _, keyA, keyB := wsFixture(t)

	out := forge(t, dirA, "workspace", "list")
	if !strings.Contains(out, keyA) || !strings.Contains(out, keyB) {
		t.Errorf("workspace list 应含两仓 key（%s / %s），got:\n%s", keyA, keyB, out)
	}

	// 仓 A 起一个活跃任务 → status 跨两个成员聚到它。
	forge(t, dirA, "task", "start", "--ref", "feat/ws-a", "--title", "a task", "--branch")
	out = forge(t, dirA, "workspace", "status", "ws")
	if !strings.Contains(out, keyA) || !strings.Contains(out, keyB) {
		t.Errorf("workspace status 应列出两仓成员，got:\n%s", out)
	}
	if !strings.Contains(out, "feat/ws-a") {
		t.Errorf("workspace status 应聚合仓 A 的活跃任务 feat/ws-a，got:\n%s", out)
	}
	if !strings.Contains(out, `共 1 个活跃任务（2 个成员仓）`) {
		t.Errorf("workspace status 聚合计数不对，got:\n%s", out)
	}
}

// TestWorkspaceDoctorDrift covers drift detection: after a member repo's directory is moved away, the registry prunes it, and doctor must surface the member as not-registered (advisory, exit 0).
//
// TestWorkspaceDoctorDrift 覆盖 drift 检出：成员仓目录被搬走后 registry 将其
// 精简，doctor 必须把该成员报为 not-registered（advisory，exit 0）。
func TestWorkspaceDoctorDrift(t *testing.T) {
	dirA, dirB, _, _ := wsFixture(t)

	// 搬走仓 B（模拟删除/移动——清单仍缓存旧路径，registry 丢弃死条目）。
	if err := os.Rename(dirB, dirB+`-gone`); err != nil {
		t.Fatalf("move repo B away: %v", err)
	}

	out := forge(t, dirA, "workspace", "doctor")
	if !strings.Contains(out, `not-registered`) {
		t.Errorf("doctor 应报 not-registered drift，got:\n%s", out)
	}
	if !strings.Contains(out, `advisory`) {
		t.Errorf("doctor 应声明全部 advisory 不阻断，got:\n%s", out)
	}
}

// TestWorkspaceDoctorDepCycle covers the advisory cross-repo dependency-cycle detection.
//
// TestWorkspaceDoctorDepCycle 覆盖跨仓依赖环的 advisory 检出：仓 A 的 ta 依赖
// keyB:feat/tb、仓 B 的 tb 依赖 keyA:feat/ta——写入侧任何检查都不拒的环
// （AddDependency 绝不遍历他仓图）→ `forge workspace doctor` 必须报
// dep-cycle。
func TestWorkspaceDoctorDepCycle(t *testing.T) {
	dirA, dirB, keyA, keyB := wsFixture(t)

	// 先建前向引用（tb 尚未创建 → 容忍，stderr 给 advisory），再建回边——
	// 两条边闭合成环。
	forge(t, dirA, "task", "start", "--ref", "feat/ta", "--title", "a task", "--branch", "--depends-on", keyB+`:feat/tb`)
	forge(t, dirB, "task", "start", "--ref", "feat/tb", "--title", "b task", "--branch", "--depends-on", keyA+`:feat/ta`)

	out := forge(t, dirA, "workspace", "doctor")
	if !strings.Contains(out, `dep-cycle`) {
		t.Errorf("doctor 应报 dep-cycle 跨仓依赖环，got:\n%s", out)
	}
	if !strings.Contains(out, keyA+`:feat/ta`) || !strings.Contains(out, keyB+`:feat/tb`) {
		t.Errorf("dep-cycle finding 应点名完整 key:ref 环，got:\n%s", out)
	}
}
