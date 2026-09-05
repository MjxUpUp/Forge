package evalkit

// escapeaudit.go — 逃生舱库存与 unfulfilled-waiver 复查（mechanism-hardening
// P1-2，expect 语义 v1）。依据 FSE 2025（50.8% 豁免为幽灵且只增不减）与
// Rust #[expect]/TS @ts-expect-error 的 unfulfilled 语义：豁免本身要成为被复查
// 的对象。v1 判定是**候选信号**而非判定（Rust expect 的已知误报先例——跨平台/
// 条件下"未触发"≠"不需要"），命名与输出都带"候选"。
//
// 三个信号（全部 deterministic，从 checklog 聚合）：
//  1. 库存：按 gate 聚合 escape 行数 + 涉及任务数 + reason 分布
//  2. 永久化：同一 gate 在 ≥3 个不同任务被豁免（Fowler 库存观 + dive_05
//     "同一豁免跨 N 任务反复出现=事实永久化"）
//  3. unfulfilled 候选：某任务 escape 后，同任务后续存在该 gate 的 PASS 行且
//     无新 escape——门禁后来过了，豁免可能已不需要

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// PerpetuationThreshold 是永久化信号的门槛（同一 gate 跨任务数）。
const PerpetuationThreshold = 3

// EscapeInventory 是逃生舱库存快照。
type EscapeInventory struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Total       int                `json:"total"`
	Gates       []EscapeGateStats  `json:"gates"`
	Findings    []string           `json:"findings,omitempty"`
}

// EscapeGateStats 是单个 gate 的豁免统计。
type EscapeGateStats struct {
	Gate        string            `json:"gate"`          // Meta 命名；旧行按 Detail 首词兜底
	Count       int               `json:"count"`
	Tasks       int               `json:"tasks"`         // 涉及任务数（去重）
	Reasons     map[string]int     `json:"reasons,omitempty"`
	UnfulfilledCandidates int      `json:"unfulfilled_candidates,omitempty"` // 后续 pass 且无新 escape 的任务数
}

// BuildEscapeInventory 聚合全史 escape 行成库存。
//
// BuildEscapeInventory aggregates escape-hatch rows into an inventory.
func BuildEscapeInventory(entries []checklog.Entry, now time.Time) EscapeInventory {
	inv := EscapeInventory{GeneratedAt: now}
	type taskState struct {
		escaped bool
		passed  bool // escape 之后存在该 gate 相关 pass（粗匹配：同任务、时间晚于 escape 的 pass 行——gate 名匹配 Meta 或 Detail 前缀）
	}
	gateStats := map[string]*EscapeGateStats{}
	gateTasks := map[string]map[string]*taskState{}

	gateOf := func(e checklog.Entry) string {
		if g := checklog.EscapeGateOf(&e); g != "" {
			return g
		}
		// 旧行兜底：Detail 形如 "escape-hatch: doc gate bypassed ..."——取冒号后
		// 到 " bypassed"/";" 前的短语。没有 Meta 的历史行至少能按散文前缀聚合。
		d := e.Detail
		if i := strings.Index(d, "escape-hatch: "); i >= 0 {
			rest := d[i+len("escape-hatch: "):]
			if j := strings.Index(rest, " bypassed"); j > 0 {
				return rest[:j]
			}
			if j := strings.Index(rest, ";"); j > 0 {
				return rest[:j]
			}
		}
		return "(unknown)"
	}

	for i := range entries {
		e := &entries[i]
		if e.Check != checklog.CheckEscapeHatch {
			continue
		}
		inv.Total++
		g := gateOf(*e)
		st := gateStats[g]
		if st == nil {
			st = &EscapeGateStats{Gate: g, Reasons: map[string]int{}}
			gateStats[g] = st
			gateTasks[g] = map[string]*taskState{}
		}
		st.Count++
		st.Reasons[checklog.EscapeReasonOf(e)]++
		key := e.TaskRef
		if key == "" {
			key = "(no-task)"
		}
		if _, ok := gateTasks[g][key]; !ok {
			gateTasks[g][key] = &taskState{}
		}
		gateTasks[g][key].escaped = true
		gateTasks[g][key].passed = false // 重置：最新 escape 之后的 pass 才算
	}
	// 第二遍：escape 之后同任务的 pass 行（同 gate 粗匹配）→ unfulfilled 候选。
	// entries 时间序（LoadAllAll 契约）。pass 行的 gate 匹配：pass 行没有 escape
	// Meta——按 Check 名与 gate 名的映射（doc-gate→doc-lint? 不对齐）。
	// 诚实边界：v1 用"同任务在最后一次 escape 后存在任一 PASS 级行"作候选信号
	//（任务整体走绿了，被豁免的门禁大概率也过了）——粗但方向对，输出标"候选"。
	for i := range entries {
		e := &entries[i]
		if e.TaskRef == "" || e.Passed != true || e.Check == checklog.CheckEscapeHatch {
			continue
		}
		for g, tasks := range gateTasks {
			if ts, ok := tasks[e.TaskRef]; ok && ts.escaped && !ts.passed {
				_ = g
				ts.passed = true
			}
		}
	}
	for g, st := range gateStats {
		st.Tasks = len(gateTasks[g])
		for _, ts := range gateTasks[g] {
			if ts.passed {
				st.UnfulfilledCandidates++
			}
		}
		if st.Tasks >= PerpetuationThreshold {
			inv.Findings = append(inv.Findings, fmt.Sprintf(
				"永久化信号：gate %q 被 %d 个不同任务豁免（≥%d）——事实永久化，建议转为显式配置或修复根因（Fowler 库存观）",
				g, st.Tasks, PerpetuationThreshold))
		}
		if st.UnfulfilledCandidates > 0 {
			inv.Findings = append(inv.Findings, fmt.Sprintf(
				"unfulfilled 候选：gate %q 有 %d 个任务在 escape 后又出现过 pass 行——豁免可能已不需要（候选信号非判定，Rust expect 误报先例），建议逐个复查摘除",
				g, st.UnfulfilledCandidates))
		}
	}
	keys := make([]string, 0, len(gateStats))
	for g := range gateStats {
		keys = append(keys, g)
	}
	sort.Strings(keys)
	for _, g := range keys {
		inv.Gates = append(inv.Gates, *gateStats[g])
	}
	return inv
}
