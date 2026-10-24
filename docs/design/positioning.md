# 定位：coding agent 循环的验证与状态层

> 从根 README 拆出的完整论述；根 README 只保留摘要与指针。

状态：已发布——2026-10 从根 README 拆出，内容为当前口径。

**Forge is the verification and state layer for coding agent loops.** 用 harness engineering 的话说（Böckeler）：computational sensor suite + durable state——代码能验证的交给确定性传感器，Forge 补的是持久状态；用 loop engineering 的话说（Osmani）：loop 五构件里公开缺的正是 verification 与 memory 组件，Forge 就是这两块。

AI 编码是一个循环：写代码 → 运行 → 读反馈 → 修正 → 再写。这个循环由 coding agent（Claude Code、Codex）驱动，**Forge 不替代循环本身**——它补上循环最容易缺的两层：

- **验证层** — 每一轮产出物经门禁检验：编译通过、断言没被弱化、改代码前确实读过代码、文件未被绕道篡改、高危操作经人确认。循环跑得越快，越需要自动化验证兜底，而不是靠人盯着。
- **状态层** — 跨循环的任务状态：3 道门禁（实现 → 验证 → 完成）、活跃任务追踪、门禁历史。"做到哪了 / 是否达标"有持久化、可审计的记录，而不是只活在 agent 的上下文里（上下文一压缩就丢）。

换言之，coding agent 负责**跑循环**，Forge 负责**让每一轮循环产出可信、状态可追**。Forge 是 deterministic 的——范围是 "deterministic where code can verify"：行为/意图正确性是任何传感器的 remit 之外，那部分留给 review 与人。验证的判据与证据链归产品而非模型厂商，所以这一层不随模型代际贬值——模型变强时该删的是脚手架（feedforward），不是传感器（feedback）。

<details>
<summary><b>📖 为什么是确定性门禁，而不是让模型自检？</b></summary>

代码可执行（executable）是编码域相对研究/对话域的结构性优势——跑一遍 test / lint / compiler 拿到的退出码是**事实**，模型自评只是**概率判断**。Forge 的门禁尽量把判定交给确定性检查器，而非 LLM-as-judge：

- **分层共识**（"If code can verify it, don't ask an LLM to judge it"）：LLM 审查作 advisory，deterministic build/test 作 hard gate。 Sonar 的 AC/DC（Agent Centric Development Cycle）是 Guide→Verify→Solve 三段闭环；同一确定性引擎被前移到 agent 内环，做 maker-checker 分离。
- **Code-as-Harness 宣言**（[arXiv:2605.18747](https://arxiv.org/abs/2605.18747)）：「termination should be governed by verification rather than by model confidence」——结束条件由验证决定，不由模型自信度决定。
- **反直觉但关键**：模型越强、自主循环越长，越需要非模型的客观检查兜底——产出越快，无人复核的代码就越多；门禁是把吞吐量从 liability 变回 leverage 的那一个组件。

Forge 退出码三态（`BLOCKED` 硬阻断 / `ADVISORY` 软信号 / 无前缀干净通过）即这一思路的落地：LLM 判定走 advisory，deterministic 事实走 hard。机械可判的模式（cheat-scan / scope-drift / read-before-edit / verify-acceptance）优先抽成 deterministic 扫描器，LLM-reviewer 退到只做语义判断。

</details>
