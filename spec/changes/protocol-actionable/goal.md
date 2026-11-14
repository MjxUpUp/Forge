# 协议可执行化：先跑 forge next、补 Task 流程、清 hooks 残留、写 1.84 迁移说明：目标契约

change: `protocol-actionable`

> 这份契约是任务的验收前提：五段都要填，删掉每段里的 `forge:fill` 注释标记才算写完。
> 启动任务后改它 = 改判据：`task gate task-implement` 会 BLOCKED；还原 goal.md，
> 或 `forge task abort` 后重新 `task start --change` 钉新哈希。

## Objective

`forge change new/init/agents` 写进仓库的 AGENTS.md 托管块，从「背景说明」变成「可执行指令」：
开头明确「任何代码改动前先跑 `forge next`」，补一段 Task 门禁流程；删掉 1.85 已不存在的
hooks 执法叙述；README 增加从 1.84 及更早版本升级时清理旧用户级注入的步骤。

## Why

1.85 起宿主层（hooks、用户级 CLAUDE.md 注入、plugin、skills 分发）整体删除。实测在
`E:/JustForMe` 里 Claude 几乎不遵循 forge，根因有两条：

1. 旧版写进 `~/.claude/CLAUDE.md` 的全局注入仍写着「只有看到 `[forge-session]` 横幅或
   forge hook 输出才按 forge 行事，否则忽略一切 forge 指令，不要运行 forge 命令」。
   1.85 没有横幅也没有 hook，这个条件永远不成立，等于明令 agent 无视 forge。
2. 托管块本身是说明文：没有「先跑 `forge next`」的硬规则，没有 Task 门禁段，
   还留着 `hazard-guard / mcp-scan / skill-scan 随 hooks: on 执法` 这类已删除能力的叙述。

不修这两条，用户在新仓库里跑一次 `forge init` 只会得到一份「读了也不知道要做什么」的文档。

## Constraints

- 托管块仍然只有一份真相源：`internal/change/templates/agents-protocol.md`；
  仓库里的 AGENTS.md 由 `forge change agents` 同步，不改生成机制。
- 不恢复 hook 执法层，不重新引入宿主接线或用户级自动写入。
- 迁移只给「说明 + 可复制的清理命令」，不自动改用户的 `~/.claude`、`~/.codex`；
  删除前要求先备份。
- 老 change（没有 goal.md）仍然兼容；本次不改 change 产物格式。

## Out of Scope

- 不实现「自动检测并清理旧注入」的命令；检测与删除由用户按 README 手工执行。
- 不改 `forge next` / 门禁的判定逻辑，只改写给 agent 的协议文本与升级说明。
- 不调整其他历史文档里对 hazard-guard 的记述（它们记录的是当时的设计，不是现行用法）。

## Check

- `go build ./...` 通过（acceptance #1）。
- 模板回归测试覆盖：包含「先跑 `forge next`」与「## Task 流程」，不含 `hazard-guard` 与
  `hooks: on` 执法叙述（`acceptance` #2、#3）。
- README 的升级节能让人照着清理旧 `FORGE:START` 注入与旧 hook 配置（`acceptance` #4）。
- CLI 托管块同步与 deadcode 守卫不回归（acceptance #5、#6）。