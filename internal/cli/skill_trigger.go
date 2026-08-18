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
	"sort"
	"strconv"
	"strings"
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
	// agent 传 ""（→ contextChannelDelivered 走 claude 默认行）：此子命令是调试入口，无
	// --agent 上下文；生产落章走 runSkillTriggerHook（带真实 agent）。
	//
	// agent passes "" (→ contextChannelDelivered takes the claude default row): this
	// subcommand is a debug entry with no --agent context; production stamping goes through
	// runSkillTriggerHook (with the real agent).
	rendered, err := runSkillTriggerCore(hookInput, root, cmd.Root().Version, "", skillTriggerDryRun)
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
// （仅 UserPromptSubmit 时模型可见，其余事件 stdout 被 kimi 丢弃——见 internal/agentbridge/kimi-hook-routing.md），无渲染则静默。
func runSkillTriggerHook(hookInput HookInput, root, version, agent string) error {
	// kimi 0.35.0 drops allow-path stdout from the model context for every event except
	// UserPromptSubmit (verified via wire.jsonl: advisories reached the model 0 times across a
	// 42-edit session). Running the engine here on PreToolUse/PostToolUse/Stop/SessionStart would
	// (a) never reach the model and (b) record checklog entries (recordSkillTriggerHits, inside
	// runSkillTriggerCore) that mislead `forge skills usage` into reporting delivered triggers the
	// model never saw — the false-prosperity observability bug. Bail BEFORE runSkillTriggerCore so
	// neither the render nor the marker/checklog side-effects happen. This also neutralizes stale
	// installed manifests still binding skill-trigger to other events. UserPromptSubmit is the only
	// kimi-reachable inject channel. BuildKimiPluginHooks now emits skill-trigger only there; this
	// guard is defense in depth (and the single point that kills the false checklog regardless of
	// which manifest a stale install carries).
	if agent == "kimi" && hookInput.HookEventName != "UserPromptSubmit" {
		return nil
	}
	rendered, err := runSkillTriggerCore(hookInput, root, version, agent, false)
	if agent == "kimi" {
		if err == nil && rendered != "" {
			fmt.Print(rendered)
		}
		return nil
	}
	// skill-trigger never blocks (advisory injection) — the allow-with-detail path of
	// the per-agent emitter picks each host's context channel. The old fixed
	// `{"decision":"approve"}` envelope was Claude-only shape noise, and codex marks
	// decision:"approve" as a FAILED hook — the injection would never land there.
	//
	// skill-trigger 永不阻断（advisory 注入）——走 per-agent emitter 的
	// allow-with-detail 路径，按宿主选择上下文通道。旧的固定
	// `{"decision":"approve"}` envelope 是 Claude 专属形态的噪声，且 codex 会把
	// decision:"approve" 判为 FAILED hook——注入在那里永远落不了地。
	detail := ""
	if err == nil {
		detail = rendered
	}
	return emitAgentOutput(agent, hookInput.HookEventName, "skill-trigger", true, detail)
}

// runSkillTriggerCore 是判定 + 渲染核心（hook 链 / 子命令 / dry-run 共用）。
// 返回渲染文本（无命中返 ""，调用方按需 wrap）。无 canonical 源 / 无 triggers 声明 → 静默 ""。
// agent 用于落章送达判定（contextChannelDelivered）；"" 走 claude 默认行（调试子命令路径）。
func runSkillTriggerCore(hookInput HookInput, root, version, agent string, dryRun bool) (string, error) {
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
	hits, suppressed := skilltrigger.Eval(ctx, all, noise)
	if dryRun {
		fmt.Fprintf(os.Stderr, "[skill-trigger] canonical=%s event=%s tool=%s prompt_len=%d 命中=%d 抑制=%d\n",
			canonicalDir, ctx.Event, ctx.ToolName, len(ctx.Prompt), len(hits), len(suppressed))
		for _, h := range hits {
			fmt.Fprintf(os.Stderr, "  命中 %s（%s）\n", h.Skill, h.Reason)
		}
		for _, s := range suppressed {
			fmt.Fprintf(os.Stderr, "  抑制 %s（cause=%s）\n", s.Skill, s.Cause)
		}
	}
	if len(hits) == 0 && len(suppressed) == 0 {
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
		recordSuppressed(root, ctx, suppressed, filepath.Join(baseDir, "skill-trigger"), agent, version)
		// 记录触发的 canonical skill 到 checklog——让 skill 触达在下游可观测（usage/effectiveness）。
		// 否则 skill-trigger 静默注入 AdditionalContext、零持久轨迹，Forge 无法回答"哪些 skill 真触发过"
		// （dogfood 0 触发盲区）。CheckSkillTrigger 不计入 evidence strength（观测非验证）——见 BuildEvidenceChain。
		recordSkillTriggerHits(root, ctx, hits, filepath.Join(baseDir, "skill-trigger"), agent, version)
	}
	if len(hits) == 0 {
		// 只有抑制（如 stop-cap 触顶 / 全员 cooldown）：渲染无内容，但抑制记录已落。
		return "", nil
	}
	return skilltrigger.Render(hits, ctx), nil
}

// recordSuppressed 处理 Eval 返回的抑制事件（辩论 P1）：
//   - cooldown：计数器 +1，等该 skill 下次真实触发时回填 Meta（SuppressedCounter.Take）；
//   - stop-max-rounds：每次触顶记一条 warn advisory（CheckKimiPluginStale 同款模式：Passed=true
//     保持中性，warn 信号走 Level；Detail 刻意不含 " hit (" 标记，SkillFromTriggerDetail
//     返回 ""，usage/funnel 计数零污染）。
//
// recordSuppressed handles suppressed events returned by Eval (debate P1):
//   - cooldown: counter +1, backfilled into Meta at the skill's next actual fire
//     (SuppressedCounter.Take);
//   - stop-max-rounds: ONE warn advisory per cap trip (the CheckKimiPluginStale pattern:
//     Passed=true stays neutral, the warn signal rides Level; the Detail deliberately
//     lacks the " hit (" marker so SkillFromTriggerDetail returns "" — zero pollution of
//     usage/funnel counts).
func recordSuppressed(root string, ctx skilltrigger.Context, suppressed []skilltrigger.Suppressed, counterDir, agent, version string) {
	if len(suppressed) == 0 {
		return
	}
	counter := skilltrigger.NewFileSuppressedCounter(counterDir)
	var stopCapped []string
	for _, s := range suppressed {
		switch s.Cause {
		case skilltrigger.SuppressCooldown:
			_ = counter.Incr(ctx.SessionID, s.Skill)
		case skilltrigger.SuppressStopCap:
			stopCapped = append(stopCapped, s.Skill)
		}
	}
	if len(stopCapped) == 0 {
		return
	}
	delivered, channel := contextChannelDelivered(agent, ctx.Event)
	sort.Strings(stopCapped)
	_ = checklog.Record(root, &checklog.Entry{
		Check:        checklog.CheckSkillTrigger,
		Passed:       true,
		Checked:      true,
		TaskRef:      taskRefForSession(root, ctx.SessionID),
		SessionID:    ctx.SessionID,
		Detail:       fmt.Sprintf("skill-trigger: stop-round-cap 达到上限，抑制 %d 个潜在注入（%s）", len(stopCapped), strings.Join(stopCapped, ",")),
		Source:       checklog.EvidenceDeterministic,
		Level:        checklog.LevelWarn,
		Delivered:    &delivered,
		Channel:      channel,
		ForgeVersion: version,
		Meta: map[string]string{
			checklog.MetaKeyCause:  skilltrigger.SuppressStopCap,
			checklog.MetaKeySkills: strings.Join(stopCapped, ","),
		},
	})
}

// taskRefForSession 提取 session 绑定的活跃 task ref（与 recordSkillTriggerHits 的判定一致，
// 抽出为单一实现防两处漂移）。
//
// taskRefForSession extracts the active task ref bound to the session (same rule as
// recordSkillTriggerHits, extracted to a single implementation so the two cannot drift).
func taskRefForSession(root, sessionID string) string {
	if active, err := taskpipeline.ActiveTaskState(root, sessionID); err == nil && active != nil {
		return active.TaskRef
	}
	return ""
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
//
// v2（辩论 P0）：每条命中落 Entry.Meta 结构化证据（键契约 checklog.MetaKey*，单一真相源）——
// matched_keyword/match_source（per-keyword 分析主键）、when、trigger_index/trigger_sig（规则
// 身份，sig 抗数组重排）、prompt_hash/prompt_len（项目盐哈希，聚类不存原文）、
// suppressed_since_last（cooldown 抑制回填）、excerpt（opt-in FORGE_TRIGGER_EXCERPT=1，脱敏
// ±96 rune 窗口）。缺键语义 = 「未知/不适用」，读方不得当零值。
//
// v2 (debate P0): each hit lands a structured evidence payload in Entry.Meta (key contract
// checklog.MetaKey*, single source of truth) — matched_keyword/match_source (primary key
// for per-keyword analysis), when, trigger_index/trigger_sig (rule identity, sig survives
// array reordering), prompt_hash/prompt_len (project-salted hash — clustering without
// storing raw text), suppressed_since_last (cooldown backfill), excerpt (opt-in
// FORGE_TRIGGER_EXCERPT=1, redacted ±96-rune window). Absent-key semantics =
// "unknown/n-a", never zero-value.
func recordSkillTriggerHits(root string, ctx skilltrigger.Context, hits []skilltrigger.Hit, counterDir, agent, version string) {
	taskRef := taskRefForSession(root, ctx.SessionID)
	counter := skilltrigger.NewFileSuppressedCounter(counterDir)
	excerptOn := os.Getenv("FORGE_TRIGGER_EXCERPT") == "1"
	// L1 送达章：一次 hook 调用里所有 hit 走同一 (agent, event) 通道，判定一次、逐条落章。
	// Delivered/Channel/ForgeVersion 让 checklog 成为「真到达模型上下文」的真相源——usage 漏斗的
	// 送达分母从此可靠，死通道宿主的命中不再虚计成送达（kimi 2026-08-15 修复的全宿主泛化）。
	//
	// L1 delivery stamp: all hits in one hook invocation ride the same (agent, event) channel —
	// verdict computed once, stamped per entry. Delivered/Channel/ForgeVersion make checklog the
	// ground truth of "actually reached model context" — the usage funnel's delivery denominator
	// becomes trustworthy, and dead-channel host hits stop counting as delivered (the all-host
	// generalization of the kimi 2026-08-15 fix).
	delivered, channel := contextChannelDelivered(agent, ctx.Event)
	for _, h := range hits {
		meta := map[string]string{
			checklog.MetaKeyMatchSource:  h.MatchSource,
			checklog.MetaKeyTriggerIndex: strconv.Itoa(h.TriggerIndex),
			checklog.MetaKeyTriggerSig:   h.TriggerSig,
			checklog.MetaKeyPromptLen:    strconv.Itoa(h.PromptLen),
		}
		if h.MatchedKeyword != "" {
			meta[checklog.MetaKeyMatchedKeyword] = h.MatchedKeyword
		}
		if h.Trigger.When != "" {
			meta[checklog.MetaKeyWhen] = h.Trigger.When
		}
		if h.PromptHash != "" {
			meta[checklog.MetaKeyPromptHash] = h.PromptHash
		}
		// 抑制回填：读取并清零该 skill 本 session 累计的 cooldown 抑制（>0 才落键，条目保持精瘦）。
		//
		// Suppression backfill: take-and-reset this skill's accumulated cooldown suppressions
		// in this session (key written only when >0, keeping entries lean).
		if n := counter.Take(ctx.SessionID, h.Skill); n > 0 {
			meta[checklog.MetaKeySuppressedSinceLast] = strconv.Itoa(n)
		}
		if excerptOn {
			// 摘录只记命中来源的那段文本，±96 rune，先过 secret 脱敏再落（默认关——R4/R7
			// 隐私折中：hash 恒记已足聚类，摘录 opt-in 只服务 triage/挖矿）。
			//
			// The excerpt captures only the matched source text, ±96 runes, redacted before
			// landing (off by default — the R4/R7 privacy compromise: the always-recorded
			// hash already suffices for clustering; the opt-in excerpt serves triage/mining).
			if ex := skilltrigger.Excerpt(ctx, h.MatchSource, h.MatchedKeyword, 96); ex != "" {
				meta[checklog.MetaKeyExcerpt] = util.RedactSecrets(ex)
			}
		}
		// Detail 经 checklog.DetailForSkillTrigger 单一真相源构造，与读取方
		// (skillseval.SkillCountsFromChecklog → checklog.SkillFromTriggerDetail) 共契约，杜绝
		// 两侧手工镜像格式串漂移致被动触发信号静默丢失（minor-1：跨包 stringly-typed 契约）。
		//
		// Detail is built via the checklog.DetailForSkillTrigger single source of truth, sharing the contract
		// with the reader (skillseval.SkillCountsFromChecklog → checklog.SkillFromTriggerDetail), eliminating
		// hand-mirrored format-string drift that would silently drop passive-trigger signals (minor-1).
		detail := checklog.DetailForSkillTrigger(h.Skill, ctx.Event, h.Reason)
		if err := checklog.Record(root, &checklog.Entry{
			Check:        checklog.CheckSkillTrigger,
			Passed:       true,
			Checked:      true,
			ToolName:     ctx.ToolName,
			TaskRef:      taskRef,
			SessionID:    ctx.SessionID,
			Detail:       detail,
			Source:       checklog.EvidenceDeterministic,
			Level:        checklog.LevelAdvisory,
			Delivered:    &delivered,
			Channel:      channel,
			ForgeVersion: version,
			Meta:         meta,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "[skill-trigger] warning: checklog record failed: %v\n", err)
		}
	}
}
