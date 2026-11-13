package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/artifactchain"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// TestChainDigestStale 钉住 human 档审批的链漂移检测（docs/design/task-tree-guard.md §6）：
// 审批时记下的链定义指纹与当前 schema 不一致 → 回报「重批」，否则静默。
// 缺失/空 digest（存量审批、无 schema）一律不报——缺失绝不读成漂移。
func TestChainDigestStale(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", filepath.Join(root, "data"))

	approve := func(digest string) *TaskState {
		return &TaskState{ArtifactApprovals: map[string]tasktypes.ArtifactApproval{
			"design": {ChainDigest: digest},
		}}
	}

	if got := ChainDigestStale(root, nil); got != nil {
		t.Fatalf("nil state 必须返回 nil，got %v", got)
	}
	if got := ChainDigestStale(root, &TaskState{}); got != nil {
		t.Fatalf("无审批必须返回 nil，got %v", got)
	}
	if got := ChainDigestStale(root, approve("whatever")); got != nil {
		t.Fatalf("无 schema 时必须 fail-open（不能把默认链读成漂移），got %v", got)
	}

	p := artifactchain.SchemaPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("stages:\n  - design\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cur := artifactchain.Digest(root)
	if cur == "" {
		t.Fatal("夹具前提不成立：有 schema 却算不出指纹")
	}

	if got := ChainDigestStale(root, approve(cur)); got != nil {
		t.Fatalf("指纹一致必须静默，got %v", got)
	}
	if got := ChainDigestStale(root, approve("")); got != nil {
		t.Fatalf("存量审批（无 ChainDigest）必须静默，got %v", got)
	}
	if err := os.WriteFile(p, []byte("stages:\n  - design\n  - verify\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ChainDigestStale(root, approve(cur))
	if len(got) != 1 {
		t.Fatalf("链定义变更必须回报 1 条，got %v", got)
	}
	if !strings.Contains(got[0], "重批") || !strings.Contains(got[0], "design") {
		t.Fatalf("回报必须指名阶段与出路（重批），got %q", got[0])
	}
}
