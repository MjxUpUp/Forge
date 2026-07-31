# release-readiness — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c771dc26c2c0f0-ae213ff9] accept

- **Skill**: release-readiness
- **DecidedAt**: 2026-07-31T18:02:47Z

### Diagnosis

整体审查发现两处命令错误：:144 嵌套引号 grep 实际只匹配开引号一个字符（正常非空默认值全命中，假阳性淹没 M5）；:70 awk 有 \>? typo 且 rule1 置 flag 后同行 rule2 即 exit，该命令实际从不输出任何内容

### Revision

:144 拆成明确匹配空串的两条简单 grep（空双引号/空单引号各一）；:70 typo 统一为 \[<NEW_VER>\]? 并加 next 修复同行 exit bug、去掉从未赋值的 seen 变量

### Evidence

在 /tmp 用样例 CHANGELOG 实测：原写法与仅修 typo 均无输出，加 next 后正确打印目标章节；两条 grep 对空串默认值得命中、非空值不误报
