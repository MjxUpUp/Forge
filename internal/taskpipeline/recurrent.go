package taskpipeline

// recurrent.go implements recurrence-driven advisory→hard promotion: the bridge between Forge's
// two drive modes (soft advisory vs hard blocking).
//
// recurrent.go 实现复发驱动的 advisory→hard 升档——Forge 两种驱动模式（软 advisory vs 硬 blocking）之间的桥。
//
// Problem this solves (dogfood + 近一周 multi-project data):
// advisory checks that only WARN (test-coverage at task-verify, scope-drift throughout) rely on
// agent self-discipline and systematically leak — 近一周 4 项目 52 任务里 testing 低分 ×27、scope 低分 ×29。
// But a blanket hard gate misfires: scope-impact recall is only ~44% (hardening would reject half
// of legitimate changes), and test-coverage hard on a small fix is a false positive.
//
// 本机制解决的问题（dogfood + 近一周多项目数据）：
// 只 WARN 的 advisory check（task-verify 的 test-coverage、全程的 scope-drift）靠 agent 自律，
// 系统性漏——近一周 4 项目 52 任务里 testing 低分 ×27、scope 低分 ×29。但一刀切硬门禁会误伤：
// scope 影响集召回率仅 ~44%（硬拦会拒一半合法改动），test-coverage 硬拦小修是假阳性。
//
// Balance = recurrence (per-project) AND severity (per-task); both axes must hold to harden:
//   - Recurrence axis: a scoring dimension that has gone low (<70) ≥ threshold times in this
//     project's completed-task history → it is a project-level systemic gap, the exact case where
//     "advisory relies on self-discipline" has already been proven to fail. New projects (<threshold
//     tasks) never recur → never harden → never false-positive on the unfamiliar.
//   - Severity axis: THIS task's drift/missing is non-trivial (missing>0 for test-coverage;
//     drift≥severe threshold for scope-drift). Single-file scope drift is a normal impact-prediction
//     miss and stays advisory even on recurrent projects.
//   - Both true → promote advisory to BLOCKED; otherwise stays advisory. Escape hatches still apply
//     (FORGE_TEST_COVERAGE / FORGE_RECURRENT_HARDEN), and CheckScopeDrift stays excluded from the
//     evidence chain (hardening only flips the gate verdict, never Strength).
//
// 平衡 = 复发轴（per-project）AND 严重度轴（per-task）；两轴同时成立才升硬：
//   - 复发轴：某评分维度在本项目已完成任务历史里低分（<70）≥ 阈值次 → 是项目级系统性缺口，
//     正是「advisory 靠自律」已被证明失效的场景。新项目（<阈值任务）永不复发→永不升硬→
//     永不对陌生项目假阳性。
//   - 严重度轴：本次 drift/缺失非轻微（test-coverage 为 missing>0；scope-drift 为 drift≥严重阈值）。
//     单文件 scope drift 是正常影响预测失误，即便在复发项目也保持 advisory。
//   - 两者皆真 → advisory 升 BLOCKED；否则保持 advisory。逃生舱仍生效
//     （FORGE_TEST_COVERAGE / FORGE_RECURRENT_HARDEN），CheckScopeDrift 仍不计入证据链
//     （升硬只翻 gate 裁定，绝不改 Strength）。

import (
	"os"
	"strconv"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// dimTesting / dimScope are the low-score dimension names recurrence hardening keys on, taken from
// the single source of truth (scoringtypes) so a rename there propagates here. Other dimensions
// (efficiency/tool-selection/...) are not hardened — they are scoring-only signals with no
// corresponding advisory gate to promote.
//
// dimTesting / dimScope 是复发升硬所键入的低分维度名，取自单一真相源（scoringtypes），
// 那边改名此处自动跟随。其他维度（efficiency/tool-selection/...）不升硬——它们是纯评分信号，
// 无对应的 advisory gate 可升。
var (
	dimTesting = string(scoringtypes.DimensionTesting)
	dimScope   = string(scoringtypes.DimensionScope)
)

// recurrentDimThresholdDefault is the default recurrence threshold: a dimension going low (<70) at
// least this many times in project history counts as a systemic gap. 3 aligns with the
// testCoverageHardGateThreshold intuition ("3 = systemic") and — critically — means a brand-new
// project with <3 completed tasks can never trip recurrence hardening (no false positives while the
// project has no track record to learn from).
//
// recurrentDimThresholdDefault 是默认复发阈值：某维度在本项目历史低分（<70）≥ 此数即视为系统性缺口。
// 3 对齐 testCoverageHardGateThreshold 的「3 即系统性」直觉，且——关键——意味着完成 <3 任务的新项目
// 永不触发复发升硬（项目无履历可学时不误伤）。
const recurrentDimThresholdDefault = 3

// scopeDriftSevereThreshold is the minimum out-of-scope source-file count for scope-drift to count
// as "severe" on the per-task axis. Single-file drift is a normal impact-prediction miss (recall
// ~44%) and must stay advisory even on recurrent projects; only multi-file drift — a signal the
// agent materially departed from its plan — is eligible for hardening.
//
// scopeDriftSevereThreshold 是 scope-drift 在 per-task 严重度轴上「严重」的最小超 scope 源文件数。
// 单文件 drift 是正常的影响预测失误（召回率 ~44%），即便在复发项目也须保持 advisory；
// 只有多文件 drift——agent 实质偏离计划的信号——才够格升硬。
const scopeDriftSevereThreshold = 3

const (
	// recurrentHardenDisableEnv turns off recurrence-driven hardening globally
	// (FORGE_RECURRENT_HARDEN=disable), reverting both test-coverage and scope-drift to pure
	// advisory. Unlike the gate-bypass escape hatches (FORGE_TEST_COVERAGE), this does NOT cap
	// Strength at Weak: it expresses "this project prefers advisory", not "this task skipped
	// verification" — the project was already in advisory mode, so there is nothing to discount.
	//
	// recurrentHardenDisableEnv 全局关闭复发升硬（FORGE_RECURRENT_HARDEN=disable），把 test-coverage
	// 与 scope-drift 都退回纯 advisory。与 gate-bypass 逃生舱（FORGE_TEST_COVERAGE）不同，它不 cap
	// Strength 到 Weak：它表达「本项目偏好 advisory」而非「本任务跳过验证」——项目本就在 advisory
	// 模式，无可打折。
	recurrentHardenDisableEnv = "FORGE_RECURRENT_HARDEN"
	// recurrentThresholdEnv overrides the recurrence threshold (FORGE_RECURRENT_THRESHOLD=N, N>0).
	//
	// recurrentThresholdEnv 覆盖复发阈值（FORGE_RECURRENT_THRESHOLD=N，N>0）。
	recurrentThresholdEnv = "FORGE_RECURRENT_THRESHOLD"
)

// lowDimCounts tallies how often each low-score dimension (<70) appears across a project's completed
// conclusions. Pure function over the conclusion slice (mirrors health.Summarize's lowCounts but is
// independently testable without building a full Summary). Used by executor's BLOCKED message,
// which surfaces the exact recurrence count so the agent sees why this task hardened.
//
// lowDimCounts 统计各低分维度（<70）在项目已完成结论里出现的次数。基于结论切片的纯函数
// （镜像 health.Summarize 的 lowCounts，但无需构造完整 Summary 即可独立单测）。供 executor 的
// BLOCKED 消息使用——消息里带出确切复发计数，让 agent 看清本次为何升硬。
func lowDimCounts(cs []act.Conclusion) map[string]int {
	counts := map[string]int{}
	for _, c := range cs {
		for _, d := range c.LowDimensions {
			counts[d]++
		}
	}
	return counts
}

// dimRecurrent reports whether dimension dim has gone low (<70) at least threshold times across the
// given conclusions — the recurrence axis. Returns false on empty input or threshold<=0 (fail-open).
// Callers feed loadConclusions(root) (which itself fails open on read errors).
//
// dimRecurrent 报告维度 dim 是否在给定结论里低分（<70）≥ threshold 次——复发轴。空输入或
// threshold<=0 返回 false（fail-open）。调用方传入 loadConclusions(root)（其自身对读取错误 fail-open）。
func dimRecurrent(cs []act.Conclusion, dim string, threshold int) bool {
	if threshold <= 0 || len(cs) == 0 {
		return false
	}
	return lowDimCounts(cs)[dim] >= threshold
}

// recurrentThreshold returns the configured recurrence threshold: FORGE_RECURRENT_THRESHOLD if set
// to a positive int, else the default. Invalid values fall back to the default (lenient, never block).
//
// recurrentThreshold 返回配置的复发阈值：FORGE_RECURRENT_THRESHOLD 为正整数则用之，否则默认值。
// 非法值回落默认（宽松，不阻断）。
func recurrentThreshold() int {
	if s := os.Getenv(recurrentThresholdEnv); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return recurrentDimThresholdDefault
}

// recurrentHardenEnabled reports whether recurrence-driven hardening is active
// (FORGE_RECURRENT_HARDEN != "disable"). Default enabled — the soft→hard balance is opt-out, not
// opt-in: a project only ever hardens when it has proven (via ≥threshold recurrences) that advisory
// self-discipline fails. The escape hatch reverts to pure advisory without a Strength penalty.
//
// recurrentHardenEnabled 报告复发升硬是否生效（FORGE_RECURRENT_HARDEN != "disable"）。默认开启——
// 软→硬平衡是 opt-out 而非 opt-in：项目只有在已证明（≥阈值次复发）advisory 自律失效时才升硬。
// 逃生舱退回纯 advisory 且不加 Strength 惩罚。
func recurrentHardenEnabled() bool {
	return os.Getenv(recurrentHardenDisableEnv) != "disable"
}

// loadConclusions reads the project's completed-task conclusions for recurrence analysis. Fail-open:
// any error (not a forge project, unreadable act dir, ProjectFor failure) returns nil — callers
// treat nil as "no recurrence" and keep advisory, so a missing/unreadable history never blocks.
//
// loadConclusions 读项目已完成任务的结论供复发分析。fail-open：任何错误（非 forge 项目、act 目录
// 不可读、ProjectFor 失败）返回 nil——调用方把 nil 当「无复发」保持 advisory，故缺失/不可读的历史
// 永不阻断。
func loadConclusions(root string) []act.Conclusion {
	proj, err := forgedata.ProjectFor(root)
	if err != nil {
		return nil
	}
	cs, err := act.LoadAll(proj)
	if err != nil {
		return nil
	}
	return cs
}

// scopeDriftSevere reports whether an out-of-scope drift set is severe enough for the per-task axis:
// ≥ scopeDriftSevereThreshold drifted source files. Single-file drift stays advisory even on
// recurrent projects (a normal impact-prediction miss); only material multi-file drift is
// hardening-eligible.
//
// scopeDriftSevere 报告超 scope drift 集是否够 per-task 轴的「严重」：≥ scopeDriftSevereThreshold 个
// 漂移源文件。单文件 drift 即便在复发项目也保持 advisory（正常影响预测失误）；只有实质多文件
// drift 才够格升硬。
func scopeDriftSevere(drift []string) bool {
	return len(drift) >= scopeDriftSevereThreshold
}
