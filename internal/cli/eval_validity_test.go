package cli

// eval_validity_test.go — `forge eval run/decompose` 的 preflight 错误映射与
// leakage 渲染（周期总纲 R3）：违例走 errHardExit 哨兵（exit 2，结论已打印），
// 其他错误保留既有 BLOCKED 前缀文案。

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/evalkit"
)

func TestEvalRunErrorMapping(t *testing.T) {
	t.Chdir(t.TempDir()) // evalRepoRoot 解析 cwd——审计行落隔离临时目录
	spec := evalkit.RunSpec{Benchmark: "b", Split: "s", Profile: evalkit.ProfileFull, Model: "m"}
	pe := &evalkit.PreflightError{Report: &evalkit.PreflightReport{Checked: 1, DoNothingPassed: []string{"t1"}}}
	if err := evalRunError(spec, fmt.Errorf("网格格 full×m 失败: %w", pe)); !errors.Is(err, errHardExit) {
		t.Fatalf("包装过的 preflight 违例应映射为 errHardExit（exit 2），得到 %v", err)
	}
	other := evalRunError(spec, errors.New("boom"))
	if errors.Is(other, errHardExit) || !strings.HasPrefix(other.Error(), "BLOCKED: ") {
		t.Fatalf("非 preflight 错误应保留 BLOCKED 前缀且不走 hard exit: %v", other)
	}
}

func TestFormatLeakage(t *testing.T) {
	if got := formatLeakage(nil); !strings.Contains(got, "=unmeasured") {
		t.Fatalf("nil 应渲染 unmeasured: %q", got)
	}
	l := &evalkit.LeakageSummary{Status: evalkit.LeakageMeasured,
		Rate: evalkit.RateValue{Numerator: 1, Denominator: 4, Value: 0.25, Lo: 0.05, Hi: 0.7}}
	if got := formatLeakage(l); !strings.Contains(got, "0.250（1/4") {
		t.Fatalf("measured 应渲染比例与分子分母: %q", got)
	}
}
