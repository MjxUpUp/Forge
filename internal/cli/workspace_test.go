package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/specroot"
	"github.com/MjxUpUp/Forge/internal/workspace"
)

// workspace_test.go — forge workspace add 的成员提示（advisory）。
// 成员资格现在有两个来源：全局注册表与仓库自带的 spec/forge.yaml。
// add 只提示「两个都没有」的仓库——declared 仓库再报未登记就是重造
// 「status 说 managed、别处说 unknown」那种自相矛盾。

// writeWorkspaceContract 在 root 下写最小仓库契约（git 仓库语义，非必须）。
func writeWorkspaceContract(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, specroot.DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir contract dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, specroot.MarkerName), []byte("version: \"1\"\n"), 0o644); err != nil {
		t.Fatalf("write contract: %v", err)
	}
}

// createWorkspace 在当前 FORGE_DATA_HOME 下建一个空 workspace（add 要求先存在）。
func createWorkspace(t *testing.T, name string) {
	t.Helper()
	f, err := workspace.LoadForWrite()
	if err != nil {
		t.Fatalf("workspace store: %v", err)
	}
	if err := f.Create(name); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("save workspace: %v", err)
	}
}

// TestWorkspaceAdd_DeclaredContractNoHint：有 spec/forge.yaml 的仓库加入 workspace
// 不再提示「未在全局注册表」——契约本身就是成员资格。
func TestWorkspaceAdd_DeclaredContractNoHint(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	createWorkspace(t, `declared`)

	proj := t.TempDir()
	writeWorkspaceContract(t, proj)

	// 必须显式传 --path：不传会走进程 cwd（测试进程在 internal/cli），向上命中
	// Forge 仓库自己的 spec/forge.yaml，这条测试就变成恒绿的空转。
	_, stderr, err := captureOutput(t, func() error {
		if err := workspaceAddCmd.Flags().Set(`path`, proj); err != nil {
			return err
		}
		defer func() { _ = workspaceAddCmd.Flags().Set(`path`, ``) }()
		return runWorkspaceAdd(workspaceAddCmd, []string{`declared`})
	})
	if err != nil {
		t.Fatalf("workspace add: %v", err)
	}
	if strings.TrimSpace(stderr) != `` {
		t.Fatalf("declared 仓库不该收到未登记提示，stderr = %q", stderr)
	}
}

// TestWorkspaceAdd_NoMembershipHints：既无契约也无登记仍要提示——否则 add 之后
// doctor 会一直在别的会话里标这个 key，而用户不知道为什么。
func TestWorkspaceAdd_NoMembershipHints(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	createWorkspace(t, `plain`)
	proj := t.TempDir()
	// --path 缺省走 projectroot.Find()，裸目录会 ErrNoForgeConfig，故显式传路径。
	t.Chdir(proj)

	var stderr string
	_, stderr, err := captureOutput(t, func() error {
		if err := workspaceAddCmd.Flags().Set(`path`, proj); err != nil {
			return err
		}
		defer func() { _ = workspaceAddCmd.Flags().Set(`path`, ``) }()
		return runWorkspaceAdd(workspaceAddCmd, []string{`plain`})
	})
	if err != nil {
		t.Fatalf("workspace add: %v", err)
	}
	if !strings.Contains(stderr, "既没有 spec/forge.yaml") {
		t.Fatalf("无成员资格的仓库应收到提示，stderr = %q", stderr)
	}
}
