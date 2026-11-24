package change_test

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/agentproto"
	"github.com/MjxUpUp/Forge/internal/change"
	_ "github.com/MjxUpUp/Forge/internal/cli" // init 注册 rootCmd 回调；缺它命令树为 nil，块里没有命令面
	"github.com/MjxUpUp/Forge/internal/docsconsistency"
)

// 块头指纹是 skew 检测的唯一依据：没有它，agent 读到的块与二进制对不上也没人知道。
func TestAgentsBlockCarriesProtoHeader(t *testing.T) {
	tree := docsconsistency.CommandTree()
	if tree == nil {
		t.Fatal("docsconsistency.CommandTree() 为 nil——cli 的 init 未注册命令树")
	}
	cmds := agentproto.FromCobra(tree)
	if len(cmds) < 50 {
		t.Fatalf("命令树只有 %d 条命令，明显没走通", len(cmds))
	}
	opts := change.AgentsOptions{Commands: cmds, Version: "9.9.9"}
	dir := t.TempDir()

	action, err := change.EnsureAgentsBlockOpts(dir, opts)
	if err != nil {
		t.Fatalf("EnsureAgentsBlockOpts: %v", err)
	}
	if action != change.AgentsCreated {
		t.Fatalf("首次写入应为 created，实得 %q", action)
	}
	body, ok := change.ReadAgentsBlock(dir)
	if !ok {
		t.Fatal("读不回刚写的托管块")
	}
	if !strings.HasPrefix(body, agentproto.ProtoOpen+"9.9.9 ") {
		t.Fatalf("块首不是 proto 行：%q", body[:60])
	}
	v, sha, ok := agentproto.ParseProto(body)
	if !ok || v != "9.9.9" || len(sha) != 12 {
		t.Fatalf("ParseProto 读不回块头：v=%q sha=%q ok=%v", v, sha, ok)
	}
	rest := strings.TrimSpace(strings.Join(strings.Split(body, "\n")[1:], "\n"))
	if agentproto.SHA(rest) != sha {
		t.Fatalf("块头 sha=%s 与正文指纹 %s 不符（sha 必须覆盖去掉本行后的正文）", sha, agentproto.SHA(rest))
	}
	if !strings.Contains(body, "`forge change agents`") {
		t.Fatal("命令面附录里没有 change agents")
	}
	again, err := change.EnsureAgentsBlockOpts(dir, opts)
	if err != nil {
		t.Fatalf("再次写入: %v", err)
	}
	if again != change.AgentsUnchanged {
		t.Fatalf("同样的输入再写一次应为 unchanged，实得 %q（非幂等会让 --check 永远报漂移）", again)
	}
}
