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

// BuildEvidenceChain 是纯函数：对已属于某任务的 entries 按来源分桶。Source 为空
// 的条目（旧数据，或未改造的记录点写入）按 SourceForCheck 兜底，保证旧 checklog
// 也能正确分桶，底座上线无需回填历史。
func BuildEvidenceChain(entries []Entry, taskRef string) EvidenceChain {
	ec := EvidenceChain{TaskRef: taskRef, Entries: entries}
	for _, e := range entries {
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
