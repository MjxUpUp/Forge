package clitask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/workspace"
)

// TestWorkspaceContextLine pins the continuity-card / task-status workspace
// line (multi-repo workspace Step 4): rendered only for MULTI-repo memberships,
// fail-open (silent omission) on an unreadable manifest, and the declaration
// segment mirroring 未声明/none/multi(...). Uses the depRefFixture isolation
// pattern (FORGE_DATA_HOME → t.TempDir).
//
// TestWorkspaceContextLine 钉住接续卡片 / task status 的 workspace 行（多仓
// workspace Step 4）：仅多仓成员资格渲染；清单不可读 fail-open（静默省略）；
// 声明段镜像 未声明/none/multi(...)。隔离用 depRefFixture 同款模式
// （FORGE_DATA_HOME → t.TempDir）。
func TestWorkspaceContextLine(t *testing.T) {
	t.Run(`无成员资格 → 空行`, func(t *testing.T) {
		root, _, _ := depRefFixture(t)
		if got := workspaceContextLine(root, nil); got != `` {
			t.Errorf("无 workspace 时应省略，got %q", got)
		}
	})

	t.Run(`单仓 workspace 不渲染（只是标签）`, func(t *testing.T) {
		root, ownKey, _ := depRefFixture(t)
		writeDepRefWorkspace(t, ownKey) // fleet 只有本仓一个成员
		if got := workspaceContextLine(root, nil); got != `` {
			t.Errorf("单仓 workspace 应省略，got %q", got)
		}
	})

	t.Run(`多仓成员 + 未声明`, func(t *testing.T) {
		root, ownKey, _ := depRefFixture(t)
		writeDepRefWorkspace(t, ownKey, `other-key`)
		got := workspaceContextLine(root, nil)
		want := `Workspace: fleet（2 repos）· 跨仓影响: 未声明`
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run(`声明段镜像 none / multi`, func(t *testing.T) {
		root, ownKey, _ := depRefFixture(t)
		writeDepRefWorkspace(t, ownKey, `k-b`, `k-c`)
		if got := workspaceContextLine(root, &taskpipeline.CrossRepoImpact{Level: taskpipeline.CrossRepoNone}); !strings.HasSuffix(got, `跨仓影响: none`) {
			t.Errorf("none 声明段不对，got %q", got)
		}
		multi := &taskpipeline.CrossRepoImpact{Level: taskpipeline.CrossRepoMulti, Repos: []string{`k-b`, `k-c`}}
		if got := workspaceContextLine(root, multi); !strings.HasSuffix(got, `跨仓影响: multi(k-b, k-c)`) {
			t.Errorf("multi 声明段不对，got %q", got)
		}
	})

	t.Run(`一 key 属多个多仓 workspace → 单行全点名`, func(t *testing.T) {
		root, ownKey, _ := depRefFixture(t)
		writeDepRefWorkspace(t, ownKey, `k-b`)
		f := &workspace.File{}
		if err := f.Create(`second`); err != nil {
			t.Fatal(err)
		}
		if err := f.AddRepo(`second`, workspace.RepoRef{Key: ownKey}); err != nil {
			t.Fatal(err)
		}
		if err := f.AddRepo(`second`, workspace.RepoRef{Key: `k-x`}); err != nil {
			t.Fatal(err)
		}
		// Merge into the existing manifest (writeDepRefWorkspace saved "fleet").
		//
		// 合并进既有清单（writeDepRefWorkspace 已存 "fleet"）。
		existing, err := workspace.LoadForWrite()
		if err != nil {
			t.Fatal(err)
		}
		if err := existing.Create(`second`); err != nil {
			t.Fatal(err)
		}
		for _, r := range f.Find(`second`).Repos {
			if err := existing.AddRepo(`second`, r); err != nil {
				t.Fatal(err)
			}
		}
		if err := existing.Save(); err != nil {
			t.Fatal(err)
		}
		got := workspaceContextLine(root, nil)
		if !strings.Contains(got, `fleet（2 repos）`) || !strings.Contains(got, `second（2 repos）`) {
			t.Errorf("多 workspace 应单行全点名，got %q", got)
		}
		if strings.Count(got, "\n") != 0 {
			t.Errorf("必须保持单行，got %q", got)
		}
	})

	t.Run(`清单损坏 fail-open → 空行`, func(t *testing.T) {
		root, _, home := depRefFixture(t)
		if err := os.WriteFile(filepath.Join(home, `workspaces.json`), []byte(`{not json`), 0644); err != nil {
			t.Fatal(err)
		}
		if got := workspaceContextLine(root, nil); got != `` {
			t.Errorf("清单损坏应静默省略，got %q", got)
		}
	})

	t.Run(`空 root 不触碰 store`, func(t *testing.T) {
		if got := workspaceContextLine(``, nil); got != `` {
			t.Errorf("root 为空应省略，got %q", got)
		}
	})
}
