package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// task_continuity.go：task 升格为「接续真相源」的命令层。把会话内临时状态（agent 上下文，
// 压缩即丢）和靠纪律的 markdown（HANDOFF.md/AI_CONTEXT.md）替换为 task 的结构化一等公民字段 +
// forge task resume 秒级拉回。对应 session-continuity HANDOFF + cross-tool-context AI_CONTEXT 的
// 信息结构，但持久化进 DataDir/tasks/<ref>.json（refactor-data-home：runtime state 迁用户级），跨工具/跨人基于同一份记录接续。

var taskResumeCmd = &cobra.Command{
	Use:   "resume [--ref <ref>] [--json] [--no-attach]",
	Short: "拉回任务接续上下文（目标/计划/决策/下一步/阻塞/发现/产物 + 门禁进度 + git 已改未提交）",
	Long: `forge task resume 是接续真相源的入口：把 task 持久化的接续字段聚合成 HANDOFF 风格视图，
新会话冷启动一句"接手 FORGE-XXXX"即秒级拉回完整上下文——抗压缩丢失、跨工具/跨人接续。
默认自动把当前 session 锚定到 task（多向锚定的"接手方"动作）；--no-attach 仅查看不改 state。
context 命令是只读别名（等价 resume --no-attach）。`,
	RunE: runTaskResume,
}

var taskContextCmd = &cobra.Command{
	Use:   "context [--ref <ref>] [--json]",
	Short: "只读查看任务接续上下文（resume 的不改 state 别名）",
	RunE:  runTaskContext,
}

var taskDecideCmd = &cobra.Command{
	Use:   "decide --content <text> [--by <tool>] [--ref <ref>]",
	Short: "记录一条已确认决策（持久化，跨会话/跨工具不再推翻）",
	RunE:  runTaskDecide,
}

var taskNextCmd = &cobra.Command{
	Use:   "next <step> [<step>...]",
	Short: "追加下一步（可多条；HANDOFF 的下一步升格为结构化字段）",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runTaskNext,
}

var taskBlockCmd = &cobra.Command{
	Use:   "block --content <text> | --resolve <id> [--ref <ref>]",
	Short: "登记阻塞或解决阻塞（open→resolved）",
	RunE:  runTaskBlock,
}

var taskFindingCmd = &cobra.Command{
	Use:   "finding --content <text> [--source <tool>] [--evidence <text>] | --resolve <id> [--ref <ref>]",
	Short: "记录跨工具发现的问题/风险（带来源工具），或标 fixed",
	RunE:  runTaskFinding,
}

var taskAttachCmd = &cobra.Command{
	Use:   "attach --ref <ref> [--tool <tool>] [--session <sid>]",
	Short: "把一个 session+工具锚定到 task（跨工具接续的多向锚定）",
	RunE:  runTaskAttach,
}

func init() {
	taskCmd.AddCommand(taskResumeCmd)
	taskCmd.AddCommand(taskContextCmd)
	taskCmd.AddCommand(taskDecideCmd)
	taskCmd.AddCommand(taskNextCmd)
	taskCmd.AddCommand(taskBlockCmd)
	taskCmd.AddCommand(taskFindingCmd)
	taskCmd.AddCommand(taskAttachCmd)

	taskResumeCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")
	taskResumeCmd.Flags().Bool("json", false, "JSON 格式输出完整 task state")
	taskResumeCmd.Flags().Bool("no-attach", false, "仅查看，不把当前 session 锚定到 task")

	taskContextCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")
	taskContextCmd.Flags().Bool("json", false, "JSON 格式输出完整 task state")

	taskDecideCmd.Flags().String("content", "", "决策内容（新增时必填）")
	taskDecideCmd.Flags().String("by", "", "确认方（工具/人，默认探测当前工具）")
	taskDecideCmd.Flags().StringArray("affects", nil, "影响的文件/模块（可重复）")
	taskDecideCmd.Flags().String("rationale", "", "为什么这么决定（HANDOFF 纪律：写为什么不只写是什么）")
	taskDecideCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")

	taskNextCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")

	taskBlockCmd.Flags().String("content", "", "阻塞内容（新增时必填）")
	taskBlockCmd.Flags().String("resolve", "", "要解决的阻塞 ID（与 --content 互斥）")
	taskBlockCmd.Flags().String("resolution", "", "解决方式说明（--resolve 时填）")
	taskBlockCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")

	taskFindingCmd.Flags().String("content", "", "发现内容（新增时必填）")
	taskFindingCmd.Flags().String("source", "", "来源工具（默认探测当前工具）")
	taskFindingCmd.Flags().String("evidence", "", "证据（文件:行 / 命令输出）")
	taskFindingCmd.Flags().String("resolve", "", "要标 fixed 的发现 ID（与 --content 互斥）")
	taskFindingCmd.Flags().String("ref", "", "指定任务引用（不依赖分支检测）")

	taskAttachCmd.Flags().String("ref", "", "任务引用（必填：要锚定到哪个任务）")
	taskAttachCmd.Flags().String("tool", "", "该 session 所属工具（默认探测当前工具）")
	taskAttachCmd.Flags().String("session", "", "要锚定的 session ID（默认当前 session）")
}

// loadTaskOrActive 加载 --ref 指定或当前 active task，返回 (state, root)。无任务时返错误。
func loadTaskOrActive(cmd *cobra.Command) (*taskpipeline.TaskState, string, error) {
	explicitRef, _ := cmd.Flags().GetString("ref")
	root, err := findProjectRoot()
	if err != nil {
		return nil, "", err
	}
	var state *taskpipeline.TaskState
	if explicitRef != "" {
		state, err = taskpipeline.LoadTaskState(root, explicitRef)
		if err != nil {
			return nil, "", fmt.Errorf("加载任务 %q 失败: %w", explicitRef, err)
		}
	} else {
		state, err = taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
		if err != nil {
			return nil, "", fmt.Errorf("加载当前任务失败: %w", err)
		}
	}
	if state == nil {
		return nil, "", fmt.Errorf("无活跃任务（不在 feature 分支或未 task start）。用 --ref <ref> 指定，或先 forge task start")
	}
	return state, root, nil
}

func runTaskResume(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	noAttach, _ := cmd.Flags().GetBool("no-attach")
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}

	// 接手语义：resume 默认把当前 session 锚定到 task（多向锚定的接手方动作）。这样 N 个
	// session 共同推进一个 task 的关系被持久化，任意接手方 resume 即知谁参与过、用什么工具。
	// 探测失败（纯 shell 跑、无 agent env）时不锚定、仅 stderr 提示——resume 永远成功拉回
	// 上下文，锚定是附加动作：探测失败不能破坏 resume，也不能错误回退 OriginTool 归属。
	sid := taskpipeline.CurrentSessionID()
	if !noAttach {
		switch {
		case sid == "":
			fmt.Fprintln(os.Stderr, "[forge] 未探测到当前 session ID，已跳过锚定（接手方在 agent 内 resume 才自动锚定；或 forge task attach --session <sid> --tool <tool> 显式锚定）")
		case state.HasSession(sid):
			// 已锚定，无操作
		default:
			tool := detectOriginTool("")
			if tool == "" {
				fmt.Fprintf(os.Stderr, "[forge] 探测当前工具失败（无 agent env），已跳过锚定 session %s；跨工具接续请 forge task attach --ref %s --tool <tool>\n", sid, state.TaskRef)
			} else {
				state.AddSession(sid, tool)
				if err := taskpipeline.SaveTaskState(root, state); err != nil {
					return fmt.Errorf("锚定 session 失败: %w", err)
				}
				fmt.Fprintf(os.Stderr, "[forge] 已锚定当前 session %s（%s）到任务 %s\n", sid, tool, state.TaskRef)
			}
		}
	}

	if asJSON {
		out, _ := json.MarshalIndent(state, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Print(renderResume(state, gitPorcelain(root)))
	return nil
}

func runTaskContext(cmd *cobra.Command, args []string) error {
	// context = resume 的只读别名：拉回视图但不锚定 session、不改 state。
	asJSON, _ := cmd.Flags().GetBool("json")
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	if asJSON {
		out, _ := json.MarshalIndent(state, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Print(renderResume(state, gitPorcelain(root)))
	return nil
}

func runTaskDecide(cmd *cobra.Command, args []string) error {
	content, _ := cmd.Flags().GetString("content")
	if content == "" {
		return fmt.Errorf(`--content 必填（决策内容）。要解决阻塞请用 forge task block --resolve <id>，要标 fixed 发现请用 forge task finding --resolve <id>（decide 本身无 --resolve）`)
	}
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	by, _ := cmd.Flags().GetString("by")
	if by == "" {
		by = detectOriginTool("")
	}
	affects, _ := cmd.Flags().GetStringArray("affects")
	rationale, _ := cmd.Flags().GetString("rationale")
	state.AddDecision(taskpipeline.Decision{
		Content:   content,
		By:        by,
		Affects:   affects,
		Rationale: rationale,
	})
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	d := state.Decisions[len(state.Decisions)-1]
	fmt.Printf("✓ 决策已记 [%s]: %s\n", d.ID, content)
	return nil
}

func runTaskNext(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	for _, step := range args {
		state.AddNext(step)
	}
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	fmt.Printf("✓ 已追加 %d 条下一步（共 %d 条）\n", len(args), len(state.NextSteps))
	return nil
}

func runTaskBlock(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	if resolveID, _ := cmd.Flags().GetString("resolve"); resolveID != "" {
		resolution, _ := cmd.Flags().GetString("resolution")
		if !state.ResolveBlocker(resolveID, resolution) {
			return fmt.Errorf("未找到阻塞 ID %q（forge task resume 查看现有 ID）", resolveID)
		}
		if err := taskpipeline.SaveTaskState(root, state); err != nil {
			return fmt.Errorf("保存失败: %w", err)
		}
		fmt.Printf("✓ 阻塞 [%s] 已解决: %s\n", resolveID, resolution)
		return nil
	}
	content, _ := cmd.Flags().GetString("content")
	if content == "" {
		return fmt.Errorf("需要 --content <text> 新增阻塞，或 --resolve <id> 解决")
	}
	state.AddBlocker(taskpipeline.Blocker{Content: content})
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	b := state.Blockers[len(state.Blockers)-1]
	fmt.Printf("✓ 阻塞已登记 [%s]: %s\n", b.ID, content)
	return nil
}

func runTaskFinding(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	if resolveID, _ := cmd.Flags().GetString("resolve"); resolveID != "" {
		if !state.ResolveFinding(resolveID) {
			return fmt.Errorf("未找到发现 ID %q（forge task resume 查看现有 ID）", resolveID)
		}
		if err := taskpipeline.SaveTaskState(root, state); err != nil {
			return fmt.Errorf("保存失败: %w", err)
		}
		fmt.Printf("✓ 发现 [%s] 已标 fixed\n", resolveID)
		return nil
	}
	content, _ := cmd.Flags().GetString("content")
	if content == "" {
		return fmt.Errorf("需要 --content <text> 新增发现，或 --resolve <id> 标 fixed")
	}
	source, _ := cmd.Flags().GetString("source")
	if source == "" {
		source = detectOriginTool("")
	}
	evidence, _ := cmd.Flags().GetString("evidence")
	state.AddFinding(taskpipeline.Finding{
		Content:  content,
		Source:   source,
		Evidence: evidence,
	})
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	f := state.Findings[len(state.Findings)-1]
	fmt.Printf("✓ 发现已记 [%s] (%s): %s\n", f.ID, source, content)
	return nil
}

func runTaskAttach(cmd *cobra.Command, args []string) error {
	ref, _ := cmd.Flags().GetString("ref")
	if ref == "" {
		return fmt.Errorf("attach 需要 --ref <ref>（要锚定到哪个任务）")
	}
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	state, err := taskpipeline.LoadTaskState(root, ref)
	if err != nil {
		return fmt.Errorf("加载任务 %q 失败: %w", ref, err)
	}
	if state == nil {
		return fmt.Errorf("任务 %q 不存在", ref)
	}
	sid, _ := cmd.Flags().GetString("session")
	if sid == "" {
		sid = taskpipeline.CurrentSessionID()
	}
	if sid == "" {
		return fmt.Errorf("无法确定 session ID（未设 --session，且环境无当前 session）。显式传 --session <sid>")
	}
	tool, _ := cmd.Flags().GetString("tool")
	if tool == "" {
		tool = detectOriginTool("")
	}
	if tool == "" {
		return fmt.Errorf(`无法探测当前工具（无 agent env）。跨工具 attach 请显式传 --tool <tool>（如 pi/claude-code/opencode），避免把接手方 session 错误归属到创建方工具`)
	}
	already := state.HasSession(sid)
	state.AddSession(sid, tool)
	if err := taskpipeline.SaveTaskState(root, state); err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	if already {
		fmt.Printf("✓ session %s 已锚定（工具=%s），任务 %s 现有 %d 个参与 session\n", sid, tool, ref, len(state.SessionLinks))
	} else {
		fmt.Printf("✓ 已锚定 session %s（工具=%s）到任务 %s（共 %d 个参与 session）\n", sid, tool, ref, len(state.SessionLinks))
	}
	return nil
}

// gitPorcelain 返回 git status --porcelain 的行（已改未提交文件）。非 git 仓库或失败返 nil——
// resume 不依赖 git，仅作"接手方一眼看到工作区状态"的辅助。
func gitPorcelain(root string) []string {
	out, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// renderResume 把 task 接续字段渲染成 HANDOFF 风格视图。gitChanged 由 caller 传入（解耦 git，
// 使本函数可纯单测）。空接续内容时给最小状态卡，不报错——resume 永远成功，只是内容多寡。
func renderResume(state *taskpipeline.TaskState, gitChanged []string) string {
	var b strings.Builder
	w := func(s string) { b.WriteString(s + "\n") }

	w(strings.Repeat("═", 60))
	w("任务接续上下文: " + state.TaskRef)
	w(strings.Repeat("═", 60))
	kind := state.Kind
	if kind == "" {
		kind = "code"
	}
	tools := state.SessionTools()
	toolStr := "（无）"
	if len(tools) > 0 {
		toolStr = "[" + strings.Join(tools, "] [") + "]"
	}
	w(fmt.Sprintf("分支: %s   类型: %s   发起: %s", state.Branch, kind, orDash(state.OriginTool)))
	w(fmt.Sprintf("参与工具: %s", toolStr))
	w(fmt.Sprintf("门禁进度: %s", renderGateProgress(state)))
	if state.Summary != "" {
		w("标题: " + state.Summary)
	}
	if state.ParentTaskRef != "" {
		w("父任务: " + state.ParentTaskRef)
	}
	if len(state.DependsOn) > 0 {
		w("依赖: " + strings.Join(state.DependsOn, ", "))
	}
	w(strings.Repeat("─", 60))

	hasContent := state.HasContinuity()
	if state.Goal != "" {
		w("【目标】")
		w(indentBlock(state.Goal))
	}
	if state.Plan != "" {
		w("【计划】")
		w(indentBlock(state.Plan))
	}
	if len(state.Decisions) > 0 {
		w("【已确认决策】（不要推翻）")
		for _, d := range state.Decisions {
			line := fmt.Sprintf("  [%s] %s", d.ID, d.Content)
			if d.By != "" {
				line += "  — by " + d.By
			}
			w(line)
			if len(d.Affects) > 0 {
				w("        affects: " + strings.Join(d.Affects, ", "))
			}
			if d.Rationale != "" {
				w("        理由: " + d.Rationale)
			}
		}
	}
	if len(state.NextSteps) > 0 {
		w("【下一步】")
		for i, s := range state.NextSteps {
			w(fmt.Sprintf("  %d. %s", i+1, s))
		}
	}
	if len(state.Blockers) > 0 {
		w("【阻塞】")
		for _, bl := range state.Blockers {
			mark := "⚠️ "
			switch bl.Status {
			case "resolved":
				mark = "✓  "
			case "wontfix":
				mark = "⊘  "
			}
			w(fmt.Sprintf("%s[%s] %s: %s", mark, bl.ID, bl.Status, bl.Content))
			if bl.Resolution != "" {
				w("        解决: " + bl.Resolution)
			}
		}
	}
	if len(state.Findings) > 0 {
		w("【跨工具发现】")
		for _, f := range state.Findings {
			mark := "⚠️ "
			if f.Status == "fixed" {
				mark = "✓  "
			}
			w(fmt.Sprintf("%s[%s] %s %s: %s", mark, f.ID, f.Source, f.Status, f.Content))
			if f.Evidence != "" {
				w("        证据: " + f.Evidence)
			}
		}
	}
	if len(state.Artifacts) > 0 {
		w("【相关产物】")
		for _, a := range state.Artifacts {
			note := ""
			if a.Note != "" {
				note = "  — " + a.Note
			}
			w(fmt.Sprintf("  - %s: %s%s", a.Kind, a.Path, note))
		}
	}
	if !hasContent {
		w("（本任务尚无结构化接续字段。用 forge task decide/next/block/finding 补充）")
	}

	w(strings.Repeat("─", 60))
	if len(gitChanged) > 0 {
		w(fmt.Sprintf("git 已改未提交（%d）:", len(gitChanged)))
		for _, l := range gitChanged {
			w("  " + l)
		}
	} else {
		w("git 已改未提交: 无（工作区干净）")
	}
	w(strings.Repeat("═", 60))
	return stripUnsafeControl(b.String())
}

// renderGateProgress 渲染门禁进度（如 ✅实现 ✅验证 ⏳完成）。cli 包无法访问 taskpipeline
// 的私有 gatePassed，故用 History 自行判定——与 taskpipeline.gatePassed 同义。
func renderGateProgress(state *taskpipeline.TaskState) string {
	var parts []string
	for _, g := range taskpipeline.DefaultGates() {
		passed := false
		for _, r := range state.History {
			if r.Gate == g.ID && r.Passed {
				passed = true
				break
			}
		}
		if passed {
			parts = append(parts, "✅"+g.Name)
		} else if state.CurrentGate == g.ID {
			parts = append(parts, "🚦"+g.Name)
		} else {
			parts = append(parts, "⏳"+g.Name)
		}
	}
	return strings.Join(parts, " ")
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func indentBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}

// stripUnsafeControl 剥离 ANSI 转义序列和其他 C0 控制字符（保留 \n \t \r），防止 --plan-file
// 读入的外部 markdown 含恶意 ANSI（清屏 ESC[2J / 改色 ESC[31m 等）被终端解释执行。
// HTML 端 html/template 已自动转义，CLI 端对称补这层。剥离 ESC 后 ANSI 序列余下 [31m 类
// 可见文本——不再被终端解释，目的达成（残留文本无害）。也丢 DEL(0x7f) 和其他 C0 控制字符。
func stripUnsafeControl(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' || r == '\t' || r == '\r' {
			b.WriteRune(r)
			continue
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
