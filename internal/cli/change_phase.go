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

  specify  goal.md 五段没填完（老 change 没有 goal.md 时跳过）
  clarify  缺 clarify.md（边界与非目标 + 人确认），或文件不完整
  plan     proposal.md/design.md 缺失或还是脚手架
  tasks    acceptance.md/tasks.md 缺失、没有任务、或有任务没写判据编号
  ready    可以 forge task start --change <id>（老 change 没有 goal.md 时只能不带 --change）

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
		if c, _, lerr := change.Load(root, id); lerr == nil && !c.HasGoal {
			fmt.Fprintf(out, "下一步：老 change 没有 goal.md，不能 --change 钉契约：\n  - 补 spec/changes/%s/goal.md 后重新 phase，或\n  - forge task start --ref feat/%s --branch（不钉契约，implement 只记 advisory）\n", id, id)
			return nil
		}
		fmt.Fprintf(out, "下一步：forge task start --ref feat/%s --branch --change %s\n", id, id)
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
	noteFile, _ := cmd.Flags().GetString("note-file")
	if note != "" && noteFile != "" {
		return fmt.Errorf("change: --note 与 --note-file 只能给一个，别让两份正文互相覆盖")
	}
	if noteFile != "" {
		abs, aerr := filepath.Abs(noteFile)
		if aerr != nil {
			return aerr
		}
		// 词法检查先挡一层，再解析符号链接挡指向仓外的软链（相对路径按仓库根解析）。
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("change: --note-file 必须在仓库内（与 --goal-file 同一约束）：%s", noteFile)
		}
		resolved, serr := filepath.EvalSymlinks(abs)
		if serr != nil {
			return fmt.Errorf("change: 读取 --note-file: %w", serr)
		}
		if rrel, rerr := filepath.Rel(root, resolved); rerr != nil || rrel == ".." || strings.HasPrefix(rrel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("change: --note-file 指向了仓库外的文件（符号链接）：%s", noteFile)
		}
		raw, rerr := os.ReadFile(resolved)
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
	nonGoals := ""
	if g, _, _, gerr := change.LoadGoal(root, id); gerr == nil {
		for _, s := range g.Sections {
			if s.Key == "out_of_scope" {
				nonGoals = s.Body
			}
		}
	} else if change.FileGoalExists(root, id) {
		// 有 goal.md 但读不出来（缺段/脚手架未清）：clarify 不该掩盖那个错误。
		return fmt.Errorf("change: clarify 之前先把 goal.md 填完：%w", gerr)
	}
	// 老 change（没有 goal.md）允许记录澄清：非目标留空，由 --note 说明边界。
	now := time.Now()
	body := change.RenderClarify(id, note, nonGoals, now)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("change: 写 clarify.md: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "已记录 clarify.md（人确认时间 %s）\n", now.UTC().Format(time.RFC3339))
	fmt.Fprintf(cmd.OutOrStdout(), "下一步：forge change phase %s\n", id)
	return nil
}
