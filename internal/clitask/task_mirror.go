package clitask

// task_mirror.go — `forge task mirror github`（focus-batches §2c，方向 C）：把分派
// 任务镜像到 GitHub Issues（Forge 台账为主真相、issue 为组织可见面）。经 `gh` CLI
// 执行（无 gh/未登录 → 明确报错不静默）；--dry-run 只打印计划。计划层（纯函数）在
// taskpipeline/mirror.go，可独立单测；本文件只做进程编排。

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

func init() {
	taskMirrorGithubCmd.Flags().String("repo", "", "目标仓库 owner/name（缺省 gh 上下文仓库）")
	taskMirrorGithubCmd.Flags().Bool("dry-run", false, "只打印镜像计划，不调 gh")
	taskMirrorCmd.AddCommand(taskMirrorGithubCmd)
	Root.AddCommand(taskMirrorCmd)
}

var taskMirrorCmd = &cobra.Command{
	Use:   "mirror",
	Short: "issue-tracker 镜像（Forge 台账为主真相，tracker 为组织可见面）",
}

var taskMirrorGithubCmd = &cobra.Command{
	Use:   "github [--repo owner/name] [--dry-run]",
	Short: "镜像分派任务到 GitHub Issues（offered→建 issue，终态→关闭；经 gh CLI）",
	Long: `forge task mirror github 把带分派（Assignment）的任务状态镜像到 GitHub Issues：

- 无映射的分派任务 → 创建 issue（标题=摘要+ref，label=forge:<状态>）
- 终态任务（delivered/canceled/failed）→ 关闭镜像 issue（failed 追加失败 label）
- 映射存 DataDir/mirror-gh.json；--dry-run 只打印计划

Symphony 验证的入口需求：让非 Forge 用户在既有项目管理工具里观察/干预任务；
差异在 Forge 台账是持久真相源（Symphony 调度态 in-memory）。`,
	RunE: runTaskMirrorGithub,
}

// issueURLRe 从 gh issue create 的输出 URL 解析 issue 编号。
var issueURLRe = regexp.MustCompile(`/issues/(\d+)`)

func runTaskMirrorGithub(cmd *cobra.Command, args []string) error {
	repo, _ := cmd.Flags().GetString("repo")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	if !dryRun {
		if err := requireGh(); err != nil {
			return err
		}
	}
	states, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		return err
	}
	mapping, err := taskpipeline.LoadMirrorMapping(root)
	if err != nil {
		return err
	}
	plan := taskpipeline.BuildMirrorPlan(states, mapping)
	if len(plan) == 0 {
		fmt.Println("镜像计划为空（无分派任务需要镜像，或全部已同步）。")
		return nil
	}
	fmt.Printf("镜像计划：%d 条动作\n", len(plan))
	for _, a := range plan {
		fmt.Printf("  %-28s issue=%-5d %-30s %s\n", a.TaskRef, a.Issue,
			strings.Join(append(a.LabelAdd, a.LabelRemove...), ","), a.Reason)
	}
	if dryRun {
		fmt.Println("（--dry-run：未调 gh）")
		return nil
	}
	for i := range plan {
		if err := execMirrorAction(root, repo, &plan[i], mapping); err != nil {
			// 每条 create 成功后已即时持久化映射——中途失败不丢已建 issue 编号，
			// 重跑不会重复建 issue（对抗审查 should-fix，复审轮确认旧代码未修）。
			return fmt.Errorf("动作 %s 失败（已建 issue 的映射已落盘，重跑续做）: %w", plan[i].TaskRef, err)
		}
		if err := taskpipeline.SaveMirrorMapping(root, mapping); err != nil {
			return fmt.Errorf("映射持久化失败（已建 issue 编号见上方输出，可手工补 DataDir/mirror-gh.json）: %w", err)
		}
	}
	fmt.Printf("完成：映射表已更新（%d 条）→ DataDir/mirror-gh.json\n", len(mapping))
	return nil
}

// execMirrorAction 执行单条动作并把新 issue 编号写回 mapping。
func execMirrorAction(root, repo string, a *taskpipeline.MirrorAction, mapping map[string]int) error {
	if a.Issue == 0 {
		args := []string{"issue", "create", "--title", a.Title}
		if repo != "" {
			args = append(args, "--repo", repo)
		}
		for _, l := range a.LabelAdd {
			args = append(args, "--label", l)
		}
		out, err := gh(args...)
		if err != nil {
			return fmt.Errorf("gh issue create: %w（%s）", err, strings.TrimSpace(out))
		}
		m := issueURLRe.FindStringSubmatch(strings.TrimSpace(out))
		if m == nil {
			return fmt.Errorf("gh 输出无法解析 issue URL: %s", strings.TrimSpace(out))
		}
		n, _ := strconv.Atoi(m[1])
		a.Issue = n
		mapping[a.TaskRef] = n
		fmt.Printf("  ✅ %s → issue #%d\n", a.TaskRef, n)
		return nil
	}
	if len(a.LabelAdd) > 0 {
		args := []string{"issue", "edit", strconv.Itoa(a.Issue), "--add-label", strings.Join(a.LabelAdd, ",")}
		if repo != "" {
			args = append(args, "--repo", repo)
		}
		if out, err := gh(args...); err != nil {
			return fmt.Errorf("gh issue edit: %w（%s）", err, strings.TrimSpace(out))
		}
	}
	if a.Close {
		args := []string{"issue", "close", strconv.Itoa(a.Issue), "--reason", "completed"}
		if repo != "" {
			args = append(args, "--repo", repo)
		}
		if out, err := gh(args...); err != nil {
			return fmt.Errorf("gh issue close: %w（%s）", err, strings.TrimSpace(out))
		}
	}
	fmt.Printf("  ✅ %s → issue #%d %s\n", a.TaskRef, a.Issue, actionSummary(a))
	return nil
}

func actionSummary(a *taskpipeline.MirrorAction) string {
	if a.Close {
		return "closed"
	}
	return "synced"
}

// gh 执行 gh CLI；PATH 上的 gh 优先（evalkit 假二进制先例：测试可注入 FORGE_GH_BIN）。
func gh(args ...string) (string, error) {
	bin := os.Getenv("FORGE_GH_BIN")
	if bin == "" {
		bin = "gh"
	}
	out, err := exec.Command(bin, args...).CombinedOutput()
	return string(out), err
}

func requireGh() error {
	bin := os.Getenv("FORGE_GH_BIN")
	if bin == "" {
		bin = "gh"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("gh CLI 不可用（%s 不在 PATH）——安装 GitHub CLI 并 gh auth login，或用 --dry-run 查看计划", bin)
	}
	return nil
}
