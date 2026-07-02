package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// guardTestSource 是写入临时项目 internal/ci/ 的合成守护测试源码。标准库 only
// （不引入 yaml.v3），使临时项目自包含、go test 能独立编译。断言 release.yml 含
// goreleaser `needs: test`——等价于真实 internal/ci/release_workflow_test.go 的
// NeedsChain 核心断言（守护 CI 防绕过 needs 链的源头）。
const guardTestSource = `package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNeedsChain(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("read release.yml: %v", err)
	}
	if !strings.Contains(string(data), "needs: test\n") {
		t.Fatal("release.yml missing goreleaser 'needs: test' — CI bypass chain broken")
	}
}
`

// setupGuardProject 建一个临时 forge 项目，含 .github/workflows/release.yml +
// 合成的 internal/ci 守护测试，让 hook 跑 `go test ./internal/ci/` 时有真实目标。
//
// 关键：完全不碰仓库真实的 release.yml。go test ./... -race 并发调度 internal/e2e
// 与 internal/ci 两个包（默认 -p=GOMAXPROCS），若本测试改真实 release.yml，ci 包
// 的 3 个读同一文件的测试会偶发读到破坏态 → CI flaky。用临时项目隔离即根治。
//
// intact=true：needs 链完整（goreleaser needs: test）→ 守护测试绿。
// intact=false：needs 链破坏（needs: test-broken-by-e2e）→ 守护测试红。
func setupGuardProject(t *testing.T, intact bool) string {
	t.Helper()
	dir := freshProject(t) // go.mod (example.com/test, go 1.24) + main.go + .forge/

	wfDir := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	goreleaserNeeds := "    needs: test\n"
	if !intact {
		goreleaserNeeds = "    needs: test-broken-by-e2e\n"
	}
	release := "jobs:\n  test:\n    runs-on: ubuntu-latest\n  goreleaser:\n" + goreleaserNeeds
	if err := os.WriteFile(filepath.Join(wfDir, "release.yml"), []byte(release), 0644); err != nil {
		t.Fatalf("write release.yml: %v", err)
	}

	ciDir := filepath.Join(dir, "internal", "ci")
	if err := os.MkdirAll(ciDir, 0755); err != nil {
		t.Fatalf("mkdir internal/ci: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ciDir, "release_workflow_test.go"), []byte(guardTestSource), 0644); err != nil {
		t.Fatalf("write guard test: %v", err)
	}
	return dir
}

// TestWorkflowTestGuard_NonWorkflowFile_Passes: editing a non-workflow file
// (README.md) must PASS WITHOUT running go test. The case-glob gate keeps the
// guard cheap — without it every Write/Edit would trigger a test build.
func TestWorkflowTestGuard_NonWorkflowFile_Passes(t *testing.T) {
	dir := freshProject(t)
	in := hookStdin(t, "sess-wf-nonwf", "PostToolUse", "Edit", map[string]any{
		"file_path": "README.md",
	})
	stdout, _, err := forgeHook(t, dir, "workflow-test-guard", in)
	if err != nil {
		t.Fatalf("非 workflow 文件应 PASS（case-glob 不匹配，不跑测试），got error: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, `"decision":"approve"`) {
		t.Errorf("期望 decision:approve，got:\n%s", stdout)
	}
}

// TestWorkflowTestGuard_FailOpenWithoutInternalCI: editing a workflow yaml in a
// project with NO internal/ci (legacy project / CI-config guard not adopted)
// must PASS — fail-open, never block. The guard is opt-in via the presence of
// internal/ci; absence must not become a hard wall on unrelated projects.
func TestWorkflowTestGuard_FailOpenWithoutInternalCI(t *testing.T) {
	dir := freshProject(t) // forge project, no internal/ci
	in := hookStdin(t, "sess-wf-failopen", "PostToolUse", "Edit", map[string]any{
		"file_path": ".github/workflows/release.yml",
	})
	stdout, _, err := forgeHook(t, dir, "workflow-test-guard", in)
	if err != nil {
		t.Fatalf("无 internal/ci 应 fail-open PASS，got error: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, `"decision":"approve"`) {
		t.Errorf("期望 fail-open decision:approve，got:\n%s", stdout)
	}
}

// TestWorkflowTestGuard_PassesIntactWorkflow: intact needs chain → the hook's
// internal/ci guard tests run green → PASS. The paired green path with
// BlocksBrokenWorkflow — confirms the guard does not false-positive on a healthy
// workflow. Uses an isolated temp project (setupGuardProject), not the real repo.
func TestWorkflowTestGuard_PassesIntactWorkflow(t *testing.T) {
	dir := setupGuardProject(t, true)
	in := hookStdin(t, "sess-wf-intact", "PostToolUse", "Edit", map[string]any{
		"file_path": ".github/workflows/release.yml",
	})
	stdout, _, err := forgeHook(t, dir, "workflow-test-guard", in)
	if err != nil {
		t.Fatalf("完整 needs 链应 PASS（守护测试绿），got error:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"decision":"approve"`) {
		t.Errorf("期望 decision:approve，got:\n%s", stdout)
	}
}

// TestWorkflowTestGuard_BlocksBrokenWorkflow is THE end-to-end proof that the
// guard closes the "sandbox-detected anomaly → real-edit feedback" loop: a
// broken needs chain makes the hook's internal/ci guard tests fail, and the hook
// blocks with actionable guidance — no CI, no self-discipline, the block happens
// at edit time. Uses an isolated temp project (setupGuardProject, intact=false)
// rather than mutating the real release.yml — eliminates the cross-package file
// race when `go test ./... -race` runs internal/e2e and internal/ci concurrently.
func TestWorkflowTestGuard_BlocksBrokenWorkflow(t *testing.T) {
	dir := setupGuardProject(t, false)
	in := hookStdin(t, "sess-wf-block", "PostToolUse", "Edit", map[string]any{
		"file_path": ".github/workflows/release.yml",
	})
	stdout, _, err := forgeHook(t, dir, "workflow-test-guard", in)

	if err == nil {
		t.Fatalf("needs 链破坏后 hook 应 block（exit 非零），got PASS:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"decision":"block"`) {
		t.Errorf("期望 decision:block，got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "workflow-test-guard") {
		t.Errorf("stdout 缺 workflow-test-guard 标识:\n%s", stdout)
	}
	// Agent-facing guidance: point at the guard test file so the agent knows where
	// to sync assertions — without this the block reason is unactionable.
	if !strings.Contains(stdout, "internal/ci/release_workflow_test.go") {
		t.Errorf("stdout 缺指向守护测试文件的反馈:\n%s", stdout)
	}
}
