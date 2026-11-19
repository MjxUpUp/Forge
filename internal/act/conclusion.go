// Package act implements the Act feedback arm of PDCA: persists evidence-driven conclusions for each completed task in structured form, feeding them to session-retrospective / agent review to prevent experience loss.
//
// Package act 实现 PDCA 的 Act 反馈臂：把每个完成任务的证据驱动结论结构化落盘，
// 喂给 session-retrospective / agent 回顾，防"经验流失"。
//
// 设计原则（对冲 LLM-judge 盲区）：结论字段全来自 deterministic 事实（checklog 实跑证据 +
// 任务事实清单），非 agent 叙述。一个任务可能"看起来很干净但 Unverified"（agent 自述完成、
// 零实跑证据）——这正是研究指出的 Tenure 0.000 盲区，RetrospectiveNudge 只按证据强度触发回顾。
package act

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/nodestamp"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

var mu sync.Mutex

// strongRatioFloor 是 checklog.Strength 的 Strong 档 ratio 阈值（Ratio >= 0.5 → Strong，
// 否则 Weak）在 act 侧的镜像常量——EscapeCapped 的推导依赖它。checklog 内部以字面量
// 编码该阈值且未导出；此处镜像由 TestEscapeCapped_PinsChecklogStrength 守护测试对
// checklog.EvidenceChain.Strength() 的实际行为交叉钉住：checklog 改阈值 ⇒ 守护测试
// 红 ⇒ 强制重新推导，绝不静默漂移（惯例：镜像派生自单一真相源并被钉住，不手抄即忘）。
const strongRatioFloor = 0.5

// EscapeCapped reports whether this conclusion's Weak strength came from the
// verification-class escape-hatch cap — i.e. it would be Strong without the
// hatch (Ratio >= 0.5), so its "weak evidence" is an escape TAX already
// collected at scoring time, not a real evidence gap. Derived purely from
// persisted fields (Strength + Ratio): Weak && Ratio >= 0.5 ⟺ capped, because
// ratio >= 0.5 rates Strong unless EscapeDowngradedStrength caps it. Works
// retroactively on stored conclusions — zero schema change, zero migration.
//
// EscapeCapped 报告本结论的 Weak 是否来自验证类逃生舱降档——即若无逃生舱本该 Strong
// （Ratio >= 0.5），其「证据弱」是完成时刻已收的逃生【税】，不是真证据缺口。纯由
// 持久化字段（Strength + Ratio）推导：Weak 且 Ratio >= 0.5 ⟺ 被降档，因为 ratio >= 0.5
// 本评 Strong、只有 EscapeDowngradedStrength 会把它压回 Weak。对存量结论向后生效
// ——零 schema 变更、零迁移。消费方：health.NudgeActionable（告警分级，2026-09）。
func (c Conclusion) EscapeCapped() bool {
	return c.Strength == checklog.Weak.String() && c.Ratio >= strongRatioFloor
}

// Conclusion is a traceable conclusion for a completed task — evidence strength + acceptance facts.
//
// Conclusion 是一个完成任务的可追溯结论——证据强度 + 判据事实（通过数/先失败/评审轮次/
// 逃生次数）。全字段从 deterministic 来源聚合（checklog/TaskState 事实清单），供
// session-retrospective 消费：回顾"这次完成声明有多少实跑证据支撑"，而非靠 agent 临结束
// 回忆。刻意不携带任何合成分：要量化就数事实。
type Conclusion struct {
	TaskRef   string `json:"task_ref"`
	SessionID string `json:"session_id,omitempty"`
	// Strong/Weak/Unverified/NoData (checklog.EvidenceStrength.String).
	Strength string `json:"strength"` // Strong/Weak/Unverified/NoData（checklog.EvidenceStrength.String）
	// deterministic/total; 0 when total=0.
	Ratio float64 `json:"ratio"` // deterministic/total；total=0 时 0
	// Count of run (hook/gate) evidence entries.
	Deterministic int `json:"deterministic"` // 实跑（hook/gate）证据条目数
	// Count of agent self-claim entries.
	AgentClaim int `json:"agent_claim"` // agent 自述条目数
	// Number of verify-acceptance passed entries.
	AcceptancePass int `json:"acceptance_pass"` // verify-acceptance 通过条目数
	// Total number of acceptance criteria.
	AcceptanceTotal int `json:"acceptance_total"` // 验收标准总数
	// RedFirstTrials counts registered red-first criteria; RedFirstClosed counts the
	// ones that already turned green on the same command.
	//
	// RedFirstTrials / RedFirstClosed：先失败证明的登记数 / 已转绿数（事实，不是率）。
	RedFirstTrials int `json:"red_first_trials,omitempty"`
	RedFirstClosed int `json:"red_first_closed,omitempty"`
	// ReviewRounds is the number of review-pass rounds recorded on the task.
	//
	// ReviewRounds：评审轮次数（同一 reviewer 复审的轮次史）。
	ReviewRounds int `json:"review_rounds,omitempty"`
	// Escapes counts escape-hatch overrides plus waived checks on the task.
	//
	// Escapes：逃生舱（override / 豁免）次数——被免掉的门禁数，不是分数。
	Escapes     int       `json:"escapes,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
	// RetrospectiveNudge: weak evidence (Unverified/Weak) → true.
	//
	// RetrospectiveNudge：证据弱（Unverified/Weak）→ true。驱动 session-retrospective
	// 在会话结束回顾这次的完成声明——"看起来完成但没有实跑证据"正是分数看不出的盲区。
	RetrospectiveNudge bool `json:"retrospective_nudge"`
	// DesignPhases holds the design phases inferred by inferDesignPhases (e.g. requirement/api/backend). Used for phase-aware health reports (phase_pass_rate) and loop integration.
	//
	// DesignPhases 是 inferDesignPhases 推断出的设计阶段（如 requirement/api/backend）。
	// 用于 phase-aware 健康报告（phase_pass_rate）和回路接入。
	DesignPhases []string `json:"design_phases,omitempty"`
	// Stamp 携带机器归因（node_id/seq/ts_hlc/sig），由 Append 经 nodestamp.Next 落章——
	// 存量行与 fail-open 时为零值。拍平进本对象。
	nodestamp.Stamp
}

// BuildConclusion is a pure function: it aggregates the task fact sheet + evidence chain into a Conclusion.
//
// BuildConclusion 是纯函数：从任务事实清单 + 证据链聚合出 Conclusion。不碰磁盘，便于
// 单测。解耦于 taskpipeline——调用方传入已算好的 fact sheet（TaskEvidence），避免循环依赖。
func BuildConclusion(
	taskRef, sessionID string,
	ev *tasktypes.TaskEvidence,
	ec checklog.EvidenceChain,
	completedAt time.Time,
	designPhases []string,
) Conclusion {
	c := Conclusion{
		TaskRef:       taskRef,
		SessionID:     sessionID,
		CompletedAt:   completedAt,
		Strength:      ec.Strength().String(),
		Ratio:         ec.Ratio(),
		Deterministic: ec.Deterministic,
		AgentClaim:    ec.AgentClaim,
		DesignPhases:  designPhases,
	}
	if ev != nil {
		c.AcceptancePass = ev.CriteriaPassed
		c.AcceptanceTotal = ev.Criteria
		c.RedFirstTrials = ev.RedFirstTrials
		c.RedFirstClosed = ev.RedFirstClosed
		c.ReviewRounds = ev.ReviewRounds
		c.Escapes = ev.Escapes
	}
	strength := ec.Strength()
	// Act 触发：证据弱（声明主要靠 agent 自述）才值得回顾。Strong 的干净完成不 nudge
	//（无教训可沉淀），避免噪声。
	c.RetrospectiveNudge = strength == checklog.Unverified || strength == checklog.Weak
	return c
}

// Append appends one conclusion to p.ActConclusionsPath() (append-only, thread-safe).
//
// Append 把一条结论追加到 p.ActConclusionsPath()（append-only，线程安全）。
// 与 checklog 同构：JSONL，每行一条，跨任务累积（不在 task start 清空——结论是历史沉淀）。
func Append(p *forgedata.Project, c *Conclusion) error {
	mu.Lock()
	defer mu.Unlock()

	if c.CompletedAt.IsZero() {
		c.CompletedAt = time.Now()
	}
	// 机器归因戳（node-identity.md §4）：仅当调用方留零值——import/merge 保留源节点戳。
	if c.Stamp == (nodestamp.Stamp{}) {
		c.Stamp = nodestamp.Next()
	}
	if err := os.MkdirAll(p.ActDir(), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(p.ActConclusionsPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// LoadAll reads all conclusions in chronological order.
//
// LoadAll 按时序读所有 conclusion。文件不存在返回 nil。
// 用 bufio.Reader 逐行读（无 Scanner 的 1MB 单行上限）：单行损坏或异常超大只跳过该行，
// 不让整条聚合失败——dashboard/status/health 都消费它，单行异常不应让全表变 500。
func LoadAll(p *forgedata.Project) ([]Conclusion, error) {
	f, err := os.Open(p.ActConclusionsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var cs []Conclusion
	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			var c Conclusion
			// json 容忍行尾换行（trailing whitespace），无需 trim；损坏/超大行 Unmarshal 失败则跳过。
			if json.Unmarshal([]byte(line), &c) == nil {
				cs = append(cs, c)
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return nil, readErr
			}
			break
		}
	}
	// 按完成时间稳定排序（append 顺序通常已时序，但显式排序防并发/手动编辑乱序）
	slices.SortStableFunc(cs, func(a, b Conclusion) int {
		return a.CompletedAt.Compare(b.CompletedAt)
	})
	return cs, nil
}

// Latest returns the most recent conclusion, or nil if none.
//
// Latest 返回最近一条 conclusion，无则 nil。
func Latest(p *forgedata.Project) (*Conclusion, error) {
	cs, err := LoadAll(p)
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, nil
	}
	return &cs[len(cs)-1], nil
}

// Directive returns a one-line action instruction for the agent when RetrospectiveNudge fires (printed by task complete).
//
// Directive 返回 RetrospectiveNudge 时给 agent 的一行行动指令（供 task complete 打印）。
// 不 nudge（证据 Strong）时返回空串（静默，不发噪声）。指令锚定 deterministic 数字，
// 非叙述；触发条件只有证据弱，故不另设分支。
func (c Conclusion) Directive() string {
	if !c.RetrospectiveNudge {
		return ""
	}
	reason := fmt.Sprintf("完成声明证据 %s（ratio=%.2f, deterministic=%d/agent-claim=%d）——deterministic 证据不足，核查声称的验证是否真发生过",
		c.Strength, c.Ratio, c.Deterministic, c.AgentClaim)
	// 收尾落章指令（2026-09 证据式 ack）：回顾做完结环——具体命令放 forge 侧生成
	//（canonical skill 按 §13 零反向依赖，只载环境中性原则，见其 5.5 步）。载体
	// 枚举从 Carriers 单一真相源派生，不做第三份手抄镜像。
	return "→ session-retrospective: " + reason +
		"。回顾根因并按载体决策树沉淀（防再犯），完成后 `forge act retro-done --ref " + c.TaskRef +
		" --carrier <" + strings.Join(Carriers, "|") + "> [--lesson <教训>]` 落 ack（否则 nudge 留面板）。`forge act show` 看结构化结论。"
}
