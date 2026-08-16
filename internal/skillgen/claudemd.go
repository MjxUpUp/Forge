package skillgen

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/userassets"
	"github.com/MjxUpUp/Forge/internal/util"
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
		return util.AtomicWrite(path, []byte(updated), 0644)
	}

	// Create a new file, writing only the Forge section.
	//
	// 新建文件，仅写入 Forge section
	return util.AtomicWrite(path, []byte(forgeSection), 0644)
}

// GenerateAgentsMD creates or updates the project-root AGENTS.md, writing the
// quality protocol section taken over by Forge. AGENTS.md is the cross-agent instruction spec read by
// generic agents such as codex/cursor/copilot/windsurf/cline (detect.go identifies codex via .codex/,
// not via AGENTS.md). Project-root generation only happens in team mode (`forge init --project`);
// the default zero-project-write init writes the same section to the user-level ~/.codex/AGENTS.md
// via GenerateUserAgentsMD instead.
// Unlike CLAUDE.md (claude-specific, references Claude slash command),
// AGENTS.md carries the agent-agnostic protocol and points to the forge CLI surface. When the file already exists,
// only the marker-wrapped Forge section is replaced; user content outside the markers is preserved —
// the same idempotent section-replace contract as CLAUDE.md.
//
// GenerateAgentsMD 创建或更新项目根 AGENTS.md，写入 Forge 接管的
// 质量协议 section。AGENTS.md 是 codex/cursor/copilot/windsurf/cline 等
// 通用 agent 读取的跨 agent 指令规范（detect.go 用 .codex/ 识别 codex，
// 不依赖 AGENTS.md）。项目根生成只在团队模式（`forge init --project`）发生；
// 默认零项目写入 init 改由 GenerateUserAgentsMD 把同一段写到用户级
// ~/.codex/AGENTS.md。
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
		return util.AtomicWrite(path, []byte(updated), 0644)
	}
	return util.AtomicWrite(path, []byte(forgeSection), 0644)
}

func buildForgeSection(forClaude bool) string {
	return buildForgeSectionWithLevel(forClaude, false)
}

// userLevelPreamble is prepended to the user-level forge section (~/.claude/CLAUDE.md,
// ~/.codex/AGENTS.md). The user-level file is visible in EVERY project, so the section
// must not unconditionally assert "this project uses Forge" — it activates only when the
// current project is forge-initialized, and must be ignored otherwise.
//
// userLevelPreamble 前置在用户级 forge 段（~/.claude/CLAUDE.md、~/.codex/AGENTS.md）
// 段首。用户级文件对所有项目可见，段文本不能无条件断言"本项目使用 Forge"——
// 仅当当前项目已 init 时才激活，否则必须忽略。
const userLevelPreamble = "**本段为 Forge 用户级全局注入，对你的所有项目可见。仅当当前项目已执行过 `forge init`（即在 Forge 全局项目注册表中）时，才遵守以下协议；若当前项目未使用 Forge，请完全忽略本段。**\n\n"

func buildForgeSectionWithLevel(forClaude bool, userLevel bool) string {
	var sb strings.Builder
	sb.WriteString(forgeSectionStart + "\n\n")
	if userLevel {
		sb.WriteString(userLevelPreamble)
	}
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

	// Recurrence-driven hardening — the soft↔hard balance. Documents the gate behavior so an agent
	// knows a recurrent project can have task-verify BLOCKED on test-coverage/scope-drift that would
	// be mere advisory elsewhere. Without this note an agent hits the BLOCKED message cold.
	//
	// 复发驱动升硬——软↔硬平衡。记录门禁行为让 agent 知道：在复发项目里 task-verify 会对别处只是
	// advisory 的 test-coverage/scope-drift 直接 BLOCKED。无此说明 agent 会冷不丁撞上 BLOCKED。
	sb.WriteString("### 复发驱动升硬（软↔硬平衡）\n\n")
	sb.WriteString("task-verify 的 test-coverage 与 scope-drift 默认 advisory（仅提醒不阻塞）。但若本项目已完成任务历史里 testing 或 scope 维度反复低分（≥3 次）——证明 advisory 靠自律已失效——且本次严重（缺配对测试 / 超 scope 多文件 drift），则升为 HARD 阻断。双轴 AND：新项目无履历永不升硬（不误伤陌生项目），单文件 drift 即便在复发项目也保持 advisory（预测噪声不升硬）。\n\n")
	sb.WriteString("逃生舱：`FORGE_RECURRENT_HARDEN=disable` 回退纯 advisory（不加 Strength 惩罚，表达项目偏好而非跳过验证）；`FORGE_RECURRENT_THRESHOLD=N` 调阈值。\n\n")

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
	sb.WriteString("- **task-guard**（PreToolUse Write|Edit）：无任务时 Write/Edit 源码只 WARN 不拦截（`.forge/*`/`.claude/settings*` 自保护 FAIL——此类项目级文件只在团队模式/老项目存在）；feature 分支无任务时自动建任务\n")
	sb.WriteString("- **read-before-edit**（PreToolUse Write|Edit，活跃任务内）：编辑本会话未 Read 过的现存源文件 → 硬阻断（`BLOCKED`）。Edit 需精确匹配旧文本，未读即凭记忆盲改——old_string 撞中即错改入库。先 Read 再 Edit。豁免：新建文件/测试文件/非源码；批量重构逃生 `forge task override --work-activity disable`（降 evidence 强度到 Weak）。reads-log 落盘随会话存活，压缩后仍累计\n")
	sb.WriteString("- **bash-guard**（PreToolUse Bash）：无任务时 Bash 写文件只 WARN（源码随后可能被 file-sentinel quarantine）\n")
	sb.WriteString("- **file-sentinel**（PostToolUse Bash）：对比 Bash 前后文件状态，未授权源码变更 quarantine 到用户级 DataDir/quarantine/（`forge data-dir` 查看路径）\n")
	sb.WriteString("- **自保护**：项目级 `.forge/*` 和 `.claude/settings*`（仅团队模式/老项目存在）不能被直接修改；用户级资产（`~/.claude/settings.json`、`~/.claude/CLAUDE.md`、`~/.codex/AGENTS.md` 等）同样只能通过 `forge` 命令操作（`forge uninstall --restore` 可回滚）\n")
	sb.WriteString("- **skill-scan**（SessionStart）：会话开始扫描 `~/.claude/skills` 安全性（forge audit 22 规则，advisory）——补 install 门控缺口，覆盖手动 clone/junction/git pull 进入的 skill；全局 hook，不依赖 forge project\n")
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
	sb.WriteString("| task-verify 拒绝（复发升 HARD stop）：项目 testing/scope 维度反复低分 | 项目已完成任务历史里该维度低分≥阈值次（advisory 靠自律已被证明失效），且本次严重（缺配对测试 / 超 scope 多文件 drift） | 补测试或 `forge task scope add <glob>` 收编后重跑；或 `FORGE_TEST_COVERAGE=disable`（降 Weak）；或 `FORGE_RECURRENT_HARDEN=disable` 回退纯 advisory |\n")
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
// all content outside the markers is preserved as-is. Thin wrapper over
// util.ReplaceMarkedSection (shared with agentbridge's .windsurfrules upsert).
//
// replaceForgeSection 替换 FORGE:START 与 FORGE:END 标记之间的内容，
// 标记外的所有内容原样保留。util.ReplaceMarkedSection 的薄封装
// （与 agentbridge 的 .windsurfrules upsert 共享）。
func replaceForgeSection(content, newSection string) string {
	return util.ReplaceMarkedSection(content, newSection, forgeSectionStart, forgeSectionEnd)
}

// claudeConfigHome resolves the Claude Code config home: CLAUDE_CONFIG_DIR env
// first, else ~/.claude — the same convention as internal/hooks/plugin_detect.go
// ClaudeHome(). Empty string means the home could not be resolved.
//
// claudeConfigHome 解析 Claude Code 配置 home：优先 CLAUDE_CONFIG_DIR env，
// 否则 ~/.claude——与 internal/hooks/plugin_detect.go 的 ClaudeHome() 同一约定。
// 空串表示无法解析 home。
func claudeConfigHome() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// codexConfigHome resolves the codex config home: CODEX_HOME env first,
// else ~/.codex. Empty string means the home could not be resolved.
//
// codexConfigHome 解析 codex 配置 home：优先 CODEX_HOME env，否则 ~/.codex。
// 空串表示无法解析 home。
func codexConfigHome() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// dirExists reports whether path is an existing directory. Used by the user-level
// generators' detection-self-poison guard: an agent's config home exists iff the
// tool is installed (DetectAgents' signal), so the generators must only write into
// homes that already exist.
//
// dirExists 报告 path 是否为已存在目录。供用户级生成器的检测自毒防护使用：
// agent 的 config home 存在 = 该工具已安装（DetectAgents 的信号），故生成器
// 只往已存在的 home 里写。
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// upsertUserForgeSection upserts the conditional (user-level) forge section into
// a user-level instruction file. Backup-then-append: the original file is backed up
// via userassets.BackupOriginal BEFORE forge's first write, so the user can roll
// back. Same idempotent section-replace contract as the project-level generators.
//
// upsertUserForgeSection 把条件激活的（用户级）forge 段 upsert 进用户级指令文件。
// 备份+追加：forge 首次写入前经 userassets.BackupOriginal 备份原文件，用户可回滚。
// 与项目级生成器同样的幂等 section-replace 契约。
func upsertUserForgeSection(path string, forClaude bool) error {
	if err := userassets.BackupOriginal(path); err != nil {
		return fmt.Errorf("backup %s before user-level write: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}
	forgeSection := buildForgeSectionWithLevel(forClaude, true)
	existing, err := os.ReadFile(path)
	if err == nil && len(existing) > 0 {
		updated := replaceForgeSection(string(existing), forgeSection)
		return util.AtomicWrite(path, []byte(updated), 0644)
	}
	return util.AtomicWrite(path, []byte(forgeSection), 0644)
}

// GenerateUserClaudeMD upserts the (conditional) forge section into the user-level
// ~/.claude/CLAUDE.md — backup-then-append via userassets.BackupOriginal first.
// Claude home resolution: CLAUDE_CONFIG_DIR env first, else ~/.claude (same
// convention as internal/hooks/plugin_detect.go ClaudeHome()).
//
// No-op when the Claude config home does not exist: the directory's existence is
// DetectAgents' "claude is installed" signal, so creating it here would poison
// detection — machines without Claude Code would get wired as if it were installed
// (detection self-poison). Only installed tools get instruction files.
//
// GenerateUserClaudeMD 把（条件激活的）forge 段 upsert 进用户级
// ~/.claude/CLAUDE.md——先经 userassets.BackupOriginal 备份再追加。Claude home
// 解析：优先 CLAUDE_CONFIG_DIR env，否则 ~/.claude（与
// internal/hooks/plugin_detect.go 的 ClaudeHome() 同一约定）。
//
// Claude config home 不存在时 no-op：目录存在性是 DetectAgents 判断"claude 已
// 安装"的信号，在此创建它会毒化检测——没装 Claude Code 的机器会被当成已安装
// 而接线（检测自毒）。只给已安装的工具写指令文件。
func GenerateUserClaudeMD() error {
	home := claudeConfigHome()
	if home == "" {
		return fmt.Errorf("cannot resolve Claude config home (CLAUDE_CONFIG_DIR unset, user home unavailable)")
	}
	if !dirExists(home) {
		return nil // Claude Code not installed — do not create its config home (detection self-poison)
	}
	return upsertUserForgeSection(filepath.Join(home, "CLAUDE.md"), true)
}

// GenerateUserAgentsMD upserts the (conditional) forge section into the user-level
// ~/.codex/AGENTS.md (CODEX_HOME env first, else ~/.codex). Same backup contract
// as GenerateUserClaudeMD.
//
// No-op when the codex config home does not exist — same detection-self-poison
// guard as GenerateUserClaudeMD (the directory's existence is DetectAgents' "codex
// is installed" signal).
//
// GenerateUserAgentsMD 把（条件激活的）forge 段 upsert 进用户级
// ~/.codex/AGENTS.md（优先 CODEX_HOME env，否则 ~/.codex）。与
// GenerateUserClaudeMD 同样的备份契约。
//
// codex config home 不存在时 no-op——与 GenerateUserClaudeMD 同款检测自毒防护
// （目录存在性是 DetectAgents 判断"codex 已安装"的信号）。
func GenerateUserAgentsMD() error {
	home := codexConfigHome()
	if home == "" {
		return fmt.Errorf("cannot resolve codex config home (CODEX_HOME unset, user home unavailable)")
	}
	if !dirExists(home) {
		return nil // codex not installed — do not create its config home (detection self-poison)
	}
	return upsertUserForgeSection(filepath.Join(home, "AGENTS.md"), false)
}

// StripUserInstructions removes the FORGE:START/END marked section from both
// user-level files (~/.claude/CLAUDE.md and ~/.codex/AGENTS.md), preserving all
// other content. If the file becomes empty/whitespace and forge created it, the
// empty file is left in place — userassets.RestoreOriginal handles deletion.
// Idempotent. Used by forge uninstall.
//
// StripUserInstructions 从两个用户级文件（~/.claude/CLAUDE.md 与
// ~/.codex/AGENTS.md）中移除 FORGE:START/END 标记段，其余内容全部保留。
// 若文件变为空且是 forge 创建的，保留空文件——删除由
// userassets.RestoreOriginal 负责。幂等。供 forge uninstall 使用。
func StripUserInstructions() error {
	var targets []string
	if home := claudeConfigHome(); home != "" {
		targets = append(targets, filepath.Join(home, "CLAUDE.md"))
	}
	if home := codexConfigHome(); home != "" {
		targets = append(targets, filepath.Join(home, "AGENTS.md"))
	}
	for _, path := range targets {
		existing, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s for stripping: %w", path, err)
		}
		stripped := stripMarkedSection(string(existing), forgeSectionStart, forgeSectionEnd)
		if stripped == string(existing) {
			continue // no forge section — nothing to do
		}
		if err := util.AtomicWrite(path, []byte(stripped), 0644); err != nil {
			return fmt.Errorf("write stripped %s: %w", path, err)
		}
	}
	return nil
}

// stripMarkedSection removes the content between startMarker and endMarker
// (markers included), normalizing the seam so the surrounding content keeps a
// single blank line. Returns the input unchanged when the markers are missing
// or inverted (idempotent).
//
// stripMarkedSection 移除 startMarker 与 endMarker 之间的内容（含标记），
// 规整接缝使上下文之间保留单个空行。标记缺失或颠倒时原样返回（幂等）。
func stripMarkedSection(content, startMarker, endMarker string) string {
	startIdx := strings.Index(content, startMarker)
	endIdx := strings.Index(content, endMarker)
	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return content
	}
	before := strings.TrimRight(content[:startIdx], "\n")
	after := strings.TrimLeft(content[endIdx+len(endMarker):], "\n")
	switch {
	case before == "" && after == "":
		return ""
	case before == "":
		return after + "\n"
	case after == "":
		return before + "\n"
	default:
		return before + "\n\n" + after + "\n"
	}
}

// GenerateUserQualitySkill writes the forge-quality skill to the user-level
// ~/.claude/skills/forge-quality/SKILL.md from the given protocol — same content
// as the project-level GenerateQualitySkill, different target dir. Because the
// user-level skill is loaded in every project, the unconditional "本项目"
// wording is adjusted to the conditional form (minimal change).
//
// No-op when the Claude config home does not exist — same detection-self-poison
// guard as GenerateUserClaudeMD (the directory's existence is DetectAgents'
// "claude is installed" signal).
//
// GenerateUserQualitySkill 从给定 protocol 生成用户级
// ~/.claude/skills/forge-quality/SKILL.md——内容与项目级 GenerateQualitySkill
// 相同，仅目标目录不同。因用户级 skill 在所有项目中加载，无条件的"本项目"
// 措辞微调为条件式（最小改动）。
//
// Claude config home 不存在时 no-op——与 GenerateUserClaudeMD 同款检测自毒
// 防护（目录存在性是 DetectAgents 判断"claude 已安装"的信号）。
func GenerateUserQualitySkill(proto *protocol.Protocol) error {
	home := claudeConfigHome()
	if home == "" {
		return fmt.Errorf("cannot resolve Claude config home (CLAUDE_CONFIG_DIR unset, user home unavailable)")
	}
	return GenerateUserQualitySkillTo(filepath.Join(home, "skills"), proto)
}

// GenerateUserQualitySkillTo writes the forge-quality skill under the given
// skills root (e.g. ~/.claude/skills or ~/.reasonix/skills) — the shared
// user-level skill writer used by GenerateUserQualitySkill and the reasonix
// translator. Same conditional-activation content, same self-poison guard: a
// missing agent home (the parent of skillsRoot) is a no-op, so Forge never
// creates an agent's config home itself.
//
// GenerateUserQualitySkillTo 把 forge-quality skill 写到给定 skills root
// （如 ~/.claude/skills 或 ~/.reasonix/skills）——GenerateUserQualitySkill 与
// reasonix translator 共享的用户级 skill 写入器。同样的条件激活内容与自毒
// 防护：agent home（skillsRoot 的父目录）不存在时 no-op，Forge 绝不自行创建
// agent 的配置 home。
func GenerateUserQualitySkillTo(skillsRoot string, proto *protocol.Protocol) error {
	home := filepath.Dir(skillsRoot)
	if home == "" || home == "." {
		return fmt.Errorf("cannot resolve agent config home from skills root %q", skillsRoot)
	}
	if !dirExists(home) {
		return nil // agent not installed — do not create its config home (detection self-poison)
	}
	skillDir := filepath.Join(skillsRoot, "forge-quality")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("failed to create user-level quality skill dir: %w", err)
	}
	content := buildQualitySkillContent("", proto)
	// Conditional activation: the user-level skill is visible in non-forge projects
	// too, so it must not unconditionally claim "this project".
	//
	// 条件激活：用户级 skill 在非 forge 项目中也可见，不能无条件断言"本项目"。
	content = strings.Replace(content,
		"你是本项目的质量守护者。以下标准在任何开发会话中都有效。",
		"你是 Forge 项目的质量守护者。仅当当前项目已执行过 `forge init`（在 Forge 全局项目注册表中）时，以下标准才生效；当前项目未使用 Forge 时忽略本 skill。",
		1)
	// The project-info section names one concrete project — meaningless at user
	// level (the skill serves every project). Drop it.
	//
	// 项目信息章节指向单个具体项目——用户级无意义（skill 服务所有项目），移除。
	if idx := strings.Index(content, "## 当前项目信息"); idx != -1 {
		content = strings.TrimRight(content[:idx], "\n") + "\n"
	}
	return util.AtomicWrite(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644)
}
