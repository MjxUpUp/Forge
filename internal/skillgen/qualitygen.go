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

	// Task Bridge Protocol
	sb.WriteString("## Task Bridge Protocol\n\n")
	sb.WriteString("Forge task 和 Claude Code task 必须保持同步。Forge 是 source of truth（门禁、评分、经验复盘）。\n\n")
	sb.WriteString("> **⚠️ 编码前必做**：无论是从 plan mode 审批后进入编码，还是直接开始修改代码，第一步永远是 `forge task start`。不要在 master 上直接写代码。不要在写完代码后才补启任务。\n\n")
	sb.WriteString("**强制顺序**：`forge task start` → gate task-understand → gate task-design → 写代码 → gate task-implement → gate task-verify → gate task-complete\n\n")

	sb.WriteString("### 非平凡变更（>10 行）前必须启动 Forge 任务\n\n")
	sb.WriteString("1. 运行 `forge task list --json` 检查已有任务\n")
	sb.WriteString("2. 如果有未完成的任务（`completed_at` 为 null），记录其 `task_ref`，跳到步骤 5\n")
	sb.WriteString("3. 如果没有活跃任务：\n")
	sb.WriteString("   a. 从任务主题推导 conventional branch ref（格式：`<type>/<desc>`，如 `feat/add-auto-branch`、`fix/null-pointer`）\n")
	sb.WriteString("   b. 如果当前在 master/main 分支，运行：`forge task start --ref <ref> --title \"<title>\" --branch --json`\n")
	sb.WriteString("   c. 如果已在 feature 分支，运行：`forge task start --ref <ref> --title \"<title>\" --json`\n")
	sb.WriteString("   d. 如果报错 `\"already exists\"`，运行 `forge task status --ref <ref> --json` 获取已有任务\n")
	sb.WriteString("4. 存储 `task_ref` — 后续所有 gate 命令都需通过 `--ref` 指定\n")
	sb.WriteString("5. 如果同时使用 TaskCreate，在 description 中写 `Forge ref: <ref>` 建立映射\n\n")
	sb.WriteString("**分支类型前缀**：`feat/` `fix/` `refactor/` `test/` `chore/` `docs/` `ci/` `perf/` `build/` `style/`\n\n")
	sb.WriteString("**注意**：`--branch` 会自动从 master/main 创建新分支并切换。任务完成后 merge 回 master。\n\n")

	sb.WriteString("### 门禁推进\n\n")
	sb.WriteString("工作推进时，逐步验证对应的门禁（所有命令带 `--ref <ref>`）：\n\n")
	sb.WriteString("| 阶段 | 命令 | 何时运行 |\n")
	sb.WriteString("|------|------|----------|\n")
	sb.WriteString("| 理解任务 | `forge task gate task-understand --ref <ref>` | 描述意图和范围后 |\n")
	sb.WriteString("| 方案设计 | `forge task gate task-design --ref <ref>` | 描述设计方案后 |\n")
	sb.WriteString("| 代码实现 | `forge task gate task-implement --ref <ref>` | 代码编译通过、已提交（自动检查） |\n")
	sb.WriteString("| 测试验证 | `forge task gate task-verify --ref <ref>` | 测试通过后 |\n")
	sb.WriteString("| 完成确认 | `forge task gate task-complete --ref <ref>` | E2E 验证通过后 |\n\n")
	sb.WriteString("所有门禁通过后运行 `forge task complete --ref <ref>` 触发评分。\n\n")

	sb.WriteString("### 门禁要求\n\n")
	sb.WriteString("每个门禁代表一个独立的工作阶段，不是形式主义检查：\n\n")
	sb.WriteString("- **间隔 ≥ 1 分钟**：相邻门禁之间至少做 1 分钟真实工作（Read/Grep/Write 等工具调用），不要用 sleep 绕过\n")
	sb.WriteString("- **活动 ≥ 2 次工具调用**：门禁之间至少使用 2 次工具做实质性探索或分析\n")
	sb.WriteString("- **task-implement 需 HEAD 移动**：代码必须已 `git commit`（未提交的变更也会被认可）\n")
	sb.WriteString("- **task-complete 无间隔要求**：最后一道门禁跳过 timing/activity 检查\n\n")

	sb.WriteString("### 三层防御架构\n\n")
	sb.WriteString("Forge 通过三层防御确保代码变更都在任务流程内：\n\n")
	sb.WriteString("1. **task-guard**（PreToolUse Write|Edit）：拦截无活跃任务的源码写入\n")
	sb.WriteString("2. **bash-guard**（PreToolUse Bash）：检测 Bash 中的写文件模式（writeFile、cat >、sed -i 等），无任务时拦截\n")
	sb.WriteString("3. **file-sentinel**（PostToolUse Bash）：对比 Bash 执行前后的文件状态，发现未授权源码变更自动 revert\n\n")
	sb.WriteString("此外，**自保护机制**阻止直接修改 `.forge/*` 和 `.claude/settings*`——这些文件只能通过 `forge` 命令操作。\n\n")

	sb.WriteString("### 错误恢复\n\n")
	sb.WriteString("| 错误信息 | 原因 | 解决方法 |\n")
	sb.WriteString("|----------|------|----------|\n")
	sb.WriteString("| Write/Edit denied by task-guard | 无活跃任务 | `forge task start --ref <type>/<name>` |\n")
	sb.WriteString("| Bash denied by bash-guard | Bash 含写文件操作且无任务 | 先启动任务 |\n")
	sb.WriteString("| gate passed too quickly | 门禁间隔 <1 分钟 | 做真实的探索/分析工作 |\n")
	sb.WriteString("| insufficient work activity | 工具调用 <2 次 | 用 Read/Grep/Glob 探索代码 |\n")
	sb.WriteString("| HEAD not moved | task-implement 前没有新提交 | 先写代码并 `git commit` |\n")
	sb.WriteString("| Reverted by file-sentinel | Bash 写了源码但无任务 | 先启动任务，或用 Write/Edit |\n")
	sb.WriteString("| task not complete. Missing gates | 未通过所有门禁 | 先 `forge task gate task-complete --ref <ref>` |\n\n")

	sb.WriteString("### 会话结束\n\n")
	sb.WriteString("session 结束时 task-verify Stop hook 自动运行。如果连续 3 次失败，允许带警告退出（避免永久卡死）。也可设置 `FORGE_SKIP_VERIFY=1` 跳过。\n\n")

	sb.WriteString("`forge task complete` 成功后，通过 TaskUpdate 标记对应的 Claude Code task 为 completed。\n\n")

	sb.WriteString("### 例外\n\n")
	sb.WriteString("纯文档修改、单行 typo 修复、版本号 bump 不需要启动 Forge 任务。\n\n")

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
	sb.WriteString("forge task list           — 列出所有任务\n")
	sb.WriteString("```\n\n")

	// Scoring section
	sb.WriteString("## 任务质量评分\n\n")
	sb.WriteString("任务完成时自动评分（8 个维度，0-100 分，A-F 等级）：\n\n")
	sb.WriteString("| 维度 | 权重 | 说明 |\n")
	sb.WriteString("|------|------|------|\n")
	sb.WriteString("| 流程合规 | 20% | 门禁通过率、重试次数 |\n")
	sb.WriteString("| 测试充分性 | 20% | 测试文件变更比例 |\n")
	sb.WriteString("| 代码质量 | 15% | 编译门禁结果 |\n")
	sb.WriteString("| 断言保护 | 12% | 断言检查结果 |\n")
	sb.WriteString("| 变更范围 | 8% | 变更行数（小变更得分高） |\n")
	sb.WriteString("| 开发效率 | 5% | 完成耗时 |\n")
	sb.WriteString("| 工具选择 | 12% | 工具使用反模式（如用 Bash cat 而非 Read） |\n")
	sb.WriteString("| Skill 命中 | 8% | Skill 和 Forge 协议的使用率 |\n\n")
	sb.WriteString("使用 `forge task score` 查看评分详情，`forge task score --history` 查看历史。\n\n")

	// Experience review section
	sb.WriteString("## 经验复盘（低分任务）\n\n")
	sb.WriteString("当 `forge task complete` 输出包含 \"review required\" 或 \"review suggested\" 时：\n\n")
	sb.WriteString("1. 运行 `forge experience list` 确认 pending review\n")
	sb.WriteString("2. 对每个 mandatory review，执行根因分析：\n")
	sb.WriteString("   - 读取任务评分明细：`forge task score --ref <ref> --json`\n")
	sb.WriteString("   - 检查任务 git diff：`git diff main...HEAD`\n")
	sb.WriteString("   - 定位每个低分维度的具体代码问题\n")
	sb.WriteString("3. 为每个发现的模式生成 proposed rule：\n")
	sb.WriteString("   - category: gotchas（常见陷阱）/ patterns（反模式）/ apis（API 误用）\n")
	sb.WriteString("   - patterns 字段写正则，能被 experience-check.sh 扫描匹配\n")
	sb.WriteString("   - severity 与问题严重程度匹配\n")
	sb.WriteString("4. 将 proposed rules 写入 `.forge/experience/proposed/` 目录，JSON 格式：\n")
	sb.WriteString("   - id: \"exp-<hex>\"\n")
	sb.WriteString("   - source_review: \"<task-ref>\"\n")
	sb.WriteString("   - category / title / description / patterns / severity\n")
	sb.WriteString("   - status: \"proposed\"\n")
	sb.WriteString("5. 通知用户审批：`forge experience list` + `forge experience accept <id>`\n\n")

	// Project info
	sb.WriteString("## 当前项目信息\n\n")
	sb.WriteString(fmt.Sprintf("- **项目**: %s\n", p.Project))
	sb.WriteString(fmt.Sprintf("- **模式**: %s\n", p.Mode))
	sb.WriteString(fmt.Sprintf("- **项目门禁数**: %d\n", len(p.EnabledGates())))

	return sb.String()
}
