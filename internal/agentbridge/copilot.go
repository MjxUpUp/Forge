package agentbridge

// CopilotTranslator is a no-op after the user-level-assets refactor.
//
// Copilot's only integration channel was guidance text at
// .github/instructions/forge-quality.instructions.md (project-level — Copilot has no
// lifecycle hooks to enforce gates, and no plain-file user-level instruction channel
// forge can write: VS Code user instructions live in editor settings, not on disk).
// Writing project files conflicts with the zero-project-write default, so the
// translator no longer emits anything; legacy project files are stripped by
// stripProjectLevelForgeAssets. AgentCopilot remains a valid --agents value (parse
// compatibility) but translates to nothing.
//
// CopilotTranslator 在 user-level-assets 重构后是 no-op。
//
// Copilot 唯一的集成渠道是 .github/instructions/forge-quality.instructions.md 的
// guidance 文本（项目级——Copilot 没有 lifecycle hooks 可 enforce 门禁，也没有
// forge 可写的纯文件用户级指令渠道：VS Code 用户指令存在编辑器设置里，不在磁盘上）。
// 写项目文件与零项目写入默认冲突，故 translator 不再产出；遗留项目文件由
// stripProjectLevelForgeAssets 剥除。AgentCopilot 仍是合法的 --agents 值
// （解析兼容），但翻译为空。
type CopilotTranslator struct{}

func (t *CopilotTranslator) Translate(projectDir string, input *TranslationInput) error {
	return nil
}

func (t *CopilotTranslator) AgentType() AgentType {
	return AgentCopilot
}
