package capability_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/agentproto"
	"github.com/MjxUpUp/Forge/internal/capability"
)

func tree() []agentproto.Command {
	return []agentproto.Command{
		{Path: "change"},
		{Path: "change verify"},
		{Path: "task start"},
	}
}

func TestScanDocsClassifies(t *testing.T) {
	src := strings.Join([]string{
		"# 说明",
		"跑 forge change verify demo 封印。",
		"旧命令 forge hook install 已经不能用。",
		"forge freeze --status（已删除）",
		"forge task start --ref x",
	}, "\n")
	got := capability.ScanText("README.md", src, tree())
	if len(got) != 1 {
		t.Fatalf("期望 1 处漂移，got %d: %+v", len(got), got)
	}
	d := got[0]
	if d.Path != "hook" || d.Line != 3 || d.File != "README.md" {
		t.Errorf("漂移定位不对：%+v", d)
	}
	if !strings.Contains(d.Text, "forge hook install") {
		t.Errorf("漂移应带原文行：%q", d.Text)
	}
}

func TestLiveDocsSkipsDeletedNotes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("forge selfcheck 是历史命令（已删除）\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "reference"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "reference", "cli.md"), []byte("forge change verify <id>\nforge selfcheck\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := capability.LiveDocs(root)
	if err != nil {
		t.Fatalf("LiveDocs: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("应收集 README.md 与 docs/reference/cli.md，got %v", files)
	}
	drift, err := capability.ScanFiles(root, files, tree())
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	if len(drift) != 1 || drift[0].Path != "selfcheck" {
		t.Fatalf("已删除语境应豁免、真漂移要报，got %+v", drift)
	}
}

func TestSurfaceCounts(t *testing.T) {
	tbl := capability.Surface(tree(), map[string]string{"change verify": "封印证据"})
	if tbl.Commands != 3 {
		t.Errorf("命令数不对：%+v", tbl)
	}
	if tbl.Annotated != 1 {
		t.Errorf("注解数不对：%+v", tbl)
	}
}
