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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/util"
)

// heldoutDisableEnv 是 held-out 门禁的逃生舱（沿 FORGE_ACCEPTANCE_GATE 模式）。
const heldoutDisableEnv = "FORGE_HELDOUT"

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

// heldoutPath 侧车文件路径（ref 里的 / 在 Windows 路径非法——替换为 __）。
func heldoutPath(root, ref string) string {
	safe := strings.ReplaceAll(ref, "/", "__")
	return filepath.Join(dataHome(root), "heldout", safe+".json")
}

// SaveHeldout 登记任务的 held-out 验收套件（forge task start --heldout <file>）。
// criteria 复用 AcceptanceCriterion（run :: expected 解析同一入口）。
func SaveHeldout(root, ref string, criteria []AcceptanceCriterion) error {
	path := heldoutPath(root, ref)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(criteria, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(path, body, 0o644)
}

// LoadHeldout 读任务的 held-out 套件；未登记返回 nil（区别于读失败）。
func LoadHeldout(root, ref string) ([]AcceptanceCriterion, error) {
	body, err := os.ReadFile(heldoutPath(root, ref))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []AcceptanceCriterion
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("held-out 侧车损坏（可删 %s 重登记）: %w", heldoutPath(root, ref), err)
	}
	return out, nil
}

// VerifyHeldout 实跑 held-out 套件、记录 gap 判定行、把结果（含截断输出）存回
// 侧车。state 的可见套件结果不被修改——可见侧由 VerifyAcceptance/Merge 各自管。
func VerifyHeldout(root string, state *TaskState) HeldoutResult {
	var res HeldoutResult
	criteria, err := LoadHeldout(root, state.TaskRef)
	if err != nil || len(criteria) == 0 {
		return res // 未登记 / 读失败：门禁未运行（Checked=false）
	}
	res.Checked = true
	res.VisiblePassed = state.AllAcceptancePassed()
	res.HeldoutPassed = true
	for i := range criteria {
		c := &criteria[i]
		passed, output := RunTestCommand(root, c.Run)
		c.Passed = judgeAcceptance(passed, output, c.Expected)
		c.Output = truncateAcceptanceOutput(output)
		c.AcceptedHeadCommit = GetHeadCommit(root)
		if !c.Passed {
			res.HeldoutPassed = false
			res.FailedHeldout = append(res.FailedHeldout, c.Run)
		}
	}
	// 结果存回侧车（输出留在侧车，不进 TaskState/checklog detail——held-out 内容
	// 不外显的承诺）。
	_ = SaveHeldout(root, state.TaskRef, criteria)

	e := &checklog.Entry{
		Check:   CheckNameHeldoutGap,
		Passed:  res.HeldoutPassed,
		Checked: true,
		TaskRef: state.TaskRef,
	}
	switch {
	case res.VisiblePassed && !res.HeldoutPassed:
		e.Level = checklog.LevelFail
		e.Detail = fmt.Sprintf("BLOCKED: held-out gap——可见验收全过但 held-out 挂 %d/%d 条（test-generalization gap，SpecBench 形态；命令清单见侧车，不外显）",
			len(res.FailedHeldout), len(criteria))
	case !res.HeldoutPassed:
		e.Level = checklog.LevelWarn
		e.Detail = fmt.Sprintf("ADVISORY: held-out 挂 %d/%d 条（可见套件也未全过——由 acceptance gate 主阻断，此处只记 gap 信号）",
			len(res.FailedHeldout), len(criteria))
	default:
		e.Level = checklog.LevelPass
		e.Detail = fmt.Sprintf("pass: held-out %d/%d 全过（visible=%v，无 gap）", len(criteria), len(criteria), res.VisiblePassed)
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
	if err != nil || len(criteria) == 0 {
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
		})
		return true, nil
	}
	res := VerifyHeldout(root, state)
	if res.HeldoutPassed {
		return true, nil
	}
	return false, []string{fmt.Sprintf("held-out 挂 %d 条（gap 形态：可见验收过了但保留集没过）", len(res.FailedHeldout))}
}
