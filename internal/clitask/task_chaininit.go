package clitask

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/artifactchain"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/spf13/cobra"
)

// task_chaininit.go — oracle-pipeline L0 的一键接线入口：`forge task chain-init`
// 写默认链 schema 且 spec stage 升 human 档——spec 里的 accept 围栏从此要经人工
// 审批（forge task artifact --approve spec --by <人>）才算定稿考卷，内容再改即
// 作废。刻意独立成命令而非 artifact 的 flag：artifact 的命令面（path+flags）是
// compat 七面快照的冻结面，加 flag 判 changed（破坏性预告流程）——新增命令只判
// added（非破坏），与 b51f601「+3 命令非破坏新增」同路。

var taskChainInitCmd = &cobra.Command{
	Use:   "chain-init [--force]",
	Short: "一键接线产物链执法：写默认链 schema 且 spec 档升 human（考卷围栏需人工签字）",
	Long: "一键接线产物链执法（oracle-pipeline L0）：写 <DataDir>/schemas/schema.yaml\n" +
		"——默认链 proposal→spec→design→plan，其中 spec 档升 human：spec 里的\n" +
		"accept 围栏（accept: <cmd> :: <expected> 行 / accept 围栏块）从此需经\n" +
		"forge task artifact --approve spec --by <审批人> 签字才算定稿考卷，\n" +
		"内容再改即作废（业务正确性的签字入口）。\n\n" +
		"默认拒绝覆盖已存在的 schema（--force 显式承担）。项目级动作，不依赖活跃任务。",
	RunE: runTaskChainInit,
}

func init() {
	Root.AddCommand(taskChainInitCmd)
	taskChainInitCmd.Flags().Bool("force", false, "覆盖已存在的 schema.yaml（默认拒绝覆盖）")
}

// runTaskChainInit 写 schema 并回显档位表。默认链骨架来自
// artifactchain.DefaultChain（与 Load 的 fail-open 缺省同一真相源）。
func runTaskChainInit(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	return runArtifactInitSchema(force)
}

// runArtifactInitSchema 一键接线产物链执法（oracle-pipeline L0）：写默认链 schema
// 且 spec stage 升 human 档。默认拒绝覆盖已有 schema（--force 显式承担）。
func runArtifactInitSchema(force bool) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	if !force {
		if _, err := os.Stat(artifactchain.SchemaPath(root)); err == nil {
			return fmt.Errorf("schema 已存在（%s）——改档请直接编辑该文件；确认覆盖用 --force", artifactchain.SchemaPath(root))
		}
	}
	chain := artifactchain.DefaultChain()
	for i := range chain.Stages {
		if chain.Stages[i].Name == "spec" {
			chain.Stages[i].Mode = artifactchain.ModeHuman
			// human 档考卷围栏指引入 Instruction——spec 的验收标准行在此档是
			// 需签字的业务考卷，不只是文本约定。
			chain.Stages[i].Instruction = "规格：验收标准（accept: <cmd> :: <expected> 行可被 forge task artifact --extract 编译成验收门禁；本档 spec 需 forge task artifact --approve spec --by <审批人> 签字定稿，内容再改即作废）"
		}
	}
	if err := artifactchain.WriteSchema(root, chain); err != nil {
		return fmt.Errorf("写入 schema 失败: %w", err)
	}
	loaded, warns := artifactchain.Load(root)
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, w)
	}
	fmt.Printf("已接线产物链 schema：%s\n", artifactchain.SchemaPath(root))
	for _, s := range loaded.Stages {
		fmt.Printf("  %-12s %s\n", s.Name, s.Mode)
	}
	fmt.Println("spec 档 human：考卷围栏（accept: 行）经 forge task artifact --approve spec --by <审批人> 签字定稿；proposal/design/plan 维持 advisory。")
	return nil
}
