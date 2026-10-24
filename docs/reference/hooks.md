# Hook 参考（完整）

> 从根 README 拆出的 Hook 全表：27 个内置 Hook 的触发时机与职责。
> 根 README 保留三层防御模型，具体名单在这里。

```
Layer 1: PreToolUse 快速拦截
  ├─ task-guard: Write/Edit → 检查任务状态 + 自保护（forge 配置层）
  └─ bash-guard: Bash → 检测写文件模式

Layer 2: PostToolUse 文件监控
  └─ file-sentinel: Bash → 对比执行前后 git 状态，未授权变更隔离到 DataDir/quarantine/（可恢复）

Layer 3: 会话结束验证
  └─ task-verify: 检查任务完成度 + 主分支保护 + 自身版本
```

<details>
<summary><b>📖 内置 Hook 完整清单（27 个）</b></summary>

| Hook | 触发时机 | 功能 |
|------|----------|------|
| **task-guard** | Write/Edit 前 | 无活跃任务时 WARN（仅 `.forge/*`/`.claude/settings*` 自保护 FAIL——此类项目级文件只在团队模式/老项目存在），保护 Forge 配置不被篡改 |
| **freeze-guard** | Write/Edit 前 | `forge freeze <路径>...` 激活后硬阻断冻结路径之外的 Write/Edit——「只改这里别动其他」的 session 级硬护栏（on-demand-guards /freeze 的 forge 侧落地）；多路径、相对路径归一化、Windows 大小写不敏感；排在 task-guard 之前优先判定；`forge freeze --off` 解除 |
| **read-before-edit** | Write/Edit 前（活跃任务内） | 编辑本会话未 Read 过的现存源文件 → 硬阻断（`BLOCKED`）。Edit 需精确匹配旧文本，未读即凭记忆盲改——old_string 撞中即错改入库，先 Read 再 Edit。豁免新建文件/测试文件/非源码；批量重构逃生 `forge task override --work-activity disable`（记 checklog 审计；work-activity 是节奏门禁，不降 evidence 强度）。reads-log 落盘随会话存活，压缩后仍累计 |
| **assertion-check** | Write/Edit 前 | 检测断言弱化（t.Fatal → t.Log、assert! 被删除等），advisory 提醒不阻塞（agent 自检） |
| **bash-guard** | Bash 前 | 检测命令中的写文件模式（writeFile、cat >、sed -i 等），无任务时 WARN（源码随后被 file-sentinel 隔离） |
| **gate-cmd-form** | Bash 前 | 门禁命令嵌组合形态（分号续行/截断管道/grep 掩蔽/\|\| 链/多门禁连刷）时 **BLOCKED 拦截**——分号让前一门禁 BLOCKED 后链条照走、截断管道吞掉 BLOCKED 文本面，退出码契约被系统性削弱（设计 C；delivery-hardening 墙-1a：1.67 机械版本门 ratchet 生效，会话阻断上限 5 有界；逃生 FORGE_GATE_CMD_FORM=0 落 checklog 审计；重发为单条门禁命令或仅用 && 串联） |
| **task-drift** | Bash 前（活跃任务内） | git 边界命令（commit/merge/建分支）发生在任务分支之外时拦截——自 1.66 起由 advisory 转 BLOCK（机械版本门），会话阻断上限 5 次后回落 advisory；逃生 `FORGE_TASK_DRIFT=0`（落 checklog 审计）；出口：回任务分支提交、推进门禁或 `forge task abort` |
| **hazard-guard** | Bash 前 | 高危命令（`rm -rf`、`git push --force`、`DROP TABLE/SCHEMA`、`TRUNCATE`、`GRANT ALL`、`kubectl delete`、`docker system prune`、无 WHERE 的 `DELETE/UPDATE`、解释器内联删除如 `python -c "os.remove(...)"` 等）human-in-the-loop 拦截：block + 指引 agent 向用户说明风险 → 用户在自己的终端运行 `forge hazard confirm --last` 登记 5min 限时标记（confirm 只接受真人终端）→ 重试放行（confirm 链是唯一放行路径，`FORGE_ALLOW_HAZARD` 已移除） |
| **auto-compile** | Write/Edit 后 | advisory 提醒用对应技术栈编译命令自检（go build / cargo check / mvn / tsc 等），不强制编译 |
| **workflow-test-guard** | Write/Edit 后 | 改 `.github/workflows/*.yml` 后自动跑 `internal/ci` 守护测试，把"沙盒异常"即时反馈给 agent（不依赖 CI 兜底），是 release.yml test→goreleaser→npm needs 链的实时守护层 |
| **file-sentinel** | Bash 后 | 监控文件变更，未授权修改隔离到 DataDir/quarantine/（`forge data-dir` 查看路径，可恢复，不删除） |
| **tool-track** | Read/Skill/Agent/Grep/Glob/Bash 后 | 静默记录工具调用到 toollog（Read/Skill/Agent 记名称、Bash 记截断命令、Grep/Glob 记截断 input——探索调用也计入 work-activity），供 read-before-edit 门禁与 efficiency 维度评分判断（agent 是否先读代码再改、质量 skill 是否被驱动） |
| **failure-track** | 命令失败后（PostToolUseFailure） | 记录 CheckToolFailure 观察；失败文本命中编译/测试类指纹（`undefined:`/`error TS`/`error[E`/`--- FAIL` 等）时注入 compile-fix-loop skill 事实性指针（advisory 不阻断——失败已发生） |
| **subagent-track** | 子 agent 结束（SubagentStop） | 记录 agent_id/agent_type/交付长度+首行摘要到 checklog（归因数据——此前子 agent 活动 forge 侧零记录）；纯观察，无输出无阻断 |
| **test-nudge** | Write/Edit 后（活跃任务内） | 事中测试提醒：连写 ≥3 个源码文件且无配对测试写入时注入一次 test-discipline skill 事实性提示（advisory，每连写只提示一次）；测试写入重置计数；无任务静默；执法仍在 task-verify 门禁 |
| **task-verify** | 会话结束 | 任务门禁/主分支保护到 stderr+checklog（advisory）；活跃任务 + 未提交代码变更时有界阻断 Stop（每会话最多 3 次，超限回落 advisory；逃生 `FORGE_TASK_VERIFY_STOP=0`） |
| **review-stop** | 会话结束 | code-review-gate 自动挡：未审源码变更 block 会话结束。task 模式不重复拦（task-complete 门禁 ReviewPassed 硬前置已强制），非 task 模式按 diff stamp 决策；并发会话检测——其他 session 有活跃任务时放行（调研 session 不被拦） |
| **skill-scan** | 会话开始 | advisory：扫描 ~/.claude/skills 安全性（`forge skills audit`，21 条安全规则），补 install 门控缺口（手动 clone/junction/git pull 进入的 skill），全局 hook 不依赖 forge project |
| **mcp-scan** | 会话开始 | advisory：扫描项目级 `.mcp.json` 的 server 配置（管道执行/任意包执行 npx·uvx·dlx·bunx/内联代码/非 https URL/env 明文凭证），补 skill-scan 盲区（攻击者可经 PR 植入恶意 server，clone 即自动连接）；只审 config 层，runtime tool description 注入（Tool Poisoning）不在能力内，全局 hook |
| **init-suggest** | 会话开始 | advisory，接管决策引擎（P2 起默认 **ask**）：declined（标记+注册表双检）→静默；已接管→顺带 dedupe；外来 harness 信号（P4）→让位并记 declined；takeover 偏好 auto（`FORGE_TAKEOVER`/`forge config`/legacy `FORGE_AUTO_INIT=1`）→静默 init；off→静默；ask→每项目首次询问一次（拒绝→`forge off`；全程询问 →`forge config set` takeover 为 auto）。v1.22 起 init 零项目写入 |
| **task-resume** | 会话开始 | advisory：只输出 `[forge-session]` 管理横幅一行（用户级 CLAUDE.md 据此判断是否启用 forge 协议）。不注入接续视图、不锚定 session、不推送待认领任务——接续按需拉取：用户说「继续某某任务」走 `forge task resume --match`，说「还有哪些任务没完成」走 `forge task list --open`；项目级 hook |
| **compact-resume** | 压缩后（claude-code / codex） | PostCompact 时设 `ResumeStale=true` 标志（PostCompact 不在 additionalContext 注入点，只设标志等下个 prompt 重注入），context-rot 抗机制根治层·设标志半边 |
| **resume-reinject** | 用户提交时（claude-code / codex） | 检测 `ResumeStale=true`（刚压缩过）→ 输出当前任务的完整接续上下文并清标志；未压缩时静默。压缩只丢当前 session 的工作记忆，补回它不打扰用户，context-rot 抗机制根治层·重注入半边 |
| **conventions-context** | 会话开始 + 压缩后 | advisory：`forge conventions init` 建档后每会话注入 ≤15 行规范摘要（stack/lint 命令、规范声明文件、惯例要点），压缩后重注入；无档案但仓库已有 AGENTS.md 等规范时每会话一次建档建议 |
| **conventions-write** | Write/Edit 前 | advisory：写源码或测试文件时注入本仓库规范声明文件指针与同目录范例文件（每目录每会话一次）；无档案时静默 |
| **doc-lint** | PostToolUse（Write\|Edit） | advisory：被写的 .md 命中 D1-D11 L1 规则时注入命中清单（规则/行号/修复方向，按命中签名会话内去重），写时反馈提前于 complete 的 doc gate（output-readability-gates-v2.md P1-C），Go 原生实现 |
| **skill-trigger** | 多事件（Pre/PostToolUse、SessionStart、Stop、UserPromptSubmit，按宿主能力接线） | 通用声明式 skill 触发判定：按各 skill `metadata.triggers` 的 event/when 条件（coding_intent / source_changed_uncommitted / test_command_failed / task_active_no_review / skill_file_touched）匹配上下文，advisory 注入 skill 加载指引；Go 原生实现（Go 原生共 9 个：本 hook 与 failure-track、subagent-track、test-nudge、doc-lint、gate-cmd-form、task-drift、conventions-context、conventions-write；其余 18 个为内嵌 bash 脚本） |

</details>
