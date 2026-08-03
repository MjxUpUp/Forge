package agentbridge

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/skillgen"
)

// ReasonixTranslator wires forge at USER level into reasonix: the forge-quality
// skill goes into ~/.reasonix/skills/forge-quality/SKILL.md — the reasonix home
// skills directory, its native skill mechanism (SKILL.md files are loaded into the
// session skill index). reasonix has no lifecycle-hook extension point (its tool
// set is fixed), so there is nothing else to wire: no hooks.json, no settings, no
// MCP (MCP was deliberately torn out of forge — internal/agentbridge/mcpconfig.go).
//
// No-op when the reasonix home does not exist — same detection-self-poison guard as
// the claude-code translator: Forge never creates an agent's config home itself
// (creating ~/.reasonix would flip the "reasonix is installed" signal and re-trigger
// wiring on every init). The content is the shared conditional-activation skill
// (skillgen.GenerateUserQualitySkillTo) — visible in every project, effective only
// in forge-registered ones.
//
// Project-level .reasonix/ assets are never written: the default zero-project-write
// model keeps the project dir clean, and reasonix's global skills are visible in
// every project anyway.
//
// ReasonixTranslator 在用户级接线 reasonix：forge-quality skill 进
// ~/.reasonix/skills/forge-quality/SKILL.md——reasonix home skills 目录，即其原生
// skill 机制（SKILL.md 文件会被加载进会话 skill 索引）。reasonix 没有
// lifecycle-hook 扩展点（工具集固定），故除此之外无需接线：无 hooks.json、无
// settings、无 MCP（MCP 已被 forge 主动拆除——internal/agentbridge/mcpconfig.go）。
//
// reasonix home 不存在时 no-op——与 claude-code translator 同款检测自毒防护：
// Forge 绝不自行创建 agent 的配置 home（创建 ~/.reasonix 会翻转"reasonix 已装"
// 信号，让每次 init 都重新接线）。内容为共享的条件激活 skill
// （skillgen.GenerateUserQualitySkillTo）——对所有项目可见，仅在 forge 注册项目
// 中生效。
//
// 从不写项目级 .reasonix/ 资产：默认零项目写入模型保持项目目录干净，且 reasonix
// 的全局 skill 本就对所有项目可见。
type ReasonixTranslator struct{}

func (t *ReasonixTranslator) AgentType() AgentType {
	return AgentReasonix
}

func (t *ReasonixTranslator) Translate(projectDir string, input *TranslationInput) error {
	if input.Protocol == nil {
		return nil // nothing to render without a protocol (mirrors claude-code)
	}
	home, err := ReasonixConfigHome()
	if err != nil {
		return fmt.Errorf("reasonix: %w", err)
	}
	// No-op when reasonix is not installed — GenerateUserQualitySkillTo's self-poison
	// guard skips a missing home; Forge never creates the agent's config home.
	//
	// reasonix 未安装时 no-op——GenerateUserQualitySkillTo 的自毒防护跳过不存在的
	// home；Forge 绝不创建 agent 的配置 home。
	if err := skillgen.GenerateUserQualitySkillTo(filepath.Join(home, "skills"), input.Protocol); err != nil {
		return fmt.Errorf("reasonix: %w", err)
	}
	return nil
}

// ReasonixConfigHome resolves reasonix's config home: $REASONIX_HOME when set,
// otherwise ~/.reasonix (the reasonix home directory). Env override doubles as test
// isolation (same pattern as KIMI_CODE_HOME / CODEX_HOME).
//
// ReasonixConfigHome 解析 reasonix 的配置 home：设了 $REASONIX_HOME 用它，否则
// ~/.reasonix（reasonix home 目录）。env 覆盖同时充当测试隔离（与
// KIMI_CODE_HOME / CODEX_HOME 同模式）。
func ReasonixConfigHome() (string, error) {
	if h := os.Getenv("REASONIX_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user home: %w", err)
	}
	return filepath.Join(home, ".reasonix"), nil
}
