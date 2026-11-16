package dashboard

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/nodestamp"
)

// feed_node_test.go —— Pulse 节点归因（多机器 Phase 3）：feed 事件携带来源机器的
// node_id，同步后的多机器看板一眼可答「这事发生在哪台机器」。

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
