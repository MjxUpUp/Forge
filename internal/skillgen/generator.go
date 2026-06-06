package skillgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Harness/forge/internal/pipeline"
)

// GenerateSkill creates the .claude/skills/forge-pipeline/SKILL.md orchestration skill.
func GenerateSkill(projectDir string, p *pipeline.Pipeline) error {
	skillDir := filepath.Join(projectDir, ".claude", "skills", "forge-pipeline")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("failed to create skill dir: %w", err)
	}

	content := buildSkillContent(p)
	path := filepath.Join(skillDir, "SKILL.md")
	return os.WriteFile(path, []byte(content), 0644)
}

func buildSkillContent(p *pipeline.Pipeline) string {
	var sb strings.Builder

	sb.WriteString("---\n")
	sb.WriteString("name: forge-pipeline\n")
	sb.WriteString("description: \"Forge 管道执行 — 当用户要求运行门禁管道、执行管道、或继续开发流程时使用\"\n")
	sb.WriteString("---\n\n")
	sb.WriteString("# Forge 管道编排技能\n\n")
	sb.WriteString("你是 Forge 管道编排器。你的职责是按依赖顺序执行门禁，验证每个门禁，并处理失败。\n\n")
	sb.WriteString("## 核心流程\n\n")
	sb.WriteString("```\n")
	sb.WriteString("1. forge status --json → 获取当前管道状态\n")
	sb.WriteString("2. 找到下一个待执行的 gate\n")
	sb.WriteString("3. dispatch subagent 执行该 gate 的 prompt\n")
	sb.WriteString("4. 执行完成后运行: forge gate <gate-id>\n")
	sb.WriteString("5. 读取 .forge/gates/<gate-id>/status.json\n")
	sb.WriteString("6. passed → 执行下一个 gate\n")
	sb.WriteString("7. failed → dispatch 修复 subagent，然后重新验证\n")
	sb.WriteString("8. 全部通过后运行 forge status 显示最终状态\n")
	sb.WriteString("```\n\n")

	sb.WriteString("## 管道信息\n\n")
	sb.WriteString(fmt.Sprintf("- **项目**: %s\n", p.Project))
	sb.WriteString(fmt.Sprintf("- **模式**: %s\n", p.Mode))
	sb.WriteString(fmt.Sprintf("- **Gate 数量**: %d\n\n", len(p.EnabledGates())))

	sb.WriteString("## Gate 定义\n\n")
	for _, gate := range p.EnabledGates() {
		sb.WriteString(fmt.Sprintf("### %s (%s)\n\n", gate.Name, gate.ID))
		sb.WriteString(fmt.Sprintf("- **依赖**: %s\n", formatDeps(gate.DependsOn)))
		sb.WriteString(fmt.Sprintf("- **失败策略**: %s\n", gate.OnFailure))
		if gate.RequiresHumanApproval {
			sb.WriteString("- **需要人工审批**: 是\n")
		}
		if len(gate.Artifacts.Outputs) > 0 {
			sb.WriteString(fmt.Sprintf("- **产出物**: %s\n", strings.Join(gate.Artifacts.Outputs, ", ")))
		}
		sb.WriteString("\n")

		if gate.Prompt != "" {
			sb.WriteString("**Subagent 指令**:\n\n")
			sb.WriteString("```\n")
			sb.WriteString(gate.Prompt)
			sb.WriteString("\n```\n\n")
		}

		if len(gate.Checks) > 0 {
			sb.WriteString("**检查规则**:\n\n")
			for _, c := range gate.Checks {
				sb.WriteString(fmt.Sprintf("- `%s` (type: %s)\n", c.Name, c.Type))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("## 执行指令\n\n")
	sb.WriteString("当用户要求运行管道时，按以下步骤执行：\n\n")
	sb.WriteString("1. 运行 `forge status --json` 查看当前状态\n")
	sb.WriteString("2. 如果有未完成的 gate：\n")
	sb.WriteString("   a. 使用 Agent 工具 dispatch 一个 subagent，prompt 为该 gate 的指令\n")
	sb.WriteString("   b. subagent 完成后，运行 `forge gate <gate-id>` 验证\n")
	sb.WriteString("   c. 检查验证结果\n")
	sb.WriteString("3. 如果验证失败：\n")
	sb.WriteString("   a. 读取 `.forge/gates/<gate-id>/status.json` 获取失败详情\n")
	sb.WriteString("   b. dispatch 一个修复 subagent，prompt 包含失败详情\n")
	sb.WriteString("   c. 重新运行 `forge gate <gate-id>`\n")
	sb.WriteString("   d. 如果 `on_failure: abort`，最多重试一次后停止\n")
	sb.WriteString("4. 如果验证通过，继续下一个 gate\n")
	sb.WriteString("5. 所有 gate 通过后，运行 `forge status` 显示完成状态\n")

	return sb.String()
}

func formatDeps(deps []string) string {
	if len(deps) == 0 {
		return "无"
	}
	return strings.Join(deps, ", ")
}
