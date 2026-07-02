package agentbridge

// AllTranslators returns all available translators in a deterministic order.
func AllTranslators() []Translator {
	return []Translator{
		&ClaudeCodeTranslator{},
		&CursorTranslator{},
		&CopilotTranslator{},
		&WindsurfTranslator{},
		&CodexTranslator{},
		&OpencodeTranslator{},
		&PiTranslator{},
	}
}

// TranslateForAgents translates Forge config for the specified agents.
// If agents is empty, it does nothing.
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

// translatorMap creates a lookup map from agent type to translator.
func translatorMap(translators []Translator) map[AgentType]Translator {
	m := make(map[AgentType]Translator, len(translators))
	for _, t := range translators {
		m[t.AgentType()] = t
	}
	return m
}
