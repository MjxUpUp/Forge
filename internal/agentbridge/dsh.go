package agentbridge

// DshTranslator is a deliberate no-op, in the CopilotTranslator mould: DeepSeek
// Harness (dsh) supports typed interception points (tools/pre-execute,
// tools/post-execute, agent/pre-step, agent/session-start, agent/turn-stopping)
// and forge gates them via the Cordis wrapper at plugins/forge-dsh (npm package
// @agent_forge/forge-dsh) — the plugin IS the wiring path. What dsh has no
// analogue of is a user-level config file forge could merge hooks into: hook
// wiring rides dsh's plugin/bundle layer (cordis.patch.yml + `dsh plugin
// --profile web add`, which shells out to pnpm and may hit the network), so
// `forge init --agents dsh` writes nothing and only prints the install
// instruction in the init summary (internal/cli/init.go). AgentDsh remains a
// valid --agents value (parse + detect compatibility) but translates to nothing.
//
// The wrapper's hook roster mirrors ForgeHookSpec via plugins/forge-dsh/lib/
// spec.json — TestDshPluginSpecMirrorsSpec pins that mirror so a spec change
// can never silently drift from what dsh enforces.
//
// DshTranslator 是刻意的 no-op（CopilotTranslator 同款）：DeepSeek Harness
// （dsh）支持类型化拦截点（tools/pre-execute、tools/post-execute、
// agent/pre-step、agent/session-start、agent/turn-stopping），forge 经
// plugins/forge-dsh 的 Cordis 包装层（npm 包 @agent_forge/forge-dsh）对它们
// 实施门禁——plugin 即接线路径。dsh 没有的，是 forge 可以把 hooks 合并进去的
// **用户级配置文件**：hook 接线走 dsh 的 plugin/bundle 层（cordis.patch.yml +
// `dsh plugin --profile web add`，后者 shell 到 pnpm 且可能触网），故
// `forge init --agents dsh` 不写任何文件，只在 init 摘要里打印安装指引
// （internal/cli/init.go）。AgentDsh 仍是合法的 --agents 值（解析 + 检测兼容），
// 但翻译为空。
//
// 包装层的 hook 名册经 plugins/forge-dsh/lib/spec.json 镜像
// ForgeHookSpec——TestDshPluginSpecMirrorsSpec 钉住该镜像，spec 变更不可能
// 静默漂离 dsh 实际执行的名册。
type DshTranslator struct{}

func (t *DshTranslator) Translate(projectDir string, input *TranslationInput) error {
	return nil
}

func (t *DshTranslator) AgentType() AgentType {
	return AgentDsh
}
