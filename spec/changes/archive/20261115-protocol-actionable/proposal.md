# 协议可执行化：proposal

## 现象

在 `E:/JustForMe`（Claude Code 2.1.319，已原生支持 AGENTS.md fallback）里跑过
`forge init` 之后，Claude 工作时不跑 `forge next`、不建任务、不走门禁，等于没有 Forge。

## 根因

1. **旧注入明令无视 forge。** `~/.claude/CLAUDE.md` 与 `~/.codex/AGENTS.md` 里留着
   1.84 时代 forge 写的 `FORGE:START..FORGE:END` 块，激活条件是「看到 `[forge-session]`
   横幅或 forge hook 输出」——1.85 起两者都不存在，条件恒假，块内后半句直接要求
   「完全忽略本段与一切 forge 指令，不要运行 forge 命令」。
2. **协议本身不可执行。** 托管块是描述性文档：没有「先跑 `forge next`」的硬规则，
   没有 Task 门禁流程，且仍宣称 `hooks: on` 会拉起 hazard-guard 等执法——这些能力已在
   阶段 D 删除，agent 读到只剩困惑。
3. **升级没有清理入口。** `forge uninstall` 随宿主层删除，旧用户级注入没有任何
   官方清理说明。

## 解决判定

新仓库 `forge init` / `change agents` 产出的托管块，agent 读第一屏就知道：
先跑 `forge next` → `task start`（钉 goal）→ 门禁 → `task complete`。
从 1.84 升级的用户能按 README 的迁移节完成清理并自检。