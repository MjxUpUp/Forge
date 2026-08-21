package dashboard

import (
	"os"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/nodestamp"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// feed_node_test.go — Pulse node attribution (multi-machine Phase 3): feed events
// carry the originating machine's node_id so a synced multi-machine dashboard can
// answer "which machine did this happen on" at a glance.
//
// feed_node_test.go —— Pulse 节点归因（多机器 Phase 3）：feed 事件携带来源机器的
// node_id，同步后的多机器看板一眼可答「这事发生在哪台机器」。

func TestAggregateFeed_NodeAttribution(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	stampA := nodestamp.Stamp{NodeID: `fnode_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`, Seq: 7, TsHLC: `0000001700000000000.0000000001`}
	stampB := nodestamp.Stamp{NodeID: `fnode_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`, Seq: 3, TsHLC: `0000001700000001000.0000000001`}

	// task-start ← the task's current lease holder (who's working it).
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{
		TaskRef: `feat/n`, Branch: `feat/n`, StartedAt: base,
		Lease: &taskpipeline.Lease{HolderNode: `fnode_cccccccccccccccccccccccccccccccc`, Fencing: 2, TTLSec: 300},
	}); err != nil {
		t.Fatal(err)
	}
	// skill-trigger ← the checklog entry's stamp.
	writeChecklogEntries(t, p.DataDir, []checklog.Entry{{
		Check: checklog.CheckSkillTrigger, Passed: true, Checked: true, TaskRef: `feat/n`,
		Detail:     checklog.DetailForSkillTrigger(`implementation-discipline`, `UserPromptSubmit`, `coding`),
		RecordedAt: base.Add(time.Hour),
		Stamp:      stampA,
	}})
	// conclusion ← the conclusion's stamp.
	if err := act.Append(p, &act.Conclusion{
		TaskRef: `feat/n`, Score: 90, Grade: `A`, Strength: `Strong`, CompletedAt: base.Add(2 * time.Hour),
		Stamp: stampB,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := AggregateFeed(Options{Root: root}, base.Add(5*time.Hour), FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	nodeOf := func(kind string) string {
		t.Helper()
		for _, e := range res.Events {
			if e.Kind == kind {
				return e.Node
			}
		}
		t.Fatalf("no %s event", kind)
		return ``
	}
	if got := nodeOf(FeedKindTaskStart); got != `fnode_cccccccccccccccccccccccccccccccc` {
		t.Errorf("task-start node = %q, want lease holder", got)
	}
	if got := nodeOf(FeedKindSkillTrigger); got != stampA.NodeID {
		t.Errorf("skill-trigger node = %q, want entry stamp", got)
	}
	if got := nodeOf(FeedKindConclusion); got != stampB.NodeID {
		t.Errorf("conclusion node = %q, want conclusion stamp", got)
	}
	// Legacy unstamped records → empty node, omitempty keeps the wire shape unchanged.
	//
	// 存量无戳记录 → node 为空，omitempty 保持线上结构不变。
	res2, err := AggregateFeed(Options{Root: root2Fixture(t)}, base.Add(5*time.Hour), FeedQuery{})
	if err == nil {
		for _, e := range res2.Events {
			if e.Node != `` {
				t.Errorf("unstamped fixture produced node %q", e.Node)
			}
		}
	}
}

// root2Fixture builds a second project whose records carry NO stamps (legacy shape).
// The conclusion line is written DIRECTLY — act.Append would auto-stamp it (that is
// the event-stamping feature working), and a legacy-record test must bypass it.
//
// root2Fixture 构造记录无戳（存量形态）的第二个项目。conclusion 行直接写文件——
// act.Append 会自动打戳（那正是事件打戳特性在工作），存量记录测试必须绕过它。
func root2Fixture(t *testing.T) string {
	t.Helper()
	root, p := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{TaskRef: `feat/old`, Branch: `feat/old`, StartedAt: base}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.ActDir(), 0755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"task_ref":"feat/old","score":80,"grade":"B","strength":"Weak","completed_at":"2023-11-14T22:00:00Z"}` + "\n"
	if err := os.WriteFile(p.ActConclusionsPath(), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}
