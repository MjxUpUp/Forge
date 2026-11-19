package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/MjxUpUp/Forge/internal/skillcap"
)

var (
	skillDescription string
	skillWhen        string
	skillLesson      string
	skillEvidence    string
	skillDir         string
	skillForce       bool
	skillJSON        bool
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "把一次 retro 的教训沉淀成 SKILL.md（skill 资本化）",
	Long: `把一次 retro 的教训沉淀成 SKILL.md。

用法：
  forge skill new <id> --description "一句话做什么" \
      --when "什么时机该用它" --lesson "根因与做法" [--evidence "commit/测试"]

写出的文件是 <skills>/<id>/SKILL.md（默认 skills/），frontmatter 带 name 与
description（description 内联 Use when），正文是最小可执行版本。已存在时拒绝覆盖，
确需重写加 --force。

刻意不做「自动总结」：从会话里挑出可复用的教训是判断，不是模板填空；本命令只负责
把已经想清楚的四段内容落盘。
`,
}

var skillNewCmd = &cobra.Command{
	Use:   "new <id>",
	Short: "从 retro 结论生成一份 SKILL.md",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		d := skillcap.Draft{
			ID:          args[0],
			Description: skillDescription,
			WhenToUse:   skillWhen,
			Lesson:      skillLesson,
			Evidence:    skillEvidence,
		}
		path, err := skillcap.Write(root, skillDir, d, skillForce)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if skillJSON {
			return json.NewEncoder(out).Encode(map[string]string{"path": path, "id": d.ID})
		}
		fmt.Fprintf(out, "已写入 %s\n下一步：host 会按 description 的 Use when 路由；触发词接线按宿主另行决定。\n", path)
		return nil
	},
}

func init() {
	skillNewCmd.Flags().StringVar(&skillDescription, "description", "", "一句话说明这个 skill 做什么（必填；宿主靠它路由）")
	skillNewCmd.Flags().StringVar(&skillWhen, "when", "", "什么时机该用（必填，写进正文 When to use）")
	skillNewCmd.Flags().StringVar(&skillLesson, "lesson", "", "根因与做法（必填，写进 Procedure）")
	skillNewCmd.Flags().StringVar(&skillEvidence, "evidence", "", "证据（commit / 测试 / 门禁记录，可选）")
	skillNewCmd.Flags().StringVar(&skillDir, "dir", "skills", "skills 根目录（相对仓库根）")
	skillNewCmd.Flags().BoolVar(&skillForce, "force", false, "覆盖已有 SKILL.md")
	skillNewCmd.Flags().BoolVar(&skillJSON, "json", false, "输出 JSON")
	skillCmd.AddCommand(skillNewCmd)
	skillCmd.GroupID = "lifecycle"
	rootCmd.AddCommand(skillCmd)
}
