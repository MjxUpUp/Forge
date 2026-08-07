// Package cli skill_trigger.go 是通用 skill-trigger 框架的 CLI 入口与判定核心。
//
// 设计要点（与 plan §1 的偏离，技术正确性驱动）：
// plan 原假设 thin-wrapper bash（exec forge skill trigger --hook）能透传 stdin，但 runHook
// (hook.go) 已 io.ReadAll(os.Stdin) 消费 stdin 且未设 shCmd.Stdin，子进程拿到空 stdin。
// task-resume/resume-reinject 等 thin-wrapper 不依赖 stdin（用 forge data 渲染），故未暴露；
// skill-trigger 必须从 HookInput 取 Event/Prompt/Tool/command/exit_code（只能来自 stdin）。
// 故采用 runHook 特例方案：name=="skill-trigger" 时 Go 内直接判定 + 渲染，复用 runHook 已
// normalize 的 hookInput 与 agent stdin normalize，不经 bash embed，避开 stdin 透传难题。
//
// Package cli skill_trigger.go is the CLI entry + evaluation core of the generic skill-trigger
// framework. Deviates from plan §1 (driven by technical correctness): the thin-wrapper bash
// assumption breaks because runHook consumes stdin; skill-trigger needs live HookInput fields,
// so a runHook special-case evaluates + renders in Go, reusing the already-normalized hookInput.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/skillscanonical"
	"github.com/MjxUpUp/Forge/internal/skilltrigger"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

var (
	skillTriggerDryRun bool
	skillTriggerEvent  string
)

// skillTriggerCmd 是 `forge skills trigger` 子命令——主要供 --dry-run 调试（模拟事件、stderr
// 打扫描/命中详情、不写 marker）。生产 hook 链走 `forge hook skill-trigger` → runHook 特例 →
// runSkillTriggerHook，不经此子命令。
var skillTriggerCmd = &cobra.Command{
	Use:    "trigger",
	Short:  "(internal) 通用 skill-trigger 判定入口（声明式触发 + 注入指引）",
	Hidden: true,
	RunE:   runSkillTriggerCmd,
}

func init() {
	skillTriggerCmd.Flags().BoolVar(&skillTriggerDryRun, "dry-run", false, "调试：stderr 打扫描/命中详情，不写 marker")
	skillTriggerCmd.Flags().StringVar(&skillTriggerEvent, "event", "", "覆盖 HookInput 的事件名（调试模拟其他事件）")
	skillsCmd.AddCommand(skillTriggerCmd)
}

// runSkillTriggerCmd 处理 `forge skills trigger`：读 stdin HookInput，调核心，stdout 打渲染结果。
func runSkillTriggerCmd(cmd *cobra.Command, args []string) error {
	var hookInput HookInput
	stdinData, _ := io.ReadAll(os.Stdin)
	if len(stdinData) > 0 {
		if err := json.Unmarshal(stdinData, &hookInput); err != nil {
			fmt.Fprintf(os.Stderr, "[skill-trigger] warning: stdin JSON parse failed: %v\n", err)
		}
	}
	if skillTriggerEvent != "" {
		hookInput.HookEventName = skillTriggerEvent
	}
	root, _ := findProjectRoot()
	rendered, err := runSkillTriggerCore(hookInput, root, cmd.Root().Version, skillTriggerDryRun)
	if err != nil {
		return err
	}
	if rendered != "" {
		fmt.Print(rendered)
	}
	return nil
}

// runSkillTriggerHook 是 runHook 的 skill-trigger 特例入口：复用 runHook 已 normalize 的
// hookInput，Go 内判定 + 渲染 + 输出 HookOutput JSON（不经 bash embed）。
// kimi 下按 kimi 协议输出：skill-trigger 永不阻断（advisory），渲染文本直接打 stdout
// （kimi 把 allow 路径 stdout 注入上下文），无渲染则静默。
func runSkillTriggerHook(hookInput HookInput, root, version, agent string) error {
	rendered, err := runSkillTriggerCore(hookInput, root, version, false)
	if agent == "kimi" {
		if err == nil && rendered != "" {
			fmt.Print(rendered)
		}
		return nil
	}
	if err != nil {
		// 判定异常不阻断 hook 链——skill-trigger 是 advisory 注入，fail-open。
		fmt.Println(`{"decision":"approve"}`)
		return nil
	}
	out := HookOutput{Decision: "approve"}
	if rendered != "" {
		out.HookSpecificOutput = &HookSpecificOutput{
			HookEventName:     hookInput.HookEventName,
			AdditionalContext: truncate(rendered, maxAdditionalContextLen),
		}
	}
	data, _ := json.Marshal(out)
	fmt.Println(string(data))
	return nil
}

// runSkillTriggerCore 是判定 + 渲染核心（hook 链 / 子命令 / dry-run 共用）。
// 返回渲染文本（无命中返 ""，调用方按需 wrap）。无 canonical 源 / 无 triggers 声明 → 静默 ""。
func runSkillTriggerCore(hookInput HookInput, root, version string, dryRun bool) (string, error) {
	// 全局禁用早返——避免仍跑 Resolve+LoadAll（扫所有 SKILL.md 解析 frontmatter）增加 hook 链延迟。
	if os.Getenv("FORGE_SKILL_TRIGGER") == "0" {
		return "", nil
	}
	// ok（isExternal）仅区分 env 覆盖（FORGE_SKILLS_CANONICAL）vs embed cache，二者都是有效
	// canonical 源；不可用 !ok 判无源——embed fallback 返回 ok=false 会被误拒，导致生产所有
	// 事件静默 PASS（P0：1.14.0 skill-trigger 框架因此完全失效）。
	canonicalDir, _, err := skillscanonical.Resolve(version)
	if err != nil || canonicalDir == "" {
		if dryRun {
			fmt.Fprintf(os.Stderr, "[skill-trigger] 无 canonical 源（version=%s）\n", version)
		}
		return "", nil
	}
	all := skilltrigger.LoadAll(canonicalDir)
	if len(all) == 0 {
		if dryRun {
			fmt.Fprintf(os.Stderr, "[skill-trigger] canonical=%s 无 triggers 声明\n", canonicalDir)
		}
		return "", nil
	}
	ctx := buildTriggerContext(hookInput, root)
	// marker/stop-rounds 是 session 态短命数据，写 $TMPDIR（系统定期清理）而非 GlobalHome
	// （后者无清理机制会无限增长，F6）。与 reads-log/task-resume（同用 $TMPDIR）一致。
	baseDir := os.TempDir()
	// dry-run 用 InMemory（绕过 cooldown/max-rounds，看原始命中）；生产用 File（落盘 marker）。
	var noise skilltrigger.NoiseController
	if dryRun {
		noise = skilltrigger.NewInMemoryNoiseController()
	} else {
		noise = skilltrigger.NewFileNoiseController(filepath.Join(baseDir, "skill-trigger"))
	}
	hits := skilltrigger.Eval(ctx, all, noise)
	if dryRun {
		fmt.Fprintf(os.Stderr, "[skill-trigger] canonical=%s event=%s tool=%s prompt_len=%d 命中=%d\n",
			canonicalDir, ctx.Event, ctx.ToolName, len(ctx.Prompt), len(hits))
		for _, h := range hits {
			fmt.Fprintf(os.Stderr, "  命中 %s（%s）\n", h.Skill, h.Reason)
		}
	}
	if len(hits) == 0 {
		return "", nil
	}
	// 落盘副作用：Eval 只读判定（ShouldFire/StopRoundAllowed），确认注入后才 Mark/IncrStopRound。
	if !dryRun {
		for _, h := range hits {
			_ = noise.Mark(ctx.SessionID, h.Skill, ctx.Now)
		}
		if ctx.Event == "Stop" {
			_ = noise.IncrStopRound(ctx.SessionID)
		}
		// 记录触发的 canonical skill 到 checklog——让 skill 触达在下游可观测（usage/effectiveness）。
		// 否则 skill-trigger 静默注入 AdditionalContext、零持久轨迹，Forge 无法回答"哪些 skill 真触发过"
		// （dogfood 0 触发盲区）。CheckSkillTrigger 不计入 evidence strength（观测非验证）——见 BuildEvidenceChain。
		recordSkillTriggerHits(root, ctx, hits)
	}
	return skilltrigger.Render(hits, ctx), nil
}

// buildTriggerContext 把 HookInput 转成引擎 Context（agent-neutral）。
func buildTriggerContext(hookInput HookInput, root string) skilltrigger.Context {
	ctx := skilltrigger.Context{
		Event:       hookInput.HookEventName,
		Prompt:      hookInput.Prompt,
		ToolName:    hookInput.ToolName,
		SessionID:   util.SanitizeSessionID(hookInput.SessionID),
		ProjectRoot: root,
		Now:         time.Now(),
	}
	if len(hookInput.ToolInput) > 0 {
		_ = json.Unmarshal(hookInput.ToolInput, &ctx.ToolInput)
	}
	if len(hookInput.ToolOutput) > 0 {
		_ = json.Unmarshal(hookInput.ToolOutput, &ctx.ToolOutput)
	}
	return ctx
}

// recordSkillTriggerHits records each fired canonical skill to checklog so skill reach is observable downstream
// (`forge skills usage`/`effectiveness`). skill-trigger otherwise injects silently into AdditionalContext with
// zero persistent trail — Forge could not answer "which canonical skills actually fired" (the dogfood 0-trigger
// blind spot). CheckSkillTrigger is deterministic (the engine evaluates declared triggers, agent cannot forge) and
// excluded from evidence strength (observation, not verification) — see checklog.BuildEvidenceChain. task_ref is
// attached only when an active (non-completed) task is bound to this session, so post-complete Stop triggers are
// not misattributed to a finished task.
//
// recordSkillTriggerHits 把每个触发的 canonical skill 落进 checklog，让 skill 触达在下游可观测
// （`forge skills usage`/`effectiveness`）。否则 skill-trigger 经 AdditionalContext 静默注入、零持久轨迹——
// Forge 无法回答"哪些 canonical skill 真触发过"（dogfood 0 触发盲区）。CheckSkillTrigger 是 deterministic
// （引擎实算声明式触发，agent 无法伪造）且不计入 evidence strength（观测非验证）——见 checklog.BuildEvidenceChain。
// task_ref 仅当本 session 绑定了活跃（未完成）任务时附加，避免 complete 之后的 Stop 触发被错挂到已完成任务。
func recordSkillTriggerHits(root string, ctx skilltrigger.Context, hits []skilltrigger.Hit) {
	taskRef := ""
	if active, err := taskpipeline.ActiveTaskState(root, ctx.SessionID); err == nil && active != nil {
		taskRef = active.TaskRef
	}
	for _, h := range hits {
		// Detail 经 checklog.DetailForSkillTrigger 单一真相源构造，与读取方
		// (skillseval.SkillCountsFromChecklog → checklog.SkillFromTriggerDetail) 共契约，杜绝
		// 两侧手工镜像格式串漂移致被动触发信号静默丢失（minor-1：跨包 stringly-typed 契约）。
		//
		// Detail is built via the checklog.DetailForSkillTrigger single source of truth, sharing the contract
		// with the reader (skillseval.SkillCountsFromChecklog → checklog.SkillFromTriggerDetail), eliminating
		// hand-mirrored format-string drift that would silently drop passive-trigger signals (minor-1).
		detail := checklog.DetailForSkillTrigger(h.Skill, ctx.Event, h.Reason)
		if err := checklog.Record(root, &checklog.Entry{
			Check:     checklog.CheckSkillTrigger,
			Passed:    true,
			Checked:   true,
			ToolName:  ctx.ToolName,
			TaskRef:   taskRef,
			SessionID: ctx.SessionID,
			Detail:    detail,
			Source:    checklog.EvidenceDeterministic,
			Level:     checklog.LevelAdvisory,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "[skill-trigger] warning: checklog record failed: %v\n", err)
		}
	}
}
