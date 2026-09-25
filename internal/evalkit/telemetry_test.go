package evalkit

// telemetry_test.go — C4/C7 聚合的固定夹具断言（含 insufficient 与 0 的区分）。

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

func testDictionary(t *testing.T) *Dictionary {
	t.Helper()
	d, err := LoadDictionary(filepath.Join("..", "..", "evals", "forge", "metrics.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestAggregateTelemetry(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	mk := func(check checklog.CheckName, passed, checked bool, session, detail string) {
		if err := checklog.Record(root, &checklog.Entry{
			Check: check, Passed: passed, Checked: checked,
			SessionID: session, Detail: detail,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk(checklog.CheckEscapeHatch, true, true, "s1", "FORGE_TEST_COVERAGE")
	mk(checklog.CheckTakeoverPolicy, true, true, "s1", "takeover off by user（project policy layer）")
	mk(checklog.CheckTaskVerify, true, true, "s1", "agent claim")
	mk(checklog.CheckTaskVerify, false, true, "s2", "agent claim")
	mk(checklog.CheckTaskComplete, true, true, "s2", "agent claim")
	mk(checklog.CheckAutoCompile, false, true, "s2", "ADVISORY: build hints")

	// 聚合数学用小下限字典断言；仓内字典的下限纪律单独断言（insufficient）。
	dict := testDictionary(t)
	for i := range dict.Metrics {
		dict.Metrics[i].MinSamples = 1
	}
	rep, err := Aggregate(root, dict, now)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Entries != 6 || rep.Sessions != 2 {
		t.Fatalf("entries/sessions 期望 6/2: %d/%d", rep.Entries, rep.Sessions)
	}
	find := func(id string) RateValue {
		for _, r := range rep.Rates {
			if r.MetricID == id {
				return r
			}
		}
		t.Fatalf("缺指标 %s", id)
		return RateValue{}
	}
	if got := find("gate_escape_rate"); got.Value != 0.5 || got.Insufficient {
		t.Fatalf("escape 率应 1/2=0.5: %+v", got)
	}
	if got := find("off_churn"); got.Numerator != 1 {
		t.Fatalf("off_churn 应 1: %+v", got)
	}
	if got := find("self_gate_pass_rate"); got.Value != 2.0/3.0 {
		t.Fatalf("自举通过率应 2/3: %+v", got)
	}
	if got := find("gate_fire_rate"); got.Value != 1 {
		t.Fatalf("gate_fire 仅 1 条 advisory: %+v", got)
	}
	if got := find("wait_turns"); !got.Insufficient {
		t.Fatalf("wait_turns v1 必须 insufficient（无数据≠0）: %+v", got)
	}
	if len(rep.Weeks) == 0 {
		t.Fatal("应有周趋势桶")
	}
	// 仓内字典（真实下限）：sessions=2 < gate_escape_rate 下限 5 → insufficient。
	repoDict := testDictionary(t)
	rep2, err := Aggregate(root, repoDict, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep2.Rates {
		if r.MetricID == "gate_escape_rate" && !r.Insufficient {
			t.Fatalf("样本低于字典下限应 insufficient: %+v", r)
		}
	}
}
