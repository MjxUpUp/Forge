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
	changeCmd.AddCommand(changeStatusCmd)
	changeCmd.AddCommand(changeGateCmd)
	changeCmd.AddCommand(changeAgentsCmd)

	changeInitCmd.Flags().String("profile", "mini", "门禁档位：mini / standard / full")
	changeNewCmd.Flags().String("title", "", "change 标题（缺省用 id）")
	changeListCmd.Flags().Bool("json", false, "JSON 格式输出")
	changeVerifyCmd.Flags().Duration("timeout", change.DefaultCheckTimeout, "单条验收命令的超时")
	changeNewCmd.Flags().Bool("fast", false, "快速通道：只写 proposal 与 acceptance（判据与证据照旧）")
	changeStatusCmd.Flags().Bool("json", false, "JSON 格式输出")
	changeGateCmd.Flags().Int("task", 0, "tasks.md 里的任务序号（从 1 开始）")
	changeGateCmd.Flags().Duration("timeout", change.DefaultCheckTimeout, "单条判据的超时")
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
没有安装 forge 的机器上，照着这个目录结构手工产出同样的文件，协议一样成立。

循环里只认 forge change status <id> 给出的下一步：它从文件与证据推导，
不依赖对话记忆。逐节点推进用 forge change gate <id> --task <N>。`,
}

var changeInitCmd = &cobra.Command{
	Use:   "init [--profile <mini|standard|full>]",
	Short: "在仓库里声明契约（spec/forge.yaml + spec/changes/ + 缺省 AGENTS.md）",
	RunE:  runChangeInit,
}

var changeNewCmd = &cobra.Command{
	Use:   "new <id> [--title <标题>] [--fast]",
	Short: "新建一个 change 骨架（四份模板；--fast 只要 proposal 与 acceptance）",
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

var changeStatusCmd = &cobra.Command{
	Use:   "status <id> [--json]",
	Short: "当前状态、判定与下一步（循环只执行这里给出的 next steps）",
	Args:  cobra.ExactArgs(1),
	RunE:  runChangeStatus,
}

var changeAgentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "同步 AGENTS.md 的 forge 托管块（标记外一字不改）",
	RunE:  runChangeAgents,
}

var changeGateCmd = &cobra.Command{
	Use:   "gate <id> --task <N> [--timeout 10m]",
	Short: "只跑某条任务声明的判据（节点证据不封印整个 change）",
	Args:  cobra.ExactArgs(1),
	RunE:  runChangeGate,
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

func runChangeAgents(_ *cobra.Command, _ []string) error {
	root, err := changeRoot()
	if err != nil {
		return err
	}
	action, err := change.EnsureAgentsBlock(root)
	if err != nil {
		return err
	}
	switch action {
	case change.AgentsCreated:
		fmt.Println("已写入 AGENTS.md（整份新建，含 forge 托管块）")
	case change.AgentsAppended:
		fmt.Println("已把 forge 托管块追加到 AGENTS.md 末尾（原有内容未动）")
	case change.AgentsUpdated:
		fmt.Println("已更新 AGENTS.md 里的 forge 托管块（标记外未动）")
	default:
		fmt.Println("AGENTS.md 的 forge 托管块已是最新，无需改动")
	}
	return nil
}

func runChangeNew(cmd *cobra.Command, args []string) error {
	id := strings.TrimSpace(args[0])
	title, _ := cmd.Flags().GetString("title")
	root, err := changeRoot()
	if err != nil {
		return err
	}
	fast, _ := cmd.Flags().GetBool("fast")
	var dir string
	if fast {
		dir, err = change.NewFast(root, id, title)
	} else {
		dir, err = change.New(root, id, title)
	}
	if err != nil {
		return err
	}
	rel := dir
	if r, rerr := filepath.Rel(root, dir); rerr == nil {
		rel = filepath.ToSlash(r)
	}
	fmt.Printf("已创建 %s\n", rel)
	fmt.Println()
	if fast {
		fmt.Println("快速通道：写 proposal（为什么做）与 acceptance（怎么算完），删掉两份文件里的 forge:fill 标记。")
	} else {
		fmt.Println("按顺序写：proposal（为什么做）→ design（怎么做）→ tasks（拆步骤）→ acceptance（可执行判据）")
	}
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
			note = "证据已失效（判据、被验证文件或 commit 变过），需重跑 verify"
		case c.Sealed && !c.DiffBound:
			note = "证据已封印，但未绑定 git 工作区（非 git 仓库，封印强度较弱）"
		case c.Sealed && c.BoundHead != "":
			note = fmt.Sprintf("证据已封印 @%s（其后提交不在证据范围）", shortHead(c.BoundHead))
		case c.Sealed:
			note = "证据已封印（未绑定 commit）"
		case c.Failed:
			note = "上次 verify 没通过，证据留在 evidence/ 里，别删——删掉就只剩更早那次封印了"
		case !c.HasAcceptance:
			note = "缺少 acceptance.md"
		}
		if c.Fast {
			if note == "" {
				note = "fast（快速通道：只要求 proposal + acceptance）"
			} else {
				note = "fast；" + note
			}
		}
		fmt.Printf("%-32s %-10s %-8s %s\n", c.ID, c.Stage, evidenceCell(c), note)
	}
	// 归档目录名不合 <YYYYMMDD>-<id> 约定时 List 会跳过它。跳过可以，
	// 但要让人知道有东西被跳过了，否则等于悄悄丢记录。
	if bad, berr := change.InvalidArchiveNames(root); berr == nil && len(bad) > 0 {
		fmt.Println()
		fmt.Printf("⚠ spec/changes/archive/ 下有 %d 个目录不符合 <YYYYMMDD>-<id> 约定，未计入列表：%s\n",
			len(bad), strings.Join(bad, "、"))
	}
	return nil
}

// shortHead 把 commit 缩成 7 位；空值原样返回。
func shortHead(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
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

	// 先读一次状态：告诉人这是首次验证还是重跑，旧证据绑在哪个 commit 上。
	// verify 自己会再读一次并重算指纹——这里只用于展示，不参与判定。
	if prev, _, err := change.Load(root, id); err == nil && prev.EvidenceCount > 0 {
		switch {
		case prev.Sealed && prev.BoundHead != "":
			fmt.Printf("上次证据：%d 份（已封印 @%s）\n\n", prev.EvidenceCount, shortHead(prev.BoundHead))
		case prev.Stale:
			fmt.Printf("上次证据：%d 份（已失效，判据或工作区变过）\n\n", prev.EvidenceCount)
		default:
			fmt.Printf("上次证据：%d 份\n\n", prev.EvidenceCount)
		}
	} else {
		fmt.Println("上次证据：无")
		fmt.Println()
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

	if len(res.Manual) > 0 || len(res.ManualDone) > 0 {
		fmt.Println()
		if len(res.Manual) > 0 {
			fmt.Println("人工检查项（不计入封印，由人确认）：")
			for _, m := range res.Manual {
				fmt.Printf("  - %s\n", m)
			}
		}
		if len(res.ManualDone) > 0 {
			fmt.Println("已勾选的人工项（勾选不改判定，机器不会代你确认）：")
			for _, m := range res.ManualDone {
				fmt.Printf("  ✓ %s\n", m)
			}
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
	// 验收未通过是"门禁结论"，不是用法错误：走 errHardExit 哨兵 → exit 2。
	// 普通 error 会变成 exit 1，调用方（hook、脚本）容易把它当成"命令跑挂了"
	// 而 fail-open 放行——门禁必须用退出码把结论说清楚。
	return errHardExit
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

func runChangeStatus(cmd *cobra.Command, args []string) error {
	id := strings.TrimSpace(args[0])
	asJSON, _ := cmd.Flags().GetBool("json")
	root, err := changeRoot()
	if err != nil {
		return err
	}
	st, err := change.StatusOf(root, id)
	if err != nil {
		return err
	}
	if asJSON {
		return st.WriteJSON(os.Stdout)
	}
	fast := ""
	if st.Fast {
		fast = "  fast"
	}
	fmt.Printf("change: %s   stage: %s%s\n", st.ID, st.Stage, fast)
	switch st.LastVerdict {
	case "sealed":
		fmt.Println("验证：已封印")
	case "stale":
		fmt.Println("验证：证据已失效（判据或工作区变过）")
	case "failed":
		fmt.Println("验证：上次没通过")
	default:
		fmt.Println("验证：还没有记录")
	}
	if st.Attempts > 0 {
		fmt.Printf("整条验证记录：%d 次\n", st.Attempts)
	}
	if len(st.Nodes) > 0 {
		fmt.Println()
		fmt.Println("节点：")
		for _, n := range st.Nodes {
			// [x] 只表示人在 tasks.md 里勾了这条；gate 通过是 [✓]——
			// 机器能确认判据过了，确认不了"这条任务真的做完了"。
			// 证据优先于勾选：一条任务既勾了又跑过 gate，显示 [✓]——勾选只是
			// 进度记录，机器能确认的只有判据过没过。
			mark := "[ ]"
			switch {
			case n.State == change.NodePassed:
				mark = "[✓]"
			case n.State == change.NodeBlocked:
				mark = "[✗]"
			case n.State == change.NodeFailed:
				mark = "[!]"
			case n.Task.Done:
				mark = "[x]"
			}
			line := fmt.Sprintf("  %s %d. %s", mark, n.Task.Index, n.Task.Text)
			if n.State == change.NodeFailed || n.State == change.NodeBlocked {
				line += "（" + n.LastError + "）"
			}
			fmt.Println(line)
		}
	}
	fmt.Println()
	if st.Verdict == "blocked" {
		fmt.Println("⛔ 已停下：")
		if st.Blocked != "" {
			// 只说"停下了"而不说停在哪，人还得翻 JSON 才知道卡什么。
			fmt.Println("  " + st.Blocked)
		}
	} else {
		fmt.Println("下一步：")
	}
	for _, step := range st.NextSteps {
		fmt.Printf("  - %s\n", step)
	}
	if st.Verdict == "blocked" && st.Handoff != "" {
		// 恢复命令单独放在这里：它不是下一步，agent 不该照跑。
		fmt.Println()
		fmt.Println("人工处理：" + st.Handoff)
	}
	return nil
}

func runChangeGate(cmd *cobra.Command, args []string) error {
	id := strings.TrimSpace(args[0])
	if err := change.ValidID(id); err != nil {
		return err
	}
	taskNo, _ := cmd.Flags().GetInt("task")
	if taskNo <= 0 {
		return fmt.Errorf("change: 需要 --task <N>（tasks.md 里的任务序号，从 1 开始）")
	}
	timeout, _ := cmd.Flags().GetDuration("timeout")
	root, err := changeRoot()
	if err != nil {
		return err
	}
	res, err := change.GateTask(context.Background(), root, id, taskNo, change.GateOptions{Timeout: timeout})
	if err != nil {
		return err
	}
	fmt.Printf("gate %s --task %d\n\n", id, taskNo)
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
	relEv := res.EvidenceDir
	if r, rerr := filepath.Rel(root, res.EvidenceDir); rerr == nil {
		relEv = filepath.ToSlash(r)
	}
	fmt.Println()
	if res.Summary.Passed {
		fmt.Printf("✅ 节点通过，证据：%s\n", relEv)
		fmt.Printf("   节点证据不封印整条 change——整件事做完还要 forge change verify %s\n", id)
		return nil
	}
	fmt.Printf("❌ 节点未通过（%d 条判据失败），证据：%s\n", failed, relEv)
	if fc := firstFailure(res.Summary.Checks); fc != nil {
		fmt.Printf("   先修这一条：[%d] %s\n", fc.Index, firstLine(fc.Command))
	}
	return errHardExit
}
