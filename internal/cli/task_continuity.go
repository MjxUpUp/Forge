package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/hostcap"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// task_continuity.go: the command layer where task becomes the "single source of
// continuity". It replaces in-session ephemeral state (agent context, lost on
// compaction) and discipline-driven markdown (HANDOFF.md/AI_CONTEXT.md) with structured
// first-class fields on task + a sub-second forge task resume. It mirrors the
// information shape of session-continuity HANDOFF + cross-tool-context AI_CONTEXT, but
// persists into the user-level DataDir/tasks/<ref>.json (refactor-data-home), so
// same-machine cross-tool/cross-person handoff works off a single record. Note the
// boundary: user-level state does not travel with the repo — cross-MACHINE handoff is
// out of scope here (a future task export/import would be the explicit vehicle).
//
// task_continuity.go：task 升格为「接续真相源」的命令层。把会话内临时状态（agent 上下文，
// 压缩即丢）和靠纪律的 markdown（HANDOFF.md/AI_CONTEXT.md）替换为 task 的结构化一等公民字段 +
// forge task resume 秒级拉回。对应 session-continuity HANDOFF + cross-tool-context AI_CONTEXT 的
// 信息结构，但持久化进用户级 DataDir/tasks/<ref>.json（refactor-data-home），同机跨工具/跨人
// 基于同一份记录接续。注意边界：用户级 state 不随仓库走——跨机器接续不在本层范围
// （未来的 task export/import 才是显式载体）。

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
	taskResumeCmd.Flags().Bool("hook", false, "SessionStart hook 模式：零未完成任务静默退出；有唯一活跃任务 attach 当前 session 并输出 PASS+接续视图；多任务歧义输出 PASS+候选清单（供 forge hook task-resume 调用）")
	taskResumeCmd.Flags().Bool("compact-flag", false, "PostCompact hook 模式：压缩完成时标记「刚压缩过」（有 session ID 写 per-session sentinel，无则置 ResumeStale；压缩根治层·设标志半边，gap#2；供 forge hook compact-resume 调用）")
	taskResumeCmd.Flags().Bool("reinject", false, "UserPromptSubmit hook 模式：若本 session 刚压缩（per-session sentinel 或 legacy ResumeStale）则重注入完整 handoff 并清标记（压缩根治层·重注入半边，gap#2；供 forge hook resume-reinject 调用）")

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

// loadTaskOrActive loads the task specified by --ref or the current active task, and
// returns (state, root). Returns an error when there is no task.
//
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
	hookMode, _ := cmd.Flags().GetBool("hook")
	compactFlag, _ := cmd.Flags().GetBool("compact-flag")
	reinject, _ := cmd.Flags().GetBool("reinject")

	if compactFlag {
		// PostCompact hook (compaction root-cause layer · set-flag half, gap#2): on
		// compaction completion, mark "just compacted" for this session (per-session
		// sentinel, or the legacy ResumeStale bool without a session ID); the next
		// UserPromptSubmit --reinject of this session detects it and re-injects the full
		// handoff. Silent exit 0 (no stdout): PostCompact cannot inject additionalContext
		// (Claude Code docs explicitly list it as a non-injection point — it can only
		// block or follow up), so this hook only does the follow-up mark. No active task
		// → idempotent no-op. Any failure degrades to stderr + exit 0 (advisory, does
		// not block).
		//
		// PostCompact hook（压缩根治层·设标志半边，gap#2）：压缩完成 → 为本 session
		// 标记「刚压缩过」（per-session sentinel；无 session ID 则置 legacy ResumeStale），
		// 本 session 下个 UserPromptSubmit 的 --reinject 检测到即重注入完整 handoff。
		// 静默 exit 0（无 stdout）：PostCompact 不能注入 additionalContext（Claude Code 文档
		// 明确不在注入点列表，只能 block 或 follow-up），故此 hook 只做 follow-up 设标记。
		// 无活跃任务 → 幂等不操作。任何失败降级 stderr + exit 0（advisory 不阻塞）。
		root, err := findProjectRoot()
		if err != nil {
			return nil
		}
		if err := renderHookCompactFlag(root); err != nil {
			fmt.Fprintf(os.Stderr, "[forge] compact-resume advisory hook failed: %v\n", err)
		}
		return nil
	}

	if reinject {
		// UserPromptSubmit hook (compaction root-cause layer · re-inject half, gap#2): if
		// this session was just compacted → re-inject the full handoff + clear the mark;
		// otherwise silent. UserPromptSubmit stdout enters context (Claude Code docs:
		// UserPromptSubmit is on the additionalContext injection-point list); runHook
		// wraps it as additionalContext, so the PASS+handoff protocol matches the
		// SessionStart task-resume one.
		//
		// UserPromptSubmit hook（压缩根治层·重注入半边，gap#2）：若本 session 刚压缩过
		// → 重注入完整 handoff + 清标记；否则静默。UserPromptSubmit 的 stdout
		// 进 context（Claude Code 文档：UserPromptSubmit 在 additionalContext 注入点列表），
		// runHook 包成 additionalContext 注入，故 PASS+handoff 协议同 SessionStart task-resume。
		root, err := findProjectRoot()
		if err != nil {
			return nil
		}
		out, err := renderHookReinject(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[forge] resume-reinject advisory hook failed: %v\n", err)
			return nil
		}
		fmt.Print(out) // 无活跃/未压缩 out="" 静默；有则 "PASS\n<handoff>"
		return nil
	}

	// hook mode (SessionStart auto-injection; bash thin wrapper `exec forge task resume --hook`):
	// unambiguous active task → attach the current session + emit "PASS\n<handoff>"; multiple
	// in flight with no context match → emit "PASS\n<inventory>"; zero incomplete tasks →
	// silent (no injection, no error). runHook's extractDetail strips the PASS prefix to get
	// detail, and detail is injected into additionalContext via the generic truncate fallback.
	// Every branch exits 0 — SessionStart never blocks because of resume.
	//
	// hook 模式（SessionStart 自动注入；bash thin wrapper `exec forge task resume --hook`）：
	// 无歧义活跃任务 → attach 当前 session + 输出 "PASS\n<handoff>"；多任务在进行且无
	// 上下文匹配 → 输出 "PASS\n<盘点清单>"；零未完成任务 → 静默（不注入、不报错）。
	// runHook 的 extractDetail 去掉 PASS 前缀取 detail，detail 经通用 truncate 兜底注入
	// additionalContext。任何分支都 exit 0——SessionStart 绝不因 resume 阻塞。
	if hookMode {
		root, err := findProjectRoot()
		if err != nil {
			return nil // 非 forge 项目（runHook 对非全局 hook 已 outputAllow exit，双保险）
		}
		out, err := renderHookResume(root)
		if err != nil {
			// advisory hook: any failure degrades to a stderr notice + exit 0, never
			// blocking SessionStart. err comes from attachCurrentSession's SaveTaskState
			// failure (extreme edge cases like disk failure); if we returned err, cobra
			// would exit 1, the bash wrapper would also exit 1, and runHook would take the
			// Decision:block branch — violating the "every branch exits 0" promise above.
			// Degrading instead of blocking is the correct semantics for an advisory hook.
			//
			// advisory hook：任何失败都降级到 stderr 提示 + exit 0，绝不阻塞 SessionStart。
			// err 来自 attachCurrentSession 的 SaveTaskState 失败（极端边界如磁盘故障）；若
			// return err，cobra exit 1，bash wrapper 也 exit 1，runHook 走 Decision:block 分支，
			// 违反上方承诺的任何分支都 exit 0。降级而非阻塞是 advisory hook 的正确语义。
			fmt.Fprintf(os.Stderr, "[forge] task-resume advisory hook failed: %v\n", err)
			return nil
		}
		fmt.Print(out) // 无活跃任务 out="" 静默；有则 "PASS\n<handoff>"
		return nil
	}

	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}

	// Auto-claim (design §3 "claim | worker task claim 或 resume 自动 | offered → claimed"): when the
	// resolved task is offered to the current agent, claim it implicitly — removing the manual
	// `task claim` step (the worker just resumes). Non-fatal: a TOCTOU race (another session claimed
	// between resolve and mutate) logs to stderr and renders without claiming; resume never fails
	// because of an auto-claim race. Suppressed under --json (JSON consumers stay clean). IsOfferedTo
	// already guards Assignment!=nil + Status==offered + Agent==agent, so state is claimable iff true.
	//
	// 自动认领（设计 §3「claim | worker task claim 或 resume 自动 | offered → claimed」）：解析出的
	// 任务若正 offered 给当前 agent，则隐式认领——省去手动 task claim（worker 直接 resume）。非致命：
	// TOCTOU 竞态（resolve 与 mutate 之间被别的 session 认领）只 stderr 提示并按未认领渲染；resume
	// 绝不因自动认领竞态失败。--json 下不输出提示（JSON 消费方保持干净）。IsOfferedTo 已守
	// Assignment!=nil + Status==offered + Agent==agent，故 state 可认领当且仅当它为 true。
	agent := resolveOriginTool(root, ``)
	if agent != `` && state.IsOfferedTo(agent) {
		if claimed, claimErr := tryAutoClaim(root, state.TaskRef, agent); claimed {
			if reloaded, e := taskpipeline.LoadTaskState(root, state.TaskRef); e == nil && reloaded != nil {
				state = reloaded // 拾取新的 ClaimedAt/Status 供 renderResume
			}
			if !asJSON {
				fmt.Fprintf(os.Stderr, `[forge] 已自动认领任务 %s（%s）`+realNewlineString, state.TaskRef, agent)
			}
			// 认领成功但 session 锚定失败：认领是持久的，锚定是副作用——镜像 runTaskClaim 的提示，
			// 免得用户在 session 未锚定时毫无信号。
			if claimErr != nil && !asJSON {
				fmt.Fprintf(os.Stderr, `[forge] session 锚定失败（不影响认领）: %v`+realNewlineString, claimErr)
			}
		} else if claimErr != nil {
			// 输掉 TOCTOU 竞态（别的 session 已认领）：重载使 renderResume 显示当前盘上状态，
			// 而非陈旧的 offered 快照。非致命——stderr 提示已告知用户自动认领未生效。
			if reloaded, e := taskpipeline.LoadTaskState(root, state.TaskRef); e == nil && reloaded != nil {
				state = reloaded
			}
			fmt.Fprintf(os.Stderr, `[forge] 自动认领未生效（不影响 resume）: %v`+realNewlineString, claimErr)
		}
	}

	// Handoff semantics: resume by default anchors the current session to the task (the
	// "takeover" action of multi-way anchoring). This persists the relationship of N
	// sessions collaborating on one task — any handoff-party resume knows who participated
	// and with which tool. On detection failure (pure shell, no agent env), do not anchor
	// and only print a stderr notice — resume always succeeds in pulling back context;
	// anchoring is a side action: a detection failure must not break resume or wrongly
	// attribute OriginTool ownership.
	//
	// 接手语义：resume 默认把当前 session 锚定到 task（多向锚定的接手方动作）。这样 N 个
	// session 共同推进一个 task 的关系被持久化，任意接手方 resume 即知谁参与过、用什么工具。
	// 探测失败（纯 shell 跑、无 agent env）时不锚定、仅 stderr 提示——resume 永远成功拉回
	// 上下文，锚定是附加动作：探测失败不能破坏 resume，也不能错误回退 OriginTool 归属。
	if !noAttach {
		if _, err := attachCurrentSession(state, root, false); err != nil {
			return err
		}
	}

	if asJSON {
		out, _ := json.MarshalIndent(state, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	fmt.Print(renderResume(state, gitPorcelain(root), workspaceContextLine(root, state.CrossRepoImpact)))
	return nil
}

// attachCurrentSession anchors the current session to the task (the "takeover" action of
// multi-way anchoring). Returns (attached, error): attached=true means a session link was
// actually added (first-time anchor); already-anchored or detection failure returns false
// without erroring. silent=true (hook mode) suppresses the stderr notice — hook stderr is
// invisible to the user, and attach is a silent side effect. Detection failure (no
// session ID / no agent env) is silently skipped: attach is an additional action and must
// not break resume.
//
// attachCurrentSession 把当前 session 锚定到 task（多向锚定的接手方动作）。返回 (attached, error)：
// attached=true 表示实际新增了 session 链接（首次锚定）；已锚定或探测失败返 false 不报错。
// silent=true（hook 模式）不输出 stderr 提示——hook 的 stderr 用户不可见，attach 是静默副作用。
// 探测失败（无 session ID / 无 agent env）静默跳过：attach 是附加动作，不能破坏 resume。
func attachCurrentSession(state *taskpipeline.TaskState, root string, silent bool) (bool, error) {
	sid := taskpipeline.CurrentSessionID()
	if sid == "" {
		if !silent {
			fmt.Fprintln(os.Stderr, "[forge] 未探测到当前 session ID，已跳过锚定（接手方在 agent 内 resume 才自动锚定；或 forge task attach --session <sid> --tool <tool> 显式锚定）")
		}
		return false, nil
	}
	if state.HasSession(sid) {
		return false, nil // 已锚定，无操作
	}
	tool := resolveOriginTool(root, "")
	if tool == "" {
		if !silent {
			fmt.Fprintf(os.Stderr, "[forge] 探测当前工具失败（无 agent env），已跳过锚定 session %s；跨工具接续请 forge task attach --ref %s --tool <tool>\n", sid, state.TaskRef)
		}
		return false, nil
	}
	// Mutate under the per-task lock with a fresh reload: the state passed in was loaded
	// before the lock and may be stale (concurrent attach/decide from another worktree).
	//
	// 在 per-task 锁内重载再改：传入的 state 是取锁前加载的，可能已过期（其他
	// worktree 的并发 attach/decide）。
	attached := false
	err := taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		if s.HasSession(sid) {
			return nil
		}
		s.AddSession(sid, tool)
		// A fresh attach = THIS machine picking up the work — claim the node lease so
		// the advisory tracks who is ACTUALLY working, not who created the task
		// (fail-open, advisory-only; sync-convergence.md §4).
		//
		// 新锚定 = 本机接手工作——认领节点租约，让 advisory 追踪真正在干活的机器
		// 而非任务创建者（fail-open、仅 advisory；sync-convergence.md §4）。
		taskpipeline.ClaimLeaseForCurrentNode(s)
		attached = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("锚定 session 失败: %w", err)
	}
	if attached && !silent {
		fmt.Fprintf(os.Stderr, "[forge] 已锚定当前 session %s（%s）到任务 %s\n", sid, tool, state.TaskRef)
	}
	return attached, nil
}

// renderHookResume produces the SessionStart hook-mode output. With an unambiguous
// active task it attaches the current session (silent) + returns "PASS\n<handoff>"
// (naming any other in-flight tasks in one line). Without one it falls back to
// renderTaskInventory: a compact candidate list when tasks are in flight, "" (silent)
// only when nothing is. The pure logic other than findProjectRoot is factored out so
// runTaskResume's hook branch and unit tests can share it (tests pass root directly,
// independent of cwd). Truncation is delegated to the runHook generic path
// (hook.go truncate(detail, maxAdditionalContextLen)).
//
// renderHookResume 产出 SessionStart hook 模式输出。有无歧义的活跃任务时 attach 当前
// session（silent）+ 返 "PASS\n<handoff>"（一行列出其余在进行任务）。无则兜底
// renderTaskInventory：有任务在进行给紧凑候选清单，零任务才返 ""（静默）。把
// findProjectRoot 之外的纯逻辑提出，供 runTaskResume 的 hook 分支与单元测试共用
// （测试直接传 root，不依赖 cwd）。截断交给 runHook 通用路径
// （hook.go truncate(detail, maxAdditionalContextLen)）。
func renderHookResume(root string) (string, error) {
	state, _ := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if state == nil {
		// No unambiguous active task (≥2 in flight, or branch doesn't match): fall back
		// to an inventory of ALL incomplete tasks. Interaction flow this serves: user
		// invokes agent → agent checks for in-flight tasks → continue one (user picks)
		// or start new. Previously this path was silent — step 2 of that flow broke
		// whenever multiple tasks were in flight: the agent never learned they existed.
		//
		// 无无歧义的活跃任务（≥2 个在进行，或分支不匹配）：兜底为全部未完成任务的
		// 盘点。服务的交互流程：用户唤起 agent → agent 检查有无进行中的任务 → 接续
		// 某个（用户选择）或开新任务。此前这条路径静默——多任务在进行时流程第 2 步
		// 就断了：agent 根本不知道有任务存在。
		return appendOfferedBlock(root, nil, renderTaskInventory(root)), nil
	}
	if _, err := attachCurrentSession(state, root, true); err != nil {
		return "", err
	}
	handoff := renderResume(state, gitPorcelain(root), workspaceContextLine(root, state.CrossRepoImpact))
	// The current task resumes automatically, but the handoff party should still know
	// the full in-flight set — one line naming the other incomplete tasks. The list is
	// capped (zombie accumulation would otherwise produce a giant line and push the
	// closing discipline line past runHook's tail-truncation), and the whole handoff
	// is re-stripped afterwards: TaskRef comes from user --ref input and renderResume's
	// strip ran BEFORE this insertion (code-review P1 — ANSI injection asymmetry vs
	// renderTaskInventory, which strips after building).
	//
	// 当前任务自动接续，但接手方仍应知道完整的在进行集合——一行列出其余未完成任务。
	// 列表带上限（僵尸任务堆积会产生巨长行，把收尾的纪律行顶过 runHook 的尾部截断），
	// 且拼接后对整个 handoff 重新 strip：TaskRef 来自用户 --ref 输入，renderResume 的
	// strip 在插入之前就跑了（code-review P1——与 renderTaskInventory 先构建后 strip
	// 不对称的 ANSI 注入）。
	if others := otherIncompleteTasks(root, state.TaskRef); len(others) > 0 {
		shown := others
		suffix := ""
		if len(shown) > inventoryListCap {
			shown = shown[:inventoryListCap]
			suffix = fmt.Sprintf(" 等 %d 个", len(others))
		}
		handoff = stripUnsafeControl(strings.Replace(handoff,
			"→ 接续纪律用 session-continuity skill",
			fmt.Sprintf("另有 %d 个未完成任务: %s（forge task resume --ref <ref> 可切换）\n→ 接续纪律用 session-continuity skill",
				len(others), strings.Join(shown, ", ")+suffix), 1))
	}
	return appendOfferedBlock(root, state, passPrefix+handoff), nil
}

// realNewlineString is a single newline expressed without a "\n" rune literal or a double-quoted
// source literal — Windows quote-corrosion turns ASCII " into CJK curly quotes in Go source, and
// the project rule forbids \n inside backtick raw strings (it is a literal backslash-n there).
// Built from the numeric byte value 10 so source stays ASCII-clean.
//
// realNewlineString 是单个换行，不用 \n rune 字面也不用双引号源字面——Windows 引号腐蚀会把 Go
// 源里的 ASCII " 转成 CJK 弯引号，且项目规则禁止反引号 raw string 内的 \n（那里是字面 backslash-n）。
// 用数值字节 10 构造，源码保持 ASCII 干净。
var realNewlineString = string([]byte{10})

// passPrefix is the SessionStart injection marker (runHook.extractDetail strips it to recover the
// detail payload). Built from realNewlineString for the same ASCII-clean reason (no "PASS\n" literal).
//
// passPrefix 是 SessionStart 注入标记（runHook.extractDetail 据此剥离取 detail 载荷）。用
// realNewlineString 构造，同属 ASCII 干净（无 "PASS\n" 字面）。
var passPrefix = `PASS` + realNewlineString

// tryAutoClaim attempts offered→claimed under the per-task lock, then anchors the current session to
// the task (the same sequence as runTaskClaim, minus its CLI surface). Returns (claimed, err):
// claimed=true iff the mutation actually flipped Status to claimed. A race (another session claimed
// first) surfaces as a Claim error → (false, err); the caller logs it and continues. SetActiveTaskRef
// mirrors runTaskClaim (unconditional, no conflict check — the worker invoked resume, so re-anchoring
// is intended).
//
// tryAutoClaim 在 per-task 锁下尝试 offered→claimed，再把当前 session 锚到 task（与 runTaskClaim
// 同序，只是无其 CLI 表面）。返回 (claimed, err)：claimed=true 仅当 mutation 确实把 Status 翻成
// claimed。竞态（别的 session 先认领）体现为 Claim 错误 → (false, err)，调用方记日志后继续。
// SetActiveTaskRef 镜像 runTaskClaim（无条件、无冲突检查——worker 主动 resume 故重新锚定是预期）。
func tryAutoClaim(root, ref, agent string) (bool, error) {
	claimed := false
	err := taskpipeline.MutateTaskState(root, ref, func(s *taskpipeline.TaskState) error {
		if e := s.Claim(agent); e != nil {
			return e // errClaimNotOffered / errClaimWrongAgent / errNoAssignment
		}
		claimed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	if sid := taskpipeline.CurrentSessionID(); sid != `` {
		if e := taskpipeline.SetActiveTaskRef(root, sid, ref); e != nil {
			return claimed, e
		}
	}
	return claimed, nil
}

// appendOfferedBlock appends the additive "待认领" notification (design §8 ②) to the existing
// SessionStart output. Additive: it never alters the existing handoff/inventory, only appends —
// overlap with the inventory is acceptable (the user explicitly chose this over precedence). No-op
// (returns out unchanged) when: the agent is unknown (codex/cursor/opencode/codebuddy attribution
// gap — agent-neutral); no offered-to-me tasks; offered-zombies (excluded — a >7d-stale offer is
// exactly the noise this push avoids; zombies surface via task mine/dashboard); the active task
// itself (in the handoff branch it can be an offered-to-me task resolved via branch/single-incomplete
// — listing it as "待认领" while also handing it off would be contradictory); or nothing freshly
// notifiable after NotifiedAt dedup. NotifiedAt is set to now via MutateTaskState ONLY on actual emit
// (gated by a non-empty fresh set + non-empty block); the in-lock Status==AssignOffered guard
// prevents clobbering a concurrent claim/abandon.
//
// appendOfferedBlock 把「待认领」通知（设计 §8 ②）附加到现有 SessionStart 输出之后。additive：
// 绝不改动现有 handoff/盘点，只追加——与盘点的重叠可接受（用户显式选择此方案而非优先级）。下列情况
// 返 out 不变（no-op）：agent 未知（codex/cursor/opencode/codebuddy 归因缺口——agent-neutral）；无
// offered 给我的任务；offered 僵尸（排除——>7d 的陈旧 offer 正是本推送要避免的噪声，僵尸经 task
// mine/看板上浮）；活跃任务自身（handoff 分支里它可能是个经 branch/单任务解析出的 offered 给我的
// 任务——一边 handoff 一边又列「待认领」会自相矛盾）；或 NotifiedAt 去重后无可新鲜推送的。
// NotifiedAt 仅在实际推送时（非空 fresh 集 + 非空 block）经 MutateTaskState 设为 now；锁内
// Status==AssignOffered 守卫防覆盖并发的 claim/abandon。
func appendOfferedBlock(root string, active *taskpipeline.TaskState, out string) string {
	agent := resolveOriginTool(root, ``)
	if agent == `` {
		return out
	}
	all, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		return out
	}
	now := time.Now()
	activeRef := ``
	if active != nil {
		activeRef = active.TaskRef
	}
	var fresh []*taskpipeline.TaskState
	for _, s := range all {
		if s == nil || !s.IsOfferedTo(agent) {
			continue
		}
		if s.TaskRef == activeRef {
			continue // 活跃任务已 handoff，不重复列待认领
		}
		if z, _ := taskpipeline.IsOfferedZombie(s, now); z {
			continue // 僵尸 offered 任务不推送（避免轰炸，僵尸经 mine/看板上浮）
		}
		if s.Assignment.ShouldNotify(now) {
			fresh = append(fresh, s)
		}
	}
	if len(fresh) == 0 {
		return out
	}
	block := renderOfferedBlock(active, fresh, root)
	if block == `` {
		return out
	}
	// 仅在实际推送时落 NotifiedAt（锁内重载 + Status 守卫防并发覆盖）。
	for _, s := range fresh {
		ref := s.TaskRef
		_ = taskpipeline.MutateTaskState(root, ref, func(st *taskpipeline.TaskState) error {
			if st.Assignment != nil && st.Assignment.Status == taskpipeline.AssignOffered {
				t := now
				st.Assignment.NotifiedAt = &t
			}
			return nil
		})
	}
	trimmed := strings.TrimRight(out, realNewlineString)
	if trimmed == `` {
		return passPrefix + block
	}
	return trimmed + realNewlineString + block
}

// renderOfferedBlock renders the tiered "待认领" block (design §8 ②). When the active task is part of
// an orchestration chain (has a ParentTaskRef), same-chain offered siblings are listed with a per-line
// readiness marker (✅可开干 / ⏳阻塞中, ready-first then ref-ascending); any non-sibling offered
// tasks collapse to a count. Otherwise (no active, or active not in a chain) the whole set collapses
// to a one-line count. Readiness reuses PendingDependencies (the same primitive task mine --blocked
// uses), so push and gate cannot disagree. Output is ANSI-stripped (refs/summaries are user-controlled).
//
// renderOfferedBlock 渲染分档「待认领」块（设计 §8 ②）。活跃任务属编排链（有 ParentTaskRef）时，
// 同链 offered 兄弟逐行列出并带就绪标记（✅可开干 / ⏳阻塞中，就绪优先再按 ref 升序）；非同链的
// offered 折叠成计数。否则（无活跃，或活跃不在链中）整集折叠成一行计数。就绪复用
// PendingDependencies（task mine --blocked 同原语），使推送与门禁不会不一致。输出经 ANSI 剥离
// （ref/标题为用户可控）。
func renderOfferedBlock(active *taskpipeline.TaskState, offered []*taskpipeline.TaskState, root string) string {
	siblings := offeredChainSiblings(active, offered)
	var others []*taskpipeline.TaskState
	if len(siblings) > 0 {
		seen := make(map[string]bool, len(siblings))
		for _, s := range siblings {
			seen[s.TaskRef] = true
		}
		for _, s := range offered {
			if !seen[s.TaskRef] {
				others = append(others, s)
			}
		}
	} else {
		others = offered
	}
	// 预算就绪一次（PendingDependencies 每调一次都做磁盘 I/O），让排序比较器与逐行标记共用一次
	// 查询，而非 O(n log n) 次冗余 load。
	readyMap := make(map[string]bool, len(siblings))
	for _, s := range siblings {
		readyMap[s.TaskRef] = len(taskpipeline.PendingDependencies(root, s.DependsOn)) == 0
	}
	ready := func(s *taskpipeline.TaskState) bool { return readyMap[s.TaskRef] }
	var b strings.Builder
	w := func(s string) { b.WriteString(s); b.WriteString(realNewlineString) }
	if len(siblings) > 0 {
		sort.SliceStable(siblings, func(i, j int) bool {
			ri, rj := ready(siblings[i]), ready(siblings[j])
			if ri != rj {
				return ri // 就绪优先
			}
			return siblings[i].TaskRef < siblings[j].TaskRef
		})
		w(`【同链待认领（按就绪序）】`)
		for _, s := range siblings {
			mark := `⏳阻塞中`
			if ready(s) {
				mark = `✅可开干`
			}
			line := `  ` + mark + `  ` + s.TaskRef
			if s.Summary != `` {
				line += ` — ` + truncateRunes(s.Summary, inventoryFieldCap)
			}
			w(line)
		}
		if len(others) > 0 {
			w(fmt.Sprintf(`另有 %d 个非同链待认领（forge task mine 查看）`, len(others)))
		}
		w(`→ forge task resume --ref <ref> 接手同链任务，或 forge task mine 查看全部`)
	} else {
		w(fmt.Sprintf(`本 project 有 %d 个待认领任务（forge task mine 查看，或 forge task resume --ref <ref> 接手）`, len(others)))
	}
	return strings.TrimRight(stripUnsafeControl(b.String()), realNewlineString)
}

// offeredChainSiblings returns the subset of `offered` whose ParentTaskRef equals active.ParentTaskRef
// (v1 chain definition: exact-string match — the canonical fan-out shape where one orchestrator
// delegates N children each sharing the parent ref). Returns nil when active is nil or has no parent
// (the "active IS the orchestrator" case falls to the one-liner — orchestrators use task mine/
// dashboard; the SessionStart push is worker-facing). Walk-up to a root orchestrator and
// inter-sibling DependsOn topo are deferred to v2.
//
// offeredChainSiblings 返回 `offered` 中 ParentTaskRef 等于 active.ParentTaskRef 的子集（v1 链定义：
// 精确串匹配——一个编排器派 N 个子任务、各子共享父 ref 的典型 fan-out）。active 为 nil 或无父时
// 返 nil（「active 即编排器」回落到一行式——编排器用 task mine/看板；SessionStart 推送面向 worker）。
// 上溯到根编排器与兄弟间 DependsOn 拓扑推迟到 v2。
func offeredChainSiblings(active *taskpipeline.TaskState, offered []*taskpipeline.TaskState) []*taskpipeline.TaskState {
	if active == nil || active.ParentTaskRef == `` {
		return nil
	}
	var out []*taskpipeline.TaskState
	for _, o := range offered {
		if o == nil {
			continue
		}
		if o.ParentTaskRef == active.ParentTaskRef && o.TaskRef != active.TaskRef {
			out = append(out, o)
		}
	}
	return out
}

// inventoryListCap bounds how many tasks the SessionStart inventory lists — the output
// is injected into context every session start, so it must stay compact.
//
// inventoryListCap 限定 SessionStart 盘点列出的任务数——输出每次会话启动都进
// 上下文，必须保持紧凑。
const inventoryListCap = 8

// inventoryFieldCap bounds the per-field length (summary/next-step) inside one inventory
// line. runHook tail-truncates at 9500 chars, so unbounded fields could push the closing
// AskUserQuestion instruction — the whole point of the inventory — past the cut.
//
// inventoryFieldCap 限定盘点单行内字段（标题/下一步）的长度。runHook 从尾部截断
// （9500 字符），字段不设界会把末尾的 AskUserQuestion 引导行——盘点的存在意义——
// 先切掉。
const inventoryFieldCap = 60

// truncateRunes shortens s to at most n runes, appending an ellipsis when cut.
//
// truncateRunes 把 s 截到最多 n 个 rune，被截时追加省略号。
func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// otherIncompleteTasks returns the refs of incomplete tasks other than currentRef.
//
// otherIncompleteTasks 返回 currentRef 之外其他未完成任务的 ref。
func otherIncompleteTasks(root, currentRef string) []string {
	all, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		return nil
	}
	var refs []string
	for _, s := range all {
		if s.CompletedAt == nil && s.TaskRef != currentRef {
			refs = append(refs, s.TaskRef)
		}
	}
	return refs
}

// renderTaskInventory renders a compact inventory of all incomplete tasks for the
// SessionStart hook's ambiguous case: no unambiguous active task, but work is in
// flight. Returns "" (silent) only when nothing is in flight — the clean new-task
// path. The closing instruction tells the agent to use its own structured-question
// tool (AskUserQuestion) to let the user pick: hooks cannot do interactive HITL
// (verified: kimi has no ask channel, SessionStart is observation-only on every host),
// so the agent is the interaction layer.
//
// renderTaskInventory 为 SessionStart hook 的歧义场景渲染全部未完成任务的紧凑盘点：
// 无无歧义的活跃任务但有工作在进行。仅当零任务在进行时返 ""（静默）——干净的
// 新任务路径。末尾指示告诉 agent 用它自己的结构化提问工具（AskUserQuestion）让
// 用户选择：hook 做不了交互式 HITL（已验证：kimi 无 ask 通道，SessionStart 在所有
// host 上都是 observation-only），故 agent 是交互层。
func renderTaskInventory(root string) string {
	all, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		return ""
	}
	var open []*taskpipeline.TaskState
	for _, s := range all {
		if s.CompletedAt == nil {
			open = append(open, s)
		}
	}
	if len(open) == 0 {
		return ""
	}
	var b strings.Builder
	w := func(s string) { b.WriteString(s + "\n") }
	w(fmt.Sprintf("检测到 %d 个未完成任务（无当前上下文匹配，需选择接续或开新任务）：", len(open)))
	shown := open
	if len(shown) > inventoryListCap {
		shown = shown[:inventoryListCap]
	}
	for i, s := range shown {
		line := fmt.Sprintf("  %d. %s", i+1, s.TaskRef)
		if s.Branch != "" && s.Branch != s.TaskRef {
			line += " [分支 " + s.Branch + "]"
		}
		if s.Summary != "" {
			line += " — " + truncateRunes(s.Summary, inventoryFieldCap)
		}
		line += " — 门禁 " + renderGateProgress(s)
		if len(s.NextSteps) > 0 {
			line += " — 下一步: " + truncateRunes(s.NextSteps[0], inventoryFieldCap)
		}
		w(line)
	}
	if len(open) > inventoryListCap {
		w(fmt.Sprintf("  …还有 %d 个（forge task list 查看全部）", len(open)-inventoryListCap))
	}
	w("→ 用结构化提问工具（AskUserQuestion）让用户选择：接续某个任务（forge task resume --ref <ref>），或开新任务（forge task start）。")
	return "PASS\n" + stripUnsafeControl(strings.TrimRight(b.String(), "\n"))
}

// renderHookCompactFlag produces the PostCompact hook side effect: marks "just compacted"
// for the current session. With a session ID (all hook-driven hosts — runHook injects
// FORGE_SESSION_ID) it writes a per-session sentinel file (taskpipeline.MarkResumeStale),
// leaving the shared task json untouched; without one (legacy/manual) it falls back to
// setting the task-scoped ResumeStale bool. No active task → idempotent no-op, no error.
// This is the "set-flag" half of the compaction root-cause layer (gap#2) — PostCompact
// cannot inject context, so it only marks "just compacted" and waits for the next
// UserPromptSubmit's renderHookReinject to re-inject. The pure logic other than
// findProjectRoot is factored out so unit tests can pass root directly.
//
// renderHookCompactFlag 产出 PostCompact hook 的副作用：为当前 session 标记「刚压缩过」。
// 有 session ID（所有 hook 驱动的 host——runHook 注入 FORGE_SESSION_ID）时写
// per-session sentinel 文件（taskpipeline.MarkResumeStale），不动共享的 task json；
// 无 session ID（legacy/手动）时回落到置 task-scoped 的 ResumeStale bool。无活跃任务
// → 幂等不操作不报错。这是压缩根治层（gap#2）的「设标志」半边——PostCompact 不能注入
// context，只标记「刚压缩过」，等下个 UserPromptSubmit 的 renderHookReinject 重注入。
// 把 findProjectRoot 之外的纯逻辑提出，供单测直接传 root。
func renderHookCompactFlag(root string) error {
	state, _ := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if state == nil {
		return nil
	}
	if sid := taskpipeline.CurrentSessionID(); sid != "" {
		// 幂等：sentinel 已存在时 AtomicWrite 覆写同一路径，无副作用
		return taskpipeline.MarkResumeStale(root, sid)
	}
	// Legacy 回落（无 session ID）：task-scoped bool，两 session 共享 task 时可能互相
	// 消费（见 types.go ResumeStale 注释），仅 hook 外手动场景才走到。
	if state.ResumeStale {
		return nil
	}
	return taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		s.ResumeStale = true
		return nil
	})
}

// renderHookReinject produces the UserPromptSubmit hook-mode output: if the current
// session was just compacted → return "PASS\n<handoff>" and clear the mark; otherwise
// return "" (silent, no injection). "Just compacted" is judged per session: the
// per-session sentinel (taskpipeline.ConsumeResumeStale clears it on read), or the
// legacy task-scoped ResumeStale bool (cleared + persisted here) for sessions without
// an ID and for tasks marked by older binaries. This is the "re-inject" half of the
// compaction root-cause layer (gap#2) — the first user prompt after a compaction
// auto-restores the full continuity context, without relying on the agent to call
// `forge task resume` proactively. Consuming the mark guarantees only one re-injection
// (the next prompt is silent), and per-session sentinels mean one session's prompt
// never consumes another session's mark.
//
// renderHookReinject 产出 UserPromptSubmit hook 模式输出：若当前 session 刚压缩过
// → 返 "PASS\n<handoff>" 并清标记；否则返 ""（静默，不注入）。「刚压缩过」按
// session 判定：per-session sentinel（taskpipeline.ConsumeResumeStale 读即清），或
// legacy 的 task-scoped ResumeStale bool（此处清零并持久化）——后者服务无 session ID
// 的 session 和被旧版 binary 标记过的 task。这是压缩根治层（gap#2）的「重注入」
// 半边——压缩后第一个 user prompt 自动恢复完整接续上下文，不靠 agent 主动
// forge task resume。消费标记保证只重注入一次（下个 prompt 静默），per-session
// sentinel 保证一个 session 的 prompt 不会消费掉别的 session 的标记。
func renderHookReinject(root string) (string, error) {
	sid := taskpipeline.CurrentSessionID()
	state, _ := taskpipeline.ActiveTaskState(root, sid)
	if state == nil {
		// No active task: still consume any orphaned mark of this session so it cannot
		// mis-fire later under a different task.
		//
		// 无活跃任务：仍消费本 session 可能残留的标记，避免日后在别的 task 下误触发。
		taskpipeline.ConsumeResumeStale(root, sid)
		return "", nil
	}
	stale := false
	if state.ResumeStale {
		// Legacy task-scoped mark (no-session fallback or written by an older binary):
		// honor it once and clear it. Cleared FIRST, before consuming the sentinel: if the
		// clear fails (lock timeout/disk error) we return with the sentinel UNTOUCHED, so
		// the next prompt retries the re-injection — the error path must err toward
		// injecting twice, never toward losing the one recovery (code-review P1).
		//
		// Legacy 的 task-scoped 标记（无 session 回落或旧版 binary 所留）：兑现一次并清零。
		// 先清零、后消费 sentinel：清零失败（锁超时/磁盘错误）时 sentinel 原样保留返回，
		// 下个 prompt 重试重注入——错误路径宁可多注一次，绝不丢掉唯一的一次恢复
		// （code-review P1）。
		if err := taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
			s.ResumeStale = false
			return nil
		}); err != nil {
			return "", err
		}
		stale = true
	}
	if taskpipeline.ConsumeResumeStale(root, sid) {
		stale = true
	}
	if !stale {
		// P3 cold-start backfill: hosts that drop SessionStart hook output (kimi 0.35.0 —
		// SessionStart is observation-only there, verified via wire.jsonl) never receive the
		// cold-start task handoff that SessionStart's renderHookResume produces. The
		// UserPromptSubmit channel is the one inject channel those hosts DO reach, so we
		// backfill the active-task handoff here on the first prompt of such a session. Gated
		// by sessionStartOutputDropped(agent) and deduped by a per-session sentinel — one
		// session needs its cold-start handoff exactly once. Silent on Claude Code / codex /
		// ... (they inject SessionStart output, so they already got the handoff; backfilling
		// would duplicate it) and silent on kimi after the first prompt (sentinel set).
		//
		// Known gap (pre-existing, NOT introduced by P3): the offered-tasks ("待认领") block
		// is NOT recovered here. appendOfferedBlock advances NotifiedAt during SessionStart
		// regardless of whether its stdout reaches the model, so by the time this backfill
		// runs the freshly-offered tasks read as already-notified (ShouldNotify=false) and
		// the block is omitted. SessionStart already lost it on kimi; P3 recovers the active
		// -task handoff (the primary value) but leaves the offered-block loss in place.
		// Properly fixing it means gating NotifiedAt on actual delivery inside the shared
		// appendOfferedBlock (used by SessionStart on ALL hosts) — a separate, larger change.
		//
		// 已知缺口（既有，非 P3 引入）：offered-tasks（"待认领"）块在此不恢复。
		// appendOfferedBlock 在 SessionStart 期间无论 stdout 是否触达模型都推进 NotifiedAt，
		// 故本回填跑时刚 offered 的任务读为已通知（ShouldNotify=false），块被省略。SessionStart
		// 在 kimi 上本就丢了它；P3 恢复活跃任务 handoff（主价值）但保留 offered 块丢失现状。
		// 彻底修复需在共享的 appendOfferedBlock（SessionStart 在所有 host 上用）内部把
		// NotifiedAt 门控在实际送达上——是独立的更大改动。
		//
		// P3 冷启动回填：丢弃 SessionStart hook 输出的 host（kimi 0.35.0——SessionStart 在那里
		// 是 observation-only，经 wire.jsonl 核实）永远收不到 SessionStart 的 renderHookResume
		// 产出的冷启动 task handoff。UserPromptSubmit 是这些 host 唯一能触达的注入通道，故在此——
		// 这类 session 的首个 prompt——回填同一份 handoff。门控为 sessionStartOutputDropped(agent)
		// 且用 per-session sentinel 去重——一个 session 恰需一次冷启动 handoff。对 Claude Code/
		// codex/... 静默（它们注入 SessionStart 输出，已拿到 handoff，回填会重复），对 kimi 首个
		// prompt 之后也静默（sentinel 已设）。
		agent := resolveOriginTool(root, "")
		if sid != "" && sessionStartOutputDropped(agent) && !taskpipeline.IsColdStartInjected(root, sid) {
			// Render FIRST, mark the sentinel only on success: renderHookResume's only error
			// source is attachCurrentSession→MutateTaskState under per-task lock timeout / disk
			// error. Leaving the sentinel unset on a failed render lets the next prompt retry
			// the backfill; marking first would persist the sentinel on a failed render and
			// permanently suppress the handoff for the whole session (code-review MEDIUM-1).
			// The mark itself is best-effort (error ignored): a mark failure only risks one
			// duplicate handoff next prompt, never a lost one — mirroring the compact-reinject
			// path's emit-then-best-effort-mark.
			//
			// 先渲染，仅成功才设 sentinel：renderHookResume 唯一错误源是
			// attachCurrentSession→MutateTaskState 在 per-task 锁超时/磁盘错误下。渲染失败时不设
			// sentinel 使下个 prompt 重试回填；先标记会在渲染失败时持久化 sentinel 并永久抑制本
			// session 的 handoff（code-review MEDIUM-1）。标记本身 best-effort（忽略错误）：失败只
			// 冒下一次重复 handoff 的风险，绝不丢——镜像 compact-reinject 路径的「先发后尽力标记」。
			out, err := renderHookResume(root)
			if err != nil {
				return "", err
			}
			_ = taskpipeline.MarkColdStartInjected(root, sid)
			return out, nil
		}
		return "", nil
	}
	handoff := renderResume(state, gitPorcelain(root), workspaceContextLine(root, state.CrossRepoImpact))
	// Plan 4 (mid-way checkpoint explicit persist · active driving): right after a
	// compaction, if the task has not persisted any "mid-way thread" (decision/next
	// step), the handoff can only restore the goal/plan set at task start — and that
	// mid-way working memory is exactly what compaction loses. So we append a strong
	// nudge pushing the agent toward forge task decide/next to persist explicitly, so
	// the next compaction's handoff no longer rebuilds from zero. This is the only
	// high-signal moment in checkpoint-driven mode: a normal turn's resume-reinject is
	// silent (ResumeStale=false), zero noise; only the one prompt where compaction
	// happened triggers it. Goal/Plan do not count (already persisted at task start,
	// not a compaction-loss item) — only Decisions/NextSteps, the two mid-way thread
	// fields.
	//
	// 方案4（中途 checkpoint 显式落盘·主动驱动）：压缩刚发生时，若任务未落盘任何「中途线程」
	// （决策/下一步），handoff 只能复原 task start 时设的 goal/plan——而压缩丢的正是这期间的
	// 工作记忆。此时追加强提示把 agent 推向 forge task decide/next 显式落盘，使下次压缩的
	// handoff 不再从零重建。这是 checkpoint 驱动唯一的高信号时机：普通 turn resume-reinject
	// 静默（ResumeStale=false），零噪声；只在压缩发生的那一个 prompt 触发。Goal/Plan 不计入
	// （task start 已落盘，非压缩丢失项），只看 Decisions/NextSteps 这两个中途线程字段。
	if len(state.Decisions) == 0 && len(state.NextSteps) == 0 {
		handoff += `
⚠ 刚发生 context 压缩，但本任务尚未落盘任何中途决策/下一步——压缩丢的正是这段工作记忆，下次压缩仍会从零重建。现在立即显式落盘：
  forge task decide --content "<已确认的关键决策>"   # 不再推翻的决定
  forge task next "<当前在做>" "<接着做>"             # 下一步
  forge task block --content "<卡住的事>"             # 若有阻塞
`
	}
	// The compact-reinject just delivered a full handoff — that satisfies cold-start too.
	// Mark the cold-start sentinel so the next prompt (stale now consumed) does not ALSO
	// backfill via the cold-start path and double-inject. Best-effort: a mark failure only
	// risks one duplicate handoff, not data loss.
	//
	// compact-reinject 刚交付了完整 handoff——这也满足冷启动。设 cold-start sentinel 使下个
	// prompt（stale 已消费）不再经冷启动路径回填造成双注。best-effort：标记失败只冒一次重复
	// handoff 的风险，无数据丢失。
	_ = taskpipeline.MarkColdStartInjected(root, sid)
	return "PASS\n" + handoff, nil
}

// sessionStartOutputDropped reports whether the given host agent drops SessionStart hook
// stdout from the model context — making the SessionStart task-resume handoff never reach
// the model. Such hosts need the handoff backfilled on the UserPromptSubmit channel (the
// one inject channel they DO reach). Verified cases: kimi 0.35.0 (SessionStart
// observation-only, confirmed by wire.jsonl cross-check: 0 forge texts reached the model
// despite 42 edits + 39 Bash calls; only UserPromptSubmit stdout reaches it, laggy).
// Claude Code / codex / cursor / windsurf / opencode / pi / reasonix inject SessionStart
// output, so they do NOT need backfill and must stay excluded — backfilling there would
// duplicate the handoff SessionStart already delivered.
//
// sessionStartOutputDropped 报告给定 host agent 是否把 SessionStart hook stdout 丢弃出模型
// 上下文——使 SessionStart task-resume 的 handoff 到不了模型。这类 host 需在 UserPromptSubmit
// 通道（它们唯一能触达的注入通道）回填 handoff。已核实案例：kimi 0.35.0（SessionStart 为
// observation-only，经 wire.jsonl 交叉验证：42 次编辑 + 39 次 Bash 仍 0 条 forge 文本触达模型；
// 仅 UserPromptSubmit stdout 能触达，且滞后）。Claude Code/codex/cursor/windsurf/opencode/pi/
// reasonix 注入 SessionStart 输出，不需回填，须保持排除——回填会与 SessionStart 已交付的 handoff
// 重复。
func sessionStartOutputDropped(agent string) bool {
	// Registry-derived (hostcap DroppedStdoutEvents): any host that drops
	// SessionStart stdout needs the handoff backfilled onto UserPromptSubmit.
	// Today only kimi qualifies.
	//
	// 由注册表派生（hostcap DroppedStdoutEvents）：任何丢弃 SessionStart
	// stdout 的宿主都需要把 handoff 回填到 UserPromptSubmit。目前仅 kimi 符合。
	h := hostcap.Lookup(agent)
	return h != nil && h.DropsStdoutEvent("SessionStart")
}

func runTaskContext(cmd *cobra.Command, args []string) error {
	// context = read-only alias of resume: pulls back the view but does not anchor a
	// session and does not change state.
	//
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
	fmt.Print(renderResume(state, gitPorcelain(root), workspaceContextLine(root, state.CrossRepoImpact)))
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
		by = resolveOriginTool(root, "")
	}
	affects, _ := cmd.Flags().GetStringArray("affects")
	rationale, _ := cmd.Flags().GetString("rationale")
	var d taskpipeline.Decision
	err = taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		s.AddDecision(taskpipeline.Decision{
			Content:   content,
			By:        by,
			Affects:   affects,
			Rationale: rationale,
		})
		d = s.Decisions[len(s.Decisions)-1]
		return nil
	})
	if err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	fmt.Printf("✓ 决策已记 [%s]: %s\n", d.ID, content)
	return nil
}

func runTaskNext(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	total := 0
	err = taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		for _, step := range args {
			s.AddNext(step)
		}
		total = len(s.NextSteps)
		return nil
	})
	if err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	fmt.Printf("✓ 已追加 %d 条下一步（共 %d 条）\n", len(args), total)
	return nil
}

func runTaskBlock(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	if resolveID, _ := cmd.Flags().GetString("resolve"); resolveID != "" {
		resolution, _ := cmd.Flags().GetString("resolution")
		err = taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
			if !s.ResolveBlocker(resolveID, resolution) {
				return fmt.Errorf("未找到阻塞 ID %q（forge task resume 查看现有 ID）", resolveID)
			}
			return nil
		})
		if err != nil {
			return err
		}
		fmt.Printf("✓ 阻塞 [%s] 已解决: %s\n", resolveID, resolution)
		return nil
	}
	content, _ := cmd.Flags().GetString("content")
	if content == "" {
		return fmt.Errorf("需要 --content <text> 新增阻塞，或 --resolve <id> 解决")
	}
	var b taskpipeline.Blocker
	err = taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		s.AddBlocker(taskpipeline.Blocker{Content: content})
		b = s.Blockers[len(s.Blockers)-1]
		return nil
	})
	if err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	fmt.Printf("✓ 阻塞已登记 [%s]: %s\n", b.ID, content)
	return nil
}

func runTaskFinding(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	if resolveID, _ := cmd.Flags().GetString("resolve"); resolveID != "" {
		err = taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
			if !s.ResolveFinding(resolveID) {
				return fmt.Errorf("未找到发现 ID %q（forge task resume 查看现有 ID）", resolveID)
			}
			return nil
		})
		if err != nil {
			return err
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
		source = resolveOriginTool(root, "")
	}
	evidence, _ := cmd.Flags().GetString("evidence")
	var f taskpipeline.Finding
	err = taskpipeline.MutateTaskState(root, state.TaskRef, func(s *taskpipeline.TaskState) error {
		nf := taskpipeline.Finding{
			Content:  content,
			Source:   source,
			Evidence: evidence,
		}
		taskpipeline.EnrichFinding(root, s, &nf)
		s.AddFinding(nf)
		f = s.Findings[len(s.Findings)-1]
		return nil
	})
	if err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
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
		tool = resolveOriginTool(root, "")
	}
	if tool == "" {
		return fmt.Errorf(`无法探测当前工具（无 agent env）。跨工具 attach 请显式传 --tool <tool>（如 pi/claude-code/opencode），避免把接手方 session 错误归属到创建方工具`)
	}
	var already bool
	var participants int
	err = taskpipeline.MutateTaskState(root, ref, func(s *taskpipeline.TaskState) error {
		already = s.HasSession(sid)
		s.AddSession(sid, tool)
		participants = len(s.SessionLinks)
		return nil
	})
	if err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	if already {
		fmt.Printf("✓ session %s 已锚定（工具=%s），任务 %s 现有 %d 个参与 session\n", sid, tool, ref, participants)
	} else {
		fmt.Printf("✓ 已锚定 session %s（工具=%s）到任务 %s（共 %d 个参与 session）\n", sid, tool, ref, participants)
	}
	return nil
}

// gitPorcelain returns the lines of git status --porcelain (changed-but-uncommitted
// files). Returns nil for non-git repos or on failure — resume does not depend on git;
// this is purely a "let the handoff party see the worktree state at a glance" extra.
//
// gitPorcelain 返回 git status --porcelain 的行（已改未提交文件）。非 git 仓库或失败返 nil——
// resume 不依赖 git，仅作「接手方一眼看到工作区状态」的辅助。
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

// renderResume renders the task's continuity fields into a HANDOFF-style view.
// gitChanged is passed in by the caller (decoupling git so this function can be pure-unit
// tested). When the continuity content is empty, it emits a minimal status card without
// erroring — resume always succeeds, only the amount of content varies.
// extraHeader carries optional one-liners (today only the multi-repo workspace
// context line, workspaceContextLine) injected after the DependsOn line; empty
// entries are skipped so fail-open callers can pass the line unconditionally.
//
// renderResume 把 task 接续字段渲染成 HANDOFF 风格视图。gitChanged 由 caller 传入（解耦 git，
// 使本函数可纯单测）。空接续内容时给最小状态卡，不报错——resume 永远成功，只是内容多寡。
// extraHeader 携带可选单行（当前仅多仓 workspace 上下文行 workspaceContextLine），
// 注入在依赖行之后；空条目跳过，fail-open 调用方可无条件传。
func renderResume(state *taskpipeline.TaskState, gitChanged []string, extraHeader ...string) string {
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
	if state.ExternalOrigin.URL != "" {
		w(fmt.Sprintf(`外部来源: %s %s %s`, orDash(state.ExternalOrigin.Tracker), orDash(state.ExternalOrigin.Identifier), state.ExternalOrigin.URL))
	}
	if state.Summary != "" {
		w("标题: " + state.Summary)
	}
	if state.ParentTaskRef != "" {
		w("父任务: " + state.ParentTaskRef)
	}
	if len(state.DependsOn) > 0 {
		w("依赖: " + strings.Join(state.DependsOn, ", "))
	}
	for _, line := range extraHeader {
		if line != "" {
			w(line)
		}
	}
	if state.HasContinuity() {
		w(renderTldr(state))
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
	w("→ 接续纪律用 session-continuity skill：HANDOFF 标准格式与跨会话恢复。")
	w(strings.Repeat("═", 60))
	return stripUnsafeControl(b.String())
}

// renderTldr produces a condensed tl;dr block (goal first line / doing now / open
// blockers) inserted near the top of renderResume's output. Design intent: Claude Code /
// each host's auto-compaction (summarize) tends to keep the beginning + structured short
// text, while the full HANDOFF view is easily compressed away. A compact tl;dr near the
// top is more likely to survive in the post-compaction summary, mitigating the context
// rot that drifts handoff continuity in long tasks. This is the cross-host mitigation
// layer for gap#2 (SessionStart injection, supported by equivalent mechanisms on all
// hosts); the Claude Code-specific PostCompact re-inject chain is a separate layer (see
// the compact-resume hook).
//
// renderTldr 产出精炼 tl;dr 块（目标首行 / 现在做 / open 阻塞），插在 renderResume 输出
// 靠前位置。设计目的：Claude Code/各 host 自动压缩（summarize）倾向保留开头 + 结构化短文本，
// 完整 HANDOFF 视图易被压掉，tl;dr 紧凑靠前 → 更可能在压缩后的 summary 中存活，缓解长任务
// context rot 致接续漂移。这是 gap#2 的跨 host 缓解层（SessionStart 注入，全 host 等价物支持）；
// Claude Code 特有的 PostCompact 重注入链是另一层（见 compact-resume hook）。
func renderTldr(state *taskpipeline.TaskState) string {
	goal := strings.Split(state.Goal, "\n")[0]
	if r := []rune(goal); len(r) > 100 {
		goal = string(r[:100]) + "…"
	}
	if goal == "" {
		goal = "(未设 goal)"
	}
	nowDoing := "(无)"
	if len(state.NextSteps) > 0 {
		nowDoing = state.NextSteps[0]
	} else if state.CurrentGate != "" {
		for _, g := range taskpipeline.DefaultGates() {
			if g.ID == state.CurrentGate {
				nowDoing = "推进 " + g.Name
				break
			}
		}
	}
	var open []string
	for _, bl := range state.Blockers {
		if bl.Status != "resolved" && bl.Status != "wontfix" {
			open = append(open, bl.Content)
			if len(open) >= 2 {
				break
			}
		}
	}
	blockerStr := "无"
	if len(open) > 0 {
		blockerStr = strings.Join(open, "; ")
	}
	var b strings.Builder
	w := func(s string) { b.WriteString(s + "\n") }
	w("【tl;dr · 压缩后保留此段关键】")
	w("  目标: " + goal)
	w("  现在做: " + nowDoing)
	w("  阻塞: " + blockerStr)
	return strings.TrimRight(stripUnsafeControl(b.String()), "\n")
}

// renderGateProgress renders gate progress (e.g. ✅implement ✅verify ⏳complete). The
// cli package cannot access taskpipeline's private gatePassed, so it judges from History
// itself — same semantics as taskpipeline.gatePassed.
//
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

// stripUnsafeControl strips ANSI escape sequences and other C0 control characters
// (keeping \n \t \r), preventing malicious ANSI in external markdown read by --plan-file
// (clear-screen ESC[2J, color-change ESC[31m, etc.) from being interpreted by the
// terminal. The HTML side already escapes via html/template; the CLI side adds this
// symmetric layer. After ESC is stripped, the ANSI sequence's leftover [31m-class text
// is no longer interpreted by the terminal — the goal is achieved (the residual text is
// harmless). DEL (0x7f) and other C0 control characters are also dropped.
//
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
