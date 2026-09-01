package agentbridge

// tomlForgeMarkStart/End are the TOML-comment form of the forge managed-section
// markers, shared by every translator that injects a forge section into a config.toml.
//
// tomlForgeMarkStart/End 是 forge 托管段的 TOML 注释形态标记，所有往 config.toml
// 注入 forge 段的 translator（kimi、codex）共用。HTML 注释形态的权威源在
// util.ForgeSectionStart/End；TOML 形态不是它的字符串前缀派生（`# ` 换掉
// `<!-- -->`），故在此独立成源——曾经 codex/kimi 两处互拷已收敛于此
// （2026-09 代码普查 R3）。
const (
	tomlForgeMarkStart = "# FORGE:START"
	tomlForgeMarkEnd   = "# FORGE:END"
)
