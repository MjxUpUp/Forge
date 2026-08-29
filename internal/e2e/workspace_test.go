package e2e

// 多仓 workspace 端到端测试（docs/design/multi-repo-workspace.md）：清单
// （~/.forge/workspaces.json）、status 聚合、doctor 的 drift 与跨仓依赖环
// 检出、task-verify 的 cross-repo-impact 门禁（advisory → protocol required
// 阻断 → 声明后放行）、跨仓 DependsOn（key:ref 的 pending 语义）——全部走
// 编译产物 + 共享一个 FORGE_DATA_HOME 的两个真实 git 仓。

import (
	"os"
	"path/filepath"
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

// TestWorkspaceImpactGate covers the task-verify cross-repo-impact gate
// end-to-end: default advisory (verify passes, advisory on stderr) → protocol
// cross_repo_impact: required turns the undeclared case into a HARD stop →
// `forge task impact --level none` unblocks verify again. Also pins the Step 4
// card line on `forge task status` (未声明 → none).
//
// TestWorkspaceImpactGate 端到端覆盖 task-verify 的 cross-repo-impact 门禁：
// 默认 advisory（verify 通过，stderr 提醒）→ protocol 配
// cross_repo_impact: required 后未声明变 HARD stop → `forge task impact
// --level none` 后 verify 恢复通过。同时钉住 Step 4 的卡片行（forge task
// status 上 未声明 → none）。
func TestWorkspaceImpactGate(t *testing.T) {
	// 与 passAllGates 相同的门禁计时/工作活动豁免（这里门禁连跑）。
	t.Setenv("FORGE_GATE_MIN_INTERVAL", "0s")
	t.Setenv("FORGE_WORK_ACTIVITY", "disable")

	dirA, _, _, _ := wsFixture(t)
	forge(t, dirA, "task", "start", "--ref", "feat/xr", "--title", "cross repo", "--branch")

	// 真实改动 + commit，满足 task-implement 的内容检查。
	writeFile(t, dirA, "xr.go", "package main\n\nfunc XR() int { return 1 }\n")
	git(t, dirA, "add", "xr.go")
	git(t, dirA, "commit", "-m", "e2e: cross-repo impact probe")
	forge(t, dirA, "task", "gate", "task-implement", "--ref", "feat/xr")

	// Step 4 卡片行：status 卡片点名 workspace + 声明状态。
	out := forge(t, dirA, "task", "status")
	if !strings.Contains(out, `Workspace: ws（2 repos）· 跨仓影响: 未声明`) {
		t.Errorf("task status 缺 workspace 上下文行，got:\n%s", out)
	}

	// 默认 advisory：verify 通过，四段式提醒落 stderr。
	out, err := forgeErr(t, dirA, "task", "gate", "task-verify", "--ref", "feat/xr")
	if err != nil {
		t.Fatalf("默认 advisory 下 verify 不应被阻断: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, `未声明跨仓影响`) {
		t.Errorf("advisory 输出应含「未声明跨仓影响」，got:\n%s", out)
	}

	// 经 DataDir 的 protocol.yml 升级为 required（零项目写入时代：项目树没有
	// .forge/protocol.yml）。
	protoPath := filepath.Join(forgedata.DataDirFor(dirA), "protocol.yml")
	proto := readFile(t, ``, protoPath)
	writeFile(t, ``, protoPath, proto+"\ncross_repo_impact: required\n")

	out, err = forgeErr(t, dirA, "task", "gate", "task-verify", "--ref", "feat/xr")
	if err == nil {
		t.Fatalf("required 模式下未声明应阻断 verify\noutput: %s", out)
	}
	if !strings.Contains(out, `HARD stop`) || !strings.Contains(out, `cross_repo_impact`) {
		t.Errorf("阻断输出应含 HARD stop + cross_repo_impact 四段式，got:\n%s", out)
	}

	// 声明 none → verify 不再因 cross-repo-impact 阻断（required 翻转前该门禁
	// 本就能过，故直接断言恢复通过）。
	forge(t, dirA, "task", "impact", "--level", "none")
	out, err = forgeErr(t, dirA, "task", "gate", "task-verify", "--ref", "feat/xr")
	if err != nil {
		t.Fatalf("声明 none 后 verify 应恢复通过: %v\noutput: %s", err, out)
	}
	out = forge(t, dirA, "task", "status")
	if !strings.Contains(out, `跨仓影响: none`) {
		t.Errorf("声明后 status 卡片应显示 跨仓影响: none，got:\n%s", out)
	}
}

// TestWorkspaceCrossRepoDependsOn covers key:ref dependencies across repos.
//
// TestWorkspaceCrossRepoDependsOn 覆盖跨仓 key:ref 依赖：仓 A 的 ta 依赖
// keyB:feat/tb → ta 的 verify 被阻断且 pending 消息原样携带 key:ref → tb
// 交付后 pending 消失（verify 此时只剩 task-implement 前置未过，绝不再报
// 该依赖）。
func TestWorkspaceCrossRepoDependsOn(t *testing.T) {
	dirA, dirB, _, keyB := wsFixture(t)

	// 先在仓 B 建好 tb，让写入侧校验看到存活目标。
	forge(t, dirB, "task", "start", "--ref", "feat/tb", "--title", "b task", "--branch")
	dep := keyB + `:feat/tb`
	out, err := forgeErr(t, dirA, "task", "start", "--ref", "feat/ta", "--title", "a task", "--branch", "--depends-on", dep)
	if err != nil {
		t.Fatalf("跨仓 --depends-on 应被接受: %v\noutput: %s", err, out)
	}

	// ta verify → DependsOn 门禁先于前置检查触发，原样点名 key:ref。
	out, err = forgeErr(t, dirA, "task", "gate", "task-verify", "--ref", "feat/ta")
	if err == nil {
		t.Fatalf("上游未交付时 ta 的 verify 应被阻断\noutput: %s", out)
	}
	if !strings.Contains(out, `上游 task 未交付`) || !strings.Contains(out, dep) {
		t.Errorf("阻断输出应原样含 pending 依赖 %s，got:\n%s", dep, out)
	}

	// 走真实命令路径交付 tb（passAllGates + task complete，
	// TestMasterBranchReminder 同款）：无分派任务的 IsDelivered = IsComplete =
	// History 里门禁全过，故只写 completed_at 的夹具手术不会放行依赖方——
	// 门禁必须真过。仓 B 是多仓成员，其 verify 会带 cross-repo advisory——
	// 仅 advisory，不阻断。
	passAllGates(t, dirB, "feat/tb")
	forge(t, dirB, "task", "complete", "--ref", "feat/tb")

	// 重跑 verify：跨仓 pending 消失；只剩 task-implement 前置未过。
	out, _ = forgeErr(t, dirA, "task", "gate", "task-verify", "--ref", "feat/ta")
	if strings.Contains(out, `上游 task 未交付`) || strings.Contains(out, dep) {
		t.Errorf("tb 交付后 verify 不应再报该 pending，got:\n%s", out)
	}
	if !strings.Contains(out, `prerequisite`) {
		t.Errorf("依赖放行后应只剩前置门禁阻断（证明确已越过 DependsOn 检查），got:\n%s", out)
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
