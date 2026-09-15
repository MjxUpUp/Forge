package taskpipeline

// unusedgate.go — unused-scan 的 complete 前置门（wiring gate）。
//
// 背景（2026-09-14 req-hygiene 事故实证）：unused-scan 在 task-verify 已机械抓到
// RunReqHygiene 零引用（checklog level=fail），但 advisory「绝不阻塞」+ complete 门
// 接受 agent 自述 → 死代码随 CI success 进 main。advisory 的豁免理由（「库/反射/
// 外部消费的导出合法地无仓内调用方」）对 internal/ 下的 Go 导出不成立——Go 的
// internal 包规则禁止模块外 import，此类符号零仓内引用即真死代码（反射/注册表
// 消费的误报仍可逃生）。本门把该子集升格为 complete 硬前置，其余 findings 维持
// 纯 advisory 不变。
//
// 设计对称：test-coverage 门先例（complete 前置 + 逃生舱落审计 + 评分有代价）；
// v1 仅 env 逃生（self-report 先例），per-task override 面留给需要时再扩。

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameUnusedGate 是 complete 前置 wiring 门的 checklog 条目名——与 verify 侧
// advisory 扫描（checklog.CheckUnusedScan）区分：同名不同语义会让 trace 读者把
// 「advisory 观测行」误读成「门禁裁定行」。
const CheckNameUnusedGate checklog.CheckName = "unused-gate"

// unusedGateDisableEnv 是 wiring 门的逃生舱 env（沿 FORGE_SELF_REPORT 模式：v1 仅
// env，逃生必落 checklog 审计——见 escapeUnusedGate）。
const unusedGateDisableEnv = "FORGE_UNUSED_SCAN"

// escapeUnusedGate 是 escapeDisabled 的 which 键。switch 无 case = 仅 env 逃生
// （per-task override 需要扩 TaskOverrides 面时再加 case，self-report 同款注释）。
const escapeUnusedGate = "unused-gate"

// underInternalDir 报告仓内相对路径是否落在 internal/ 路径段下（前缀或任一路径
// 段为 internal）。"x/internal.go" 不算——internal 必须是目录段。
func underInternalDir(file string) bool {
	p := filepath.ToSlash(file)
	if p == "internal" || strings.HasPrefix(p, "internal/") {
		return true
	}
	return strings.Contains(p, "/internal/")
}

// blockingUnusedFindings 筛出 unused-scan findings 中可硬拦的子集：Go 导出符号
// （isGoExportKind——词表单一真相源在 unusedscan.go extractGo 旁）且位于 internal/
// 路径段。Go 的 internal 包规则使模块外 import 不可能——「外部消费者」豁免对此
// 子集不成立；TS/Rust 导出与非 internal 路径维持纯 advisory（外部消费/外部 API
// 面的豁免理由仍然有效）。
func blockingUnusedFindings(unused []UnusedFinding) []UnusedFinding {
	var blocking []UnusedFinding
	for _, u := range unused {
		if !isGoExportKind(u.Kind) {
			continue
		}
		if underInternalDir(u.File) {
			blocking = append(blocking, u)
		}
	}
	return blocking
}

// CheckUnusedGate is task-complete's wiring pre-flight: internal/ Go exports that this
// task added but never referenced must be wired into the real call chain (or deleted)
// before complete. Escape (FORGE_UNUSED_SCAN=disable) is audited to checklog and never
// silent. Non-internal / non-Go findings stay advisory-only (returned ok, unchanged).
//
// CheckUnusedGate 是 task-complete 的接线 pre-flight：本任务新增、位于 internal/
// 且零引用的 Go 导出符号，complete 前必须接线进真实调用链（或删除——git 里有
// 历史）。逃生（FORGE_UNUSED_SCAN=disable）落 checklog 审计，绝不静默。非 internal/
// 非 Go 的 findings 维持纯 advisory（本门不管，行为不变）。无发现也记一条 Passed
// 行——「门跑过、干净」在 trace 可追溯（test-coverage 门同款）。
func CheckUnusedGate(root string, state *TaskState) (ok bool, reasons []string) {
	if escapeDisabled(state, escapeUnusedGate, unusedGateDisableEnv) {
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckEscapeHatch,
			Passed:  true,
			Checked: true,
			Level:   checklog.LevelWarn,
			TaskRef: state.TaskRef,
			Detail:  `escape-hatch: unused gate bypassed (FORGE_UNUSED_SCAN=disable); internal/ 零引用导出未接线即可完成——review 须核查`,
			Meta:    map[string]string{"escape.gate": "unused-gate", "escape.reason": checklog.EscapeReasonEnv, "escape.owner": "env"},
		})
		return true, nil
	}

	blocking := blockingUnusedFindings(ScanUnusedSymbols(root, state))
	e := &checklog.Entry{
		Check:   CheckNameUnusedGate,
		Passed:  len(blocking) == 0,
		Checked: true,
		TaskRef: state.TaskRef,
	}
	if len(blocking) == 0 {
		e.Detail = "no blocking unwired exports（internal/ Go 导出零接线：无）"
		recordAudit(root, e)
		return true, nil
	}
	var parts []string
	for _, u := range blocking {
		loc := u.File
		if u.Line > 0 {
			loc = fmt.Sprintf("%s:%d", u.File, u.Line)
		}
		parts = append(parts, fmt.Sprintf("%s %s(%s)", loc, u.Symbol, u.Kind))
	}
	e.Detail = fmt.Sprintf("BLOCKED: %d 个 internal/ Go 导出零引用（实现了但没接线——BUG-1 形态）: %s", len(blocking), strings.Join(parts, "; "))
	recordAudit(root, e)
	reasons = append(reasons, strings.Join(parts, "; ")+"——接线进真实调用链，或删除（git 里有历史）；确属反射/注册表消费，逃生舱放行并留审计")
	return false, reasons
}
