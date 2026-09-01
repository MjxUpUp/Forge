// Package cli skill_trigger.go is the CLI entry + evaluation core of the generic skill-trigger framework.
//
// Package cli skill_trigger.go 是通用 skill-trigger 框架的 CLI 入口与判定核心。
// 2026-09 普查 A2-1 迁出 skills 簇时本文件刻意留守 cli：它是 runHook 的特例
// 路径（进程内判定 + 渲染，依赖 cli 的 HookInput/emitAgentOutput），属 hook 链
// 桥接而非 skills 命令面——命令注册仍挂 cliskills.Root。
//
// 设计要点（与 plan §1 的偏离，技术正确性驱动）：
// plan 原假设 thin-wrapper bash（exec forge skill trigger --hook）能透传 stdin，但 runHook
// (hook.go) 已 io.ReadAll(os.Stdin) 消费 stdin 且未设 shCmd.Stdin，子进程拿到空 stdin。
// task-resume/resume-reinject 等 thin-wrapper 不依赖 stdin（用 forge data 渲染），故未暴露；
// skill-trigger 必须从 HookInput 取 Event/Prompt/Tool/command/exit_code（只能来自 stdin）。
// 故采用 runHook 特例方案：name=="skill-trigger" 时 Go 内直接判定 + 渲染，复用 runHook 已
// normalize 的 hookInput 与 agent stdin normalize，不经 bash embed，避开 stdin 透传难题。
package cli

import (
	"encoding/json"
	"fmt"
	"github.com/MjxUpUp/Forge/internal/cliskills"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/hostcap"
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
	cliskills.Root.AddCommand(skillTriggerCmd)
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
// kimi 下按 kimi 协议输出：skill-trigger 永不阻断（advisory），渲染文本仅 UserPromptSubmit
// 打 stdout（其余事件 stdout 被 kimi 丢弃——见 internal/agentbridge/kimi-hook-routing.md），
// 无渲染则静默。
func runSkillTriggerHook(hookInput HookInput, root, version, agent string) error {
	// kimi 0.35.0 对除 UserPromptSubmit 外的所有事件丢弃 allow 路径 stdout（wire.jsonl
	// 实证）。引擎在其余事件上仍运行——每条命中以 Delivered=false 落 checklog（由
	// contextChannelDelivered 的 kimi 行落章）——让看板事件流与 usage 漏斗看到完整的
	// 触发图景，而非 kimi 盲区（2026-08：kimi 任务仅 1 条 skill-trigger 事件 vs claude
	// 59 条，纯观测伪影）。漏斗只计 Delivered=true（skillseval.SkillFunnel），故这些
	// 记录不可能复活旧的 pre-core bail 所防的虚假繁荣 bug——「诚实记录、绝不静默投递」
	// 取代了「记录前 bail」。只有 stdout 打印仍门控在 UserPromptSubmit（kimi 唯一送进
	// 模型上下文的通道）；其余事件打印只会被宿主丢弃。
	//
	// 有意为之的副作用（review M3）：引擎全事件运行意味着不可见事件上的命中也会推进
	// cooldown（noise.Mark）与 Stop 回合计数（IncrStopRound）——某 skill 在不可见事件
	// 命中后，其在 UserPromptSubmit（唯一可见通道）的重注入可能被 cooldown 抑制，模型
	// 实际看到的注入比 bail 时代更少。这与既有宿主行为一致（windsurf 全事件无通道、
	// codex Stop 无通道，其命中本就消耗 cooldown）：cooldown 记的是「触发发生了」而非
	// 「送达了」，kimi 只是回到同一语义。若未来要按送达计 cooldown，应对全宿主统一改，
	// 而非给 kimi 特判。
	rendered, err := runSkillTriggerCore(hookInput, root, version, agent, false)
	// 在部分事件上丢弃 allow 路径 stdout 的宿主（hostcap DroppedStdoutEvents；目前
	// 仅 kimi）：引擎在这些事件上仍运行并记录（Delivered=false 保持记录诚实）。
	// 渲染出的注入本身不再随被丢的 stdout 湮灭：emitAdvisoryRouted 在不可送达
	// 事件上把它入队（kimi 把 PreToolUse stdout 当 **deny**，且丢弃
	// PostToolUse/Stop/SessionStart 的 stdout——打印只会是被丢的字节，甚至更糟的
	// 幻影阻断），并在 UserPromptSubmit 上把积压攒成一条注入、排在本 hook 自己的
	// 渲染之前。core 错误保持吞掉（advisory 层 fail-open），与引入队列前一致。
	if h := hostcap.Lookup(agent); h != nil && len(h.DroppedStdoutEvents) > 0 {
		if err != nil {
			return nil
		}
		return emitAdvisoryRouted(agent, hookInput.HookEventName, "skill-trigger", root, hookInput.SessionID, true, rendered)
	}
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
	if skilltrigger.Disabled() {
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
			if err := noise.Mark(ctx.SessionID, h.Skill, ctx.Now); err != nil {
				// A missed cooldown mark means the same skill re-fires on every event —
				// the exact flooding suppressed.go exists to prevent. Keep fail-open but visible.
				fmt.Fprintf(os.Stderr, "[skill-trigger] warning: noise mark failed: %v\n", err)
			}
		}
		if ctx.Event == "Stop" {
			if err := noise.IncrStopRound(ctx.SessionID); err != nil {
				fmt.Fprintf(os.Stderr, "[skill-trigger] warning: stop-round incr failed: %v\n", err)
			}
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
	// 单次上限（MaxHitsPerEvent）落选的 skill 尾部一句带过——控噪作用于渲染（也即 kimi
	// advisory 入队）之前，队列不会被重复命中塞满。
	var overflow []string
	for _, s := range suppressed {
		if s.Cause == skilltrigger.SuppressEventCap {
			overflow = append(overflow, s.Skill)
		}
	}
	return skilltrigger.Render(hits, ctx, overflow), nil
}

// recordSuppressed 处理 Eval 返回的抑制事件（辩论 P1）：
//   - cooldown：计数器 +1，等该 skill 下次真实触发时回填 Meta（SuppressedCounter.Take）；
//   - stop-max-rounds：**每 session 至多一条** warn advisory（review M2：无节流会在长
//     session 里逐 Stop 刷条目——source_changed_uncommitted 类 condition 编码中恒真，
//     MaxStopRounds 触顶后每个 Stop 回合都落一条，恰是 suppressed.go 声称要避免的日志
//     淹没。CheckKimiPluginStale 的 daily 节流同款思想，session 粒度足够——cap 一旦
//     触顶信息量就不再增长）。Passed=true 保持中性，warn 信号走 Level；Detail 刻意不含
//     " hit (" 标记，SkillFromTriggerDetail 返回 ""，usage/funnel 计数零污染。不带
//     Delivered/Channel 章（review n5——该事件从未向模型注入任何内容，送达语义不适用）。
func recordSuppressed(root string, ctx skilltrigger.Context, suppressed []skilltrigger.Suppressed, counterDir, agent, version string) {
	if len(suppressed) == 0 {
		return
	}
	counter := skilltrigger.NewFileSuppressedCounter(counterDir)
	var stopCapped []string
	for _, s := range suppressed {
		switch s.Cause {
		case skilltrigger.SuppressCooldown, skilltrigger.SuppressSessionCap, skilltrigger.SuppressEventCap:
			// 三类都进同一抑制计数器（「本会注入但没注」的统一语义）：cooldown 会在下次
			// 触发回填；session-cap 永无下次触发（G5 缺口天然适用）；event-cap 落选不
			// Mark、下事件即可命中，回填随之发生。
			_ = counter.Incr(ctx.SessionID, s.Skill)
		case skilltrigger.SuppressStopCap:
			stopCapped = append(stopCapped, s.Skill)
		}
	}
	if len(stopCapped) == 0 {
		return
	}
	// session 级节流 marker：已记过即静默（counter 目录同寿命）。
	marker := filepath.Join(counterDir, util.SanitizeSessionID(ctx.SessionID), "stopcap-advisory.marker")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	sort.Strings(stopCapped)
	if err := checklog.Record(root, &checklog.Entry{
		Check:        checklog.CheckSkillTrigger,
		Passed:       true,
		Checked:      true,
		TaskRef:      taskRefForSession(root, ctx.SessionID),
		SessionID:    ctx.SessionID,
		Detail:       fmt.Sprintf("skill-trigger: stop-round-cap 达到上限，抑制 %d 个潜在注入（%s）", len(stopCapped), strings.Join(stopCapped, ",")),
		Source:       checklog.EvidenceDeterministic,
		Level:        checklog.LevelWarn,
		ForgeVersion: version,
		Meta: map[string]string{
			checklog.MetaKeyCause:  skilltrigger.SuppressStopCap,
			checklog.MetaKeySkills: strings.Join(stopCapped, ","),
		},
	}); err == nil {
		_ = os.MkdirAll(filepath.Dir(marker), 0755)
		_ = util.AtomicWrite(marker, []byte("1"), 0644)
	}
}

// taskRefForSession 提取 session 绑定的活跃 task ref（与 recordSkillTriggerHits 的判定一致，
// 抽出为单一实现防两处漂移）。
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
func recordSkillTriggerHits(root string, ctx skilltrigger.Context, hits []skilltrigger.Hit, counterDir, agent, version string) {
	taskRef := taskRefForSession(root, ctx.SessionID)
	counter := skilltrigger.NewFileSuppressedCounter(counterDir)
	excerptOn := os.Getenv("FORGE_TRIGGER_EXCERPT") == "1"
	// L1 送达章：一次 hook 调用里所有 hit 走同一 (agent, event) 通道，判定一次、逐条落章。
	// Delivered/Channel/ForgeVersion 让 checklog 成为「真到达模型上下文」的真相源——usage 漏斗的
	// 送达分母从此可靠，死通道宿主的命中不再虚计成送达（kimi 2026-08-15 修复的全宿主泛化）。
	// 经 advisoryEmissionChannel 盖章（非裸 contextChannelDelivered）：kimi 不可送达事件上的
	// 命中现经 emitAdvisoryRouted 入队（UserPromptSubmit 攒发），章标 kimi/advisory-queue，
	// 漏斗据此区分「入队待投」与「永久丢失」。
	delivered, channel := advisoryEmissionChannel(agent, ctx.Event)
	for _, h := range hits {
		// 缺键 = 「未知/不适用」契约（review m5）：仅写已知项——condition-only 触发不落
		// match_source（值恒 "" 与缺键不可分）；prompt_len 只在哈希真的来自 prompt 时落
		//（tool 事件的回退哈希来自来源文本，prompt_len=0 与之 pairing 是语义错位）。
		meta := map[string]string{
			checklog.MetaKeyTriggerIndex: strconv.Itoa(h.TriggerIndex),
			checklog.MetaKeyTriggerSig:   h.TriggerSig,
		}
		if h.MatchSource != "" {
			meta[checklog.MetaKeyMatchSource] = h.MatchSource
		}
		if h.MatchedKeyword != "" {
			meta[checklog.MetaKeyMatchedKeyword] = h.MatchedKeyword
		}
		if h.Trigger.When != "" {
			meta[checklog.MetaKeyWhen] = h.Trigger.When
		}
		if h.PromptHash != "" {
			meta[checklog.MetaKeyPromptHash] = h.PromptHash
			if ctx.Prompt != "" {
				meta[checklog.MetaKeyPromptLen] = strconv.Itoa(h.PromptLen)
			}
		}
		// 抑制回填：读取并清零该 skill 本 session 累计的 cooldown 抑制（>0 才落键，条目保持精瘦）。
		if n := counter.Take(ctx.SessionID, h.Skill); n > 0 {
			meta[checklog.MetaKeySuppressedSinceLast] = strconv.Itoa(n)
		}
		if excerptOn {
			// 摘录只记命中来源的那段文本，±96 rune，先过 secret 脱敏再落（默认关——R4/R7
			// 隐私折中：hash 恒记已足聚类，摘录 opt-in 只服务 triage/挖矿）。
			if ex := skilltrigger.Excerpt(ctx, h.MatchSource, h.MatchedKeyword, 96); ex != "" {
				meta[checklog.MetaKeyExcerpt] = util.RedactSecrets(ex)
			}
		}
		// Detail 经 checklog.DetailForSkillTrigger 单一真相源构造，与读取方
		// (skillseval.SkillCountsFromChecklog → checklog.SkillFromTriggerDetail) 共契约，杜绝
		// 两侧手工镜像格式串漂移致被动触发信号静默丢失（minor-1：跨包 stringly-typed 契约）。
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
