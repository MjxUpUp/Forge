package clitask

import (
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/agentproto"
	"github.com/MjxUpUp/Forge/internal/change"
	"github.com/MjxUpUp/Forge/internal/docsconsistency"
)

// taskStartAgentsAdvisory 在开工时提醒「agent 读的那份协议块」可能落后于当前二进制。
//
// 块是 agent 在这个仓库里的入口说明书：二进制升级了而块没同步，agent 会照着旧协议
// 干活（少掉的命令它不知道，改掉的语义它还按旧的做）。这里只提示不阻断——skew 不是
// 门禁，进不了判据；但让它一直静默，就等于默认接受一个会骗人的说明书。
//
// 比较的是正文指纹（sha 覆盖散文+附录，不含 version 行），所以「同一份协议、不同
// 版本号」不会误报，只有内容真的对不上才提示。
func taskStartAgentsAdvisory(root string) {
	body, ok := change.ReadAgentsBlock(root)
	if !ok {
		return
	}
	_, gotSHA, ok := agentproto.ParseProto(body)
	if !ok {
		fmt.Fprintln(os.Stderr, "提示：AGENTS.md 的 forge 托管块没有块头指纹（旧版 forge 写入）——跑 `forge change agents` 同步")
		return
	}
	tree := docsconsistency.CommandTree()
	if tree == nil {
		return
	}
	want, err := change.AgentsBlockBody(change.AgentsOptions{Commands: agentproto.FromCobra(tree)})
	if err != nil {
		return
	}
	wantBody, wantSHA, ok := agentproto.ParseProto(want)
	if !ok || wantBody == "" {
		return // 自己生成不出带指纹的块：没有可信的比较基准，宁可沉默也不误报 skew
	}
	if gotSHA == wantSHA {
		return
	}
	fmt.Fprintf(os.Stderr, "提示：AGENTS.md 的协议块（sha=%s）与当前二进制应生成的（sha=%s）不一致——先跑 `forge change agents` 同步；不阻断（skew 不是门禁）\n", gotSHA, wantSHA)
}
