package cli

// conventions_test.go —— `forge conventions` 命令组的守卫：init 建档（档案 +
// 摘要骨架）、重跑保留已提炼摘要（--force 才覆盖）、show 呈现元数据与过期状态。
// conventions_test.go — guards for the `forge conventions` command group:
// init profiles (profile.json + summary scaffold), re-init keeps the enriched
// digest (--force regenerates), show renders metadata and staleness.

import (
	"os"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/conventions"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/registry"
)

// convCmdProject 隔离环境、注册临时项目（findProjectRoot 需要注册表成员）、
// chdir 进去，返回 root。
func convCmdProject(t *testing.T) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()
	convWrite(t, root, "AGENTS.md", "# agents\nconventions here\n")
	convWrite(t, root, "go.mod", "module example.com/x\n\ngo 1.25\n")
	if err := registry.Add(root); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	t.Chdir(root)
	return root
}

// TestConventionsInitCmd_ProfilesAndPreservesSummary pins init's contract: first
// run writes profile + scaffold.
//
// TestConventionsInitCmd_ProfilesAndPreservesSummary 钉住 init 契约：首跑写
// 档案+骨架；重跑刷新元数据但**保留** agent 提炼过的摘要（人工提炼必须
// 在机械重建下幸存）；--force 才重建骨架。
func TestConventionsInitCmd_ProfilesAndPreservesSummary(t *testing.T) {
	root := convCmdProject(t)
	dataDir := forgedata.DataDirFor(root)

	first := captureStdout(t, func() {
		conventionsInitCmd.SetArgs([]string{})
		if err := conventionsInitCmd.RunE(conventionsInitCmd, nil); err != nil {
			t.Fatalf("init RunE: %v", err)
		}
	})
	for _, want := range []string{"AGENTS.md", "go vet ./...", conventions.ProfilePath(dataDir)} {
		if !strings.Contains(first, want) {
			t.Fatalf("init output missing %q:\n%s", want, first)
		}
	}
	if _, err := os.Stat(conventions.ProfilePath(dataDir)); err != nil {
		t.Fatalf("profile.json not written: %v", err)
	}

	// 模拟 agent 提炼后的摘要，重跑 init 不带 --force：必须保留。
	enriched := "# digest\n\n## 提取要点\n- error handling: fmt.Errorf %w wrap\n"
	if err := conventions.SaveSummary(dataDir, enriched); err != nil {
		t.Fatal(err)
	}
	_ = captureStdout(t, func() {
		if err := conventionsInitCmd.RunE(conventionsInitCmd, nil); err != nil {
			t.Fatalf("re-init RunE: %v", err)
		}
	})
	if got := conventions.LoadSummary(dataDir); got != enriched {
		t.Fatalf("enriched summary must survive re-init, got:\n%s", got)
	}

	// --force：重建骨架（提取要点节回到待提取）。直接调 RunE 不经 cobra 的
	// flag 解析，故用 Flags().Set 置位（SetArgs 只在 Execute 路径生效）。
	if err := conventionsInitCmd.Flags().Set("force", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conventionsInitCmd.Flags().Set("force", "false") })
	_ = captureStdout(t, func() {
		if err := conventionsInitCmd.RunE(conventionsInitCmd, nil); err != nil {
			t.Fatalf("init --force RunE: %v", err)
		}
	})
	if got := conventions.LoadSummary(dataDir); !strings.Contains(got, "待提取") {
		t.Fatalf("--force must regenerate the scaffold, got:\n%s", got)
	}
}

// TestConventionsShowCmd_MetadataAndStaleness pins show: profile metadata
// renders, a matching tree reports consistency, a drifted tree reports STALE.
//
// TestConventionsShowCmd_MetadataAndStaleness 钉住 show：档案元数据渲染、
// 树一致时报「一致」、源漂移后报 STALE。
func TestConventionsShowCmd_MetadataAndStaleness(t *testing.T) {
	root := convCmdProject(t)

	_ = captureStdout(t, func() {
		conventionsInitCmd.SetArgs([]string{})
		if err := conventionsInitCmd.RunE(conventionsInitCmd, nil); err != nil {
			t.Fatalf("init RunE: %v", err)
		}
	})
	out := captureStdout(t, func() {
		conventionsShowCmd.SetArgs([]string{})
		if err := conventionsShowCmd.RunE(conventionsShowCmd, nil); err != nil {
			t.Fatalf("show RunE: %v", err)
		}
	})
	for _, want := range []string{"stack: go", "AGENTS.md", "与当前树一致", "summary.md"} {
		if !strings.Contains(out, want) {
			t.Fatalf("show output missing %q:\n%s", want, out)
		}
	}

	convWrite(t, root, "AGENTS.md", "# agents v2\nchanged\n")
	drifted := captureStdout(t, func() {
		if err := conventionsShowCmd.RunE(conventionsShowCmd, nil); err != nil {
			t.Fatalf("show RunE (stale): %v", err)
		}
	})
	if !strings.Contains(drifted, "STALE") {
		t.Fatalf("drifted tree must report STALE, got:\n%s", drifted)
	}
}

// TestConventionsInitCmd_RequiresForgeProject pins the root resolution gate:
// outside a registered project init refuses (a profile there would be
// write-only.
//
// TestConventionsInitCmd_RequiresForgeProject 钉住根解析门：未注册项目外
// init 拒绝（那里的档案是只写的——hook 在 forge 项目外从不触发）。
func TestConventionsInitCmd_RequiresForgeProject(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	conventionsInitCmd.SetArgs([]string{})
	err := conventionsInitCmd.RunE(conventionsInitCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "forge init") {
		t.Fatalf("init outside a forge project must refuse with a forge init pointer, got: %v", err)
	}
}

// TestConventionsLearnCmd_WritesBack pins the learn command's write-back path:
// RunE with the rule as args writes it into the digest and reports the path.
//
// TestConventionsLearnCmd_WritesBack 钉住 learn 命令的写回路径：以参数传入
// 规则的 RunE 把它写进摘要并报告路径；重复调用报未改动、不失败。
func TestConventionsLearnCmd_WritesBack(t *testing.T) {
	root := convCmdProject(t)
	dataDir := forgedata.DataDirFor(root)

	_ = captureStdout(t, func() {
		conventionsInitCmd.SetArgs([]string{})
		if err := conventionsInitCmd.RunE(conventionsInitCmd, nil); err != nil {
			t.Fatalf("init RunE: %v", err)
		}
	})
	out := captureStdout(t, func() {
		conventionsLearnCmd.SetArgs([]string{"errors:", "always", "wrap", "with", "%w"})
		if err := conventionsLearnCmd.RunE(conventionsLearnCmd, []string{"errors: always wrap with %w"}); err != nil {
			t.Fatalf("learn RunE: %v", err)
		}
	})
	if !strings.Contains(out, "已写回") || !strings.Contains(out, conventions.SummaryPath(dataDir)) {
		t.Fatalf("learn output = %q", out)
	}
	if s := conventions.LoadSummary(dataDir); !strings.Contains(s, "errors: always wrap with %w") {
		t.Fatalf("rule not in digest:\n%s", s)
	}

	dup := captureStdout(t, func() {
		if err := conventionsLearnCmd.RunE(conventionsLearnCmd, []string{"errors: always wrap with %w"}); err != nil {
			t.Fatalf("duplicate learn must not fail: %v", err)
		}
	})
	if !strings.Contains(dup, "未改动") {
		t.Fatalf("duplicate learn output = %q", dup)
	}
}
