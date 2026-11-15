package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/change"
	"github.com/spf13/cobra"
)

// change_phase.go —— 入口路由 A2：阶段机与 clarify 记录。
//
// 阶段只读文件推导（change.ComputePhase），命令本身不写状态：`phase` 是镜子，
// 不是开关。clarify 是唯一的新产物，必须人确认（--by-human）才能落盘。

func init() {
	changeCmd.AddCommand(changePhaseCmd)
	changeCmd.AddCommand(changeClarifyCmd)
	changePhaseCmd.Flags().Bool("json", false, "JSON 格式输出")
	changeClarifyCmd.Flags().Bool("by-human", false, "确认「人已回答边界/非目标」：缺它就拒绝写文件（agent 不得自批）")
	changeClarifyCmd.Flags().String("note", "", "边界与非目标正文（人给出的回答；不传则读 --note-file）")
	changeClarifyCmd.Flags().String("note-file", "", "从文件读边界与非目标正文")
	changeClarifyCmd.Flags().Bool("force", false, "覆盖已存在的 clarify.md")
}

var changePhaseCmd = &cobra.Command{
	Use:   "phase <id> [--json]",
	Short: "这个 change 走到哪一阶段、还缺什么（specify → clarify → plan → tasks → ready）",
	Long: `按磁盘上的产物推导 change 的阶段，并给出补齐的下一步命令：

  specify  goal.md 五段没填完
  clarify  缺 clarify.md（边界与非目标 + 人确认），或文件不完整
  plan     design.md 缺失或还是脚手架
  tasks    tasks.md 缺失 / 没有任务 / 有任务没写判据编号
  ready    可以 forge task start --change <id>

阶段不写状态文件：每次都从文件重建，没有"阶段文件过期"的漂移。`,
	Args: cobra.ExactArgs(1),
	RunE: runChangePhase,
}

var changeClarifyCmd = &cobra.Command{
	Use:   "clarify <id> --by-human --note \"…\"",
	Short: "记录人给出的边界与非目标（clarify.md，必须有 --by-human）",
	Long: `把「边界 / 非目标」写成 spec/changes/<id>/clarify.md。

必须先问用户，得到明确回答再加 --by-human——这个 flag 记录的是"人说过"，
agent 不得自批。已存在时要 --force 覆盖。`,
	Args: cobra.ExactArgs(1),
	RunE: runChangeClarify,
}

func runChangePhase(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	id := args[0]
	phase, missing, err := change.ComputePhase(root, id)
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
			"change":  id,
			"phase":   string(phase),
			"missing": missing,
			"ready":   phase == change.PhaseReady,
		})
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "change: %s\n阶段: %s\n", id, phase)
	if phase == change.PhaseReady {
		fmt.Fprintf(out, "下一步：forge task start --ref feat/%s --branch --title \"…\" --change %s\n", id, id)
		return nil
	}
	for _, m := range missing {
		fmt.Fprintf(out, "  - %s\n", m)
	}
	return nil
}

func runChangeClarify(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	id := args[0]
	if err := change.ValidID(id); err != nil {
		return err
	}
	byHuman, _ := cmd.Flags().GetBool("by-human")
	if !byHuman {
		return fmt.Errorf("change: clarify 需要 --by-human——先问用户「边界是什么、明确不做什么」，得到回答后再加该 flag；agent 不得自批")
	}
	note, _ := cmd.Flags().GetString("note")
	if noteFile, _ := cmd.Flags().GetString("note-file"); noteFile != "" {
		abs, aerr := filepath.Abs(noteFile)
		if aerr != nil {
			return aerr
		}
		if rel, rerr := filepath.Rel(root, abs); rerr != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("change: --note-file 必须在仓库内（与 --goal-file 同一约束）：%s", noteFile)
		}
		raw, rerr := os.ReadFile(abs)
		if rerr != nil {
			return fmt.Errorf("change: 读取 --note-file: %w", rerr)
		}
		note = string(raw)
	}
	if len([]rune(strings.TrimSpace(note))) < 10 {
		return fmt.Errorf("change: --note 太短——写清「做什么 / 不做什么」（至少一句话）")
	}

	dir := filepath.Join(root, "spec", "changes", id)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("change: 不存在 spec/changes/%s", id)
	}
	path := filepath.Join(dir, change.FileClarify)
	force, _ := cmd.Flags().GetBool("force")
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("change: %s 已存在——要重写请加 --force（历史澄清删了就没法回溯）", filepath.ToSlash(filepath.Join("spec/changes", id, change.FileClarify)))
	}
	g, _, _, err := change.LoadGoal(root, id)
	if err != nil {
		return fmt.Errorf("change: clarify 之前先把 goal.md 填完：%w", err)
	}
	nonGoals := ""
	for _, s := range g.Sections {
		if s.Key == "out_of_scope" {
			nonGoals = s.Body
		}
	}
	body := change.RenderClarify(id, note, nonGoals, time.Now())
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("change: 写 clarify.md: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "已记录 clarify.md（人确认时间 %s）\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(cmd.OutOrStdout(), "下一步：forge change phase %s\n", id)
	return nil
}
