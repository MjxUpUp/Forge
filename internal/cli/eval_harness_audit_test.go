package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/harnessaudit"
	"github.com/spf13/cobra"
)

// TestEvalHarnessAudit_JSONOnEmptyProject pins the command surface (design M): on a fresh project
// the command succeeds, --json emits one decodable Report carrying the caliber fields the
// two-machine backtest keys on, and the human render lists every section A–G.
//
// TestEvalHarnessAudit_JSONOnEmptyProject 钉住命令面（设计 M）：空项目上命令成功，--json 输出
// 可解码且带口径字段的 Report（两机回测的比对键），人读渲染列出 A–G 全部节。
func TestEvalHarnessAudit_JSONOnEmptyProject(t *testing.T) {
	root, _ := forgedatatest.RealProject(t)
	t.Chdir(root)

	cmd := &cobra.Command{}
	cmd.Flags().Bool("json", false, "")
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	var runErr error
	out := captureStdout(t, func() { runErr = runEvalHarnessAudit(cmd, nil) })
	if runErr != nil {
		t.Fatalf("harness-audit --json on empty project: %v", runErr)
	}
	var rep harnessaudit.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("output is not a Report JSON: %v\n%s", err, out)
	}
	if rep.Caliber.DedupWindow != harnessaudit.DefaultCaliber().DedupWindow || rep.Caliber.PostSealGrace == 0 {
		t.Fatalf("caliber fields must be serialized, got %+v", rep.Caliber)
	}
	if rep.D.Declared != 0 || rep.A.Total != 0 {
		t.Fatalf("empty project must report zero data, got A=%+v D=%+v", rep.A, rep.D)
	}

	plain := &cobra.Command{}
	plain.Flags().Bool("json", false, "")
	text := captureStdout(t, func() { runErr = runEvalHarnessAudit(plain, nil) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	for _, section := range []string{"A skill-trigger", "B next-hint", "C 门禁命令形态", "D refs-critical", "E 归因", "F hazard", "G 软门禁"} {
		if !strings.Contains(text, section) {
			t.Errorf("render missing section %q:\n%s", section, text)
		}
	}
}
