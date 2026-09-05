package cli

// compat_cmd.go — `forge compat snapshot|report`（mechanism-hardening P1-1，
// 可执行兼容工件）：六面确定性快照（golden 入库）+ 跨版本 diff。API Extractor
// 模型：golden 进仓 → PR diff 呈现 → 破坏性变更显式评审。

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/compat"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/spf13/cobra"
)

func init() {
	// schema 种子接缝注入（compat 不能 import taskpipeline/toolusage——依赖方向）。
	compat.SeedTaskStateForSchema = func() any { return taskpipeline.SeedTaskStateForSchema() }
	compat.SeedToolCallForSchema = func() any { return toolusage.SeedToolCallForSchema() }

	compatSnapshotCmd.Flags().String("out", "compat.snapshot.json", "快照输出路径（默认仓根，提交入库）")
	compatReportCmd.Flags().String("base", "HEAD~1", "基线 ref（git show <ref>:compat.snapshot.json）")
	compatReportCmd.Flags().Bool("json", false, "JSON 输出变更清单")
	rootCmd.AddCommand(compatCmd)
	compatCmd.AddCommand(compatSnapshotCmd, compatReportCmd)
}

var compatCmd = &cobra.Command{
	Use:   "compat",
	Short: "兼容性工件：六面快照与跨版本 diff（golden 入库，破坏性变更显式评审）",
}

var compatSnapshotCmd = &cobra.Command{
	Use:   "snapshot [--out <file>]",
	Short: "实算六面兼容快照（确定性：同树两次实算字节一致）",
	Long: `forge compat snapshot 实算六面快照：
命令树（path+flags）/ CheckName roster / 逃生舱 env 清单 / 内嵌载荷（skills
两树 SKILL.md sha256）/ 序列化 schema 键集合 / 阻断位点计数。

快照提交入库（compat.snapshot.json）——任何面的变更在 PR diff 里显式可见
（API Extractor 模型）。每面附检测边界（工件 diff 的已知盲区，诚实呈现）。`,
	RunE: runCompatSnapshot,
}

var compatReportCmd = &cobra.Command{
	Use:   "report [--base <ref>] [--json]",
	Short: "对比基线快照与当前实算：破坏性变更 exit 2（cargo-semver-checks 契约）",
	Long: `forge compat report 加载 git show <base>:compat.snapshot.json 与当前实算对比：
removed/changed（命令/检查/逃生舱/载荷/schema）→ 破坏性（exit 2）；
added → 提示同步 README/承诺表；blockings 增加 → 提示按文案契约附预告版本。

退出码：0=无破坏性变更；2=有破坏性变更（BLOCKED 契约）；1=工具故障
（基线缺失等——与"违规"分开，cargo-semver-checks 的 0/100/101 本地化）。`,
	RunE: runCompatReport,
}

func runCompatSnapshot(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	snap, err := compat.BuildSnapshot(root, rootCmd)
	if err != nil {
		return fmt.Errorf("快照实算失败: %w", err)
	}
	body, err := snap.Marshal()
	if err != nil {
		return err
	}
	out, _ := cmd.Flags().GetString("out")
	if !filepath.IsAbs(out) {
		out = filepath.Join(root, out)
	}
	if err := os.WriteFile(out, append(body, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("六面快照：%d 命令 / %d 检查 / %d 逃生舱 / %d 载荷项 / %d schema / %d 阻断文件 → %s\n",
		len(snap.Commands), len(snap.Checks), len(snap.Escapes), len(snap.Payload), len(snap.Schemas), len(snap.Blockings), out)
	fmt.Println("提交入库即 golden（PR diff 呈现；破坏性变更须过 forge compat report）")
	return nil
}

func runCompatReport(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	base, _ := cmd.Flags().GetString("base")
	asJSON, _ := cmd.Flags().GetBool("json")
	out, err := exec.Command("git", "-C", root, "show", base+":compat.snapshot.json").Output()
	if err != nil {
		return fmt.Errorf("基线快照不可读（git show %s:compat.snapshot.json）：%v——先 forge compat snapshot 并提交，或在含快照的基线上运行", base, err)
	}
	var baseSnap compat.Snapshot
	if err := json.Unmarshal(out, &baseSnap); err != nil {
		return fmt.Errorf("基线快照解析失败: %v", err)
	}
	cur, err := compat.BuildSnapshot(root, rootCmd)
	if err != nil {
		return fmt.Errorf("当前实算失败: %w", err)
	}
	changes := compat.Diff(&baseSnap, cur)
	breaking := compat.BreakingCount(changes)
	if asJSON {
		body, _ := json.MarshalIndent(changes, "", "  ")
		fmt.Println(string(body))
	} else {
		fmt.Printf("compat report（base=%s）：共 %d 项变更，破坏性 %d 项\n", base, len(changes), breaking)
		for _, c := range changes {
			mark := "  +"
			if c.Breaking {
				mark = " ✗"
			}
			fmt.Printf("%s %-9s %s\n", mark, c.Kind, c.Item)
		}
	}
	if breaking > 0 {
		return fmt.Errorf("BLOCKED: %d 项破坏性变更（对下面的强承诺面：命令/检查/逃生舱/载荷/schema 的 removed|changed）——按 docs/design/compat-commitments.md 处置：恢复该面或走预告流程（CHANGELOG 行为变更节 + 承诺表更新）", breaking)
	}
	if len(changes) > 0 {
		fmt.Println("（added/changed 非破坏项：同步 README/承诺表后 forge compat snapshot 重钉 golden）")
	}
	return nil
}
