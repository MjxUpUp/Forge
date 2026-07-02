package cli

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/pipeline"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(validateCmd)
}

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "验证 pipeline.yml 的结构完整性（DAG、规则类型、引用）",
	Long: `forge validate 检查 pipeline.yml v2 的结构正确性：
  - DAG 环检测
  - 依赖引用验证（不存在的 gate ID）
  - 重复 gate ID
  - 检查规则类型是否合法
  - 引用的 hook 脚本是否存在`,
	RunE: runValidate,
}

func runValidate(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	errs := pipeline.ValidateOnly(root)
	if len(errs) == 0 {
		fmt.Println("Pipeline valid.")
		return nil
	}

	fmt.Printf("Pipeline validation failed (%d error(s)):\n\n", len(errs))
	for i, e := range errs {
		fmt.Printf("  %d. %s\n", i+1, e.Error())
	}
	return fmt.Errorf("%d validation error(s)", len(errs))
}
