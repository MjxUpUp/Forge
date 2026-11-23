package cli

import (
	"errors"

	"github.com/MjxUpUp/Forge/internal/agentproto"
	"github.com/MjxUpUp/Forge/internal/change"
	"github.com/MjxUpUp/Forge/internal/docsconsistency"
)

// agentSurface 从运行中的命令树取投递面（与 compat 快照面 1 同一棵树、同一套剔除规则）。
//
// 树由 cli 的 init 注册进 docsconsistency（破 import 环的回调），所以这里拿到的一定是
// 当前二进制的真实命令面——而不是文档里以为的那份。
func agentSurface() ([]agentproto.Command, error) {
	tree := docsconsistency.CommandTree()
	if tree == nil {
		return nil, errors.New("cli: 命令树未注册（docsconsistency.RegisterCommandTree 没被调用）")
	}
	return agentproto.FromCobra(tree), nil
}

// agentsOpts 组装托管块生成面的输入（命令面 + 运行中二进制版本）。
func agentsOpts() (change.AgentsOptions, error) {
	cmds, err := agentSurface()
	if err != nil {
		return change.AgentsOptions{}, err
	}
	return change.AgentsOptions{Commands: cmds, Version: rootCmd.Version}, nil
}
