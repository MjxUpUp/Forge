package skillgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	forgeSectionStart = "<!-- FORGE:START -->"
	forgeSectionEnd   = "<!-- FORGE:END -->"
)

// GenerateClaudeMD creates or updates .claude/CLAUDE.md with a Forge-managed
// quality protocol section. If the file already exists, only the marked section
// is replaced — user content is preserved.
func GenerateClaudeMD(projectDir string) error {
	claudeDir := filepath.Join(projectDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude dir: %w", err)
	}

	path := filepath.Join(claudeDir, "CLAUDE.md")

	forgeSection := buildForgeSection(true)

	// Read existing file if present
	existing, err := os.ReadFile(path)
	if err == nil && len(existing) > 0 {
		// Update only the Forge section
		updated := replaceForgeSection(string(existing), forgeSection)
		return os.WriteFile(path, []byte(updated), 0644)
	}

	// Create new file with just the Forge section
	return os.WriteFile(path, []byte(forgeSection), 0644)
}

// GenerateAgentsMD creates or updates the project-root AGENTS.md with the
// Forge-managed quality protocol section. AGENTS.md is the cross-agent
// instruction standard read by codex, cursor, copilot, windsurf, and cline
// (detect.go keys codex off .codex/, not AGENTS.md — AGENTS.md is a universal
// file forge generates on every init). Unlike CLAUDE.md
// (claude-only, references Claude slash commands), AGENTS.md carries the
// agent-agnostic protocol and points at the forge CLI/MCP surface. If the file
// exists, only the marked Forge section is replaced; user content outside the
// markers is preserved — same idempotent section-replace contract as CLAUDE.md.
func GenerateAgentsMD(projectDir string) error {
	path := filepath.Join(projectDir, "AGENTS.md")
	forgeSection := buildForgeSection(false)
	existing, err := os.ReadFile(path)
	if err == nil && len(existing) > 0 {
		updated := replaceForgeSection(string(existing), forgeSection)
		return os.WriteFile(path, []byte(updated), 0644)
	}
	return os.WriteFile(path, []byte(forgeSection), 0644)
}

func buildForgeSection(forClaude bool) string {
	var sb strings.Builder
	sb.WriteString(forgeSectionStart + "\n\n")
	sb.WriteString("# Forge 质量协议\n\n")
	sb.WriteString("本项目使用 Forge 进行质量保障。请遵守以下规则：\n\n")
	sb.WriteString("## 基本规则\n\n")
	sb.WriteString("1. **修改前先说意图** — 告诉用户你打算改什么、为什么改\n")
	sb.WriteString("2. **编译必须通过** — 每次修改后用你的编译命令确认编译通过（auto-compile hook 仅 advisory 提醒，由 agent 自检）\n")
	sb.WriteString("3. **不弱化断言** — 不删除 t.Fatal、assert! 等断言（assertion-check hook 检测到弱化仅 advisory 提醒，由 agent 自检）\n")
	sb.WriteString("4. **测试伴随变更** — 新代码有对应测试\n")
	sb.WriteString("5. **提交前确认** — commit 信息描述变更内容和原因\n")
	sb.WriteString("6. **结束前验证** — 会话结束前运行测试确认无破坏\n\n")

	// Task workflow — the most critical operational guidance to prevent agents
	// from hitting task-guard/bash-guard blocks without knowing what to do.
	sb.WriteString("## Task 工作流（必读）\n\n")
	sb.WriteString("**源码变更前必须启动 Forge 任务**——无任务时 Write/Edit 源码只触发 task-guard 警告（WARN，不拦截），但 Bash 写源码（sed/cat > 等）会被 file-sentinel quarantine。更关键：脱离任务的变更不被门禁追踪和质量评分。纯文档、单行 typo 修复、版本号 bump 除外。\n\n")
	sb.WriteString("### 启动任务\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("# 在 master/main 上：创建新分支 + 启动任务\n")
	sb.WriteString("forge task start --ref feat/xxx --title \"描述\" --branch\n")
	sb.WriteString("\n")
	sb.WriteString("# 已在 feature 分支上：不加 --branch（--branch 仅在 main/master 可用）\n")
	sb.WriteString("forge task start --ref fix/xxx --title \"描述\"\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### 门禁顺序（必须按序推进，所有命令带 `--ref <ref>`）\n\n")
	sb.WriteString("1. `task-implement` — 代码写完后运行（确认有代码变更；编译/断言改为 advisory 提醒，由 agent 自检）\n")
	sb.WriteString("2. `task-verify` — 测试伴随变更（advisory 提醒，由 agent 自检）\n")
	sb.WriteString("3. `task-complete` — E2E 验证通过后运行（`forge task gate task-complete --ref <ref>`）\n\n")
	sb.WriteString("每个门禁命令：`forge task gate <id> --ref <ref>`\n\n")
	sb.WriteString("门禁全通过后运行 `forge task complete --ref <ref>` 触发评分。\n\n")
	sb.WriteString("### 中止任务（清理 ghost/卡住任务）\n\n")
	sb.WriteString("任务无法推进（如在非 git 项目半启动、门禁死循环、或临时放弃）时，用 `forge task abort --ref <ref>` 删除任务状态文件并清空 active task ref，**不评分**。代码改动保留不动。task-verify 现为 advisory（仅记录问题不阻塞会话）；但 ghost 任务仍会污染 `task list`，需手动 abort 清理。\n\n")

	// Commit timing — without this agents naturally commit AFTER complete,
	// which clears the active task ref and gets the commit quarantined by
	// file-sentinel (trap documented from a real DevWorkbench session).
	sb.WriteString("### 提交时机（重要，避免被 file-sentinel 拦）\n\n")
	sb.WriteString("`git commit` 必须在 `forge task complete` **之前**：`complete` 会清空 active task ref，之后提交源码会被 file-sentinel quarantine。正确顺序：三门禁通过 → `git commit` → `forge task complete`。若已 complete 才发现要提交，开一个 `chore/*-commit` 任务放行。\n\n")

	sb.WriteString("### 安全机制\n\n")
	sb.WriteString("- **task-guard**（PreToolUse Write|Edit）：无任务时 Write/Edit 源码只 WARN 不拦截（`.forge/*` 自保护文件才 FAIL）；feature 分支无任务时自动建任务\n")
	sb.WriteString("- **bash-guard**（PreToolUse Bash）：无任务时 Bash 写文件只 WARN（源码随后可能被 file-sentinel quarantine）\n")
	sb.WriteString("- **file-sentinel**（PostToolUse Bash）：对比 Bash 前后文件状态，未授权源码变更 quarantine 到用户级 DataDir/quarantine/（`forge data-dir` 查看路径）\n")
	sb.WriteString("- **自保护**：`.forge/*` 和 `.claude/settings*` 不能被直接修改，只能通过 `forge` 命令操作\n")
	sb.WriteString("- **skill-scan**（SessionStart）：会话开始扫描 `~/.claude/skills` 安全性（forge audit 19 规则，advisory）——补 install 门控缺口，覆盖手动 clone/junction/git pull 进入的 skill；全局 hook，不依赖 forge project\n")
	sb.WriteString("- **mcp-scan**（SessionStart）：会话开始扫描项目级 `.mcp.json` 的 server 配置安全性（管道执行 curl\\|sh / 任意包执行 npx·uvx·dlx·bunx / 内联代码 -c·-e / 非 https URL / env 明文凭证，advisory）——补 skill-scan 盲区（攻击者可经 PR 植入恶意 server，clone 即自动连接）；只审 config 层，runtime tool description 注入（Tool Poisoning）不在能力内；全局 hook，不依赖 forge project\n")
	sb.WriteString("- **task-resume**（SessionStart）：会话启动自动注入活跃任务的接续上下文（`forge task resume --hook`：目标/计划/决策/阻塞/门禁进度/git 已改未提交）+ 把当前 session 锚定到任务——接手方冷启动即知任务在哪一步，无需手动 `forge task resume`；无活跃任务静默；项目级 hook（advisory，不阻塞）\n")
	sb.WriteString("- **辅助检查（仅 WARN 不阻塞）**：先读再改/聚焦变更/避免重复等判断性规则已下沉为 forge-quality 的 Red Flags 文本。\n\n")

	sb.WriteString("### 常见错误\n\n")
	sb.WriteString("| 错误信息 | 原因 | 解决方法 |\n")
	sb.WriteString("|----------|------|----------|\n")
	sb.WriteString("| WARN [task-guard] ... allowed but not tracked | 无活跃任务时 Write/Edit 源码（仅警告，不拦截） | 启动任务让变更被追踪和评分 |\n")
	sb.WriteString("| WARN [bash-guard] ... Bash write without active task | 无任务时 Bash 写文件（仅警告，但源码会被 file-sentinel quarantine） | 先启动任务 |\n")
	sb.WriteString("| insufficient work activity | 门禁间工具调用 <1 次 | 用 Read/Grep/Glob 探索代码 |\n")
	sb.WriteString("| task-verify advisory: ... source files changed without a corresponding test | 改了源码没加对应测试文件（铁律4：测试伴随变更，advisory 仅提醒不阻塞） | 为变更的源码加 `_test.go`/`.test.ts`/`test_*.py` 等；入口(main.go/cmd)/生成物(.gen./_generated/.pb.)/纯类型文件(types/dto/models)白名单免测；不可测时设 `FORGE_TEST_COVERAGE=disable` 逃生 |\n")
	sb.WriteString("| --branch on non-main | `--branch` 只在 master/main 可用 | 已在 feature 分支时去掉 `--branch` |\n")
	sb.WriteString("| task already exists | 任务已启动 | 用 `forge task status --ref <ref>` 查看 |\n")
	sb.WriteString("| Quarantined by file-sentinel | Bash 写了源码但无任务 | 文件在用户级 DataDir/quarantine/（`forge data-dir` 查看路径），可恢复。先启动任务 |\n")
	sb.WriteString("| complete 后提交被 file-sentinel 拦 | complete 已清 active task ref | 先 commit 再 complete；或开 `chore/*-commit` 任务放行 |\n")
	sb.WriteString("| trace/老任务历史消失 | retention（默认启用）自动清超期 checklog/toollog 归档 + 已完成任务文件 | 行为正常；`FORGE_LOG_RETENTION_DAYS` 控制保留天数（默认 30，≤0 禁用）；`forge act rebuild` 全量重建，被 retention 删的任务无法重建 |\n\n")

	if forClaude {
		sb.WriteString("使用 `/forge-quality` 查看完整质量协议。\n\n")
	} else {
		// AGENTS.md is cross-agent (codex/cursor/copilot/windsurf/cline) — those
		// agents have no Claude slash commands, so point at the forge CLI / MCP
		// surface instead of the /forge-quality skill.
		sb.WriteString("通过 forge CLI（forge task/gate）或 forge MCP 工具执行上述质量流程。\n\n")
	}
	sb.WriteString(forgeSectionEnd + "\n")
	return sb.String()
}

// replaceForgeSection replaces the content between FORGE:START and FORGE:END
// markers, preserving everything outside the markers.
func replaceForgeSection(content, newSection string) string {
	startIdx := strings.Index(content, forgeSectionStart)
	endIdx := strings.Index(content, forgeSectionEnd)

	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		// No markers found — append the section
		return content + "\n" + newSection
	}

	// Replace between markers
	before := content[:startIdx]
	after := content[endIdx+len(forgeSectionEnd):]

	// newSection ends with "\n" from forgeSectionEnd+"\n", trim it
	// so we control the exact spacing between markers and after-content
	section := strings.TrimRight(newSection, "\n")

	result := before + section + "\n"

	// Clean up leading whitespace from after-content
	after = strings.TrimLeft(after, "\n")
	if after != "" {
		result += "\n" + after
	}
	return result
}
