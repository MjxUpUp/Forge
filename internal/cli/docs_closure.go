package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/agentproto"
	"github.com/MjxUpUp/Forge/internal/capability"
	"github.com/spf13/cobra"
)

var (
	docsClosureJSON   bool
	docsClosureStrict bool
)

// docs closure：把「命令树 ↔ 活文档」的闭合情况打印出来。exit code 与
// docs lint 对齐：0=无漂移（或未 --strict），2=--strict 下存在漂移。
var docsClosureCmd = &cobra.Command{
	Use:   "closure [--json] [--strict]",
	Short: "能力面闭合表：命令树与活文档（README/docs/reference/协议模板/AGENTS.md）的漂移",
	Long: `forge docs closure 把当前二进制自报的命令面与活文档逐行对照：

  - 命令面来自运行中的 cobra 命令树（与 forge change agents 的生成面同源）；
  - 活文档 = README.md、AGENTS.md、docs/reference/**、internal/change/templates/**；
  - 只报「路径开头就对不上」的引用（整条命令被删或改名）；
  - 同一行写了「已删除 / 已移除 / deprecated」的按历史语境豁免。

--strict 下有漂移即 exit 2。它不阻断 verify 封印（封印只认判据实跑），
但会进 verify 摘要：文档教了不存在的动作，是 review 该看到的事实。`,
	RunE: runDocsClosure,
}

func init() { docsCmd.AddCommand(docsClosureCmd) }

func runDocsClosure(cmd *cobra.Command, args []string) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	surface, err := agentSurface()
	if err != nil {
		return err
	}
	tbl := capability.Surface(surface, agentproto.Annotations)
	files, err := capability.LiveDocs(root)
	if err != nil {
		return err
	}
	drifts, err := capability.ScanFiles(root, files, surface)
	if err != nil {
		return err
	}
	tbl.Drifts = drifts
	if docsClosureJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(tbl); err != nil {
			return err
		}
	} else {
		fmt.Printf("命令面：%d 条命令 / %d 个 flag（agent 注解 %d 条）\n", tbl.Commands, tbl.Flags, tbl.Annotated)
		fmt.Printf("活文档：%d 份\n", len(files))
		for _, d := range drifts {
			fmt.Printf("  ✗ %s:%d  %s\n", d.File, d.Line, d.Path)
		}
		fmt.Printf("drift: %d\n", len(drifts))
	}
	if docsClosureStrict && len(drifts) > 0 {
		return fmt.Errorf("docs closure --strict：%d 处漂移（文档教了不存在的命令）", len(drifts))
	}
	return nil
}
