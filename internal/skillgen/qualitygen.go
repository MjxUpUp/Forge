package skillgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Harness/forge/internal/protocol"
	"github.com/Harness/forge/internal/pipeline"
	"github.com/Harness/forge/internal/taskpipeline"
)

// GenerateQualitySkill creates .claude/skills/forge-quality/SKILL.md — the
// quality protocol skill that is loaded at session start via CLAUDE.md reference.
// It contains quality standards, session rules, and task pipeline instructions.
func GenerateQualitySkill(projectDir string, proto *protocol.Protocol, p *pipeline.Pipeline) error {
	skillDir := filepath.Join(projectDir, ".claude", "skills", "forge-quality")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("failed to create quality skill dir: %w", err)
	}

	content := buildQualitySkillContent(proto, p)
	path := filepath.Join(skillDir, "SKILL.md")
	return os.WriteFile(path, []byte(content), 0644)
}

func buildQualitySkillContent(proto *protocol.Protocol, p *pipeline.Pipeline) string {
	var sb strings.Builder

	sb.WriteString("---\n")
	sb.WriteString("name: forge-quality\n")
	sb.WriteString("description: \"Forge 质量协议 — 每次开发会话自动执行的质量标准\"\n")
	sb.WriteString("---\n\n")

	sb.WriteString("# Forge 质量协议\n\n")
	sb.WriteString("你是本项目的质量守护者。以下标准在任何开发会话中都有效。\n\n")

	// Quality standards
	sb.WriteString("## 质量标准\n\n")
	for _, s := range proto.Standards {
		if !s.Enabled {
			continue
		}
		icon := "🔴"
		switch s.Severity {
		case "warning":
			icon = "🟡"
		case "info":
			icon = "🔵"
		}
		hookInfo := ""
		if s.EnforceHook != "" {
			hookInfo = fmt.Sprintf("（自动检查: %s）", s.EnforceHook)
		}
		sb.WriteString(fmt.Sprintf("- %s **%s**: %s %s\n", icon, s.Name, s.Description, hookInfo))
	}
	sb.WriteString("\n")

	// Session rules
	sb.WriteString("## 会话行为规则\n\n")
	for _, r := range proto.SessionRules {
		prefix := "必须"
		if !r.Mandatory {
			prefix = "建议"
		}
		trigger := ""
		switch r.Trigger {
		case "on_edit":
			trigger = "（修改代码时）"
		case "on_commit":
			trigger = "（提交代码时）"
		}
		sb.WriteString(fmt.Sprintf("- [%s] %s %s\n", prefix, r.Instruction, trigger))
	}
	sb.WriteString("\n")

	// Task pipeline section
	sb.WriteString("## 任务级管道\n\n")
	sb.WriteString("当检测到任务上下文（非 main 分支或显式任务）时，执行以下轻量门禁：\n\n")
	gates := taskpipeline.DefaultGates()
	for i, g := range gates {
		auto := ""
		if g.Auto {
			auto = " [自动]"
		}
		sb.WriteString(fmt.Sprintf("%d. **%s** (%s): %s%s\n", i+1, g.Name, g.ID, g.Description, auto))
	}
	sb.WriteString("\n")
	sb.WriteString("```\n")
	sb.WriteString("forge task start          — 开始任务（自动检测分支）\n")
	sb.WriteString("forge task status         — 查看任务门禁状态\n")
	sb.WriteString("forge task gate <id>      — 验证单道门禁\n")
	sb.WriteString("forge task complete       — 标记任务完成（自动评分）\n")
	sb.WriteString("forge task score          — 查看任务质量评分\n")
	sb.WriteString("```\n\n")

	// Scoring section
	sb.WriteString("## 任务质量评分\n\n")
	sb.WriteString("任务完成时自动评分（6 个维度，0-100 分，A-F 等级）：\n\n")
	sb.WriteString("| 维度 | 权重 | 说明 |\n")
	sb.WriteString("|------|------|------|\n")
	sb.WriteString("| 流程合规 | 25% | 门禁通过率、重试次数 |\n")
	sb.WriteString("| 测试充分性 | 25% | 测试文件变更比例 |\n")
	sb.WriteString("| 代码质量 | 20% | 编译门禁结果 |\n")
	sb.WriteString("| 断言保护 | 15% | 断言检查结果 |\n")
	sb.WriteString("| 变更范围 | 10% | 变更行数（小变更得分高） |\n")
	sb.WriteString("| 开发效率 | 5% | 完成耗时 |\n\n")
	sb.WriteString("使用 `forge task score` 查看评分详情，`forge task score --history` 查看历史。\n\n")

	// Project info
	sb.WriteString("## 当前项目信息\n\n")
	sb.WriteString(fmt.Sprintf("- **项目**: %s\n", p.Project))
	sb.WriteString(fmt.Sprintf("- **模式**: %s\n", p.Mode))
	sb.WriteString(fmt.Sprintf("- **项目门禁数**: %d\n", len(p.EnabledGates())))

	return sb.String()
}
