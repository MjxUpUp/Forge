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
	if rep.Caliber.DedupWindow != harnessaudit.DefaultCaliber().DedupWindow || rep.Caliber.DrillWindow == 0 {
		t.Fatalf("caliber fields must be serialized, got %+v", rep.Caliber)
	}
	// D.Declared 不在空项目断言里：它来自全局 canonical skill 树（与项目无关），D 批次
	// 给 skill 声明 refs_critical 后该值自然非零——语义由 harnessaudit 包内带声明的夹具钉。
	if rep.A.Total != 0 {
		t.Fatalf("empty project must report zero data, got A=%+v", rep.A)
	}
	if len(rep.LoaderWarnings) == 0 {
		// 空项目上四源都应加载成功（hazard 缺文件 = (nil,nil) 非错误）。
		t.Logf("loader warnings: %v (expected none)", rep.LoaderWarnings)
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
