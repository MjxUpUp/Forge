package agentbridge

// CopilotTranslator is a no-op — but no longer because copilot lacks hooks.
//
// CopilotTranslator 是 no-op——但不再是因为 copilot 没有 hooks。Copilot 确实支持
// lifecycle hooks（docs.github.com/en/copilot/reference/hooks-reference），且自
// Wave 2c 起 plugin pack 以 plugin 根的 plugins/forge/hooks.json 携带 gate 接线
// （copilot 文档化的 plugin-hook 位置；见 copilot_hooks.go）——marketplace 安装直接
// 接线 copilot，无手动 init 步骤。copilot 仍然没有的，是 forge 可以把 hooks 合并进
// 去的**用户级配置文件**（其 hooks 随 plugin manifest / 编辑器设置走，不在磁盘上的
// 纯文件里），故本 translator 无东西可写：`forge init --agents copilot` 设计上就是
// no-op，plugin 是唯一接线路径。遗留项目文件（.github/instructions/）由
// stripProjectLevelForgeAssets 剥除。AgentCopilot 仍是合法的 --agents 值（解析兼
// 容），但翻译为空。
type CopilotTranslator struct{}

func (t *CopilotTranslator) Translate(projectDir string, input *TranslationInput) error {
	return nil
}

func (t *CopilotTranslator) AgentType() AgentType {
	return AgentCopilot
}
