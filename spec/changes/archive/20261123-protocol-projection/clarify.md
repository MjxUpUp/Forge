# 澄清：边界与非目标

change: `protocol-projection`

## 边界（做什么 / 不做什么）

边界：托管块由命令树+注解表生成、块头带 proto/sha、change agents 加 --check/--print、task start 对旧块 advisory。非目标：不补全195条命令agent语义、不改cli.md来源、不改门禁语义与默认值、不新增顶层命令、skew不阻断。人确认：2026-11-23。

## 非目标

- 不做 195 条命令的逐条 agent 语义补全：只要求「被协议教学的命令」有注解；未注解命令的数量进 `--check` 报告，作为债登记，本次不清零。
- 不把 `docs/reference/cli.md` 改成生成物：本次只加一致性检查（能报出「文档教了不存在的动作」），不改它的来源。
- 不改 `forge task` / `forge change` 的任何门禁语义与默认值（--accept-red 硬化属于 B 批）。
- 不新增顶层命令：只给 `forge change agents` 加 `--check` 与 `--print`。
- skew 只做 advisory，不阻断任何命令。

人确认：2026-11-23T10:22:28Z
