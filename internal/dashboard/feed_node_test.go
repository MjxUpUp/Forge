package dashboard

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/nodestamp"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// feed_node_test.go —— Pulse 节点归因（多机器 Phase 3）：feed 事件携带来源机器的
// node_id，同步后的多机器看板一眼可答「这事发生在哪台机器」。

func TestAggregateFeed_NodeAttribution(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	stampA := nodestamp.Stamp{NodeID: `fnode_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`, Seq: 7, TsHLC: `0000001700000000000.0000000001`}
	stampB := nodestamp.Stamp{NodeID: `fnode_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`, Seq: 3, TsHLC: `0000001700000001000.0000000001`}

	// task-start ← 任务有效租约的持有者（谁在干活）。ClaimedAt 落值使租约在查询时点
	// 未过期（feed 不得显示过期持有者）。
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{
		TaskRef: `feat/n`, Branch: `feat/n`, StartedAt: base,
		Lease: &taskpipeline.Lease{HolderNode: `fnode_cccccccccccccccccccccccccccccccc`, Fencing: 2, TTLSec: 4 * 3600, ClaimedAt: base.Add(2 * time.Hour).UnixMilli()},
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

	// 过期租约 → 无 node chip（崩溃机器的 stale 认领不得滞留）。
	if err := taskpipeline.SaveTaskState(root, &taskpipeline.TaskState{
		TaskRef: `feat/expired`, Branch: `feat/expired`, StartedAt: base,
		Lease: &taskpipeline.Lease{HolderNode: `fnode_dddddddddddddddddddddddddddddddd`, Fencing: 1, TTLSec: 60, ClaimedAt: base.UnixMilli()},
	}); err != nil {
		t.Fatal(err)
	}

	res, err := AggregateFeed(Options{Root: root}, base.Add(5*time.Hour), FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	nodeOf := func(kind, ref string) string {
		t.Helper()
		for _, e := range res.Events {
			if e.Kind == kind && e.TaskRef == ref {
				return e.Node
			}
		}
		t.Fatalf("no %s event for %s", kind, ref)
		return ``
	}
	if got := nodeOf(FeedKindTaskStart, `feat/n`); got != `fnode_cccccccccccccccccccccccccccccccc` {
		t.Errorf("task-start node = %q, want active lease holder", got)
	}
	if got := nodeOf(FeedKindTaskStart, `feat/expired`); got != `` {
		t.Errorf("expired lease must not render a node, got %q", got)
	}
	if got := nodeOf(FeedKindSkillTrigger, `feat/n`); got != stampA.NodeID {
		t.Errorf("skill-trigger node = %q, want entry stamp", got)
	}
	if got := nodeOf(FeedKindConclusion, `feat/n`); got != stampB.NodeID {
		t.Errorf("conclusion node = %q, want conclusion stamp", got)
	}
	// 存量无戳记录 → node 为空，且线上结构不变（marshal 后无 "node" 键——只断言
	// Go 层空串逮不到 omitempty 被删的情形）。
	res2, err := AggregateFeed(Options{Root: root2Fixture(t)}, base.Add(5*time.Hour), FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res2.Events {
		if e.Node != `` {
			t.Errorf("unstamped fixture produced node %q", e.Node)
		}
		raw, merr := json.Marshal(e)
		if merr != nil {
			t.Fatal(merr)
		}
		if strings.Contains(string(raw), `"node"`) {
			t.Errorf("unstamped event wire shape changed: %s", raw)
		}
	}
}

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

// TestAggregateFeed_SigVerifyEvents：bundle-verify checklog 条目（导入侧信任判定）
// 投影成 sig-verify 事件进流——severity 取自条目 Level（warn/blocked → warn/fail），
// 标题来自结构化 Meta（verdict + signer 短标签），node 取自条目打戳（验签发生的
// 机器）。不可分类的 Level 保持 info（severity 默认绝不升级）。
func TestAggregateFeed_SigVerifyEvents(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0).UTC()
	stamp := nodestamp.Stamp{NodeID: `fnode_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee`, Seq: 9, TsHLC: `0000001700000000000.0000000009`}
	writeChecklogEntries(t, p.DataDir, []checklog.Entry{
		{Check: checklog.CheckBundleVerify, Passed: true, Checked: true, Level: checklog.LevelWarn,
			Detail: `签名者不在 trust store`, RecordedAt: base,
			Meta:  map[string]string{checklog.MetaKeyVerdict: `unknown-signer`, checklog.MetaKeySigner: `fnode_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`},
			Stamp: stamp},
		{Check: checklog.CheckBundleVerify, Passed: false, Checked: true, Level: checklog.LevelBlocked,
			Detail: `验签失败`, RecordedAt: base.Add(time.Minute),
			Meta: map[string]string{checklog.MetaKeyVerdict: `invalid`}},
		// Level 缺失（旧手写行）：EffectiveLevel 从 Passed 兜底为 pass → severity ok。
		{Check: checklog.CheckBundleVerify, Passed: true, Checked: true,
			Detail: `验签通过`, RecordedAt: base.Add(2 * time.Minute),
			Meta: map[string]string{checklog.MetaKeyVerdict: `verified`, checklog.MetaKeySigner: `fnode_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`}},
	})

	res, err := AggregateFeed(Options{Root: root}, base.Add(time.Hour), FeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var sigs []FeedEvent
	for _, e := range res.Events {
		if e.Kind == FeedKindSigVerify {
			sigs = append(sigs, e)
		}
	}
	if len(sigs) != 3 {
		t.Fatalf("sig-verify 事件数 = %d, want 3（feed 事件: %v）", len(sigs), feedKinds(res.Events))
	}
	// 降序流：最新（verified）在前。
	if sigs[0].Severity != FeedSeverityOK || sigs[0].Title != `验签通过 · 签名者 bbbbbbbb` {
		t.Errorf("verified 事件异常: %+v", sigs[0])
	}
	if sigs[1].Severity != FeedSeverityFail || sigs[1].Title != `验签失败——已拒绝导入` {
		t.Errorf("invalid 事件异常: %+v", sigs[1])
	}
	if sigs[2].Severity != FeedSeverityWarn || sigs[2].Title != `签名者未登记 aaaaaaaa——按未签名处理` {
		t.Errorf("unknown-signer 事件异常: %+v", sigs[2])
	}
	if sigs[2].Node != stamp.NodeID {
		t.Errorf("node 应携验签机器打戳, got %q", sigs[2].Node)
	}
	if sigs[1].Node != `` {
		t.Errorf("无戳条目 node 应为空, got %q", sigs[1].Node)
	}
	// 线上结构：kind 值序列化上板。
	raw, err := json.Marshal(sigs[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"kind":"sig-verify"`) {
		t.Errorf("kind 未上线: %s", raw)
	}
}
