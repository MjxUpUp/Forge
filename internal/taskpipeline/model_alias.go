package taskpipeline

import "github.com/MjxUpUp/Forge/internal/tasktypes"

// model_alias.go —— 任务数据模型的再导出门面（2026-09 代码普查 A3）。
//
// 纯数据模型（结构体、纯方法、门禁/阶段/状态常量、HMAC 完整性签名）已整体
// 下沉 internal/tasktypes 叶子包（直接依赖仅 stdlib/scoringtypes/nodeid；经 nodeid 的传递闭包另含 util 与 forgedata 两个包）；本文件用
// 类型别名 + 常量派生 + 函数委托把模型再导出，全仓既有调用点
// （cli/dashboard/datamerge/skilltrigger 等）零改动。执法不在模型里：HARD 门禁
// 执行器（executor）、状态存取（SaveTaskState/ActiveTaskState）仍住本包——
// tasktypes 不得反向依赖本包（go list -deps 可验）。EnrichFinding 亦留本包：
// 其 ChangeHash 推导调 TaskFingerprint/GetHeadCommit（执行层）。

// 类型别名：taskpipeline.TaskState 与 tasktypes.TaskState 是同一类型。
type (
	TaskGate            = tasktypes.TaskGate
	AcceptanceCriterion = tasktypes.AcceptanceCriterion
	ExternalOrigin      = tasktypes.ExternalOrigin
	Decision            = tasktypes.Decision
	Blocker             = tasktypes.Blocker
	Finding             = tasktypes.Finding
	Artifact            = tasktypes.Artifact
	Assignment          = tasktypes.Assignment
	SessionLink         = tasktypes.SessionLink
	TaskState           = tasktypes.TaskState
	CrossRepoImpact     = tasktypes.CrossRepoImpact
	TaskGateResult      = tasktypes.TaskGateResult
	ReviewRound         = tasktypes.ReviewRound
	ChecklistItem       = tasktypes.ChecklistItem
	IntentEntry         = tasktypes.IntentEntry
	StateIntegrity      = tasktypes.StateIntegrity
	DesignPhase         = tasktypes.DesignPhase
	ArtifactRef         = tasktypes.ArtifactRef
	TaskOverrides       = tasktypes.TaskOverrides
	DocReview           = tasktypes.DocReview
	Lease               = tasktypes.Lease
)

// 常量派生（值在 tasktypes 单一定义，此处只是再导出名）。
const (
	GateImplement       = tasktypes.GateImplement
	GateVerify          = tasktypes.GateVerify
	GateComplete        = tasktypes.GateComplete
	TaskKindGeneric     = tasktypes.TaskKindGeneric
	AssignOffered       = tasktypes.AssignOffered
	AssignClaimed       = tasktypes.AssignClaimed
	AssignInputRequired = tasktypes.AssignInputRequired
	AssignDelivered     = tasktypes.AssignDelivered
	AssignFailed        = tasktypes.AssignFailed
	AssignCanceled      = tasktypes.AssignCanceled
	CrossRepoNone       = tasktypes.CrossRepoNone
	CrossRepoMulti      = tasktypes.CrossRepoMulti
	PhaseRequirement    = tasktypes.PhaseRequirement
	PhaseAPI            = tasktypes.PhaseAPI
	PhaseDatabase       = tasktypes.PhaseDatabase
	PhaseFrontend       = tasktypes.PhaseFrontend
	PhaseBackend        = tasktypes.PhaseBackend
	PhaseTest           = tasktypes.PhaseTest
)

// Error sentinels re-exported (defined once in tasktypes; taskpipeline tests
// and callers reference them through this facade).
//
// 错误哨兵再导出（定义在 tasktypes，taskpipeline 测试与调用方经此引用）。
var (
	ErrAssignmentEmptyAgent = tasktypes.ErrAssignmentEmptyAgent
	ErrAssignmentExists     = tasktypes.ErrAssignmentExists
	ErrAbandonNotClaimed    = tasktypes.ErrAbandonNotClaimed
	ErrAnswerNotInputReq    = tasktypes.ErrAnswerNotInputReq
	ErrCancelTerminal       = tasktypes.ErrCancelTerminal
	ErrClaimNotOffered      = tasktypes.ErrClaimNotOffered
	ErrClaimWrongAgent      = tasktypes.ErrClaimWrongAgent
	ErrDeliverNotClaimed    = tasktypes.ErrDeliverNotClaimed
	ErrFailNotClaimed       = tasktypes.ErrFailNotClaimed
	ErrNoAssignment         = tasktypes.ErrNoAssignment
	ErrQuestionNotClaimed   = tasktypes.ErrQuestionNotClaimed
	ErrReopenNotDelivered   = tasktypes.ErrReopenNotDelivered
)

// DefaultGates delegates to the leaf model's gate list.
//
// DefaultGates 委托叶子模型返回标准门禁清单（单一事实源在 tasktypes）。
func DefaultGates() []TaskGate { return tasktypes.DefaultGates() }

// GateByID delegates to the leaf model's gate lookup.
//
// GateByID 委托叶子模型按 ID 查 gate。
func GateByID(id string) *TaskGate { return tasktypes.GateByID(id) }

// GateIDs delegates to the leaf model's ordered gate-ID list.
//
// GateIDs 委托叶子模型返回有序 gate ID 清单。
func GateIDs() []string { return tasktypes.GateIDs() }

// AllDesignPhases delegates to the leaf model's phase list.
//
// AllDesignPhases 委托叶子模型返回全部设计阶段。
func AllDesignPhases() []DesignPhase { return tasktypes.AllDesignPhases() }

// EnrichFinding stamps the review context (Round + ChangeHash — see Finding)
// onto a finding about to be recorded.
//
// EnrichFinding 把审查上下文（Round + ChangeHash——见 Finding）打到即将记录的
// finding 上。best-effort 且 fail-open：非 git 目录或 hash 出错时 ChangeHash 留空，
// 绝不阻断记录。仅当 finding 属于非当前周期（导入/回填）时调用方才显式传
// Round/ChangeHash；零值字段一律在此推导。
func EnrichFinding(root string, s *TaskState, f *Finding) {
	if f.Round == 0 {
		f.Round = len(s.ReviewRounds) + 1
	}
	if f.ChangeHash == "" {
		if h, _, err := TaskFingerprint(root, s, GetHeadCommit(root)); err == nil {
			f.ChangeHash = h
		}
	}
}
