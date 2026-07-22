package skillgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// GenerateQualitySkill creates .claude/skills/forge-quality/SKILL.md — the
// quality protocol skill that is loaded at session start via CLAUDE.md reference.
// It contains quality standards, session rules, and task pipeline instructions.
func GenerateQualitySkill(projectDir string, proto *protocol.Protocol) error {
	skillDir := filepath.Join(projectDir, ".claude", "skills", "forge-quality")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("failed to create quality skill dir: %w", err)
	}

	content := buildQualitySkillContent(projectDir, proto)
	path := filepath.Join(skillDir, "SKILL.md")
	return os.WriteFile(path, []byte(content), 0644)
}

func buildQualitySkillContent(projectDir string, proto *protocol.Protocol) string {
	var sb strings.Builder

	sb.WriteString("---\n")
	sb.WriteString("name: forge-quality\n")
	// Trigger-oriented description (Anthropic skill standard): describe WHEN to
	// invoke, not what the skill "is". A vague "auto-executed standards" phrasing
	// gives the model no signal to load this on demand. The scenarios below are
	// the real entry points — pre-coding task start, advancing gates, recovering
	// from a guard WARN.
	sb.WriteString("description: \"在 Forge 项目中开始或推进编码任务时调用——启动 forge task、推进 task-implement/verify/complete 门禁、commit 与 complete 的时机、以及 task-guard/bash-guard/file-sentinel 警告的恢复。也覆盖评分阈值与证据链反馈。遇到 forge 门禁推进、guard 警告、或任务卡住需要 abort 时使用。\"\n")
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
	sb.WriteString("Forge task 和 Claude Code task 必须保持同步。Forge 是 source of truth（门禁、评分）。\n\n")
	sb.WriteString("> **⚠️ 编码前必做**：无论是从 plan mode 审批后进入编码，还是直接开始修改代码，第一步永远是 `forge task start`。不要在 master 上直接写代码。不要在写完代码后才补启任务。\n\n")
	sb.WriteString("**强制顺序**：`forge task start` → 写代码 → gate task-implement → gate task-verify → gate task-complete\n\n")

	sb.WriteString("### 源码变更前必须启动 Forge 任务\n\n")
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
	sb.WriteString("| 代码实现 | `forge task gate task-implement --ref <ref>` | 代码编译通过、已提交（自动检查） |\n")
	sb.WriteString("| 测试验证 | `forge task gate task-verify --ref <ref>` | 测试通过后 |\n")
	sb.WriteString("| 完成确认 | `forge task gate task-complete --ref <ref>` | E2E 验证通过后 |\n\n")
	sb.WriteString("所有门禁通过后运行 `forge task complete --ref <ref>` 触发评分。\n\n")
	sb.WriteString("> **提交时机（重要）**：`git commit` 必须在 `forge task complete` **之前**——`complete` 会清空 active task ref，之后提交源码会被 file-sentinel quarantine。正确顺序：三门禁通过 → `git commit` → `forge task complete`。若已 complete 才发现要提交，开一个 `chore/*-commit` 任务放行。\n\n")

	sb.WriteString("### 门禁要求\n\n")
	sb.WriteString("每个门禁代表一个独立的工作阶段，不是形式主义检查：\n\n")
	sb.WriteString("- **间隔 ≥ 1 分钟**：相邻门禁之间至少做 1 分钟真实工作（Read/Grep/Write 等工具调用），不要用 sleep 绕过\n")
	sb.WriteString("- **活动 ≥ 1 次工具调用**：门禁之间至少使用 1 次工具做实质性探索或分析\n")
	sb.WriteString("- **task-implement 需代码变更**：feature 分支上需有新提交，或工作区有未提交改动\n")
	sb.WriteString("- **task-complete 无间隔要求**：最后一道门禁跳过 timing/activity 检查\n\n")

	sb.WriteString("### 门禁退出码契约（BLOCKED vs ADVISORY）\n\n")
	sb.WriteString("门禁结果用显式前缀标记，**退出码——不是文案——才是契约**。按退出码行动，不要靠解析散文判断状态：\n\n")
	sb.WriteString("- **`BLOCKED:`**（非零退出）= 硬阻断，gate **未通过**。看到 `BLOCKED:` = 必须修复后重跑 `forge task gate`，不是\"继续观察\"。M2 事故正是把硬错误散文（\"Read and understand the code\"）误读成提醒而跳过——此前缀专治此误读\n")
	sb.WriteString("- **`ADVISORY:`**（零退出）= 软信号，gate 仍通过，已记 checklog。\"应修但不阻断\"——可继续推进，但会降低评分\n")
	sb.WriteString("- **`✅ passed`** = 通过\n\n")

	sb.WriteString("### 三层防御架构\n\n")
	sb.WriteString("Forge 通过三层防御确保代码变更都在任务流程内：\n\n")
	sb.WriteString("1. **task-guard**（PreToolUse Write|Edit）：无活跃任务时 Write/Edit 源码只 WARN（`.forge/*` 自保护文件才 FAIL）；feature 分支无任务时自动建任务\n")
	sb.WriteString("2. **bash-guard**（PreToolUse Bash）：无任务时 Bash 写文件模式（writeFile、cat >、sed -i 等）只 WARN\n")
	sb.WriteString("3. **file-sentinel**（PostToolUse Bash）：对比 Bash 执行前后的文件状态，未授权源码变更 quarantine 到用户级 DataDir/quarantine/（`forge data-dir` 查看路径）\n\n")
	sb.WriteString("此外，**自保护机制**阻止直接修改 `.forge/*` 和 `.claude/settings*`——这些文件只能通过 `forge` 命令操作。\n\n")
	sb.WriteString("**read-before-edit**（PreToolUse Write|Edit，活跃任务内）是 read-before-modify 的 shift-left 硬门禁：编辑本会话未 Read 过的现存源文件 → `BLOCKED`。Edit 需精确匹配旧文本，未读即凭记忆盲改（M2 事故根因：old_string 撞中、错改入库）。豁免新建文件/测试文件/非源码；批量重构逃生 `forge task override --work-activity disable`（降 evidence 强度到 Weak）。reads-log 落盘随会话存活，压缩后仍累计——压缩前 Read 过的文件仍算数。\n\n")
	sb.WriteString("### 辅助质量检查（仅 WARN 不阻塞）\n\n")
	sb.WriteString("- **assertion-check/auto-compile**：检测断言弱化、提醒编译自检（advisory，仅记录不阻塞，由 agent 自律）\n\n")

	sb.WriteString("### 错误恢复\n\n")
	sb.WriteString("| 错误信息 | 原因 | 解决方法 |\n")
	sb.WriteString("|----------|------|----------|\n")
	sb.WriteString("| WARN [task-guard] ... allowed but not tracked | 无活跃任务时 Write/Edit 源码（仅警告） | 启动任务让变更被追踪和评分 |\n")
	sb.WriteString("| WARN [bash-guard] ... Bash write without active task | 无任务时 Bash 写文件（仅警告，源码会被 file-sentinel quarantine） | 先启动任务 |\n")
	sb.WriteString("| BLOCKED: passed without reading any code | task 期间 Read 次数为 0（硬阻断非提醒） | 改代码前先 Read 相关文件理解上下文，再重跑 `forge task gate` |\n")
	sb.WriteString("| BLOCKED: insufficient work activity | 工具调用 <1 次（硬阻断非提醒） | 用 Read/Grep/Glob 探索代码 |\n")
	sb.WriteString("| Quarantined by file-sentinel | Bash 写了源码但无任务 | 文件在用户级 DataDir/quarantine/（`forge data-dir` 查看路径），可恢复。先启动任务 |\n")
	sb.WriteString("| complete 后提交被 file-sentinel 拦 | complete 已清 active task ref | 先 commit 再 complete；或开 `chore/*-commit` 任务放行 |\n")
	sb.WriteString("| task not complete. Missing gates | 未通过所有门禁 | 先 `forge task gate task-complete --ref <ref>` |\n\n")

	sb.WriteString("### 会话结束\n\n")
	sb.WriteString("session 结束时 task-verify Stop hook 自动运行（advisory：仅记录问题到 checklog，不阻塞会话）。也可设置 `FORGE_SKIP_VERIFY=1` 跳过。\n\n")

	sb.WriteString("`forge task complete` 成功后，通过 TaskUpdate 标记对应的 Claude Code task 为 completed。\n\n")

	sb.WriteString("### 例外\n\n")
	sb.WriteString("纯文档修改、单行 typo 修复、版本号 bump 不需要启动 Forge 任务。\n\n")

	// Red Flags — judgmental quality rules sunk from runtime hooks (read-check,
	// scope-guard, clone-check) to declarative skill text, per the layered noise
	// treatment: hard constraints (assertion/auto-compile/task-guard/file-sentinel)
	// stay as runtime hooks because skill text cannot deterministically block;
	// judgmental rules become text the agent reads and follows, removing the
	// per-tool-call WARN noise those hooks generated.
	sb.WriteString("## Red Flags（判断性质量信号，自律遵守）\n\n")
	sb.WriteString("以下规则原为 runtime hook，现已下沉为声明式文本——agent 可读可循，去判断性噪音。违反不阻塞，但会降低任务评分。\n\n")
	sb.WriteString("- **先读再改**：修改代码前先 Read 理解上下文。read-before-edit hook 已在活跃任务内硬阻断编辑未 Read 过的现存源文件（见上）；此条覆盖 hook 之外的场景（非任务编辑、跨会话接手）——凭记忆/Grep 片段就改既有代码是 M2 类事故的温床。\n")
	sb.WriteString("- **聚焦变更**：单次任务累计变更 >400 行需自检是否聚焦；>2000 行考虑拆分提交以便 review。\n")
	sb.WriteString("- **避免重复**：文件重复行占比高（unique 行 <30%）时主动去重；精确检测用 `forge clone check`。\n\n")

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
	sb.WriteString("forge task abort [--ref]  — 中止并删除任务（清理 ghost/卡住任务，不评分）\n")
	sb.WriteString("forge task score          — 查看任务质量评分\n")
	sb.WriteString("forge task list           — 列出所有任务\n")
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
	sb.WriteString("**阈值**：A ≥ 90 / B ≥ 80 / C ≥ 70 / D ≥ 60 / F < 60。低分仅记录评分与证据链结论不再阻塞 complete。\n\n")
	sb.WriteString("使用 `forge task score` 查看评分详情，`forge task score --history` 查看历史。\n\n")

	// Project info
	sb.WriteString("## 当前项目信息\n\n")
	sb.WriteString(fmt.Sprintf("- **项目**: %s\n", filepath.Base(projectDir)))

	return sb.String()
}
