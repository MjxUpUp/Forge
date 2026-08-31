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

// forge 段标记：单一真相源在 util（ForgeSectionStart/End），runtime 包
// （conventions，及经它的 taskpipeline）无需 import 生成器层即可消费该契约；
// 本文件局部别名保持文件内调用点不变。
const (
	forgeSectionStart = util.ForgeSectionStart
	forgeSectionEnd   = util.ForgeSectionEnd
)

// GenerateClaudeMD creates or updates .claude/CLAUDE.md, writing the quality protocol section taken over by Forge.
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

	// 若已存在则读现有文件。只有确证的 ErrNotExist 才允许落到新建路径——
	// 把任何读失败（杀软锁、sharing violation、权限）当"不存在"会用仅含
	// forge 段的内容整体覆盖用户文件。
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("skillgen: read %s: %w", path, err)
	}
	if err == nil && len(existing) > 0 {
		// 仅更新 Forge section
		updated := replaceForgeSection(string(existing), forgeSection)
		return util.AtomicWrite(path, []byte(updated), 0644)
	}

	// 新建文件，仅写入 Forge section
	return util.AtomicWrite(path, []byte(forgeSection), 0644)
}

// GenerateAgentsMD creates or updates the project-root AGENTS.md, writing the quality protocol section taken over by Forge.
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
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("skillgen: read %s: %w", path, err)
	}
	if err == nil && len(existing) > 0 {
		updated := replaceForgeSection(string(existing), forgeSection)
		return util.AtomicWrite(path, []byte(updated), 0644)
	}
	return util.AtomicWrite(path, []byte(forgeSection), 0644)
}

func buildForgeSection(forClaude bool) string {
	return buildForgeSectionWithLevel(forClaude, false)
}

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
	sb.WriteString("2. **编译必须通过** — 每次修改后用你的编译命令确认编译通过（`auto-compile` hook 仅 advisory 提醒，由 agent 自检）\n")
	sb.WriteString("3. **不弱化断言** — 不删除 t.Fatal、assert! 等断言（`assertion-check` hook 检测到弱化仅 advisory 提醒，由 agent 自检）\n")
	sb.WriteString("4. **测试伴随变更** — 新代码有对应测试\n")
	sb.WriteString("5. **提交前确认** — commit 信息描述变更内容和原因\n")
	sb.WriteString("6. **结束前验证** — 会话结束前运行测试确认无破坏\n")
	// 规则 7——回复详略（结论先行 + 禁令短语指针）。短语示例保持在反引号内：
	// doclint 豁免行内代码引用，且本生成文件须过它自己的 lint。
	sb.WriteString("7. **结论先行，禁空转措辞** — 回复第一句给答案/判定/推荐；`综上所述`/`基本可以`/`问题不大` 等禁令短语与档位判据见 forge-quality skill「回复详略规则」，落盘文档用 `forge docs lint` 机器校验\n\n")

	// task workflow——最关键的操作指引，防止 agent 不知所措地撞上
	// task-guard/bash-guard 拦截。
	sb.WriteString("## Task 工作流（必读）\n\n")
	sb.WriteString("**源码变更前必须启动 Forge 任务**——无任务时 Write/Edit 源码触发 task-guard 警告（多数宿主 WARN 不拦截；dsh/zcode 宿主提升为阻断（双事故实证），kimi 已退役 promote 改走 advisory 队列攒发），但 Bash 写源码（sed/cat > 等）会被 file-sentinel quarantine。更关键：脱离任务的变更不被门禁追踪和质量评分。纯文档、单行 typo 修复、版本号 bump 除外。\n\n")
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

	// 复发驱动升硬——软↔硬平衡。记录门禁行为让 agent 知道：在复发项目里 task-verify 会对别处只是
	// advisory 的 test-coverage/scope-drift 直接 BLOCKED。无此说明 agent 会冷不丁撞上 BLOCKED。
	sb.WriteString("### 复发驱动升硬（软↔硬平衡）\n\n")
	sb.WriteString("task-verify 的 test-coverage 与 scope-drift 默认 advisory（仅提醒不阻塞）。但若本项目已完成任务历史里 testing 或 scope 维度反复低分（≥3 次）——证明 advisory 靠自律已失效——且本次严重（缺配对测试 / 超 scope 多文件 drift），则升为 HARD 阻断。双轴 AND：新项目无履历永不升硬（不误伤陌生项目），单文件 drift 即便在复发项目也保持 advisory（预测噪声不升硬）。\n\n")
	sb.WriteString("逃生舱：`FORGE_RECURRENT_HARDEN=disable` 回退纯 advisory（不加 Strength 惩罚，表达项目偏好而非跳过验证）；`FORGE_RECURRENT_THRESHOLD=N` 调阈值。\n\n")

	sb.WriteString("### 中止任务（清理 ghost/卡住任务）\n\n")
	sb.WriteString("任务无法推进（如在非 git 项目半启动、门禁死循环、或临时放弃）时，用 `forge task abort --ref <ref>` 删除任务状态文件并清空 active task ref，**不评分**。代码改动保留不动。task-verify 的 test-coverage/编译/断言为 advisory（仅记录不阻塞），但 skill-decisions guardrail（改 SKILL.md 未记决策）与 work-activity 仍 HARD stop；ghost 任务无论是否阻塞都污染 `task list`，需手动 abort 清理。\n\n")

	// 提交时机——若无此说明 agent 自然会在 complete 之后才 commit，
	// 而 complete 会清空 active task ref，导致 commit 被 file-sentinel
	// quarantine（此 trap 来自一次真实 DevWorkbench 会话）。task-complete
	// 门禁对未提交变更发 ADVISORY（CheckNameUncommittedAtComplete），
	// 把顺序滑落在 active task ref 清空前照出来。
	sb.WriteString("### 提交时机（重要，避免被 file-sentinel 拦）\n\n")
	sb.WriteString("`git commit` 必须在 `forge task complete` **之前**：`complete` 会清空 active task ref，之后提交源码会被 file-sentinel quarantine。正确顺序：三门禁通过 → `git commit` → `forge task complete`。若已 complete 才发现要提交，开一个 `chore/*-commit` 任务放行。task-complete 门禁检测到工作区未提交变更时会发 `ADVISORY` 提醒——见到即先 commit 再 complete。\n\n")

	sb.WriteString("### 安全机制\n\n")
	sb.WriteString("- **freeze-guard**（PreToolUse Write|Edit）：`forge freeze` 激活期间只允许写指定路径内文件，越界即硬阻断（`FAIL`）；`forge freeze --status` 看冻结范围，`--off` 解除。排在 task-guard 之前判定（freeze 优先契约）\n")
	sb.WriteString("- **task-guard**（PreToolUse Write|Edit）：无任务时 Write/Edit 源码 WARN（dsh/zcode 宿主提升为阻断，kimi 经 advisory 队列送达；`.forge/*`/`.claude/settings*` 自保护 FAIL——此类项目级文件只在团队模式/老项目存在）；feature 分支无任务时自动建任务\n")
	sb.WriteString("- **read-before-edit**（PreToolUse Write|Edit，活跃任务内）：编辑本会话未 Read 过的现存源文件 → 硬阻断（`BLOCKED`）。Edit 需精确匹配旧文本，未读即凭记忆盲改——old_string 撞中即错改入库。先 Read 再 Edit。豁免：新建文件/测试文件/非源码；批量重构逃生 `forge task override --work-activity disable`（记 checklog 审计；work-activity 是节奏门禁，不降 evidence 强度）。reads-log 落盘随会话存活，压缩后仍累计\n")
	sb.WriteString("- **bash-guard**（PreToolUse Bash）：无任务时 Bash 写文件只 WARN（源码随后可能被 file-sentinel quarantine）\n")
	sb.WriteString("- **hazard-guard**（PreToolUse Bash）：高危命令（`rm -rf` 深目录/盘根/引号包裹逃逸等指纹）硬阻断，须 human-in-the-loop 确认——授权判定：用户本回合已明确指令/确认过该操作则直接 `forge hazard confirm --last` 放行一次（无需二次确认），否则先用所在工具的提问确认机制向用户说明风险获确认再 confirm；误报可 `forge hazard status` 查看\n")
	sb.WriteString("- **file-sentinel**（PostToolUse Bash）：对比 Bash 前后文件状态，未授权源码变更 quarantine 到用户级 DataDir/quarantine/（`forge data-dir` 查看路径）\n")
	sb.WriteString("- **workflow-test-guard**（PostToolUse Write|Edit）：改 `.github/workflows/*.yml` 后自动跑 internal/ci 守护测试——CI workflow 沙盒异常的实时反馈层（fail 输出提示修复方向，不阻断写入）\n")
	sb.WriteString("- **tool-track**（PostToolUse Read|Skill|Agent|Grep|Glob|Bash）：记录工具使用到 toollog（评分 efficiency/维度数据源），永不阻断；Skill/Agent 调用也记录（质量 skill 是否被驱动可追溯）；Bash/Grep/Glob 记截断命令/input——探索调用计入 work-activity 探索轴\n")
	sb.WriteString("- **failure-track**（PostToolUseFailure Bash）：命令失败后记录 CheckToolFailure 观察；失败文本命中编译/测试类指纹（undefined:/error TS/error[E/FAIL 等）时注入 compile-fix-loop skill 事实性指针（advisory 不阻断——失败已发生，阻断无意义）\n")
	sb.WriteString("- **subagent-track**（SubagentStop）：子 agent 结束时记录 agent_id/agent_type/交付长度+首行摘要到 checklog（归因数据——此前子 agent 活动 forge 侧零记录）；纯观察，无输出无阻断\n")
	sb.WriteString("- **test-nudge**（PostToolUse Write|Edit，活跃任务内）：事中测试提醒——连写 ≥3 个源码文件且无配对测试写入时注入一次 test-discipline skill 事实性提示（advisory，每连写只提示一次）；任何测试文件写入重置计数；无活跃任务静默；执法仍在 task-verify 门禁\n")
	sb.WriteString("- **conventions-context**（SessionStart + PostCompact）：项目规范档案的会话摘要——`forge conventions init` 建档后每次会话开始注入 ≤15 行规范摘要（stack/lint 命令、规范声明文件、agent 提取的惯例要点；压缩后重注入，摘要过期限时提示重扫）；无档案但仓库已声明 AGENTS.md 等规范时每会话一次建档建议（advisory 不阻断）\n")
	sb.WriteString("- **conventions-write**（PreToolUse Write|Edit）：写入时刻规范注入——写源码/测试文件时注入本仓库规范声明文件指针 + 同目录范例文件（模仿既有代码风格；每目录每会话一次）；无档案静默（advisory 不阻断）\n")
	sb.WriteString("- **skill-trigger**（Pre/PostToolUse + SessionStart/Stop/UserPromptSubmit）：声明式 skill 触发——按各 skill `metadata.triggers` 的 event/keywords/when 条件匹配上下文，命中时注入「请加载该 skill」提示（advisory，注入内容出现在 hook 上下文里）；`FORGE_SKILL_TRIGGER=0` 全局关闭\n")
	sb.WriteString("- **自保护**：项目级 `.forge/*` 和 `.claude/settings*`（仅团队模式/老项目存在）不能被直接修改；用户级资产（`~/.claude/settings.json`、`~/.claude/CLAUDE.md`、`~/.codex/AGENTS.md` 等）同样只能通过 `forge` 命令操作（`forge uninstall --restore` 可回滚）\n")
	sb.WriteString("- **skill-scan**（SessionStart）：会话开始扫描 `~/.claude/skills` 安全性（forge audit 21 规则，advisory）——补 install 门控缺口，覆盖手动 clone/junction/git pull 进入的 skill；全局 hook，不依赖 forge project\n")
	sb.WriteString("- **mcp-scan**（SessionStart）：会话开始扫描项目级 `.mcp.json` 的 server 配置安全性（管道执行 curl\\|sh / 任意包执行 npx·uvx·dlx·bunx / 内联代码 -c·-e / 非 https URL / env 明文凭证，advisory）——补 skill-scan 盲区（攻击者可经 PR 植入恶意 server，clone 即自动连接）；只审 config 层，runtime tool description 注入（Tool Poisoning）不在能力内；全局 hook，不依赖 forge project\n")
	sb.WriteString("- **task-resume**（SessionStart）：会话启动自动注入活跃任务的接续上下文（`forge task resume --hook`：目标/计划/决策/阻塞/门禁进度/git 已改未提交）+ 把当前 session 锚定到任务——接手方冷启动即知任务在哪一步，无需手动 `forge task resume`；无活跃任务静默；项目级 hook（advisory，不阻塞）\n")
	sb.WriteString("- **init-suggest**（SessionStart，全局）：非 forge 的 git 项目首次会话检测为 forge 候选时自动 `forge init`（v1.22 起零项目写入）；`forge suggest decline` 按项目退出，`forge suggest status/reset` 管理标记\n")
	sb.WriteString("- **compact-resume** + **resume-reinject**（PostCompact + UserPromptSubmit）：上下文压缩后自动重注入完整任务接续上下文（compact-resume 置标志 → 下一条用户消息 resume-reinject 注入），不靠 agent 主动 `forge task resume`；部分宿主无 PostCompact 事件，压缩场景回落 SessionStart tl;dr 档\n")
	sb.WriteString("- **review-stop**（Stop）：Stop 链上的硬门禁——非 task 模式下存在未审查源码变更时拦截会话停止（exit-2 block），提示「派只读子 agent 审查当前 diff 后运行 `forge review pass`」；task 模式下直接放行（审查由 task-complete 门禁强制）。task-verify 同为 Stop hook 但只 advisory 不阻塞\n")
	sb.WriteString("- **辅助检查（仅 WARN 不阻塞）**：聚焦变更/避免重复等判断性规则已下沉为 forge-quality 的 Red Flags 文本；「先读再改」不在此列——活跃任务内它是 read-before-edit 硬门禁（见上），仅任务外场景（非任务编辑、跨会话接手）是 Red Flags 自律文本。\n\n")

	sb.WriteString("### 常见错误\n\n")
	sb.WriteString("| 错误信息 | 原因 | 解决方法 |\n")
	sb.WriteString("|----------|------|----------|\n")
	sb.WriteString("| WARN [task-guard] Untracked source edit（第1次三段式提示，第2次升档 STOP） | 无活跃任务时 Write/Edit 源码（放行但计数升档） | 启动任务让变更被追踪和评分 |\n")
	sb.WriteString("| WARN [bash-guard] ... Bash write without active task | 无任务时 Bash 写文件（仅警告，但源码会被 file-sentinel quarantine） | 先启动任务 |\n")
	sb.WriteString("| FAIL [hazard-guard] 高危操作已拦截（需 human-in-the-loop 确认） | Bash 命令命中高危指纹（`rm -rf` 深目录/盘根/引号逃逸等） | 确需执行：用户本回合已明确指令/确认过该操作 → 直接 `forge hazard confirm --last` 放行一次，无需二次确认；否则先用所在工具的提问确认机制说明风险获确认；非误报勿绕过 |\n")
	sb.WriteString("| FAIL [freeze-guard] ... 路径不在冻结允许清单 | `forge freeze` 激活期间写了清单外路径 | `forge freeze --status` 看允许范围；确实要写：与用户确认后 `forge freeze --off` 解除 |\n")
	sb.WriteString("| BLOCKED: task-complete requires code-review-gate | complete 前置门禁：diff 未经真实代码审查 | 派只读子 agent 审查当前 diff → 修复发现 → **重新派只读子 agent 复审修复**（修复者不能自证）→ `forge review pass --note \"<复审结论>\"`（距上次基线有源码变更时裸 pass 被拒；确认无需复审用 `--acknowledge-changes` 自我承担，记 WARN self-refresh 审计）→ 再过 task-complete |\n")
	sb.WriteString("| insufficient work activity | 门禁间工具调用 <1 次 | 用 Read/Grep/Glob 探索代码 |\n")
	sb.WriteString("| task-verify advisory: ... source files changed without a corresponding test | 改了源码没加对应测试文件（铁律4：测试伴随变更，advisory 仅提醒不阻塞） | 为变更的源码加 `_test.go`/`.test.ts`/`test_*.py` 等；入口(main.go/cmd)/生成物(.gen./_generated/.pb.)/纯类型文件(types/dto/models)白名单免测；不可测时用 `forge task override --test-coverage disable`（per-task，优先于 `FORGE_TEST_COVERAGE=disable` env，不污染他任务；验证类逃生降 evidence 强度到 Weak，重证据任务按证据缩放豁免） |\n")
	sb.WriteString("| task-verify 拒绝（复发升 HARD stop）：项目 testing/scope 维度反复低分 | 项目已完成任务历史里该维度低分≥阈值次（advisory 靠自律已被证明失效），且本次严重（缺配对测试 / 超 scope 多文件 drift） | 补测试或 `forge task scope add <glob>` 收编后重跑；或 `FORGE_TEST_COVERAGE=disable`（降 Weak；重证据任务按证据缩放豁免）；或 `FORGE_RECURRENT_HARDEN=disable` 回退纯 advisory |\n")
	sb.WriteString("| task-verify 拒绝（HARD stop）：改了 skill ... 的 SKILL.md 未记决策 | 改 `skills/<name>/SKILL.md`（行为契约）未在 `decisions.md` 新增 `## [d-` 决策条目（guardrail） | `forge skills decide --skill <name> --outcome <accept/reject> --diagnosis <为何改> --revision <改了啥> --evidence <依据>` 记四元组；trivial 改动用 `forge task override --skill-decisions disable`（per-task，优先于 `FORGE_SKILL_DECISIONS=disable` env，降 evidence 到 Weak；重证据任务按证据缩放豁免） |\n")
	sb.WriteString("| task-complete 拒绝：验收 #N 未实跑/基于旧代码/未通过 | task 声明了 acceptance（`task start --accept`），complete 时校验每条须快照新鲜且 `Passed`（deterministic pre-flight；快照绑任务 HeadCommit 锚定的源码内容指纹——verify 后 commit 不再使快照过期，验收后改源码才会） | `forge task verify-acceptance` 实跑回扣（验收后改码须重跑使快照刷新）；验收不可机器执行用 `forge task override --acceptance-gate disable`（per-task，优先于 `FORGE_ACCEPTANCE_GATE=disable` env，降 evidence 到 Weak；重证据任务按证据缩放豁免） |\n")
	sb.WriteString("| doc gate 拒绝：L1 lint 硬失败 / L2 回检未记录·过期·低分 / Critical 未决 | 任务变更了 markdown 产物，complete 前须过文档回检（输出→回检循环） | `forge docs lint <paths>` 修 L1 → 按 doc-review skill 评审（产出者不能自检）→ `forge task doc-review --passed pass --score <N>`；逃生 `forge task override --doc-gate disable`（3 轮未过后须人工确认；降 evidence 到 Weak） |\n")
	sb.WriteString("| --branch on non-main | `--branch` 只在 master/main 可用 | 已在 feature 分支时去掉 `--branch` |\n")
	sb.WriteString("| task already exists | 任务已启动 | 用 `forge task status --ref <ref>` 查看 |\n")
	sb.WriteString("| Quarantined by file-sentinel | Bash 写了源码但无任务 | 文件在用户级 DataDir/quarantine/（`forge data-dir` 查看路径），可恢复。先启动任务 |\n")
	sb.WriteString("| complete 后提交被 file-sentinel 拦 | complete 已清 active task ref | 先 commit 再 complete；或开 `chore/*-commit` 任务放行 |\n")
	sb.WriteString("| trace/老任务历史消失 | retention（默认启用）自动清超期 checklog/toollog 归档 + 已完成任务文件 | 行为正常；`FORGE_LOG_RETENTION_DAYS` 控制保留天数（默认 30，≤0 禁用）；`forge act rebuild` 全量重建，被 retention 删的任务无法重建 |\n\n")

	if forClaude {
		sb.WriteString("使用 `/forge-quality` 查看完整质量协议。\n\n")
	} else {
		// AGENTS.md 是跨 agent 的（codex/cursor/copilot/windsurf/cline）——这些
		// agent 没有 Claude slash command，故指向 forge CLI surface，
		// 而非 /forge-quality skill。
		sb.WriteString("通过 forge CLI（forge task/gate）执行上述质量流程。\n\n")
	}
	sb.WriteString(forgeSectionEnd + "\n")
	return sb.String()
}

// replaceForgeSection 替换 FORGE:START 与 FORGE:END 标记之间的内容，
// 标记外的所有内容原样保留。util.ReplaceMarkedSection 的薄封装
// （与 agentbridge 的 .windsurfrules upsert 共享）。
func replaceForgeSection(content, newSection string) string {
	return util.ReplaceMarkedSection(content, newSection, forgeSectionStart, forgeSectionEnd)
}

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

// dirExists 报告 path 是否为已存在目录。供用户级生成器的检测自毒防护使用：
// agent 的 config home 存在 = 该工具已安装（DetectAgents 的信号），故生成器
// 只往已存在的 home 里写。
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

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
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("skillgen: read %s: %w", path, err)
	}
	if err == nil && len(existing) > 0 {
		updated := replaceForgeSection(string(existing), forgeSection)
		return util.AtomicWrite(path, []byte(updated), 0644)
	}
	return util.AtomicWrite(path, []byte(forgeSection), 0644)
}

// GenerateUserClaudeMD upserts the (conditional) forge section into the user-level ~/.claude/CLAUDE.md — backup-then-append via userassets.BackupOriginal first.
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

// GenerateUserAgentsMD upserts the (conditional) forge section into the user-level ~/.codex/AGENTS.md (CODEX_HOME env first, else ~/.codex).
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

// StripUserInstructions removes the FORGE:START/END marked section from both user-level files (~/.claude/CLAUDE.md and ~/.codex/AGENTS.md), preserving all other content.
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
		stripped := util.StripMarkedSection(string(existing), forgeSectionStart, forgeSectionEnd)
		if stripped == string(existing) {
			continue // no forge section — nothing to do
		}
		if err := util.AtomicWrite(path, []byte(stripped), 0644); err != nil {
			return fmt.Errorf("write stripped %s: %w", path, err)
		}
	}
	return nil
}

// GenerateUserQualitySkill writes the forge-quality skill to the user-level skill roots from the given protocol — same content as the project-level GenerateQualitySkill, different target dirs.
//
// GenerateUserQualitySkill 从给定 protocol 生成用户级 forge-quality skill——
// 内容与项目级 GenerateQualitySkill 相同，仅目标目录不同。目标：
//   - ~/.claude/skills（Claude Code，claudeConfigHome 解析）
//   - ~/.agents/skills（跨 agent 共享约定目录——kimi 等 agent-neutral 宿主从
//     这里读 forge-quality；与 skillsdist.TargetAgents 同约定，无 env 覆盖）
//
// 因用户级 skill 在所有项目中加载，无条件的"本项目"措辞微调为条件式（最小改动）。
//
// 每个目标的 home 不存在时各自 no-op——与 GenerateUserClaudeMD 同款检测自毒
// 防护（config home 存在 = 已安装信号）。这同时刷新已退役生成器留下的
// ~/.agents 孤儿副本（此后无任何代码路径重写它，内容早已腐烂）。
func GenerateUserQualitySkill(proto *protocol.Protocol) error {
	home := claudeConfigHome()
	if home == "" {
		return fmt.Errorf("cannot resolve Claude config home (CLAUDE_CONFIG_DIR unset, user home unavailable)")
	}
	if err := GenerateUserQualitySkillTo(filepath.Join(home, "skills"), proto); err != nil {
		return err
	}
	// 次要副本尽力而为：~/.agents 副本是 ~/.claude 正本的镜像，此处写失败
	// （权限、只读目录）不得在主目标已落盘时拖垮整个调用——告警到 stderr，
	// 不返回 error。
	if userHome, err := os.UserHomeDir(); err == nil {
		if err := GenerateUserQualitySkillTo(filepath.Join(userHome, ".agents", "skills"), proto); err != nil {
			fmt.Fprintf(os.Stderr, "warning: ~/.agents forge-quality skill refresh failed (primary ~/.claude copy written): %v\n", err)
		}
	}
	return nil
}

// GenerateUserQualitySkillTo writes the forge-quality skill under the given skills root (e.g. ~/.claude/skills, ~/.agents/skills or ~/.reasonix/skills) — the shared user-level skill writer used by GenerateUserQualitySkill and the reasonix translator.
//
// GenerateUserQualitySkillTo 把 forge-quality skill 写到给定 skills root
// （如 ~/.claude/skills、~/.agents/skills 或 ~/.reasonix/skills）——
// GenerateUserQualitySkill 与 reasonix translator 共享的用户级 skill 写入器。
// 同样的条件激活内容与自毒防护：agent home（skillsRoot 的父目录）不存在时
// no-op，Forge 绝不自行创建 agent 的配置 home。
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
	// 条件激活：用户级 skill 在非 forge 项目中也可见，不能无条件断言"本项目"。
	content = strings.Replace(content,
		"你是本项目的质量守护者。以下标准在任何开发会话中都有效。",
		"你是 Forge 项目的质量守护者。仅当当前项目已执行过 `forge init`（在 Forge 全局项目注册表中）时，以下标准才生效；当前项目未使用 Forge 时忽略本 skill。",
		1)
	// 项目信息章节指向单个具体项目——用户级无意义（skill 服务所有项目），移除。
	if idx := strings.Index(content, "## 当前项目信息"); idx != -1 {
		content = strings.TrimRight(content[:idx], "\n") + "\n"
	}
	return util.AtomicWrite(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644)
}
