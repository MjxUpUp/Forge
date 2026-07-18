package cli

import (
	"github.com/spf13/cobra"
)

// skillsCanonicalFlag 是 --canonical flag 的值（覆盖 env 与内置 embed 库）。
var skillsCanonicalFlag string

// skillsCmd 是 `forge skills` 父命令。自带 PersistentPreRunE 覆盖 root 的——
// 保留 update 检查，跳过 findProjectRoot+autoSync，让 skill 分发在非 forge 项目
// （用户全局目录）也能跑，不要求当前目录是 forge 项目。
var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Skill 库管理与分发",
	Long: `forge skills — 管理 canonical skill 库并分发到各 AI 工具。

子命令：
  list          列出 canonical 库中的 skill
  validate      R1-R11 规范校验（exit code 编码结果）
  audit         19 条安全规则审查
  install       分发到全局/项目目标（link/copy）
  drift-check   检测分发分叉（dry-run）
  adapters      部署 skill-routing adapter 单文件
  usage         使用度量（undertrigger 分析）
  eval-gen      生成 eval 测试 prompt 清单

canonical 源优先级：--canonical > $FORGE_SKILLS_CANONICAL > 内置 embed 库。`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// 覆盖 root.PersistentPreRunE：只保留 update 检查，不要求 forge 项目、不 autoSync。
		// 全局分发（~/.claude/skills 等）应在任意目录可用。
		checkForUpdate(cmd.Root().Version, cmd)
		return nil
	},
}

func init() {
	skillsCmd.PersistentFlags().StringVar(&skillsCanonicalFlag, "canonical", "",
		"skill 库源目录（覆盖 $FORGE_SKILLS_CANONICAL 与内置 embed 库）")
	rootCmd.AddCommand(skillsCmd)
}
