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

// GenerateClaudeMD creates or updates .claude/CLAUDE.md, writing the
// quality protocol section taken over by Forge. When the file already exists, only the marker-wrapped section is replaced —
// user content is preserved.
//
// GenerateClaudeMD 创建或更新 .claude/CLAUDE.md，写入 Forge 接管的
// 质量协议 section。文件已存在时只替换标记包裹的 section——
// 用户内容保留。
func GenerateClaudeMD(projectDir string) error {
	claudeDir := filepath.Join(projectDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude dir: %w", err)
	}

	path := filepath.Join(claudeDir, "CLAUDE.md")

	forgeSection := buildForgeSection(true)

	// If it already exists, read the existing file.
	//
	// 若已存在则读现有文件
	existing, err := os.ReadFile(path)
	if err == nil && len(existing) > 0 {
		// Only update the Forge section.
		//
		// 仅更新 Forge section
		updated := replaceForgeSection(string(existing), forgeSection)
		return os.WriteFile(path, []byte(updated), 0644)
	}

	// Create a new file, writing only the Forge section.
	//
	// 新建文件，仅写入 Forge section
	return os.WriteFile(path, []byte(forgeSection), 0644)
}

// GenerateAgentsMD creates or updates the project-root AGENTS.md, writing the
// quality protocol section taken over by Forge. AGENTS.md is the cross-agent instruction spec read by
// generic agents such as codex/cursor/copilot/windsurf/cline (detect.go identifies codex via .codex/,
// not via AGENTS.md — AGENTS.md is a generic file forge generates on every init).
// Unlike CLAUDE.md (claude-specific, references Claude slash command),
// AGENTS.md carries the agent-agnostic protocol and points to the forge CLI surface. When the file already exists,
// only the marker-wrapped Forge section is replaced; user content outside the markers is preserved —
// the same idempotent section-replace contract as CLAUDE.md.
//
// GenerateAgentsMD 创建或更新项目根 AGENTS.md，写入 Forge 接管的
// 质量协议 section。AGENTS.md 是 codex/cursor/copilot/windsurf/cline 等
// 通用 agent 读取的跨 agent 指令规范（detect.go 用 .codex/ 识别 codex，
// 不依赖 AGENTS.md——AGENTS.md 是 forge 每次 init 都会生成的通用文件）。
// 与 CLAUDE.md（claude 专属、引用 Claude slash command）不同，
// AGENTS.md 承载 agent-agnostic 协议并指向 forge CLI surface。文件已存在时，
// 仅替换标记包裹的 Forge section，标记外的用户内容保留——
// 与 CLAUDE.md 同样的幂等 section-replace 契约。
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

	// task workflow — the most critical operating instructions, preventing agents from blindly running into
	// task-guard/bash-guard interception.
	//
	// task workflow——最关键的操作指引，防止 agent 不知所措地撞上
	// task-guard/bash-guard 拦截。
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
	sb.WriteString("2. `task-verify` — 测试伴随变更（advisory）+ skill-decisions guardrail（改 SKILL.md 须记决策）与 work-activity（门禁间无工具调用）HARD stop；编译/断言 advisory 由 agent 自检\n")
	sb.WriteString("3. `task-complete` — E2E 验证通过后运行（`forge task gate task-complete --ref <ref>`）\n\n")
	sb.WriteString("每个门禁命令：`forge task gate <id> --ref <ref>`\n\n")
	sb.WriteString("**门禁退出码契约**：`forge task gate` 非 0 退出 = 硬阻断（输出 `BLOCKED:` 前缀），必须修复后重跑，不是提醒；零退出但见 `ADVISORY:` 前缀 = 软信号（gate 仍过，已记 checklog，应修但不阻断）。按退出码行动，不要靠解析文案判断（硬阻断散文易被误读成提醒而跳过）。\n\n")
	sb.WriteString("门禁全通过后运行 `forge task complete --ref <ref>` 触发评分。\n\n")
	sb.WriteString("### 中止任务（清理 ghost/卡住任务）\n\n")
	sb.WriteString("任务无法推进（如在非 git 项目半启动、门禁死循环、或临时放弃）时，用 `forge task abort --ref <ref>` 删除任务状态文件并清空 active task ref，**不评分**。代码改动保留不动。task-verify 的 test-coverage/编译/断言为 advisory（仅记录不阻塞），但 skill-decisions guardrail（改 SKILL.md 未记决策）与 work-activity 仍 HARD stop；ghost 任务无论是否阻塞都污染 `task list`，需手动 abort 清理。\n\n")

	// Commit timing — without this note an agent will naturally commit only after complete,
	// and complete clears the active task ref, causing the commit to be
	// quarantined by file-sentinel (this trap comes from a real DevWorkbench session).
	//
	// 提交时机——若无此说明 agent 自然会在 complete 之后才 commit，
	// 而 complete 会清空 active task ref，导致 commit 被 file-sentinel
	// quarantine（此 trap 来自一次真实 DevWorkbench 会话）。
	sb.WriteString("### 提交时机（重要，避免被 file-sentinel 拦）\n\n")
	sb.WriteString("`git commit` 必须在 `forge task complete` **之前**：`complete` 会清空 active task ref，之后提交源码会被 file-sentinel quarantine。正确顺序：三门禁通过 → `git commit` → `forge task complete`。若已 complete 才发现要提交，开一个 `chore/*-commit` 任务放行。\n\n")

	sb.WriteString("### 安全机制\n\n")
	sb.WriteString("- **task-guard**（PreToolUse Write|Edit）：无任务时 Write/Edit 源码只 WARN 不拦截（`.forge/*` 自保护文件才 FAIL）；feature 分支无任务时自动建任务\n")
	sb.WriteString("- **read-before-edit**（PreToolUse Write|Edit，活跃任务内）：编辑本会话未 Read 过的现存源文件 → 硬阻断（`BLOCKED`）。Edit 需精确匹配旧文本，未读即凭记忆盲改——old_string 撞中即错改入库。先 Read 再 Edit。豁免：新建文件/测试文件/非源码；批量重构逃生 `forge task override --work-activity disable`（降 evidence 强度到 Weak）。reads-log 落盘随会话存活，压缩后仍累计\n")
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
	sb.WriteString("| task-verify advisory: ... source files changed without a corresponding test | 改了源码没加对应测试文件（铁律4：测试伴随变更，advisory 仅提醒不阻塞） | 为变更的源码加 `_test.go`/`.test.ts`/`test_*.py` 等；入口(main.go/cmd)/生成物(.gen./_generated/.pb.)/纯类型文件(types/dto/models)白名单免测；不可测时用 `forge task override --test-coverage disable`（per-task，优先于 `FORGE_TEST_COVERAGE=disable` env，不污染他任务；用了降 evidence 强度到 Weak） |\n")
	sb.WriteString("| task-verify 拒绝（HARD stop）：改了 skill ... 的 SKILL.md 未记决策 | 改 `skills/<name>/SKILL.md`（行为契约）未在 `decisions.md` 新增 `## [d-` 决策条目（guardrail） | `forge skills decide --skill <name> --outcome <accept/reject> --diagnosis <为何改> --revision <改了啥> --evidence <依据>` 记四元组；trivial 改动用 `forge task override --skill-decisions disable`（per-task，优先于 `FORGE_SKILL_DECISIONS=disable` env，降 evidence 到 Weak） |\n")
	sb.WriteString("| task-complete 拒绝：验收 #N 未实跑/基于旧代码/未通过 | task 声明了 acceptance（`task start --accept`），complete 时校验每条须 `AcceptedHeadCommit==HEAD` 且 `Passed`（deterministic pre-flight） | `forge task verify-acceptance` 实跑回扣（验收后改码须重跑使快照刷新）；验收不可机器执行用 `forge task override --acceptance-gate disable`（per-task，优先于 `FORGE_ACCEPTANCE_GATE=disable` env，降 evidence 到 Weak） |\n")
	sb.WriteString("| --branch on non-main | `--branch` 只在 master/main 可用 | 已在 feature 分支时去掉 `--branch` |\n")
	sb.WriteString("| task already exists | 任务已启动 | 用 `forge task status --ref <ref>` 查看 |\n")
	sb.WriteString("| Quarantined by file-sentinel | Bash 写了源码但无任务 | 文件在用户级 DataDir/quarantine/（`forge data-dir` 查看路径），可恢复。先启动任务 |\n")
	sb.WriteString("| complete 后提交被 file-sentinel 拦 | complete 已清 active task ref | 先 commit 再 complete；或开 `chore/*-commit` 任务放行 |\n")
	sb.WriteString("| trace/老任务历史消失 | retention（默认启用）自动清超期 checklog/toollog 归档 + 已完成任务文件 | 行为正常；`FORGE_LOG_RETENTION_DAYS` 控制保留天数（默认 30，≤0 禁用）；`forge act rebuild` 全量重建，被 retention 删的任务无法重建 |\n\n")

	if forClaude {
		sb.WriteString("使用 `/forge-quality` 查看完整质量协议。\n\n")
	} else {
		// AGENTS.md is cross-agent (codex/cursor/copilot/windsurf/cline) — these
		// agents have no Claude slash command, so they point to the forge CLI surface,
		// not the /forge-quality skill.
		//
		// AGENTS.md 是跨 agent 的（codex/cursor/copilot/windsurf/cline）——这些
		// agent 没有 Claude slash command，故指向 forge CLI surface，
		// 而非 /forge-quality skill。
		sb.WriteString("通过 forge CLI（forge task/gate）执行上述质量流程。\n\n")
	}
	sb.WriteString(forgeSectionEnd + "\n")
	return sb.String()
}

// replaceForgeSection replaces the content between FORGE:START and FORGE:END markers;
// all content outside the markers is preserved as-is.
//
// replaceForgeSection 替换 FORGE:START 与 FORGE:END 标记之间的内容，
// 标记外的所有内容原样保留。
func replaceForgeSection(content, newSection string) string {
	startIdx := strings.Index(content, forgeSectionStart)
	endIdx := strings.Index(content, forgeSectionEnd)

	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		// Markers not found — append the section.
		//
		// 未找到标记——追加该 section
		return content + "\n" + newSection
	}

	// Replace content between the markers.
	//
	// 替换标记之间的内容
	before := content[:startIdx]
	after := content[endIdx+len(forgeSectionEnd):]

	// The trailing newline of newSection comes from forgeSectionEnd+newline; TrimRight it first,
	// to precisely control the spacing between markers and the surrounding content.
	//
	// newSection 末尾的换行来自 forgeSectionEnd+换行，先 TrimRight 掉它，
	// 以精确控制标记之间以及与后续内容之间的间距
	section := strings.TrimRight(newSection, "\n")

	result := before + section + "\n"

	// Strip leading blank lines from the after-content.
	//
	// 清除 after-content 的前导空白
	after = strings.TrimLeft(after, "\n")
	if after != "" {
		result += "\n" + after
	}
	return result
}
