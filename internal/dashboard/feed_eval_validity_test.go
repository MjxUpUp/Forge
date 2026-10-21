package dashboard

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

// TestAggregateFeed_EvalValidityEvents 钉住周期总纲 R3/R5 的两个新检查名进
// eval 投影：preflight 拒绝启动是 fail 级、标题按检查名结构化构造（不落
// 「自评测事件」兜底），golden test 层查询是 ok 级观察。
func TestAggregateFeed_EvalValidityEvents(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	writeChecklogEntries(t, p.DataDir, []checklog.Entry{
		{Check: checklog.CheckEvalPreflight, Passed: false, Checked: true, Level: checklog.LevelBlocked,
			Detail: "preflight 违例: do_nothing_passed 1", RecordedAt: base},
		{Check: checklog.CheckEvalGoldenTestQuery, Passed: true, Checked: true,
			Detail: "golden test 层查询: 第 3 次", RecordedAt: base.Add(time.Minute)},
	})
	res, err := AggregateFeed(Options{Root: root}, base.Add(time.Hour), FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		`评测前置违例（拒绝启动）`:    FeedSeverityFail,
		`golden test 层查询`: FeedSeverityOK,
	}
	got := map[string]string{}
	for _, e := range res.Events {
		if e.Kind == FeedKindEval {
			got[e.Title] = e.Severity
		}
	}
	for title, sev := range want {
		if got[title] != sev {
			t.Fatalf("eval 事件 %q severity 应为 %q，实际事件集 %v", title, sev, got)
		}
	}
}
