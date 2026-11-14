package clitask

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MjxUpUp/Forge/internal/change"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// resolveGoalContract 解析 task start 的 goal 契约来源（task-goal-contract）。
//
// 两条入口二选一：
//   - --change <id>：读 spec/changes/<id>/goal.md（第五份产物，随 git 走）；
//   - --goal-file <path>：读仓库内任意五段式 goal.md（临时任务/非 change 流程）。
//
// 与 --goal 自由文本互斥：自由文本没有五段、也没有哈希可钉，两套语义同时给会
// 让"契约到底指哪份"变得无法判定。返回的 summary 取 Objective 段，供 resume
// 一句话拉回目标（state.Goal 原有的展示位）。
func resolveGoalContract(cmd *cobra.Command, root string) (*tasktypes.GoalContract, string, error) {
	changeID, _ := cmd.Flags().GetString("change")
	goalFile, _ := cmd.Flags().GetString("goal-file")
	goalText, _ := cmd.Flags().GetString("goal")

	if changeID == "" && goalFile == "" {
		return nil, "", nil
	}
	if changeID != "" && goalFile != "" {
		return nil, "", fmt.Errorf("--change 与 --goal-file 只能选一个（前者从 change 产物读，后者从任意仓库文件读）")
	}
	if goalText != "" {
		return nil, "", fmt.Errorf("--goal 与 --change/--goal-file 互斥：契约文件自带 Objective/Why/Constraints/Out of Scope/Check 五段，自由文本会让门禁无法判定契约")
	}

	if changeID != "" {
		// change id 是路径分量：校验必须在拼路径之前，否则 --change ../x 会把
		// 契约钉到仓库外（ChangeDir 直接 Join）。LoadGoal 里再守一次，防未来
		// 别的调用方绕过这里。
		if err := change.ValidID(changeID); err != nil {
			return nil, "", err
		}
		g, sum, rel, err := change.LoadGoal(root, changeID)
		if err != nil {
			return nil, "", err
		}
		return &tasktypes.GoalContract{Path: rel, SHA256: sum, Change: changeID}, g.Sections[0].Body, nil
	}

	abs := goalFile
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, "", fmt.Errorf("--goal-file 必须位于仓库内（%q 不在 %q 下）：goal 契约要随 git 走，仓外文件无法被评审/追溯", goalFile, root)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", fmt.Errorf("读取 --goal-file %q 失败: %w", goalFile, err)
	}
	// 符号链接不能成为逃逸口：路径在仓库内、但链接指向仓外时，git 里只有链接
	// 本身，评审/追溯看不到真实内容。按解析后的真实路径再判一次边界。
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		rootResolved, _ := filepath.EvalSymlinks(root)
		if realRel, rerr := filepath.Rel(rootResolved, resolved); rerr != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
			return nil, "", fmt.Errorf("--goal-file 解析符号链接后落在仓库外（%q → %q）：契约内容必须随 git 走，链接指向仓外文件无法被评审/追溯", goalFile, resolved)
		}
	}
	g, err := change.ParseGoal(string(raw))
	if err != nil {
		return nil, "", fmt.Errorf("--goal-file %q：%w", goalFile, err)
	}
	return &tasktypes.GoalContract{Path: filepath.ToSlash(rel), SHA256: change.GoalHash(raw)}, g.Sections[0].Body, nil
}
