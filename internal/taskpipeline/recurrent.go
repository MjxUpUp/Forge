package taskpipeline

// recurrent.go 实现复发驱动的 advisory→hard 升档——Forge 两种驱动模式（软 advisory vs 硬 blocking）之间的桥。
//
// 本机制解决的问题（dogfood + 近一周多项目数据）：
// 只 WARN 的 advisory check（task-verify 的 test-coverage、全程的 scope-drift）靠 agent 自律，
// 系统性漏——近一周 4 项目 52 任务里 testing 低分 ×27、scope 低分 ×29。但一刀切硬门禁会误伤：
// scope 影响集召回率仅 ~44%（硬拦会拒一半合法改动），test-coverage 硬拦小修是假阳性。
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

// dimTesting / dimScope 是复发升硬所键入的低分维度名，取自单一真相源（scoringtypes），
// 那边改名此处自动跟随。其他维度（efficiency/tool-selection/...）不升硬——它们是纯评分信号，
// 无对应的 advisory gate 可升。
var (
	dimTesting = string(scoringtypes.DimensionTesting)
	dimScope   = string(scoringtypes.DimensionScope)
)

// recurrentDimThresholdDefault 是默认复发阈值：某维度在本项目历史低分（<70）≥ 此数即视为系统性缺口。
// 3 对齐 testCoverageHardGateThreshold 的「3 即系统性」直觉，且——关键——意味着完成 <3 任务的新项目
// 永不触发复发升硬（项目无履历可学时不误伤）。
const recurrentDimThresholdDefault = 3

// scopeDriftSevereThreshold 是 scope-drift 在 per-task 严重度轴上「严重」的最小超 scope 源文件数。
// 单文件 drift 是正常的影响预测失误（召回率 ~44%），即便在复发项目也须保持 advisory；
// 只有多文件 drift——agent 实质偏离计划的信号——才够格升硬。
const scopeDriftSevereThreshold = 3

const (
	// recurrentHardenDisableEnv 全局关闭复发升硬（FORGE_RECURRENT_HARDEN=disable），把 test-coverage
	// 与 scope-drift 都退回纯 advisory。与 gate-bypass 逃生舱（FORGE_TEST_COVERAGE）不同，它不 cap
	// Strength 到 Weak：它表达「本项目偏好 advisory」而非「本任务跳过验证」——项目本就在 advisory
	// 模式，无可打折。
	recurrentHardenDisableEnv = "FORGE_RECURRENT_HARDEN"
	// recurrentThresholdEnv 覆盖复发阈值（FORGE_RECURRENT_THRESHOLD=N，N>0）。
	recurrentThresholdEnv = "FORGE_RECURRENT_THRESHOLD"
)

// lowDimClearThreshold 是复发升硬的「明确低」切线：65，非 70 的展示切线。AutoDesign
// margin 校准表明切线附近 0-3 分差距 ≈ 抛硬币——维度在 67↔70 抖动会让二值 LowDimensions
// 反复进出，按这个抖动升硬就是把噪声当复发。65 在 70 切线下留出 5 分噪声带：只有
// 无争议的低分才向阈值累计。
const lowDimClearThreshold = 65.0

// lowDimCounts 统计各低分维度在项目已完成结论里出现的次数。结论带 DimScores（每维度原始分）
// 时只计明确低（score <= lowDimClearThreshold）；早于 DimScores 的存量结论回落到二值 <70 的
// LowDimensions（其 67 也计入——可接受，数字只是丢了）。基于结论切片的纯函数。供 executor 的
// BLOCKED 消息使用——消息里带出确切复发计数，让 agent 看清本次为何升硬。
func lowDimCounts(cs []act.Conclusion) map[string]int {
	counts := map[string]int{}
	for _, c := range cs {
		if len(c.DimScores) > 0 {
			for _, d := range c.DimScores {
				if d.Score <= lowDimClearThreshold {
					counts[d.Dimension]++
				}
			}
			continue
		}
		for _, d := range c.LowDimensions {
			counts[d]++
		}
	}
	return counts
}

// dimRecurrent 报告维度 dim 是否在给定结论里明确低分（<= lowDimClearThreshold；存量结论：二值
// <70）≥ threshold 次——复发轴。空输入或 threshold<=0 返回 false（fail-open）。调用方传入
// loadConclusions(root)（其自身对读取错误 fail-open）。
func dimRecurrent(cs []act.Conclusion, dim string, threshold int) bool {
	if threshold <= 0 || len(cs) == 0 {
		return false
	}
	return lowDimCounts(cs)[dim] >= threshold
}

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

// recurrentHardenEnabled 报告复发升硬是否生效（FORGE_RECURRENT_HARDEN != "disable"）。默认开启——
// 软→硬平衡是 opt-out 而非 opt-in：项目只有在已证明（≥阈值次复发）advisory 自律失效时才升硬。
// 逃生舱退回纯 advisory 且不加 Strength 惩罚。
func recurrentHardenEnabled() bool {
	return os.Getenv(recurrentHardenDisableEnv) != "disable"
}

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

// scopeDriftSevere 报告超 scope drift 集是否够 per-task 轴的「严重」：≥ scopeDriftSevereThreshold 个
// 漂移源文件。单文件 drift 即便在复发项目也保持 advisory（正常影响预测失误）；只有实质多文件
// drift 才够格升硬。
func scopeDriftSevere(drift []string) bool {
	return len(drift) >= scopeDriftSevereThreshold
}
