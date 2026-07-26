package agentbridge

// AllTranslators returns all available translators in deterministic order.
//
// AllTranslators 以 deterministic 顺序返回所有可用 translator。
func AllTranslators() []Translator {
	return []Translator{
		&ClaudeCodeTranslator{},
		&CursorTranslator{},
		&CopilotTranslator{},
		&WindsurfTranslator{},
		&CodexTranslator{},
		&OpencodeTranslator{},
		&ClineTranslator{},
	}
}

// TranslateForAgents translates the Forge config to the specified agents. No-op when agents is empty.
//
// TranslateForAgents 把 Forge 配置翻译给指定 agents。agents 为空时 no-op。
func TranslateForAgents(projectDir string, agents []AgentType, input *TranslationInput) []error {
	if len(agents) == 0 {
		return nil
	}

	translators := translatorMap(AllTranslators())
	var errs []error

	for _, agent := range agents {
		t, ok := translators[agent]
		if !ok {
			continue
		}
		if err := t.Translate(projectDir, input); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// translatorMap builds a lookup table from agent type to translator.
//
// translatorMap 构建 agent type 到 translator 的查找表。
func translatorMap(translators []Translator) map[AgentType]Translator {
	m := make(map[AgentType]Translator, len(translators))
	for _, t := range translators {
		m[t.AgentType()] = t
	}
	return m
}
