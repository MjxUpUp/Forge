// Package act implements the Act feedback arm of PDCA: persists evidence-driven
// conclusions for each completed task in structured form, feeding them to
// session-retrospective / agent review to prevent experience loss.
//
// Design principle (counters the LLM-judge blind spot): every conclusion field
// comes from deterministic data (checklog run-evidence + scoring), not agent
// narrative. A task may be "high-score but Unverified" (agent self-claims done,
// zero run-evidence) — exactly the Tenure 0.000 blind spot research flagged;
// RetrospectiveNudge fires on evidence strength (not score alone).
//
// Package act 实现 PDCA 的 Act 反馈臂：把每个完成任务的证据驱动结论结构化落盘，
// 喂给 session-retrospective / agent 回顾，防"经验流失"。
//
// 设计原则（对冲 LLM-judge 盲区）：结论字段全来自 deterministic 数据（checklog 实跑证据 +
// 评分），非 agent 叙述。一个任务可能"高分但 Unverified"（agent 自述完成、零实跑证据）——
// 这正是研究指出的 Tenure 0.000 盲区，RetrospectiveNudge 据证据强度（非仅分数）触发回顾。
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
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

var mu sync.Mutex

// Conclusion is a traceable conclusion for a completed task — score + evidence
// strength + acceptance pass-rate + low-score dimensions. All fields are aggregated
// from deterministic sources (scoring/checklog/TaskState), for session-retrospective
// consumption: reviewing "how much run-evidence backs this completion claim".
//
// Conclusion 是一个完成任务的可追溯结论——score + 证据强度 + 验收通过率 + 低分维度。
// 全字段从 deterministic 来源聚合（评分/checklog/TaskState），供 session-retrospective
// 消费：回顾"这次完成声明有多少实跑证据支撑"，而非靠 agent 临结束回忆。
type Conclusion struct {
	TaskRef   string `json:"task_ref"`
	SessionID string `json:"session_id,omitempty"`
	// 0-100; 0 when unscored.
	Score float64 `json:"score"`           // 0-100；未评分时 0
	Grade string  `json:"grade,omitempty"` // A/B/C/D/F
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
	// Scoring dimensions below 70.
	LowDimensions []string `json:"low_dimensions,omitempty"` // <70 的评分维度
	// DimScores carries EVERY dimension's raw score (not just the <70 binary). Noise-band
	// consumers (recurrence hardening) need the number: a 67 and a 40 are both "low" to
	// LowDimensions, but only the 40 is clearly low once the 0-3pt boundary flap around the
	// 70 cut is discounted (AutoDesign margin calibration). Filled by BuildConclusion when
	// score != nil; legacy conclusions (written before this field) unmarshal to nil —
	// consumers must fall back to LowDimensions.
	//
	// DimScores 携带每个维度的原始分（而非只有 <70 二值）。噪声带消费方（复发升硬）
	// 需要数字：67 和 40 在 LowDimensions 里都是「低」，但按 AutoDesign margin 校准，
	// 70 切线附近 0-3 分的边界抖动折价后只有 40 是明确低。BuildConclusion 在 score != nil
	// 时填充；存量结论（早于该字段）反序列化为 nil——消费方须回落 LowDimensions。
	DimScores   []DimScore `json:"dim_scores,omitempty"`
	CompletedAt time.Time  `json:"completed_at"`
	// RetrospectiveNudge: weak evidence (Unverified/Weak) or low score (<70) → true.
	// Drives session-retrospective to review this completion claim at session end —
	// especially the "high-score but weak-evidence" blind spot (score cannot tell whether
	// the agent actually verified anything).
	//
	// RetrospectiveNudge：证据弱（Unverified/Weak）或低分（<70）→ true。驱动 session-retrospective
	// 在会话结束回顾这次的完成声明——尤其"高分但证据弱"的盲区（分数看不出 agent 是否真验证过）。
	RetrospectiveNudge bool `json:"retrospective_nudge"`
	// DesignPhases holds the design phases inferred by inferDesignPhases (e.g.
	// requirement/api/backend). Used for phase-aware health reports (phase_pass_rate)
	// and loop integration.
	//
	// DesignPhases 是 inferDesignPhases 推断出的设计阶段（如 requirement/api/backend）。
	// 用于 phase-aware 健康报告（phase_pass_rate）和回路接入。
	DesignPhases []string `json:"design_phases,omitempty"`
}

// DimScore is one scoring dimension's raw score — the number behind the <70 binary, so
// noise-band consumers can tell a 40 (clearly low) from a 67 (boundary flap).
//
// DimScore 是单个评分维度的原始分——<70 二值背后的数字，让噪声带消费方能区分
// 40（明确低）与 67（边界抖动）。
type DimScore struct {
	Dimension string  `json:"dimension"`
	Score     float64 `json:"score"`
}

// BuildConclusion is a pure function: it aggregates score + evidence chain + acceptance
// results into a Conclusion. Does not touch disk, easy to unit-test. Decoupled from
// taskpipeline — the caller (task.go) extracts raw values from TaskState and passes them
// in, avoiding circular dependencies.
//
// BuildConclusion 是纯函数：从评分 + 证据链 + 验收结果聚合出 Conclusion。不碰磁盘，
// 便于单测。解耦于 taskpipeline——调用方（task.go）从 TaskState 提取原始值传入，避免循环依赖。
func BuildConclusion(
	taskRef, sessionID string,
	score *scoringtypes.ScoreResult,
	ec checklog.EvidenceChain,
	acceptancePass, acceptanceTotal int,
	completedAt time.Time,
	designPhases []string,
) Conclusion {
	c := Conclusion{
		TaskRef:         taskRef,
		SessionID:       sessionID,
		AcceptancePass:  acceptancePass,
		AcceptanceTotal: acceptanceTotal,
		CompletedAt:     completedAt,
		Strength:        ec.Strength().String(),
		Ratio:           ec.Ratio(),
		Deterministic:   ec.Deterministic,
		AgentClaim:      ec.AgentClaim,
		DesignPhases:    designPhases,
	}
	if score != nil {
		c.Score = score.Overall
		c.Grade = score.Grade
		c.DimScores = make([]DimScore, 0, len(score.Dimensions))
		for _, d := range score.Dimensions {
			c.DimScores = append(c.DimScores, DimScore{Dimension: string(d.Dimension), Score: float64(d.Score)})
			if d.Score < 70 {
				c.LowDimensions = append(c.LowDimensions, string(d.Dimension))
			}
		}
	}
	strength := ec.Strength()
	// Act trigger: weak evidence (claim mostly agent self-narrated) or low score — both
	// deserve review. A clean Strong-and->=70 completion is not nudged (no lesson to
	// distill), avoiding noise.
	//
	// Act 触发：证据弱（声明主要靠 agent 自述）或低分——两者都值得回顾。Strong 且>=70 的干净
	// 完成不 nudge（无教训可沉淀），避免噪声。
	c.RetrospectiveNudge = strength == checklog.Unverified || strength == checklog.Weak || (score != nil && score.Overall < 70)
	return c
}

// Append appends one conclusion to p.ActConclusionsPath() (append-only, thread-safe).
// Isomorphic to checklog: JSONL, one entry per line, accumulates across tasks (not
// cleared at task start — conclusions are historical deposits).
//
// Append 把一条结论追加到 p.ActConclusionsPath()（append-only，线程安全）。
// 与 checklog 同构：JSONL，每行一条，跨任务累积（不在 task start 清空——结论是历史沉淀）。
func Append(p *forgedata.Project, c *Conclusion) error {
	mu.Lock()
	defer mu.Unlock()

	if c.CompletedAt.IsZero() {
		c.CompletedAt = time.Now()
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

// LoadAll reads all conclusions in chronological order. Returns nil if file absent.
// Uses bufio.Reader to read line-by-line (no Scanner 1MB single-line cap): a corrupted
// or abnormally large line is skipped, never failing the whole aggregation —
// dashboard/status/health all consume it, a single bad line should not turn the table 500.
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
			// json tolerates trailing newline (trailing whitespace), no trim needed; corrupted or
			// oversized lines that fail Unmarshal are skipped.
			//
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
	// Stable sort by completion time (append order is usually chronological already,
	// but explicit sort guards against concurrent or manual-edit reordering).
	//
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

// Directive returns a one-line action instruction for the agent when RetrospectiveNudge
// fires (printed by task complete). Returns empty string when Strong and >=70 (silent,
// no noise). The directive is anchored to deterministic numbers, not narrative.
//
// Directive 返回 RetrospectiveNudge 时给 agent 的一行行动指令（供 task complete 打印）。
// Strong 且>=70 时返回空串（静默，不发噪声）。指令锚定 deterministic 数字，非叙述。
func (c Conclusion) Directive() string {
	if !c.RetrospectiveNudge {
		return ""
	}
	var reason string
	switch c.Strength {
	case checklog.Unverified.String(), checklog.Weak.String():
		reason = fmt.Sprintf("完成声明证据 %s（ratio=%.2f, deterministic=%d/agent-claim=%d）——deterministic 证据不足，核查声称的验证是否真发生过",
			c.Strength, c.Ratio, c.Deterministic, c.AgentClaim)
	default:
		// Reaching here means low-score trigger (strength=Strong/NoData but score<70).
		//
		// 走到这说明是低分触发（strength=Strong/NoData 但 score<70）
		reason = fmt.Sprintf("任务评分 %.0f (%s)", c.Score, c.Grade)
	}
	if len(c.LowDimensions) > 0 {
		reason += "，低分维度：" + strings.Join(c.LowDimensions, "/")
	}
	return "→ session-retrospective: " + reason + "。回顾根因并按载体决策树沉淀（防再犯）。`forge act show` 看结构化结论。"
}
