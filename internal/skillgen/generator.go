package skillgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/pipeline"
)

// GenerateSkill creates the .claude/skills/forge-pipeline/SKILL.md orchestration skill.
// inferredGateIDs lists gates that were auto-detected as completed at init time.
// When empty, no artifact fallback section is added.
func GenerateSkill(projectDir string, p *pipeline.Pipeline, inferredGateIDs []string) error {
	skillDir := filepath.Join(projectDir, ".claude", "skills", "forge-pipeline")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("failed to create skill dir: %w", err)
	}

	content := buildSkillContent(p, inferredGateIDs)
	path := filepath.Join(skillDir, "SKILL.md")
	return os.WriteFile(path, []byte(content), 0644)
}

func buildSkillContent(p *pipeline.Pipeline, inferredGateIDs []string) string {
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
	sb.WriteString("5. 读取 gate status（`forge gate <gate-id>` 或 `forge data-dir`/gates/<gate-id>/status.json）\n")
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
	sb.WriteString("   a. 运行 `forge gate <gate-id>` 获取失败详情（或读 `forge data-dir`/gates/<gate-id>/status.json）\n")
	sb.WriteString("   b. dispatch 一个修复 subagent，prompt 包含失败详情\n")
	sb.WriteString("   c. 重新运行 `forge gate <gate-id>`\n")
	sb.WriteString("   d. 如果 `on_failure: abort`，最多重试一次后停止\n")
	sb.WriteString("4. 如果验证通过，继续下一个 gate\n")
	sb.WriteString("5. 所有 gate 通过后，运行 `forge status` 显示完成状态\n")

	// Add artifact fallback section when gates were inferred at init time
	if len(inferredGateIDs) > 0 {
		sb.WriteString("\n## Artifact 回退策略\n\n")
		sb.WriteString("以下门禁在项目初始化时被自动检测为已完成（无 gate 产出物）：\n\n")
		for _, id := range inferredGateIDs {
			sb.WriteString(fmt.Sprintf("- `%s`\n", id))
		}
		sb.WriteString("\n")
		sb.WriteString("当执行某个 gate 的 prompt 时，如果引用了上述推断门禁的产出物（如 `{{.GateArtifacts \"gate-1-prd\" \"prd.md\"}}`），")
		sb.WriteString("`forge data-dir`/gates/<id>/ 目录下不会有这些文件。此时应该：\n\n")
		sb.WriteString("1. **不要尝试读取 `forge data-dir`/gates/<inferred-id>/ 下的文件** — 它们不存在\n")
		sb.WriteString("2. **直接从项目中获取等价信息**：\n")
		sb.WriteString("   - `prd.md` → 阅读项目的 README.md、现有代码结构和功能描述\n")
		sb.WriteString("   - `acceptance-criteria.json` → 从现有代码和测试中推断验收条件\n")
		sb.WriteString("   - `plan.md` / `tasks.json` → 从项目目录结构和 git log 推断已完成的任务\n")
		sb.WriteString("   - `test-results.json` → 直接运行项目的测试获取结果\n")
		sb.WriteString("   - `coverage.json` → 运行覆盖率工具获取\n")
		sb.WriteString("3. **将推断内容作为上下文传给 subagent**，而不是期望从 gate 目录读取\n")
	}

	return sb.String()
}

func formatDeps(deps []string) string {
	if len(deps) == 0 {
		return "无"
	}
	return strings.Join(deps, ", ")
}
