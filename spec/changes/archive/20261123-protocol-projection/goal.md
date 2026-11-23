# goal：protocol-projection

## Objective

agent 投递面（AGENTS.md 里的 `forge:begin`/`forge:end` 托管块）改为由 forge 自己的命令树生成，不再是手写模板：块内「命令速查」一节从 `forge compat snapshot` 面 1（cobra 命令路径 + flag 名）派生，块体首行带 `proto=<forge 版本> sha=<块体哈希>`。可观察的完成状态：同一命令树两次生成字节一致；块里不可能出现命令树里没有的命令或 flag（构造一条已删命令必须被拒）；`forge change agents --check` 能报出块与二进制之间的漂移；`forge task start` 在块与二进制不一致时打印一条 advisory（不阻断）。

## Out of Scope

- 不做 195 条命令的逐条 agent 语义补全：只要求「被协议教学的命令」有注解；未注解命令的数量进 `--check` 报告，作为债登记，本次不清零。
- 不把 `docs/reference/cli.md` 改成生成物：本次只加一致性检查（能报出「文档教了不存在的动作」），不改它的来源。
- 不改 `forge task` / `forge change` 的任何门禁语义与默认值（--accept-red 硬化属于 B 批）。
- 不新增顶层命令：只给 `forge change agents` 加 `--check` 与 `--print`。
- skew 只做 advisory，不阻断任何命令。

## Check

判据见 acceptance.md 的围栏 #1–#5。先失败证明用 #1：`internal/protocol` 包尚不存在，当前必然红。
