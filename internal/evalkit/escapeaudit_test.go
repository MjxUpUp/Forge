package evalkit

// escapeaudit_test.go — 逃生舱库存聚合的表驱动测试：元数据/旧行兜底聚合、
// 永久化信号（≥3 任务）、unfulfilled 候选（escape 后又 pass）。

import (
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

func escRow(gate, task, detail string, at time.Time) checklog.Entry {
	e := checklog.EscapeHatchEntry(gate, checklog.EscapeReasonEnv, "env", detail)
	e.TaskRef = task
	e.RecordedAt = at
	return *e
}

func passRow(task string, at time.Time) checklog.Entry {
	return checklog.Entry{Check: checklog.CheckTaskVerify, Passed: true, Checked: true,
		TaskRef: task, RecordedAt: at}
}

func TestBuildEscapeInventory(t *testing.T) {
	base := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	entries := []checklog.Entry{
		// pre-escape pass 行不算（对抗审查回归：时序校验）——t/e 只有 escape 前的 pass。
		passRow("t/e", base.Add(-time.Minute)),
		escRow("doc-gate", "t/e", "escape-hatch: doc gate bypassed", base),
		// doc-gate：3 个任务 → 永久化信号；其中 1 个后来 pass → unfulfilled 候选。
		escRow("doc-gate", "t/a", "escape-hatch: doc gate bypassed", base),
		passRow("t/a", base.Add(time.Minute)),
		escRow("doc-gate", "t/b", "escape-hatch: doc gate bypassed", base.Add(2*time.Minute)),
		escRow("doc-gate", "t/c", "escape-hatch: doc gate bypassed", base.Add(3*time.Minute)),
		// 旧行（无 Meta）：Detail 散文兜底聚合到同名 gate。
		{Check: checklog.CheckEscapeHatch, Passed: true, TaskRef: "t/d",
			Detail:     "escape-hatch: doc gate bypassed (per-task override or FORGE_DOC_GATE=disable)",
			RecordedAt: base.Add(4 * time.Minute)},
		// test-coverage：单任务、escape 后无 pass——不进候选。
		escRow("test-coverage", "t/x", "escape-hatch: test-coverage gate bypassed", base.Add(5*time.Minute)),
	}
	inv := BuildEscapeInventory(entries, base.Add(time.Hour))
	if inv.Total != 6 {
		t.Fatalf("总豁免数应 5: %+v", inv)
	}
	var doc *EscapeGateStats
	for i := range inv.Gates {
		if inv.Gates[i].Gate == "doc-gate" {
			doc = &inv.Gates[i]
		}
	}
	if doc == nil {
		t.Fatalf("doc-gate 未聚合: %+v", inv.Gates)
	}
	// 新行按 Meta 聚合（doc-gate）；旧行按散文兜底聚合为独立桶 "doc gate"
	//（空格形态，与连字符 Meta 名不同键——诚实分离而非强并，聚合口径注释在
	// escapeaudit.go 的 gateOf）。
	if doc.Count != 4 || doc.Tasks != 4 {
		t.Fatalf("doc-gate（Meta 形态）count/tasks 不符: %+v", doc)
	}
	var legacyDoc *EscapeGateStats
	for i := range inv.Gates {
		if inv.Gates[i].Gate == "doc gate" {
			legacyDoc = &inv.Gates[i]
		}
	}
	if legacyDoc == nil || legacyDoc.Count != 1 {
		t.Fatalf("旧行散文兜底桶缺失: %+v", inv.Gates)
	}
	if doc.UnfulfilledCandidates != 1 {
		t.Fatalf("t/a escape 后 pass → 1 个候选: %+v", doc)
	}
	joined := strings.Join(inv.Findings, "\n")
	if !strings.Contains(joined, "永久化信号") || !strings.Contains(joined, "doc-gate") {
		t.Fatalf("缺永久化 finding: %s", joined)
	}
	if !strings.Contains(joined, "unfulfilled 候选") {
		t.Fatalf("缺 unfulfilled finding: %s", joined)
	}
	// test-coverage 单任务不进任何 finding。
	if strings.Contains(joined, "test-coverage") {
		t.Fatalf("单任务不应触发 finding: %s", joined)
	}
}

func TestBuildEscapeInventory_Empty(t *testing.T) {
	inv := BuildEscapeInventory(nil, time.Now())
	if inv.Total != 0 || len(inv.Gates) != 0 || len(inv.Findings) != 0 {
		t.Fatalf("空输入应零库存: %+v", inv)
	}
}
