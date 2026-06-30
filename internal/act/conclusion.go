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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

var mu sync.Mutex

// filePath returns the act log path relative to project root.
func filePath(root string) string {
	return filepath.Join(root, ".forge", "act", "conclusions.jsonl")
}

// Conclusion 是一个完成任务的可追溯结论——score + 证据强度 + 验收通过率 + 低分维度。
// 全字段从 deterministic 来源聚合（评分/checklog/TaskState），供 session-retrospective
// 消费：回顾"这次完成声明有多少实跑证据支撑"，而非靠 agent 临结束回忆。
type Conclusion struct {
	TaskRef         string    `json:"task_ref"`
	SessionID       string    `json:"session_id,omitempty"`
	Score           float64   `json:"score"`                    // 0-100；未评分时 0
	Grade           string    `json:"grade,omitempty"`          // A/B/C/D/F
	Strength        string    `json:"strength"`                 // Strong/Weak/Unverified/NoData（checklog.EvidenceStrength.String）
	Ratio           float64   `json:"ratio"`                    // deterministic/total；total=0 时 0
	Deterministic   int       `json:"deterministic"`            // 实跑（hook/gate）证据条目数
	AgentClaim      int       `json:"agent_claim"`              // agent 自述条目数
	AcceptancePass  int       `json:"acceptance_pass"`          // verify-acceptance 通过条目数
	AcceptanceTotal int       `json:"acceptance_total"`         // 验收标准总数
	LowDimensions   []string  `json:"low_dimensions,omitempty"` // <70 的评分维度
	CompletedAt     time.Time `json:"completed_at"`
	// RetrospectiveNudge：证据弱（Unverified/Weak）或低分（<70）→ true。驱动 session-retrospective
	// 在会话结束回顾这次的完成声明——尤其"高分但证据弱"的盲区（分数看不出 agent 是否真验证过）。
	RetrospectiveNudge bool `json:"retrospective_nudge"`
}

// BuildConclusion 是纯函数：从评分 + 证据链 + 验收结果聚合出 Conclusion。不碰磁盘，
// 便于单测。解耦于 taskpipeline——调用方（task.go）从 TaskState 提取原始值传入，避免循环依赖。
func BuildConclusion(
	taskRef, sessionID string,
	score *scoringtypes.ScoreResult,
	ec checklog.EvidenceChain,
	acceptancePass, acceptanceTotal int,
	completedAt time.Time,
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
	}
	if score != nil {
		c.Score = score.Overall
		c.Grade = score.Grade
		for _, d := range score.Dimensions {
			if d.Score < 70 {
				c.LowDimensions = append(c.LowDimensions, string(d.Dimension))
			}
		}
	}
	strength := ec.Strength()
	// Act 触发：证据弱（声明主要靠 agent 自述）或低分——两者都值得回顾。Strong 且>=70 的干净
	// 完成不 nudge（无教训可沉淀），避免噪声。
	c.RetrospectiveNudge = strength == checklog.Unverified || strength == checklog.Weak || (score != nil && score.Overall < 70)
	return c
}

// Append 把一条结论追加到 .forge/act/conclusions.jsonl（append-only，线程安全）。
// 与 checklog 同构：JSONL，每行一条，跨任务累积（不在 task start 清空——结论是历史沉淀）。
func Append(root string, c *Conclusion) error {
	mu.Lock()
	defer mu.Unlock()

	if c.CompletedAt.IsZero() {
		c.CompletedAt = time.Now()
	}
	dir := filepath.Dir(filePath(root))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(filePath(root), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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

// LoadAll reads all conclusions in chronological order. Returns nil if absent.
func LoadAll(root string) ([]Conclusion, error) {
	f, err := os.Open(filePath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var cs []Conclusion
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var c Conclusion
		if err := json.Unmarshal(scanner.Bytes(), &c); err != nil {
			continue // skip malformed lines
		}
		cs = append(cs, c)
	}
	// 按完成时间稳定排序（append 顺序通常已时序，但显式排序防并发/手动编辑乱序）
	sort.SliceStable(cs, func(i, j int) bool {
		return cs[i].CompletedAt.Before(cs[j].CompletedAt)
	})
	return cs, scanner.Err()
}

// Latest returns the most recent conclusion, or nil if none.
func Latest(root string) (*Conclusion, error) {
	cs, err := LoadAll(root)
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, nil
	}
	return &cs[len(cs)-1], nil
}

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
		// 走到这说明是低分触发（strength=Strong/NoData 但 score<70）
		reason = fmt.Sprintf("任务评分 %.0f (%s)", c.Score, c.Grade)
	}
	if len(c.LowDimensions) > 0 {
		reason += "，低分维度：" + strings.Join(c.LowDimensions, "/")
	}
	return "→ session-retrospective: " + reason + "。回顾根因并按载体决策树沉淀（防再犯）。`forge act show` 看结构化结论。"
}
