package checklog

// EvidenceChain 把一个任务散落在 checklog.jsonl（含归档历史）里的证据条目，
// 聚成"声称做了哪些验证、其中多少有 deterministic 证据"的结构化视图。
//
// 它是路线 1（证据链底座）的核心产出：review 子 agent 和评分不再只能看 diff，
// 而是先看本任务是否有足够 deterministic 证据支撑"完成"声明——对冲 LLM-judge
// 看不出"agent 跳过前置就声明完成"的盲区。
type EvidenceChain struct {
	TaskRef string
	// Entries 按时间序（来自 LoadForTask）。Source 为空的旧条目按 SourceForCheck
	// 在分桶时兜底推断，但 Entries 内保留原值（不回填），调用方需显式推断时自理。
	Entries       []Entry
	Deterministic int // Source=deterministic（含空 Source 兜底）的条目数
	AgentClaim    int // Source=agent-claim 的条目数
}

// Total returns the total number of evidence entries (deterministic + agent-claim).
func (ec EvidenceChain) Total() int {
	return ec.Deterministic + ec.AgentClaim
}

// Ratio returns the deterministic share of evidence: Deterministic/Total.
// 1.0 = fully deterministic-backed; 0.0 = no deterministic evidence. Returns 0
// when Total==0 — callers should check Strength()==NoData first to distinguish
// "no evidence" from "all agent-claim".
func (ec EvidenceChain) Ratio() float64 {
	if ec.Total() == 0 {
		return 0
	}
	return float64(ec.Deterministic) / float64(ec.Total())
}

// EvidenceStrength 把 Ratio 离散成 review 可据以行动的档位。重点不是数字本身，
// 而是它该"驱动"什么：一个"完成"声明主要靠 agent 自述撑着（Weak/Unverified），
// 正是 LLM-judge 盲区所在（agent 跳过真实验证就声明完成）——此时 reviewer 必须
// 核查声称的验证是否真发生过，而不只读 diff。Strong=deterministic 占多数，声明可信。
//
// 档位（阈值 0.5 = "deterministic 占多数"）：
//   - NoData:     无任何证据（total 0）——中性，无可校准。
//   - Unverified: 有 agent-claim 但零 deterministic——声明全无实跑支撑，最高信号盲区。
//   - Weak:       有 deterministic 但 agent-claim 占多数（ratio<0.5）。
//   - Strong:     deterministic 占多数（ratio>=0.5）。
type EvidenceStrength int

const (
	NoData EvidenceStrength = iota
	Unverified
	Weak
	Strong
)

// String returns a human-readable band name for trace/review-status output.
func (s EvidenceStrength) String() string {
	switch s {
	case NoData:
		return "NoData"
	case Unverified:
		return "Unverified"
	case Weak:
		return "Weak"
	case Strong:
		return "Strong"
	}
	return "Unknown"
}

// Strength 把证据链分到 review 可行动的档位。语义与阈值见 EvidenceStrength 文档。
func (ec EvidenceChain) Strength() EvidenceStrength {
	if ec.Total() == 0 {
		return NoData
	}
	if ec.Deterministic == 0 && ec.AgentClaim > 0 {
		return Unverified
	}
	if ec.Ratio() < 0.5 {
		return Weak
	}
	return Strong
}

// BuildEvidenceChain 是纯函数：对已属于某任务的 entries 按来源分桶。Source 为空
// 的条目（旧数据，或未改造的记录点写入）按 SourceForCheck 兜底，保证旧 checklog
// 也能正确分桶，底座上线无需回填历史。
func BuildEvidenceChain(entries []Entry, taskRef string) EvidenceChain {
	ec := EvidenceChain{TaskRef: taskRef, Entries: entries}
	for _, e := range entries {
		// Advisory/meta checks record OBSERVATIONS, not verification outcomes — they
		// must NOT count toward evidence strength. scope-drift is an advisory signal
		// (agent changed unplanned source); counting it as "deterministic verification"
		// would inflate Strength and mask the very blind-spot EvidenceChain exists to
		// surface. The entry still lands in Entries (forge trace shows it); only the
		// bucket counts skip it. Drift is also typically a NEGATIVE signal — treating
		// it as positive evidence would be doubly wrong.
		// cheat-scan is the same kind of advisory observation (mechanically-suspected
		// AI-cheat patterns) — a hit is a negative signal, not verification evidence.
		if e.Check == CheckScopeDrift || e.Check == CheckCheatScan {
			continue
		}
		src := e.Source
		if src == "" {
			src = SourceForCheck(e.Check)
		}
		if src == EvidenceAgentClaim {
			ec.AgentClaim++
		} else {
			ec.Deterministic++
		}
	}
	return ec
}

// ForTask 从磁盘加载一个任务的全部证据（含归档 checklog-*.jsonl）并聚合。
// 等价于 LoadForTask + BuildEvidenceChain。当前消费者：forge trace；预留给
// 未来评分/review 子 agent 一行取到证据链（避免各自重复 LoadForTask+分桶）。
func ForTask(root, taskRef string) (EvidenceChain, error) {
	entries, err := LoadForTask(root, taskRef)
	if err != nil {
		return EvidenceChain{}, err
	}
	return BuildEvidenceChain(entries, taskRef), nil
}
