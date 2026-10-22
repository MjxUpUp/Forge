---
status: implemented
owner: MjxUpUp
ask: 已合入 3e22f1b2（#121）；Open Questions 2026-10-21 全部拍板
tier: mini
---

# 技术方案 | 任务接续改为按需拉取（2026-10）

## TL;DR
SessionStart 只输出 `[forge-session]` 横幅，不再自动注入接续视图、不自动挂接 session、不推送待认领任务。接续改由用户主动发起：「继续某某任务」走 `forge task resume --match`（模糊匹配）列候选 → agent 让用户确认 → `resume --ref` 挂接；「还有哪些任务没完成」走 `forge task list --open`。没走接续就直接改代码的 session，在首次源码写入时静默挂接，只做记录、不要求补材料。压缩后的重注入保留。

## 问题与目标
- **噪音**：会话一启动，用户还没开口，SessionStart 就把接续上下文推给 agent——单一活跃任务时注入整份接续视图并挂接 session，多任务时注入候选清单（原 `renderHookResume`，基线 `dd27933f`，本方案已删除）。用户没想接续时，这是噪音，还会把 agent 往旧任务上带。
- **待认领推送没人用**：本机 `~/.forge/projects` 507 个项目、71 份任务记录，带 `assignment` 字段的为 0，checklog 与 shell 历史里也没有 `assign/claim/deliver/mine` 调用（2026-10-21 实测：扫 `~/.forge/projects/*/tasks/*.json` 中含 `"assignment"` 的文件计 0，`grep -E "forge task (assign|claim|deliver|mine)" ~/.zsh_history` 计 0）。推送对象是 host 工具名而非 session（`internal/clitask/task.go:ResolveOriginTool`），单 agent 时等于广播给自己；多 agent 场景实际靠协调者显式派活，推送用不上。另有已登记缺陷：丢弃 SessionStart 输出的 host 上，推送没送达也会记 `NotifiedAt`（`internal/clitask/task_continuity.go:renderHookReinject` 冷启动分支注释，基线 `dd27933f`）。
- **拉取入口不够用**：`resume --ref` 只认精确 ref；`task list` 只看当前项目，只显示 ref 和 active/completed（`internal/clitask/task_misc.go:runTaskList`）。
- **目标**：SessionStart 注入量降到横幅 1 行；两个用户场景各一条命令就能拿到结构化数据，agent 据此提问、用户选择。

## Non-goals
- 不改压缩后重注入（`compact-resume` + `resume-reinject` 的压缩分支）：它只补回当前 session 正在做的任务，用户不可见。
- 不改活跃任务解析顺序（`internal/taskpipeline/state.go:ActiveTaskStateWithPath`）：显式指针 → 目录绑定 → 分支 → legacy 指针，均不依赖 SessionStart 挂接，guard/门禁行为不变。
- 不删派活 CLI（`assign/claim/deliver/mine/reclaim`）和 resume 时的自动认领（`tryAutoClaim`）。
- 不删 `Assignment.NotifiedAt` 字段（只停写），不做 schema 迁移。
- 不在 CLI 里做交互式确认：hook/CLI 在 agent 环境下没有终端，确认由 agent 用结构化提问工具完成。
- 不实现「一次选多个任务并行接续」：同一 session 只能有一个活跃任务，多选时 skill 指引另开会话，CLI 不感知。
- 不给丢弃 SessionStart 输出的 host（kimi 等）补发横幅：这些 host 上横幅本来就到不了模型，维持现状。
- 不做拼音匹配：需要引入拼音字典依赖，收益不抵成本。
- 接续不追补上游产物：挂接（显式或懒挂接）不重跑 proposal/spec/design 阶段检查，不要求用户补 spec。产物检查仍只在 task-implement 门禁和 complete 前漂移检查发生（`internal/taskpipeline/artifactchain_gate.go`），默认链全部是 advisory（`internal/artifactchain/artifactchain.go:DefaultChain`）。

## 需求与验收
- REQ-1 当 managed 项目触发 SessionStart 时，`forge task resume --hook` 只输出 `PASS` + 横幅一行，与有无未完成任务、是否有派给本 agent 的任务无关；不新增 SessionLink，不写 `NotifiedAt`。
  accept[REQ-1, INV-1, INV-3, NFR-1]: go test ./internal/clitask/ -run ^TestHookResumeBannerOnly$ -count=1 :: --- PASS: TestHookResumeBannerOnly (
- REQ-2 当 host 丢弃 SessionStart 输出（kimi：hostcap `DroppedStdoutEvents` 含 SessionStart）且本 session 未压缩过时，`resume --reinject` 不再补发冷启动接续视图，输出为空。
  accept[REQ-2]: go test ./internal/clitask/ -run ^TestReinjectNoColdStartBackfill$ -count=1 :: --- PASS: TestReinjectNoColdStartBackfill (
- REQ-3 当本 session 刚压缩过时，`resume --reinject` 照旧注入当前任务的完整接续视图（回归保护）。
  accept[REQ-3]: go test ./internal/clitask/ -run ^TestReinjectAfterCompact$ -count=1 :: --- PASS: TestReinjectAfterCompact (
- REQ-4 当用户说「继续某某任务」、agent 执行 `forge task resume --match "<关键词>" [--workspace|--all-projects] --json` 时，系统在范围内的未完成任务里按「模糊匹配」一节的算法打分，返回得分最高的至多 10 个候选；该调用只读（等价 `--no-attach`），不挂接、不自动认领；零命中返回空数组、exit 0；与 `--ref` 同时给出时报错退出。
  accept[REQ-4, INV-2]: go test ./internal/clitask/ -run ^TestResumeMatch$ -count=1 :: --- PASS: TestResumeMatch (
- REQ-5 当用户说「还有哪些任务没完成」、agent 执行 `forge task list --open [--workspace|--all-projects] [--json]` 时，系统列出范围内 `CompletedAt == nil` 的任务，每条带：项目、ref、Summary、分支、门禁进度、首条下一步、未解决阻塞数、派活状态（offered/claimed/空）、最后活动时间；默认范围是当前项目，`--workspace` 是当前项目所属 forge workspace 的全部成员，`--all-projects` 是 `registry.ListManaged()`；不加 `--open` 时 `task list` 输出不变。
  accept[REQ-5]: go test ./internal/clitask/ -run ^TestTaskListOpen$ -count=1 :: --- PASS: TestTaskListOpen (
- REQ-6 当 agent 加载 forge-quality skill 时，skill 含「任务接续」一节：两个场景的触发说法、命令、确认步骤（用结构化提问工具，没有就文字提问）、多选时「当前会话接一个、其余建议另开会话」的约束。
  accept[REQ-6]: go test ./internal/skillgen/ -run ^TestQualitySkillResumeSection$ -count=1 :: --- PASS: TestQualitySkillResumeSection (
- REQ-7 当某个 session 未跑 `resume --ref`、但 task-guard 首次放行它对活跃任务的源码写入时，系统静默把该 session 写进 `SessionLinks`（与 `attachCurrentSession` 同一实现），不输出任何提示、不改 `CurrentGate`、不检查产物链。
  accept[REQ-7, INV-4]: go test ./internal/hookdispatch/ -run ^TestLazyAttachOnFirstWrite$ -count=1 :: --- PASS: TestLazyAttachOnFirstWrite (
- REQ-8 模糊匹配覆盖：错拼（`reusme-pull` 命中 `resume-pull`）、乱序词（`pull resume`）、缩写子序列（`rsmpl`）、中文片段（「接续拉取」命中 Summary「任务接续改为按需拉取」）；单字符查询和无关查询不返回噪音候选。
  accept[REQ-8]: go test ./internal/clitask/ -run ^TestFuzzyMatch$ -count=1 :: --- PASS: TestFuzzyMatch (
- NFR-1 SessionStart 下 `task-resume` 的注入不超过 1 行（不含 `PASS` 标记）。由 REQ-1 的测试断言行数。
- NFR-2 本机 `forge task list --open --all-projects --json` 墙钟 ≤ 2s。
  accept[NFR-2]: go run ./cmd/forge task list --open --all-projects --json
  （accept 行不经 shell 无法计时，只验退出码 0。墙钟手测：2026-10-21 本机全部 managed 项目 0.41s）

## 设计
### 触点
- `internal/clitask/task_continuity.go:runTaskResume` hook 分支只打横幅；新增 `--match` 分支（强制只读）
- `renderHookResume` / `appendOfferedBlock` / `renderOfferedBlock` / `offeredChainSiblings` / `otherIncompleteTasks` 删除，hook 分支改调新的 `internal/clitask/task_continuity.go:renderHookBanner`
- `internal/clitask/task_continuity.go:renderHookReinject` 删除冷启动补发分支（及 `IsColdStartInjected` / `MarkColdStartInjected` 的调用）
- `renderTaskInventory` 删除；`list --open` 与 `--match` 共用 `internal/clitask/task_scope.go:renderOpenEntryLine`
- `internal/clitask/task.go` 的 `taskListCmd` 加 `--open/--workspace/--all-projects`；`internal/clitask/task_continuity.go` 的 `taskResumeCmd` 加 `--match/--workspace/--all-projects`
- `internal/clitask/task_misc.go:runTaskList` 接 `--open`
- new: `internal/clitask/task_scope.go` 范围解析（当前项目 / workspace 成员 / 全部 managed），跨项目遍历复用 `runTaskMineAllProjects` 的写法（单项目失败 stderr 警告并跳过）
- new: `internal/hookdispatch/hook_lazy_attach.go`，由 `hook.go:runHook` 在 task-guard 脚本放行后调用（挂 task-guard 是因为它在 lite 档也接线，PostToolUse Write|Edit 组都不在 lite 档）：写入目标带源码后缀（new: `taskpipeline.IsSourceExt`，复用 test-coverage 的 `sourceExts`）且当前 session 不在活跃任务的 `SessionLinks` 里时静默挂接（REQ-7）；已挂接时只做一次内存判断，不取锁
- new: `internal/taskpipeline` 的 `AttachSession(root, ref, sid, tool)`：锁内重载 + `AddSession` + `ClaimLeaseForCurrentNode`，`clitask.attachCurrentSession` 与懒挂接共用
- `Assignment.ShouldNotify` 删除；`internal/tasktypes/types.go` 的 `NotifiedAt` 注释标为停写保留
- `internal/skillgen/qualitygen.go:buildQualitySkillContent` 加「任务接续」节
- `internal/skillgen/claudemd.go`（task-resume 描述行）、`README.md`（hook 表 task-resume 行）同步改描述
- 新 flag 写进根 `README.md` 命令参考（`internal/cli/docs_consistency_test.go` 守卫要求用户可见 flag 进 README，不进豁免表）
- 测试：`task_continuity_render_test.go`、`task_continuity_banner_test.go`、`task_assignment_lifecycle_test.go`、`internal/taskpipeline/assignment_test.go` 中钉住旧行为的用例改写或删除
- hook 接线（`internal/hooks/settings.go`）不动：`task-resume` 保留名字和挂点，只是输出变少，避免重新生成 plugin.json；仍称「SessionStart 是压缩兜底」的注释同步修正

### 接口/契约
```go
// forge task list --open --json 与 forge task resume --match --json 共用的条目
type openTaskEntry struct {
    Project      string    `json:"project"`       // forgedata.Key，失败回退 filepath.Base(root)
    Root         string    `json:"root"`
    Ref          string    `json:"ref"`
    Summary      string    `json:"summary,omitempty"`
    Branch       string    `json:"branch,omitempty"`
    Gate         string    `json:"gate"`          // renderGateProgress 的文本
    NextStep     string    `json:"next_step,omitempty"`
    OpenBlockers int       `json:"open_blockers"`
    Assignment   string    `json:"assignment,omitempty"` // offered | claimed | 空
    LastActivity time.Time `json:"last_activity"` // max(History 末条时间, StartedAt)
    MatchedOn    string    `json:"matched_on,omitempty"` // 仅 --match：ref|summary|branch|goal
}
```
- 文本输出：每条一行，字段截断沿用 `inventoryFieldCap`；跨项目时按项目分组。所有用户来源字段（ref/Summary/NextStep）输出前过 `stripUnsafeControl`。
- `--match` 关键词：去首尾空白后不能为空、≤ 200 rune，否则报错；只做字符比较，不当正则或 glob 解释。JSON 条目多一个 `score`（0–100）。

### 模糊匹配
纯函数，new: `internal/clitask/task_match.go`，不引入新依赖。
1. **归一化**：小写；按非字母数字（含 `-` `_` `/` 空白）切词；连续汉字段整体算一个词（拆单字会让散落的汉字凑齐全词档），另取相邻两字组成的二元组供中文重合档使用。
2. **逐字段打分**，取下表命中的最高档：

| 档 | 条件 | 分 |
|---|---|---|
| 精确 | 查询等于 ref | 100 |
| 前缀 | 字段以查询开头 | 90 |
| 子串 | 字段包含查询 | 80 |
| 全词 | 查询 ≥2 个词，且每个词都出现在字段里，顺序不限 | 70 |
| 错拼 | 每个查询词要么出现在字段里，要么（仅 ≥4 字符的词）能在字段里找到编辑距离 ≤ max(1, ⌊词长/4⌋) 的词；且至少一个词用到了这项放宽 | 60 |
| 中文重合 | 查询的汉字二元组在字段里出现的比例（覆盖率）≥ 0.5 | 50 + 20×(覆盖率−0.5)/0.5 |
| 子序列 | 查询字符按序出现在字段里，查询 ≥ 3 字符，且跨度 ≤ 查询长度 × 3 | 40 |

3. **字段权重**：ref ×1.0、Summary ×0.9、Branch ×0.8、Goal ×0.6；任务得分取各字段最高，`matched_on` 记录得分来源字段。
4. **噪音下限**：得分 < 40 丢弃；查询少于 2 个字符时只走精确档。用覆盖率而非 Dice 系数：标题通常远长于查询，Dice 会被标题长度稀释（「接续拉取」对「任务接续改为按需拉取」Dice 仅 0.33，覆盖率 0.67）。
5. **排序**：得分降序 → 最后活动时间降序 → ref 升序，结果确定。
6. 不论得分多高，CLI 都只返回候选、不替用户决定；由 agent 让用户确认。

### 不变量
- INV-1 SessionStart 路径不写任何任务状态：不加 SessionLink，不改 Assignment（由 REQ-1 测试断言 hook 前后任务文件字节相同）。
- INV-2 `resume --match` 不写任何任务状态（由 REQ-4 测试断言前后 state 字节相同）。
- INV-3 横幅文本不变：它是用户级 CLAUDE.md 的激活判据（REQ-1 的测试按字面钉住）。
- INV-4 挂接（`resume --ref` 或 REQ-7 懒挂接）只写 `SessionLinks` 和节点租约，不改 `CurrentGate`、不触发产物链检查、不向用户提问（由 REQ-7 测试断言 state 中除这两项外字节相同）。

## 备选对比
| 方案 | 一句话描述 | 结论 |
|---|---|---|
| A 按需拉取（推荐） | SessionStart 只留横幅，接续由用户说话触发 | — |
| B 保留推送 + 环境变量关闭 | 加 `FORGE_TASK_RESUME=0` | 否：默认仍然吵，要求用户先知道开关存在 |
| C 整个下线 task-resume hook | SessionStart 什么都不输出 | 否：横幅是 forge 协议的激活信号，去掉后用户级指令段失效 |
| D 只加一行计数提示 | 横幅 + 「有 N 个未完成任务」 | 否：用户已决定不保留，想知道时主动问 |
| 什么都不做 | 维持自动注入 | 否：上文噪音问题持续，待认领推送 0 使用仍占代码和缺陷 |

## 风险与高危步骤
- **SessionLink 变少**：不再自动挂接后，没跑 `resume --ref` 就在任务目录里改代码的 session 不会出现在 `SessionLinks`，节点租约也不会转过来。REQ-7 懒挂接补回这两项记录；它不写 session 级 `active-task-ref`，所以分支误挂守卫 `internal/taskpipeline/state.go:taskAnchoredByOtherActiveSession`（只认带新鲜 `active-task-ref` 的链接）仍看不到这个 session——与改动前自动挂接的效果相同（自动挂接同样不写该文件）。只读代码、不写源码的研究型 session 不挂接，这是有意的。活跃任务解析本身不受影响。
- **无 PostCompact 的宿主压缩后不再恢复接续上下文（已知缺口）**：zcode 压缩时以 `source=compact` 触发 SessionStart，cursor、windsurf 等没有 post-compaction 事件；改动前它们靠「每次 SessionStart 都注入接续视图」顺带恢复，现在 SessionStart 只出横幅。Claude Code 与 codex 走 PostCompact + resume-reinject，不受影响。补法需要 hook 输入新增 `source` 字段并按宿主区分（Claude Code 上 SessionStart(compact) 与 resume-reinject 会双注），留待单独决定；在此之前这些宿主压缩后需要 `forge task resume` 手动拉回。
- **`resume --ref` 不切换活跃任务（既有行为）**：它只写 SessionLink，不写 session 级 `active-task-ref`（自动认领分支除外：offered 给本 agent 的任务经 `internal/clitask/task_continuity.go:tryAutoClaim` 会写 active-task-ref）。用户从 `list --open` 选中一个不在当前分支、也没有目录绑定的任务时，后续写入仍按解析顺序归到别的任务（或无任务）。forge-quality 的接续一节因此要求先切到任务所在分支/worktree 再改代码；让 `resume --ref` 直接切换活跃任务是独立的后续改动。
- **懒挂接进了写入热路径**：每次源码写入多一次 `HasSession` 内存判断。只有首次写入取 per-task 锁写盘；锁超时或写盘失败只记 stderr、照常放行。挂在 PreToolUse 上，写入随后可能失败（如 Edit 的 old_string 不匹配），此时也已挂接——「打算改这个任务的代码」足以说明 session 在做这个任务，接受。
- **其他 host 的用户失去自动接续**：codex/cursor/windsurf 等原先靠 SessionStart 拿接续视图。缓解：forge-quality skill 在各 host 都分发；接续命令不依赖 host。
- **skill 改动的发版步骤**：改 `qualitygen.go` 后要用仓库构建的二进制重新打 plugin 包、重固定 `compat.snapshot.json`，再跑全量 `go test`。

## Open Questions
全部已决（2026-10-21）：
- [x] SessionStart 不保留计数行；待认领推送下线、派活 CLI 保留。
- [x] 丢弃 SessionStart 输出的 host 不补发横幅（见 Non-goals）。
- [x] 懒挂接纳入本方案（REQ-7），只做记录：不论任务做到哪一步，都不要求用户补 spec 等材料（INV-4）。
- [x] `--match` 做模糊匹配（REQ-8），不做拼音。

## 验证与回滚
- 端到端：在一个有 2 个未完成任务的 managed 项目里新开 Claude Code 会话，确认启动上下文只有横幅；说「还有哪些任务没完成」，agent 调 `list --open` 并让用户选；选中后 `resume --ref` 输出完整接续视图，`forge task context --ref <ref> --json` 的 `session_links` 含本 session。
- 全量：`go vet ./...`、`go test ./...`。
- 回滚：revert 单个提交即可。无 schema 变更；`NotifiedAt` 字段保留，新旧二进制读写同一份任务 json 互不破坏。
