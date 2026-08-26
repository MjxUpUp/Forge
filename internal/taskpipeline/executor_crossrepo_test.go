package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/workspace"
)

// TestAssessCrossRepoImpact covers the pure decision table: single-repo
// memberships never trigger; undeclared multi-repo membership flags; declared
// none/multi-with-valid-repos pass; malformed declarations (empty repos /
// foreign keys / unknown level) each get their own verdict for the correction
// advisory.
//
// TestAssessCrossRepoImpact 覆盖纯判定表：单仓成员资格永不触发；多仓未声明
// 标记；已声明 none/multi-repos 合法 通过；畸形声明（repos 空 / 越界 key /
// 未知 level）各得专属 verdict 供修正 advisory。
func TestAssessCrossRepoImpact(t *testing.T) {
	ws := func(name string, keys ...string) workspace.Workspace {
		w := workspace.Workspace{Name: name}
		for _, k := range keys {
			w.Repos = append(w.Repos, workspace.RepoRef{Key: k})
		}
		return w
	}

	cases := []struct {
		name        string
		memberships []workspace.Workspace
		impact      *CrossRepoImpact
		want        crossRepoVerdict
		wantForeign []string
	}{
		{`无成员资格`, nil, nil, crossRepoSkip, nil},
		{`仅单仓 workspace`, []workspace.Workspace{ws(`solo`, `me`)}, nil, crossRepoSkip, nil},
		{`多仓未声明`, []workspace.Workspace{ws(`fleet`, `me`, `other`)}, nil, crossRepoUndeclared, nil},
		// One single-repo + one multi membership: the multi one obliges.
		//
		// 一个单仓 + 一个多仓成员资格：多仓那个产生义务。
		{`单仓+多仓混合`, []workspace.Workspace{ws(`solo`, `me`), ws(`fleet`, `me`, `other`)}, nil, crossRepoUndeclared, nil},
		{`声明 none`, []workspace.Workspace{ws(`fleet`, `me`, `other`)}, &CrossRepoImpact{Level: CrossRepoNone}, crossRepoOK, nil},
		{`multi 合法 repos`, []workspace.Workspace{ws(`fleet`, `me`, `other`)}, &CrossRepoImpact{Level: CrossRepoMulti, Repos: []string{`other`}}, crossRepoOK, nil},
		// The repo's own key is a valid impact target (self-impact is legal to declare).
		//
		// 本仓自身 key 也是合法影响目标（允许声明自我影响）。
		{`multi 含本仓 key`, []workspace.Workspace{ws(`fleet`, `me`, `other`)}, &CrossRepoImpact{Level: CrossRepoMulti, Repos: []string{`me`}}, crossRepoOK, nil},
		{`multi 空 repos`, []workspace.Workspace{ws(`fleet`, `me`, `other`)}, &CrossRepoImpact{Level: CrossRepoMulti}, crossRepoMultiEmptyRepos, nil},
		{`multi 越界 key`, []workspace.Workspace{ws(`fleet`, `me`, `other`)}, &CrossRepoImpact{Level: CrossRepoMulti, Repos: []string{`other`, `stranger`}}, crossRepoMultiForeignRepos, []string{`stranger`}},
		// Overlapping memberships: keys from ANY multi workspace are valid targets.
		//
		// 重叠成员资格：任一多仓 workspace 的 key 都是合法目标。
		{`重叠 workspace 并集`, []workspace.Workspace{ws(`a`, `me`, `x`), ws(`b`, `me`, `y`)}, &CrossRepoImpact{Level: CrossRepoMulti, Repos: []string{`x`, `y`}}, crossRepoOK, nil},
		{`未知 level`, []workspace.Workspace{ws(`fleet`, `me`, `other`)}, &CrossRepoImpact{Level: `maybe`}, crossRepoBadLevel, nil},
	}
	for _, c := range cases {
		got, foreign := assessCrossRepoImpact(c.memberships, c.impact)
		if got != c.want {
			t.Errorf("%s: verdict = %v, want %v", c.name, got, c.want)
		}
		if strings.Join(foreign, `,`) != strings.Join(c.wantForeign, `,`) {
			t.Errorf("%s: foreign = %v, want %v", c.name, foreign, c.wantForeign)
		}
	}
}

// setupCrossRepoRepo builds a temp git repo with one commit and returns
// (root, key) — the shared fixture of the gate integration tests below.
//
// setupCrossRepoRepo 建一个带一次提交的临时 git 仓，返回 (root, key)——
// 下方门禁集成测试共用的 fixture。
func setupCrossRepoRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	key, err := forgedata.Key(dir)
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	return dir, key
}

// setupCrossRepoWorkspace writes a two-member workspace manifest (this repo +
// a phantom member) into the isolated FORGE_DATA_HOME.
//
// setupCrossRepoWorkspace 往隔离的 FORGE_DATA_HOME 写一个两成员 workspace
// 清单（本仓 + 一个幻影成员）。
func setupCrossRepoWorkspace(t *testing.T, key string) {
	t.Helper()
	f := &workspace.File{}
	if err := f.Create(`fleet`); err != nil {
		t.Fatal(err)
	}
	if err := f.AddRepo(`fleet`, workspace.RepoRef{Key: key, Path: `/self`}); err != nil {
		t.Fatal(err)
	}
	if err := f.AddRepo(`fleet`, workspace.RepoRef{Key: `other-key`, Path: `/other`}); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("workspace Save: %v", err)
	}
}

// crossRepoEntries returns the checklog entries of the cross-repo-impact check
// for the task.
//
// crossRepoEntries 返回本任务 cross-repo-impact 检查的 checklog 条目。
func crossRepoEntries(t *testing.T, root, ref string) []checklog.Entry {
	t.Helper()
	entries, err := checklog.LoadForTask(root, ref)
	if err != nil {
		t.Fatalf("LoadForTask: %v", err)
	}
	var out []checklog.Entry
	for _, e := range entries {
		if e.Check == checklog.CheckCrossRepoImpact {
			out = append(out, e)
		}
	}
	return out
}

// TestCrossRepoImpact_SkipsWithoutMembership: no workspace manifest at all →
// the check stays fully silent (no stderr, no checklog) — the single-repo
// majority must not pay noise for a feature they never opted into.
//
// TestCrossRepoImpact_SkipsWithoutMembership：完全没有 workspace 清单 →
// 检查彻底静默（无 stderr、无 checklog）——单仓多数派不该为从未启用的
// 特性付噪音税。
func TestCrossRepoImpact_SkipsWithoutMembership(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir, _ := setupCrossRepoRepo(t)

	state := &TaskState{TaskRef: `test-crossrepo-skip`, Branch: `feat/test`}
	state.RecordGateResult("task-implement", true, "")

	var gateErr error
	stderr := captureStderr(t, func() {
		_, gateErr = ExecuteTaskGate(dir, "task-verify", state)
	})
	if gateErr != nil {
		t.Fatalf("无 workspace 时 task-verify 应照常通过, got: %v", gateErr)
	}
	if strings.Contains(stderr, `跨仓`) {
		t.Errorf("无成员资格时不应有跨仓 advisory, stderr: %q", stderr)
	}
	if got := crossRepoEntries(t, dir, `test-crossrepo-skip`); len(got) != 0 {
		t.Errorf("无成员资格时不应记 cross-repo-impact 条目, got %+v", got)
	}
}

// TestCrossRepoImpact_AdvisoryDefault pins the fail-open default: a multi-repo
// member task without a declaration still PASSES task-verify (advisory on
// stderr + an advisory-level checklog entry), so enabling workspaces never
// breaks existing flows until the project opts into `required`.
//
// TestCrossRepoImpact_AdvisoryDefault 钉住 fail-open 默认：多仓成员任务未
// 声明仍通过 task-verify（stderr advisory + advisory 级 checklog 条目）——
// 启用 workspace 在项目显式 opt-in required 之前绝不破坏既有流程。
func TestCrossRepoImpact_AdvisoryDefault(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir, key := setupCrossRepoRepo(t)
	setupCrossRepoWorkspace(t, key)

	state := &TaskState{TaskRef: `test-crossrepo-advisory`, Branch: `feat/test`}
	state.RecordGateResult("task-implement", true, "")

	var gateErr error
	stderr := captureStderr(t, func() {
		_, gateErr = ExecuteTaskGate(dir, "task-verify", state)
	})
	if gateErr != nil {
		t.Fatalf("默认 advisory 不应阻断 task-verify, got: %v", gateErr)
	}
	if !strings.Contains(stderr, `未声明跨仓影响`) || !strings.Contains(stderr, `forge task impact --level none`) {
		t.Errorf("stderr 应含四段式 advisory（WHAT/HOW）, got: %q", stderr)
	}

	got := crossRepoEntries(t, dir, `test-crossrepo-advisory`)
	if len(got) != 1 {
		t.Fatalf("应记 1 条 cross-repo-impact 条目, got %+v", got)
	}
	e := got[0]
	if e.Passed || e.EffectiveLevel() != checklog.LevelAdvisory {
		t.Errorf("条目应 Passed=false + advisory 级, got Passed=%v Level=%s", e.Passed, e.EffectiveLevel())
	}
}

// TestCrossRepoImpact_RequiredBlocks pins the protocol escalation: with
// cross_repo_impact: required, an undeclared multi-repo task is BLOCKED
// (four-part message + blocked-level audit); declaring --level none then
// passes and records a pass entry. Declaring multi with a foreign key stays
// advisory (correction hint), never a block.
//
// TestCrossRepoImpact_RequiredBlocks 钉住 protocol 升级：配
// cross_repo_impact: required 时未声明的多仓任务被 BLOCKED（四段式消息 +
// blocked 级审计）；声明 --level none 后放行并记通过条目。声明 multi 带
// 越界 key 保持 advisory（修正提示），绝不阻断。
func TestCrossRepoImpact_RequiredBlocks(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir, key := setupCrossRepoRepo(t)
	setupCrossRepoWorkspace(t, key)

	proto := protocol.DefaultProtocol()
	proto.CrossRepoImpact = `required`
	if err := protocol.SaveDataDir(dir, proto); err != nil {
		t.Fatalf("SaveDataDir: %v", err)
	}

	state := &TaskState{TaskRef: `test-crossrepo-required`, Branch: `feat/test`}
	state.RecordGateResult("task-implement", true, "")

	_, err := ExecuteTaskGate(dir, "task-verify", state)
	if err == nil {
		t.Fatal(`required 模式未声明应 BLOCKED`)
	}
	if !strings.HasPrefix(err.Error(), blockedPrefix) {
		t.Fatalf("应是 GateBlocked（HARD stop）, got: %v", err)
	}
	for _, seg := range []string{`WHAT:`, `WHY:`, `HOW:`, `REF:`, `forge task impact --level none`} {
		if !strings.Contains(err.Error(), seg) {
			t.Errorf("BLOCKED 消息缺四段式片段 %q, got: %q", seg, err.Error())
		}
	}

	// Blocked entries must hit disk (BLOCKED 必落盘) with an explicit blocked level.
	//
	// 阻断条目必须落盘（BLOCKED 必落盘）且显式标 blocked 级。
	got := crossRepoEntries(t, dir, `test-crossrepo-required`)
	if len(got) != 1 || got[0].Passed || got[0].EffectiveLevel() != checklog.LevelBlocked {
		t.Fatalf("应有 1 条 blocked 级未通过条目, got %+v", got)
	}

	// A malformed declaration (foreign key) is a correction advisory, not a block.
	//
	// 畸形声明（越界 key）是修正 advisory，不是阻断。
	state.CrossRepoImpact = &CrossRepoImpact{Level: CrossRepoMulti, Repos: []string{`stranger`}, DeclaredAt: time.Now()}
	var gateErr error
	stderr := captureStderr(t, func() {
		_, gateErr = ExecuteTaskGate(dir, "task-verify", state)
	})
	if gateErr != nil {
		t.Fatalf("声明畸形应 advisory 放行而非阻断, got: %v", gateErr)
	}
	if !strings.Contains(stderr, `跨仓影响声明需修正`) {
		t.Errorf("stderr 应含修正提示, got: %q", stderr)
	}

	// A proper declaration passes and records a pass entry.
	//
	// 正常声明放行并记通过条目。
	state.CrossRepoImpact = &CrossRepoImpact{Level: CrossRepoNone, DeclaredAt: time.Now()}
	if _, err := ExecuteTaskGate(dir, "task-verify", state); err != nil {
		t.Fatalf("声明 none 后应通过, got: %v", err)
	}
	got = crossRepoEntries(t, dir, `test-crossrepo-required`)
	last := got[len(got)-1]
	if !last.Passed {
		t.Errorf("声明后应有通过条目, got %+v", last)
	}
}

// TestCrossRepoImpact_CorruptManifestFailsOpen: an unreadable workspaces.json
// degrades the check to a warn-level INFRA entry + pass (never a block) — the
// manifest is a global store outside the project's control.
//
// TestCrossRepoImpact_CorruptManifestFailsOpen：不可读的 workspaces.json 把
// 检查降级为 warn 级 INFRA 条目并放行（绝不阻断）——清单是项目掌控之外的
// 全局 store。
func TestCrossRepoImpact_CorruptManifestFailsOpen(t *testing.T) {
	home := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, home)
	dir, _ := setupCrossRepoRepo(t)
	if err := os.WriteFile(filepath.Join(home, `workspaces.json`), []byte(`{broken`), 0644); err != nil {
		t.Fatal(err)
	}

	state := &TaskState{TaskRef: `test-crossrepo-infra`, Branch: `feat/test`}
	state.RecordGateResult("task-implement", true, "")

	var gateErr error
	stderr := captureStderr(t, func() {
		_, gateErr = ExecuteTaskGate(dir, "task-verify", state)
	})
	if gateErr != nil {
		t.Fatalf("清单损坏应 fail-open 放行, got: %v", gateErr)
	}
	if !strings.Contains(stderr, `fail-open`) {
		t.Errorf("stderr 应含 fail-open 提示, got: %q", stderr)
	}
	got := crossRepoEntries(t, dir, `test-crossrepo-infra`)
	if len(got) != 1 || got[0].EffectiveLevel() != checklog.LevelWarn || got[0].Checked {
		t.Fatalf("应有 1 条 warn 级 Checked=false 的 INFRA 条目, got %+v", got)
	}
}
