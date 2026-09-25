// disposition.go — evidence-based ack of RetrospectiveNudge.
//
// disposition.go —— RetrospectiveNudge 的证据式 ack。
//
// 问题（2026-09）：会话结束 hook 提醒 agent 回顾（Directive），但「回顾是否真发生」
// 不可观测——nudge 要么 14 天后静默过期，要么一直挂面板。错过的回顾与已处理的回顾
// 不可区分。
//
// 最小诚实信号：session-retrospective 收尾时调 `forge act retro-done`，按结论
// identity（TaskRef + SessionID + CompletedAt 三元组）追加一行 Disposition。被 ack
// 的 nudge 退出 dashboard 告警（NudgeRecent/NudgeActionable——见 health.SummarizeAtAcked），
// NudgeCount（全量真相）与其余聚合不动。agent 忘记调用 ⇒ nudge 留在面板——
// 「错过的回顾」第一次可见，这恰是特性而非缺陷。
//
// 与 checklog 证据链同理：ack 本身也是 agent 声明，但其价值不依赖可信——谎报 ack
// 只会让自己少看一条告警，与谎报「验证过了」的代价结构不同（前者自损，后者损系统）。
package act

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/nodestamp"
)

// Carriers is the closed vocabulary of retrospective carriers (single source of
// truth) — mirrors the session-retrospective skill's carrier decision tree
// (memory / skill / CLAUDE.md / code / hook / CI) plus `none` (lesson logged,
// no carrier warranted). CLI validates against this list; dashboard only
// displays. Unknown values are rejected at the write boundary to keep the data
// column clean.
//
// Carriers 是回顾载体的封闭词汇表（单一真相源）——镜像 session-retrospective skill
// 的载体决策树（memory / skill / CLAUDE.md / code / hook / CI）加 `none`（有教训
// 但无需落载体）。CLI 按此校验；dashboard 只展示。未知值在写边界拒绝，保持数据列干净。
var Carriers = []string{`memory`, `skill`, `claudemd`, `code`, `hook`, `ci`, `none`}

// IsValidCarrier reports whether c is a known carrier (case-sensitive, closed set).
//
// IsValidCarrier 报告 c 是否已知载体（大小写敏感、封闭集合）。
func IsValidCarrier(c string) bool {
	for _, k := range Carriers {
		if c == k {
			return true
		}
	}
	return false
}

// Disposition is one retro-done ack: identifies the acked Conclusion by the
// identity triple (TaskRef + SessionID + CompletedAt — all copied from the
// conclusion at ack time) plus the retrospective outcome (carrier + lesson).
//
// Disposition 是一条 retro-done ack：以 identity 三元组（TaskRef + SessionID +
// CompletedAt——ack 时从结论逐字复制）标识被 ack 的 Conclusion，附回顾产出
// （载体 + 教训）。三元组而非裸 ref：同 ref 任务重做（新 session 新完成时刻）不得
// 被旧 ack 误伤——acked 的是「那一次结论」，不是「那个名字」。
type Disposition struct {
	TaskRef   string `json:"task_ref"`
	SessionID string `json:"session_id,omitempty"`
	// CompletedAt of the ACKED conclusion (identity triple member) — not the ack time.
	CompletedAt time.Time `json:"completed_at"` // 被 ack 结论的完成时刻（identity 成员），非 ack 时刻
	Carrier     string    `json:"carrier"`      // Carriers 之一
	// Lesson is the one-line distilled lesson (optional, human-facing).
	Lesson     string    `json:"lesson,omitempty"` // 一句话提炼的教训（可选，人读）
	RecordedAt time.Time `json:"recorded_at"`
	// Stamp 携带机器归因（node_id/seq/ts_hlc/sig），由 AppendDisposition 经
	// nodestamp.Next 落章——与 Conclusion.Append 同构。存量/导入行为零值。
	nodestamp.Stamp
}

// AppendDisposition appends one ack to p.ActDispositionsPath() (append-only,
// thread-safe) — same JSONL discipline as Append for conclusions: cross-task
// cumulative, never cleared, zero-valued stamps filled via nodestamp.Next.
//
// AppendDisposition 把一条 ack 追加到 p.ActDispositionsPath()（append-only、线程
// 安全）——与结论的 Append 同构：JSONL、每行一条、跨任务累积、零值戳经
// nodestamp.Next 落章。
func AppendDisposition(p *forgedata.Project, d *Disposition) error {
	mu.Lock()
	defer mu.Unlock()

	if d.RecordedAt.IsZero() {
		d.RecordedAt = time.Now()
	}
	if d.Stamp == (nodestamp.Stamp{}) {
		d.Stamp = nodestamp.Next()
	}
	if err := os.MkdirAll(p.ActDir(), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(p.ActDispositionsPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// LoadDispositions reads all acks in recorded order. Missing file → (nil, nil)
// (legitimate empty state, same contract as LoadAll).
//
// LoadDispositions 按记录序读全部 ack。文件缺失返回 (nil, nil)（合法空状态，与
// LoadAll 契约一致）。损坏/超大行跳过不致命——看板不该因单行脏数据 500。
func LoadDispositions(p *forgedata.Project) ([]Disposition, error) {
	f, err := os.Open(p.ActDispositionsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var ds []Disposition
	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			var d Disposition
			// json 容忍行尾换行；损坏/超大行 Unmarshal 失败则跳过（与 LoadAll 同构）。
			if json.Unmarshal([]byte(line), &d) == nil {
				ds = append(ds, d)
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return nil, readErr
			}
			break
		}
	}
	return ds, nil
}

// AckMatcher builds the conclusion predicate for health.SummarizeAtAcked: a
// conclusion is acked iff its identity triple (TaskRef + SessionID +
// CompletedAt) matches some disposition. Empty SessionID on both sides matches
// (empty = empty); non-empty mismatch does not.
//
// AckMatcher 构造 health.SummarizeAtAcked 的结论谓词：某结论被 ack ⟺ 其 identity
// 三元组（TaskRef + SessionID + CompletedAt）与某条 disposition 匹配。双方
// SessionID 均空时命中（空=空）；非空不等不命中。
func AckMatcher(ds []Disposition) func(Conclusion) bool {
	return func(c Conclusion) bool {
		for _, d := range ds {
			if d.TaskRef == c.TaskRef && d.SessionID == c.SessionID && d.CompletedAt.Equal(c.CompletedAt) {
				return true
			}
		}
		return false
	}
}
