package tasktypes

import "time"

// model.go —— TaskState 内嵌的域结构体（自 taskpipeline 各文件拆出随模型下沉，
// 2026-09 代码普查 A3）。纯数据 + 纯方法；操作这些结构的执行层逻辑
// （ClaimLease/doc gate/规格校验/覆盖裁决）仍住 taskpipeline。

// Lease is one node's claim on a task (TaskState.lease).
//
// Lease 是一个节点对任务的认领（TaskState.lease）。
type Lease struct {
	HolderNode string `json:"holder_node"`       // fnode_<32hex>
	TsHLC      string `json:"ts_hlc"`            // 认领时刻 HLC
	TTLSec     int64  `json:"ttl_sec"`           // 租约时长（秒）
	Fencing    int64  `json:"fencing"`           // 单调任期号（合并决胜）
	ClaimedAt  int64  `json:"claimed_at_unixms"` // 认领时刻墙钟毫秒（TTL 判定用）
}

// ExpiresAt is the moment the lease lapses (claimed + TTL) — the single source of the expiry formula; ActiveAt / LeaseStatus / the dashboard projection all derive from it.
//
// ExpiresAt 是租约过期时刻（认领 + TTL）——过期公式的唯一出处；ActiveAt /
// LeaseStatus / 看板投影都从它派生。
func (l *Lease) ExpiresAt() time.Time {
	if l == nil {
		return time.Time{} // 与 ActiveAt 的 nil-safe 对称——导出方法不留给调用方裸调用陷阱
	}
	return time.UnixMilli(l.ClaimedAt).Add(time.Duration(l.TTLSec) * time.Second)
}

// ActiveAt reports whether the lease is unexpired at now (the single "expiry means free" rule — LeaseStatus and the dashboard feed both derive from it, no second copy).
//
// ActiveAt 报告租约在 now 时刻是否未过期（「过期即自由」规则的唯一出处——
// LeaseStatus 与 dashboard feed 都从它派生，不留第二份拷贝）。
func (l *Lease) ActiveAt(now time.Time) bool {
	if l == nil {
		return false
	}
	return now.Before(l.ExpiresAt())
}

// DocReview is the L2 evidence recorded by `forge task doc-review` after a rubric review (doc-review skill).
//
// DocReview 是 `forge task doc-review` 在 rubric 评审（doc-review skill）后记录的
// L2 证据。回检者不能是产出者（防作弊纪律 1）；HeadCommit 把评审绑定到代码
// 快照——评审后改文档则失效（freshness，与 acceptance 同构）。
type DocReview struct {
	Passed      bool      `json:"passed"`
	RubricScore int       `json:"rubric_score"`
	Round       int       `json:"round"`
	Reviewer    string    `json:"reviewer,omitempty"`
	ReviewedAt  time.Time `json:"reviewed_at,omitempty"`
	HeadCommit  string    `json:"head_commit,omitempty"`
	// DocsFingerprint pins the reviewed content (sha256 over changed-doc paths + contents, see DocContentFingerprint).
	//
	// DocsFingerprint 钉住被评审的内容（变更文档路径+内容的 sha256，见
	// DocContentFingerprint）。只绑 HEAD 会漏掉评审后 complete 前的未提交
	// 修改；指纹补上该盲区。v1.43.0 之前的评审为空 → 视为未设置（仅查 HEAD）。
	DocsFingerprint string `json:"docs_fingerprint,omitempty"`
}

// ArtifactRef is TaskState's verifiable pointer to a spec file (I5): Path is DataDir-relative (portable across machines), Hash is the content sha256 (first 16 hex).
//
// ArtifactRef 是 TaskState 指向 spec 文件的可验证指针（I5）：Path 相对 DataDir
// （跨机可移植——project key 在不同机器映射到不同绝对 DataDir），Hash 是内容 sha256
// 前 16 hex。失配 = 引用建立后文件被手改——按漂移上浮，绝不静默。
type ArtifactRef struct {
	Path      string    `json:"path"`
	Hash      string    `json:"hash"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TaskOverrides holds the per-task escape-hatch settings.
//
// TaskOverrides 承载 per-task 逃生舱设置。优先于全局 env，是方案5 的「防泄漏」机制：
// 一个任务逃生（经 `forge task override`）不污染同 shell 的其他任务——全局 env
// FORGE_WORK_ACTIVITY / FORGE_TEST_COVERAGE / FORGE_SKILL_DECISIONS 仍作 CI/测试
// fallback，但 per-task override 是推荐路径。值"disable"= 禁用对应门禁。
//
// 用了验证类逃生舱 → checklog CheckEscapeHatch → evidence Strength cap Weak（让逃生
// 有代价，对冲「硬门禁 + 全局逃生舱 = 假硬门禁」反噬；2026-08 起证据缩放——
// ratio>=0.85 且 det>=20 的重证据任务豁免，见 checklog.EscapeDowngradedStrength）。
// work-activity（节奏门禁）永不 cap Strength。
type TaskOverrides struct {
	WorkActivity   string `json:"work_activity,omitempty"`   // "disable" 跳过 read-before-edit / work-activity 门禁
	TestCoverage   string `json:"test_coverage,omitempty"`   // "disable" 跳过 test-coverage 门禁
	AcceptanceGate string `json:"acceptance_gate,omitempty"` // "disable" 跳过 task-complete acceptance pre-flight 门禁
	SkillDecisions string `json:"skill_decisions,omitempty"` // "disable" 跳过 skill-decisions guardrail（改 SKILL.md 必须记决策）
	DocGate        string `json:"doc_gate,omitempty"`        // "disable" 跳过 task-complete doc pre-flight（输出→回检门禁；轮次上限后的放行须人工确认后走这里）
}

// DesignPhase identifies a design phase involved in a task.
//
// DesignPhase 标识任务涉及的设计阶段。
type DesignPhase string

const (
	PhaseRequirement DesignPhase = "requirement" // 需求设计（PRD / 需求文档）
	PhaseAPI         DesignPhase = "api"         // API 设计（OpenAPI / proto / 接口定义）
	PhaseDatabase    DesignPhase = "database"    // 数据库设计（migrations / schema）
	PhaseFrontend    DesignPhase = "frontend"    // 前端设计（组件/页面/路由）
	PhaseBackend     DesignPhase = "backend"     // 后端设计（services / domain / 业务逻辑）
	PhaseTest        DesignPhase = "test-design" // 测试用例设计（test 文件）
)

// AllDesignPhases returns every design phase.
//
// AllDesignPhases 返回全部设计阶段。
func AllDesignPhases() []DesignPhase {
	return []DesignPhase{
		PhaseRequirement, PhaseAPI, PhaseDatabase,
		PhaseFrontend, PhaseBackend, PhaseTest,
	}
}
