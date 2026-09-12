package taskpipeline

// heldout.go — held-out gap 门禁（focus-batches §2a，方向 B）：验收双套件——可见
// 测试（state.Acceptance，agent 可见）+ held-out 测试（侧车文件，仅 verify 时执行）。
// 依据 SpecBench（arXiv 2605.21384）："we use the gap in pass rates on these two
// suites to quantify reward hacking"，gap 随代码量每十倍增 28pp；评测界已共识
// held-out 集（SWE-Bench Pro 私有集 / METR 43× 作弊率差），但无产品把它做成运行时
// 任务门禁——这是 Forge 的差异化空位。
//
// 存储：DataDir/heldout/<ref>.json 侧车（不进 TaskState——task status/trace 不展示，
// 结构上与 agent 常读的任务状态分离）。诚实边界：侧车在本地磁盘，蓄意翻找的 agent
// 技术上可读——v1 的防线是结构分离 + 不外显 + complete 复跑；真加密留给企业版。
//
// 判定：可见全过而 held-out 任一挂 → cheat-suspect（BLOCKED 形态）；可见未全过时
// held-out 结果照记（gap 信号完整），阻断由既有 acceptance gate 负责。complete 时
// 复跑 held-out（防验收后改码的 staleness——测试本来就该在 complete 边界再跑一次）。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/util"
)

// heldoutPath 侧车文件路径（ref 里的 / 在 Windows 路径非法——替换为 __）。
func heldoutPath(root, ref string) string {
	safe := strings.ReplaceAll(ref, "/", "__")
	return filepath.Join(dataHome(root), "heldout", safe+".json")
}

// heldoutDisableEnv 是 held-out 门禁的逃生舱（沿 FORGE_ACCEPTANCE_GATE 模式）。
const heldoutDisableEnv = "FORGE_HELDOUT"

// heldoutRotateThreshold 是轮换建议的实跑次数阈值（W4）：held-out 集每被实跑
// 一次，agent 就多一次观察它的机会——SpecBench 的 2900 行哈希表「编译器」实证了
// 记忆化攻击的真实形态。到达阈值后 PASS 判定降级为 warn（轮换建议），新集必须
// 人来出（「测试的测试」是人工边界，forge 不自造测试）。
const heldoutRotateThreshold = 5

// CheckNameHeldoutGap 记录一次双套件 gap 判定（deterministic——forge 自己跑
// held-out 命令，agent 无法伪造结果侧）。
const CheckNameHeldoutGap checklog.CheckName = "acceptance-heldout-gap"

// HeldoutResult 是 VerifyHeldout 的结构化结果。
type HeldoutResult struct {
	// Checked=false：无 held-out 侧车（未登记）或读侧失败——门禁未运行。
	Checked bool
	// VisiblePassed / HeldoutPassed：两套件是否全过（空套件视为过）。
	VisiblePassed bool
	HeldoutPassed bool
	// FailedHeldout：挂掉的 held-out 命令（不含输出——输出在侧车里，不外显）。
	FailedHeldout []string
}

// heldoutSidecar 是侧车 v2 形态（W4）：criteria 之外带使用计数与内容哈希——
// 轮换触发与「防静默换锚」的数据面。旧侧车是裸数组（无计数），读取时兼容
// （RunCount 视为 0，从该次实跑起累计）。
type heldoutSidecar struct {
	Criteria []AcceptanceCriterion `json:"criteria"`
	// RunCount 是该集被 VerifyHeldout 实跑的累计次数（轮换触发的计数器）。
	RunCount int `json:"run_count,omitempty"`
	// PinnedAt 是登记/最后一次重出集的时间。
	PinnedAt time.Time `json:"pinned_at,omitempty"`
	// Hash 是 criteria 当前内容的 sha256 前 16 位（每次实跑重算并回写——内容
	// 被手改则哈希变，checklog Meta 的前后对比即暴露「静默换锚」）。
	Hash string `json:"hash,omitempty"`
}

// heldoutContentHash 计算 criteria 内容指纹（sha256 前 16 位）。用确定性序列化
// 而非文件字节——旧数组形态与 v2 形态的同一套 criteria 得到同一哈希。
func heldoutContentHash(criteria []AcceptanceCriterion) string {
	body, err := json.Marshal(criteria)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:16]
}

// loadHeldoutSidecar 读侧车（兼容 v1 裸数组与 v2 对象）。
func loadHeldoutSidecar(root, ref string) (*heldoutSidecar, bool, error) {
	body, err := os.ReadFile(heldoutPath(root, ref))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var sc heldoutSidecar
	if err := json.Unmarshal(body, &sc); err == nil && sc.Criteria != nil {
		if sc.Hash == "" {
			sc.Hash = heldoutContentHash(sc.Criteria)
		}
		return &sc, true, nil
	}
	// v1 裸数组
	var legacy []AcceptanceCriterion
	if err := json.Unmarshal(body, &legacy); err != nil {
		return nil, false, fmt.Errorf("held-out 侧车损坏（可删 %s 重登记）: %w", heldoutPath(root, ref), err)
	}
	return &heldoutSidecar{Criteria: legacy, Hash: heldoutContentHash(legacy)}, true, nil
}

// saveHeldoutSidecar 原子回写侧车 v2。
func saveHeldoutSidecar(root, ref string, sc *heldoutSidecar) error {
	path := heldoutPath(root, ref)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(path, body, 0o644)
}

// SaveHeldout 登记任务的 held-out 验收套件（forge task start --heldout <file>）。
// criteria 复用 AcceptanceCriterion（run :: expected 解析同一入口）。登记即重置
// 使用计数与锚定时间（重出集 = 新锚）。
func SaveHeldout(root, ref string, criteria []AcceptanceCriterion) error {
	return saveHeldoutSidecar(root, ref, &heldoutSidecar{
		Criteria: criteria,
		PinnedAt: time.Now(),
		Hash:     heldoutContentHash(criteria),
	})
}

// LoadHeldout 读任务的 held-out 套件；未登记返回 nil（区别于读失败）。
func LoadHeldout(root, ref string) ([]AcceptanceCriterion, error) {
	sc, exists, err := loadHeldoutSidecar(root, ref)
	if err != nil || !exists {
		return nil, err
	}
	return sc.Criteria, nil
}

// VerifyHeldout 实跑 held-out 套件、记录 gap 判定行、把结果（含截断输出）存回
// 侧车。state 的可见套件结果不被修改——可见侧由 VerifyAcceptance/Merge 各自管。
// W4：每次实跑递增使用计数；到达轮换阈值时 PASS 判定降级为 warn（保留集可能已
// 被记忆——记忆化是 SpecBench 实证的真实攻击面），checklog Meta 带当前内容哈希
// 与计数（前后对比暴露静默换锚）；新集必须人来出。
func VerifyHeldout(root string, state *TaskState) HeldoutResult {
	var res HeldoutResult
	sc, exists, err := loadHeldoutSidecar(root, state.TaskRef)
	if err != nil {
		// 侧车损坏与"未登记"不同：已登记过的保留集读不回来是治理信号，留 warn 痕
		// （与 FORGE_HELDOUT=disable 必留 escape-hatch 痕对称——对抗审查 note）。侧车
		// 被删除（IsNotExist）与从未登记不可区分，v1 无法探测删除，诚实写在注释。
		recordAudit(root, &checklog.Entry{
			Check: CheckNameHeldoutGap, Passed: false, Checked: false,
			Level:   checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  "ADVISORY: held-out 侧车存在但损坏，门禁未运行：" + err.Error(),
		})
		return res
	}
	if !exists || len(sc.Criteria) == 0 {
		return res // 未登记：门禁未运行（Checked=false）
	}
	res.Checked = true
	res.VisiblePassed = state.AllAcceptancePassed()
	res.HeldoutPassed = true
	for i := range sc.Criteria {
		c := &sc.Criteria[i]
		passed, output := RunTestCommand(root, c.Run)
		c.Passed = judgeAcceptance(passed, output, c.Expected)
		c.Output = truncateAcceptanceOutput(output)
		c.AcceptedHeadCommit = GetHeadCommit(root)
		if !c.Passed {
			res.HeldoutPassed = false
			res.FailedHeldout = append(res.FailedHeldout, c.Run)
		}
	}
	// W4：使用计数递增 + 哈希重算（内容被手改则哈希漂移——checklog 前后对比即
	// 暴露静默换锚）。结果存回侧车（输出留在侧车，不进 TaskState/checklog
	// detail——held-out 内容不外显的承诺）。
	sc.RunCount++
	sc.Hash = heldoutContentHash(sc.Criteria)
	_ = saveHeldoutSidecar(root, state.TaskRef, sc)
	rotateDue := sc.RunCount%heldoutRotateThreshold == 0

	e := &checklog.Entry{
		Check:   CheckNameHeldoutGap,
		Passed:  res.HeldoutPassed,
		Checked: true,
		TaskRef: state.TaskRef,
		Meta: map[string]string{
			"heldout.run_count": fmt.Sprint(sc.RunCount),
			"heldout.hash":      sc.Hash,
		},
	}
	runNote := fmt.Sprintf("（held-out 第 %d 次实跑，内容哈希 %s）", sc.RunCount, sc.Hash)
	switch {
	case res.VisiblePassed && !res.HeldoutPassed:
		e.Level = checklog.LevelFail
		e.Detail = fmt.Sprintf("BLOCKED: held-out gap——可见验收全过但 held-out 挂 %d/%d 条（test-generalization gap，SpecBench 形态；命令清单见侧车，不外显）%s",
			len(res.FailedHeldout), len(sc.Criteria), runNote)
	case !res.HeldoutPassed:
		e.Level = checklog.LevelWarn
		e.Detail = fmt.Sprintf("ADVISORY: held-out 挂 %d/%d 条（可见套件也未全过——由 acceptance gate 主阻断，此处只记 gap 信号）%s",
			len(res.FailedHeldout), len(sc.Criteria), runNote)
	default:
		if rotateDue {
			// 轮换触发：PASS 降级为 warn——保留集跑了 N 次，agent 有充足机会记忆它；
			// 新集必须人来出（人工边界），故只提示不自动重生成。
			e.Level = checklog.LevelWarn
			e.Detail = fmt.Sprintf("ADVISORY: held-out %d/%d 全过，但已实跑 %d 次到达轮换阈值——保留集可能已被记忆，建议删除侧车重出新集（新集人工出，forge 不自造）%s",
				len(sc.Criteria), len(sc.Criteria), sc.RunCount, runNote)
		} else {
			e.Level = checklog.LevelPass
			e.Detail = fmt.Sprintf("pass: held-out %d/%d 全过（visible=%v，无 gap）%s", len(sc.Criteria), len(sc.Criteria), res.VisiblePassed, runNote)
		}
	}
	recordAudit(root, e)
	return res
}

// CheckHeldoutFresh 是 task-complete 的 held-out pre-flight：登记了 held-out 的
// 任务在完成边界复跑双套件（防"验收后改码"的 staleness——测试在 complete 边界
// 本就该再跑一次；复跑成本即测试成本，无额外惩罚）。未登记放行；逃生
// FORGE_HELDOUT=disable 落 escape-hatch 留痕。
func CheckHeldoutFresh(root string, state *TaskState) (ok bool, reasons []string) {
	criteria, err := LoadHeldout(root, state.TaskRef)
	if err != nil {
		// 侧车损坏与未登记不同（复审 note）：已登记的保留集读不回来是治理信号，
		// 留 warn 痕后放行——complete 路径与 verify 路径（VerifyHeldout 的 warn）对称。
		recordAudit(root, &checklog.Entry{
			Check: CheckNameHeldoutGap, Passed: false, Checked: false,
			Level:   checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  "ADVISORY: held-out 侧车存在但损坏，complete 边界门禁未运行：" + err.Error(),
		})
		return true, nil
	}
	if len(criteria) == 0 {
		return true, nil
	}
	if os.Getenv(heldoutDisableEnv) == "disable" {
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckEscapeHatch,
			Passed:  true,
			Checked: true,
			Level:   checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  `escape-hatch: held-out gate bypassed (FORGE_HELDOUT=disable)`,
			Meta:    map[string]string{"escape.gate": "held-out", "escape.reason": checklog.EscapeReasonEnv, "escape.owner": "env"},
		})
		return true, nil
	}
	res := VerifyHeldout(root, state)
	if res.HeldoutPassed {
		return true, nil
	}
	return false, []string{fmt.Sprintf("held-out 挂 %d 条（gap 形态：可见验收过了但保留集没过）", len(res.FailedHeldout))}
}
