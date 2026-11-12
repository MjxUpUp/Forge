package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/change"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/specroot"
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
	changeCmd.AddCommand(changeCheckCmd)

	changeInitCmd.Flags().String("profile", "mini", "门禁档位：mini / standard / full")
	changeNewCmd.Flags().String("title", "", "change 标题（缺省用 id）")
	changeListCmd.Flags().Bool("json", false, "JSON 格式输出")
	changeVerifyCmd.Flags().Duration("timeout", change.DefaultCheckTimeout, "单条验收命令的超时")
	changeNewCmd.Flags().Bool("fast", false, "快速通道：只写 proposal 与 acceptance（判据与证据照旧）")
	changeStatusCmd.Flags().Bool("json", false, "JSON 格式输出")
	changeGateCmd.Flags().Int("task", 0, "tasks.md 里的任务序号（从 1 开始）")
	changeGateCmd.Flags().Duration("timeout", change.DefaultCheckTimeout, "单条判据的超时")
	changeCheckCmd.Flags().Bool("staged", false, "只看暂存区（pre-commit 用；暂存区出现 spec/changes/ 之外的改动则退化为全量）")
	changeCheckCmd.Flags().Bool("json", false, "JSON 格式输出")
	changeAgentsCmd.Flags().String("pointers", "", "同时维护指向 AGENTS.md 的指针文件：claude / cursor / all / none（缺省只维护已存在的 CLAUDE.md；.cursor/rules/ 目录存在时补 forge.mdc）")
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
	Short: "在仓库里声明契约（spec/forge.yaml + spec/changes/）+ 同步 AGENTS.md 托管块与已存在的入口指针",
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
	Short: "同步 AGENTS.md 的 forge 托管块与 agent 入口指针（标记外不动）",
	Long: `同步 AGENTS.md 的 forge 托管块，并维护指向它的 agent 入口指针：

  CLAUDE.md                 Claude Code 读这个文件；块里是 @AGENTS.md 导入
  .cursor/rules/forge.mdc   Cursor 规则；块里让 agent 先读 AGENTS.md

指针文件不存在时默认不创建（替不用这个 agent 的仓库建文件是污染）：CLAUDE.md
不存在就不建；例外是 .cursor/rules/ 目录已存在时补 forge.mdc（说明仓库已在用
Cursor）。要凭空创建就点名：--pointers claude、--pointers cursor、--pointers
all；--pointers none 完全不动指针。

合并已有文件时：有托管块只改块内；没有块就在末尾追加，原有字节与行尾不动；
标记不成对或多对报错且不动文件。`,
	Args: cobra.NoArgs,
	RunE: runChangeAgents,
}

var changeCheckCmd = &cobra.Command{
	Use:   "check [--staged] [--json]",
	Short: "检查每个未归档 change 的证据是否成立（CI 与 pre-commit 的判据）",
	Long: `每个未归档 change 都必须有一份与当前代码和判据一致的封印证据，
否则退出码 2。这不是"检查有没有写文档"，是"别把没验证过的改动合进去"：

  forge change check            # 全部未归档 change（CI 用）
  forge change check --staged   # pre-commit 用：只看暂存区；出现代码等
                                # 其他改动时退化为全量（代码变了证据可能失效）`,
	Args: cobra.NoArgs,
	RunE: runChangeCheck,
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
	fmt.Println("本次改动（已就位）：")
	for _, p := range created {
		fmt.Printf("  + %s\n", p)
	}
	fmt.Println()
	fmt.Println("下一步：")
	fmt.Printf("  forge change new <id> --title \"...\"    # 新建一个 change\n")
	return nil
}

func runChangeAgents(cmd *cobra.Command, _ []string) error {
	root, err := changeRoot()
	if err != nil {
		return err
	}
	if !specroot.Declared(root) {
		return fmt.Errorf("change: %s 还没有 spec/forge.yaml——先跑 `forge change init` 声明契约，再同步 AGENTS.md", root)
	}
	pointers, _ := cmd.Flags().GetString("pointers")
	create, err := parsePointerNames(pointers)
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

	if pointers == "none" {
		return nil
	}
	reports, err := change.EnsureAgentPointers(root, create)
	if err != nil {
		return err
	}
	for _, r := range reports {
		if r.Skipped {
			fmt.Printf("  %s：不存在，未创建（要启用就加 --pointers %s）\n", r.File, pointerKind(r.File))
			continue
		}
		switch r.Action {
		case change.AgentsCreated:
			fmt.Printf("  %s：已新建指针（指向 AGENTS.md）\n", r.File)
		case change.AgentsAppended:
			fmt.Printf("  %s：已追加指针（原有内容未动）\n", r.File)
		case change.AgentsUpdated:
			fmt.Printf("  %s：已更新指针（标记外未动）\n", r.File)
		default:
			fmt.Printf("  %s：指针已是最新\n", r.File)
		}
	}
	return nil
}

// parsePointerNames 解析 --pointers：空表示"只维护已存在的文件"，all 表示两个
// 都创建，none 表示全都不碰。
func parsePointerNames(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "none" {
		return nil, nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		switch name := strings.TrimSpace(part); name {
		case "":
		case "all":
			return []string{"claude", "cursor"}, nil
		case "claude", "cursor":
			out = append(out, name)
		default:
			return nil, fmt.Errorf("change: 未知的指针 %q（可选 claude / cursor / all / none）", name)
		}
	}
	return out, nil
}

// displayCommand 把多行判据压成一行：状态列表是给人扫的，命令里的换行会把
// 后面的条目挤到下一行，看起来像输出的一部分。
func displayCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	lines := strings.Split(cmd, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	out := strings.Join(lines, " ; ")
	if runes := []rune(out); len(runes) > 160 {
		out = string(runes[:160]) + "…"
	}
	return out
}

// pointerKind 把文件名翻回 --pointers 的取值，提示里能直接抄。
func pointerKind(file string) string {
	if file == change.ClaudeFile {
		return "claude"
	}
	return "cursor"
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
	if len(st.PriorAttempts) > 0 {
		// 死路记忆放在判定之前：人先看到"已经试过什么、怎么失败的"，再看
		// 下一步该做什么，否则很容易照着同一句命令再跑一遍。
		fmt.Println()
		fmt.Println("此前失败的尝试（新→旧）：")
		for _, a := range st.PriorAttempts {
			line := "  - " + a.Evidence
			if a.Task > 0 {
				line += fmt.Sprintf("（任务 %d）", a.Task)
			}
			if a.Command != "" {
				line += "：" + displayCommand(a.Command) + fmt.Sprintf("（exit %d", a.Exit)
				if a.Shell != "" {
					line += "，" + a.Shell
				}
				line += "）"
			} else {
				line += "：" + a.Reason
			}
			fmt.Println(line)
			if a.StderrFile != "" {
				fmt.Printf("    stderr：evidence/%s/%s\n", a.Evidence, a.StderrFile)
			}
			if a.StdoutFile != "" {
				fmt.Printf("    stdout：evidence/%s/%s\n", a.Evidence, a.StdoutFile)
			}
		}
		fmt.Println("  返工前先读这些证据，别重试已证伪的路。")
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

func runChangeCheck(cmd *cobra.Command, _ []string) error {
	staged, _ := cmd.Flags().GetBool("staged")
	asJSON, _ := cmd.Flags().GetBool("json")
	root, err := changeRoot()
	if err != nil {
		return err
	}
	var only []string
	skipped, note := "", ""
	if staged {
		out, gerr := exec.Command("git", "-C", root, "diff", "--cached", "--name-only", "-z").Output()
		switch {
		case gerr != nil:
			// 不是 git 仓库（或没有 git）：pre-commit 本来就装不上，退化成全量。
			note = "拿不到暂存区清单（不是 git 仓库？）——改为检查全部 change"
		default:
			paths := splitNUL(string(out))
			repoRoot := root
			if top, terr := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel").Output(); terr == nil {
				if t := strings.TrimSpace(string(top)); t != "" {
					repoRoot = t
				}
			}
			changesRoot := change.ChangesDir(root)
			only = change.TouchedChanges(repoRoot, changesRoot, paths)
			if len(paths) == 0 {
				skipped = "暂存区是空的"
				only = nil
				break
			}
			if change.HasOutsideChanges(repoRoot, changesRoot, paths) {
				// 代码改了就可能在封印之后变了，只查被碰到的 change 会放行
				// 一个注定在 CI 变红的提交——退化为全量。
				note = "暂存区有 spec/changes/ 之外的改动（代码变了证据可能失效）——改为检查全部 change"
				only = nil
				break
			}
			if len(only) == 0 {
				skipped = "暂存区没有碰到 spec/changes/ 里的 change"
			}
		}
	}
	if skipped != "" {
		// 明确的"没东西可查"：pre-commit 不该按住提交，CI 也不该因此变红。
		return printChangeCheckSkipped(asJSON, skipped)
	}
	problems, checked, err := change.CheckProblems(root, only)
	if errors.Is(err, change.ErrNoSpec) {
		return printChangeCheckSkipped(asJSON, "spec/changes/ 还不存在——没有可检查的 change")
	}
	if err != nil {
		return err
	}
	if asJSON {
		// JSON 在所有路径上都是合法 JSON：跳过也输出 {"checked":[],"problems":[],"skipped":…}，
		// 消费者不该为了"没东西可查"再写一个分支。
		payload := struct {
			Checked  []string              `json:"checked"`
			Problems []change.CheckProblem `json:"problems"`
			Skipped  string                `json:"skipped"`
			Note     string                `json:"note,omitempty"`
		}{Checked: emptyIfNil(checked), Problems: emptyIfNil(problems), Skipped: skipped, Note: note}
		raw, merr := json.MarshalIndent(payload, "", "  ")
		if merr != nil {
			return merr
		}
		fmt.Println(string(raw))
		if len(problems) > 0 {
			return errHardExit
		}
		return nil
	}
	if note != "" {
		fmt.Println(note + "。")
	}
	if len(checked) == 0 {
		fmt.Println("没有未归档的 change——无需检查。")
		return nil
	}
	if len(problems) == 0 {
		fmt.Printf("✅ %d 个 change 的证据都成立：%s\n", len(checked), strings.Join(checked, ", "))
		return nil
	}
	fmt.Printf("❌ %d 个 change 的证据不成立（共检查 %d 个）：\n", len(problems), len(checked))
	for _, p := range problems {
		fmt.Printf("  - %s：%s\n", p.ID, p.Reason)
	}
	fmt.Println()
	fmt.Println("这是合并前的人为闸门，不是让你在循环里重试：先读证据，再决定修什么。")
	return errHardExit
}

// splitNUL 切分 git -z 输出；末尾的空段不算路径。
func splitNUL(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// emptyIfNil 让 JSON 里出现 [] 而不是 null：消费者少写一个分支。
func emptyIfNil[T any](in []T) []T {
	if in == nil {
		return []T{}
	}
	return in
}

// printChangeCheckSkipped：跳过不是失败（退出码 0），但 JSON 消费者也要能读。
func printChangeCheckSkipped(asJSON bool, reason string) error {
	if asJSON {
		payload := struct {
			Checked  []string              `json:"checked"`
			Problems []change.CheckProblem `json:"problems"`
			Skipped  string                `json:"skipped"`
		}{Checked: []string{}, Problems: []change.CheckProblem{}, Skipped: reason}
		raw, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	}
	fmt.Println(reason + "。")
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
