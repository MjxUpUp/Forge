package cli

// gate.go — `forge gate` 命令族：git/PR 收口的 CLI 面（focus-batches §1c，方向 A）。
// 治理锚点组合的推送侧：本地 hook 生产证据，推送边界确定性复检——云端 agent 不经
// 本地 hook，但全部以 git/PR 为界面，push 是覆盖本地+云+CI 的最小公倍汇聚点。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// gateRoot 解析推送门禁的工作根：forge 项目优先，git 仓库兜底——push 收口的
// 设计前提就是"覆盖 forge 未接管的仓库"（云端 agent 分支、旁观协作），不能强制
// 先 forge init。
func gateRoot() (string, error) {
	if root, err := findProjectRoot(); err == nil {
		return root, nil
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("既非 forge 项目也非 git 仓库（push 门禁需要其一）")
	}
	return strings.TrimSpace(string(out)), nil
}

func init() {
	gatePushCmd.Flags().String("ref", "", "分支名（缺省当前分支）")
	gatePushCmd.Flags().Bool("dry-run", false, "只报告不阻断（exit 0）")
	gateHooksCmd.Flags().Bool("uninstall", false, "移除 pre-push 钩子")
	rootCmd.AddCommand(gateCmd)
	gateCmd.AddCommand(gatePushCmd, gateHooksCmd)
}

var gateCmd = &cobra.Command{
	Use:   "gate",
	Short: "git 推送边界门禁（治理随 git 走：cheat-scan 复检 + 未消解 BLOCKED 任务）",
}

var gatePushCmd = &cobra.Command{
	Use:   "push [--ref <branch>] [--dry-run]",
	Short: "推送前确定性复检：base...HEAD cheat-scan + 本分支未消解 BLOCKED 任务 + 证据快照",
	Long: `forge gate push 在推送边界复检（不依赖本地 hook 是否曾生效）：

1. 工作树未提交变更 → warn（推送边界不应夹带未审工作）
2. 推送范围（merge-base...HEAD）新增行重跑 7 类确定性作弊检测
3. 本分支未完成任务存在最新状态为 BLOCKED 的检查 → 阻断

结果落 checklog（gate-push 行）+ 推送证据快照 DataDir/pushes/。
CI 复跑形态：CI job 里直接跑本命令（同套判定两处生效，本地被绕过时 CI 兜底）。
阻断 exit 2；逃生（留痕）: FORGE_GATE_PUSH=disable。`,
	RunE: runGatePush,
}

var gateHooksCmd = &cobra.Command{
	Use:   "hooks install [--uninstall]",
	Short: "安装/卸载 git pre-push 钩子（core.hooksPath=.forge/git-hooks，调 forge gate push）",
	RunE:  runGateHooks,
}

func runGatePush(cmd *cobra.Command, args []string) error {
	ref, _ := cmd.Flags().GetString("ref")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	root, err := gateRoot()
	if err != nil {
		return err
	}
	res := taskpipeline.RunPushGate(root, ref)
	if res.Skipped {
		fmt.Printf("push gate skipped：%s\n", res.Reason)
		return nil
	}
	fmt.Printf("push gate：%s（base %s…HEAD，%d cheat findings，%d 未消解任务）",
		res.Ref, short(res.Base), len(res.Findings), len(res.BlockedTasks))
	if res.Dirty {
		fmt.Print(" · ⚠ 工作树有未提交变更")
	}
	fmt.Println()
	for _, f := range res.Findings {
		fmt.Printf("  CHEAT: %s %s:%d — %s\n", f.Pattern, f.File, f.Line, truncate(f.Snippet, 60))
	}
	for _, t := range res.BlockedTasks {
		fmt.Printf("  BLOCKED-TASK: %s（forge task gate 查看详情）\n", t)
	}
	if !res.Blocked() {
		fmt.Println("✅ 推送边界检查通过（证据快照已写 DataDir/pushes/）")
		return nil
	}
	if dryRun {
		fmt.Println("（--dry-run：不阻断，仅报告）")
		return nil
	}
	return fmt.Errorf("BLOCKED: 推送边界检查未通过——修复上述问题后重推；紧急放行（留痕）: FORGE_GATE_PUSH=disable")
}

func runGateHooks(cmd *cobra.Command, args []string) error {
	uninstall, _ := cmd.Flags().GetBool("uninstall")
	root, err := gateRoot()
	if err != nil {
		return err
	}
	hooksDir := filepath.Join(root, ".forge", "git-hooks")
	if uninstall {
		if err := os.Remove(filepath.Join(hooksDir, "pre-push")); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Println("已移除 .forge/git-hooks/pre-push（core.hooksPath 配置保留——目录空则 git 无钩子可跑）")
		return nil
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	script := `#!/bin/sh
# forge pre-push gate（focus-batches §1c）——治理随 git 走：推送前过同一套确定性检查。
# 由 forge gate hooks install 生成；forge 不在 PATH 时 fail-open 放行（装回 forge 即生效）。
FORGE_BIN="$(command -v forge 2>/dev/null)"
if [ -z "$FORGE_BIN" ]; then
  echo "[forge] forge 不在 PATH——pre-push 门禁跳过（fail-open）"
  exit 0
fi
exec "$FORGE_BIN" gate push --ref "$1"
`
	path := filepath.Join(hooksDir, "pre-push")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return err
	}
	// core.hooksPath 指向仓内目录（每开发者本地安装一次；目录可提交以团队共享）。
	if out, err := exec.Command("git", "-C", root, "config", "core.hooksPath", ".forge/git-hooks").CombinedOutput(); err != nil {
		return fmt.Errorf("设置 core.hooksPath 失败: %v（%s）", err, out)
	}
	fmt.Printf("pre-push 钩子已安装：%s\ncore.hooksPath=.forge/git-hooks（卸载：forge gate hooks install --uninstall）\n", path)
	_ = checklog.Record(root, &checklog.Entry{
		Check: checklog.CheckGatePush, Passed: true, Checked: true,
		Level:  checklog.LevelPass,
		Detail: "pass: git pre-push hook installed (core.hooksPath=.forge/git-hooks)",
	})
	return nil
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
