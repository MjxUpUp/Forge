package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/change"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(changeCmd)
	changeCmd.AddCommand(changeInitCmd)
	changeCmd.AddCommand(changeNewCmd)
	changeCmd.AddCommand(changeListCmd)
	changeCmd.AddCommand(changeVerifyCmd)

	changeInitCmd.Flags().String("profile", "mini", "门禁档位：mini / standard / full")
	changeNewCmd.Flags().String("title", "", "change 标题（缺省用 id）")
	changeListCmd.Flags().Bool("json", false, "JSON 格式输出")
	changeVerifyCmd.Flags().Duration("timeout", change.DefaultCheckTimeout, "单条验收命令的超时")
}

var changeCmd = &cobra.Command{
	Use:   "change",
	Short: "仓库内 change 生命周期：提案 → 设计 → 任务 → 可执行验收 → 证据封印",
	Long: `forge change 把交付过程做成仓库里的文件，而不是装在机器上的插件：

  spec/forge.yaml                     契约（有它就算 forge 项目，clone 即用）
  spec/changes/<id>/proposal.md       为什么做
  spec/changes/<id>/design.md         怎么做
  spec/changes/<id>/tasks.md          拆成哪些步骤
  spec/changes/<id>/acceptance.md     判据：可执行的 bash 命令 + 人工检查项
  spec/changes/<id>/evidence/<ts>/    实跑证据（stdout/stderr/exit + 绑定判据与 diff 的封印）

状态不单独存进度库：删掉证据就回到未验证，改判据就让旧证据作废（stale）。
没有安装 forge 的机器上，照着这个目录结构手工产出同样的文件，协议一样成立。`,
}

var changeInitCmd = &cobra.Command{
	Use:   "init [--profile <mini|standard|full>]",
	Short: "在仓库里声明契约（spec/forge.yaml + spec/changes/ + 缺省 AGENTS.md）",
	RunE:  runChangeInit,
}

var changeNewCmd = &cobra.Command{
	Use:   "new <id> [--title <标题>]",
	Short: "新建一个 change 骨架（proposal/design/tasks/acceptance 四份模板）",
	Args:  cobra.ExactArgs(1),
	RunE:  runChangeNew,
}

var changeListCmd = &cobra.Command{
	Use:   "list [--json]",
	Short: "列出全部 change 及其阶段（状态从文件系统推导）",
	RunE:  runChangeList,
}

var changeVerifyCmd = &cobra.Command{
	Use:   "verify <id> [--timeout 10m]",
	Short: "实跑 acceptance.md 里的命令并落证据（可执行判据 + 封印）",
	Args:  cobra.ExactArgs(1),
	RunE:  runChangeVerify,
}

func runChangeInit(cmd *cobra.Command, _ []string) error {
	profile, _ := cmd.Flags().GetString("profile")
	root, err := changeRoot()
	if err != nil {
		return err
	}
	created, err := change.Init(root, profile)
	if err != nil {
		return err
	}
	fmt.Println("已声明仓库契约：")
	for _, p := range created {
		fmt.Printf("  + %s\n", p)
	}
	fmt.Println()
	fmt.Println("下一步：")
	fmt.Printf("  forge change new <id> --title \"...\"    # 新建一个 change\n")
	return nil
}

func runChangeNew(cmd *cobra.Command, args []string) error {
	id := strings.TrimSpace(args[0])
	title, _ := cmd.Flags().GetString("title")
	root, err := changeRoot()
	if err != nil {
		return err
	}
	dir, err := change.New(root, id, title)
	if err != nil {
		return err
	}
	rel := dir
	if r, rerr := filepath.Rel(root, dir); rerr == nil {
		rel = filepath.ToSlash(r)
	}
	fmt.Printf("已创建 %s\n", rel)
	fmt.Println()
	fmt.Println("按顺序写：proposal（为什么做）→ design（怎么做）→ tasks（拆步骤）→ acceptance（可执行判据）")
	fmt.Printf("写完后运行：forge change verify %s\n", id)
	return nil
}

func runChangeList(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	root, err := changeRoot()
	if err != nil {
		return err
	}
	items, err := change.List(root)
	if err != nil {
		return err
	}
	if asJSON {
		raw, merr := json.MarshalIndent(items, "", "  ")
		if merr != nil {
			return merr
		}
		fmt.Println(string(raw))
		return nil
	}
	if len(items) == 0 {
		fmt.Println("还没有 change。运行 `forge change new <id> --title \"...\"` 开始。")
		return nil
	}
	fmt.Printf("%-32s %-10s %-8s %s\n", "CHANGE", "STAGE", "证据", "备注")
	for _, c := range items {
		note := ""
		switch {
		case c.Stale:
			note = "证据已失效（判据或工作区变过），需重跑 verify"
		case c.Sealed && !c.DiffBound:
			note = "证据已封印，但未绑定 git 工作区（非 git 仓库，封印强度较弱）"
		case c.Sealed:
			note = "证据已封印"
		case !c.HasAcceptance:
			note = "缺少 acceptance.md"
		}
		fmt.Printf("%-32s %-10s %-8s %s\n", c.ID, c.Stage, evidenceCell(c), note)
	}
	return nil
}

func evidenceCell(c change.Change) string {
	if c.EvidenceCount == 0 {
		return "-"
	}
	mark := ""
	if c.Sealed {
		mark = "✓"
	}
	return fmt.Sprintf("%d%s", c.EvidenceCount, mark)
}

func runChangeVerify(cmd *cobra.Command, args []string) error {
	id := strings.TrimSpace(args[0])
	// 先校验 id 再打印标题：非法 id 不该让人以为它已经跑起来了。
	if err := change.ValidID(id); err != nil {
		return err
	}
	timeout, _ := cmd.Flags().GetDuration("timeout")
	root, err := changeRoot()
	if err != nil {
		return err
	}

	fmt.Printf("verify %s\n\n", id)
	res, err := change.Verify(context.Background(), root, id, change.VerifyOptions{Timeout: timeout})
	if err != nil {
		return err
	}

	failed := 0
	for _, c := range res.Summary.Checks {
		switch {
		case c.Skipped:
			fmt.Printf("  ⏭  [%d] %s — 未执行（前一条失败）\n", c.Index, firstLine(c.Command))
		case c.Exit == 0:
			fmt.Printf("  ✅ [%d] %s — %s\n", c.Index, firstLine(c.Command), dur(c.DurationMS))
		default:
			failed++
			tag := fmt.Sprintf("exit %d", c.Exit)
			if c.TimedOut {
				tag = "超时"
			}
			fmt.Printf("  ❌ [%d] %s — %s（%s）\n", c.Index, firstLine(c.Command), tag, dur(c.DurationMS))
			if c.StderrFile != "" {
				printTail(filepath.Join(res.EvidenceDir, c.StderrFile), 15)
			}
		}
	}

	if len(res.Manual) > 0 {
		fmt.Println()
		fmt.Println("人工检查项（不计入封印，由人确认）：")
		for _, m := range res.Manual {
			fmt.Printf("  - %s\n", m)
		}
	}

	fmt.Println()
	relEv := res.EvidenceDir
	if r, rerr := filepath.Rel(root, res.EvidenceDir); rerr == nil {
		relEv = filepath.ToSlash(r)
	}
	if res.Summary.Sealed {
		if !res.Summary.DiffBound {
			fmt.Println("⚠️  证据未绑定 git 工作区（非 git 仓库）：只能证明命令跑过，证明不了它对应哪份代码")
		}
		fmt.Printf("✅ 全部通过，证据已封印：%s\n", relEv)
		return nil
	}
	fmt.Printf("❌ 验收未通过（%d 条命令失败），证据保留在：%s\n", failed, relEv)
	if fc := firstFailure(res.Summary.Checks); fc != nil {
		fmt.Printf("   先修这一条：[%d] %s\n", fc.Index, firstLine(fc.Command))
	}
	return fmt.Errorf("change %s: 验收未通过", id)
}

func firstFailure(checks []change.Check) *change.Check {
	for i := range checks {
		if !checks[i].Skipped && checks[i].Exit != 0 {
			return &checks[i]
		}
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]) + " …"
	}
	return s
}

func dur(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// printTail 打印输出文件最后 n 行——失败时人最需要看的是尾部，不是开头。
func printTail(path string, n int) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
		fmt.Println("     …（较早输出已省略）")
	}
	for _, l := range lines {
		fmt.Printf("     %s\n", l)
	}
}

// changeRoot 解析 change 命令的作用根目录：
//
//  1. 已声明契约或已登记的项目 → projectroot 的结论（与 status 同源）；
//  2. 否则退到 git 根（`change init` 要能在任何 git 仓库里跑）；
//  3. 再否则用 cwd。
//
// 第 2 步是刻意的：init 是"把一个仓库纳入协议"的动作，不能要求它先被协议纳入。
func changeRoot() (string, error) {
	if root, err := projectroot.Find(); err == nil {
		return root, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if git := forgedata.FindGitRoot(cwd); git != "" {
		return git, nil
	}
	return cwd, nil
}
