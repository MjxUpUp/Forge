package agentbridge

// ClineTranslator is a no-op after the user-level-assets refactor.
//
// Cline (a VS Code extension) has no lifecycle hooks — no PreToolUse/Stop — so it
// cannot enforce gates; its only channel was guidance rules at
// .clinerules/forge-quality.md (project-level). Cline global rules live under
// ~/Documents/Cline/Rules (platform-dependent, not a stable programmatic target), so
// there is no reliable user-level channel either. Writing project files conflicts with
// the zero-project-write default, so the translator no longer emits anything; legacy
// project files are stripped by stripProjectLevelForgeAssets. AgentCline remains a
// valid --agents value (parse compatibility) but translates to nothing.
//
// ClineTranslator 在 user-level-assets 重构后是 no-op。
//
// Cline（VS Code 扩展）没有 lifecycle hooks——无 PreToolUse/Stop——故无法 enforce
// 门禁；唯一渠道是 .clinerules/forge-quality.md 的 guidance 规则（项目级）。Cline
// 全局规则在 ~/Documents/Cline/Rules（平台相关，不是稳定的程序化写入目标），也没有
// 可靠的用户级渠道。写项目文件与零项目写入默认冲突，故 translator 不再产出；遗留
// 项目文件由 stripProjectLevelForgeAssets 剥除。AgentCline 仍是合法的 --agents 值
// （解析兼容），但翻译为空。
type ClineTranslator struct{}

func (t *ClineTranslator) Translate(projectDir string, input *TranslationInput) error {
	return nil
}

func (t *ClineTranslator) AgentType() AgentType {
	return AgentCline
}
