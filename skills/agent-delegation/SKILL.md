---
name: agent-delegation
description: >
  子代理委派与编排完整协议。Use when: 用 subagent 工具分派任务时、写子代理 prompt 时、验证子代理返回时、并行 dispatch 多个独立任务时、需要两阶段审查（spec合规+代码质量）时。
  自包含完整编排流程。SKIP: 纯对话不涉及代码时、改几行代码不值得委托开销时（自己做）、需要先理解代码才能决定怎么改时（自己先探索）。
metadata:
  pattern: pipeline + reviewer
  domain: agent-orchestration
---

# 子代理委派与编排

把任务委托给有隔离上下文的专门代理。精准构造它们的指令和上下文，确保它们专注且成功。**它们绝不继承你会话的历史——你精确构造它们需要的全部。** 这也为你自己的上下文省下用于协调工作。

**核心原则：每任务一个全新子代理 + 两阶段审查（先 spec 合规，再代码质量）= 高质量、快迭代。**

## 原则 1：委托前自己先理解

不要把你不理解的工作委托出去。委托的是**执行**，不是**理解**。

**反模式**：把一个报错直接扔给子代理让它修，自己都没读过相关代码。
**正解**：先定位到文件和行号，理解问题出在哪，把具体修复指令给子代理。

## 原则 2：Prompt 必须自包含

子代理看不到你的对话历史。不知道你之前试过什么、排除了什么、为什么这任务重要。

### Prompt 模板（implementer）

```
你正在实现 Task N: [任务名]

## Task Description
[完整粘贴 plan 里的任务文本——不要让子代理去读文件]

## Context
[场景设定：这任务在整体里处于什么位置、依赖、架构上下文]

## Before You Begin
如果对以下有疑问：
- 需求或验收标准
- 方法或实现策略
- 依赖或假设
- 任务描述里任何不清楚处
**现在就问。** 开工前提出所有顾虑。

## Your Job
需求清楚后：
1. 严格按任务规格实现
2. 写测试（任务要求 TDD 时遵循 tdd-cycle）
3. 验证实现生效
4. 提交你的工作
5. 自审（见下方）
6. 报回

工作目录：[目录]

## 状态报告（必须二选一）
- DONE / DONE_WITH_CONCERNS（说明顾虑）/ NEEDS_CONTEXT（说明缺啥）/ BLOCKED（说明卡点）
```

**禁止的偷懒委托：**
- ❌ "based on your findings, fix the bug"
- ❌ "refactor this module"
- ❌ "implement the feature described above"

这些把理解推给子代理，不是传达你已理解的内容。

## 原则 3：两阶段审查（顺序不可换）

每个任务实现后，**强制两阶段审查**：

### 阶段 A：spec 合规审查（先做）

dispatch 一个 spec reviewer 子代理，对照 plan/需求检查：
- 代码是否**完全**满足规格的每条要求
- 有没有**多做的**（加了规格没要的）
- 有没有**漏做的**

spec reviewer 发现问题 → implementer 子代理修 → re-review，直到 ✅ 才进阶段 B。

### 阶段 B：代码质量审查（spec 合规后才做）

dispatch 一个 code quality reviewer 子代理，拿 git SHA 检查：
- 测试覆盖（真实测试，非 mock 表演）
- 命名、错误处理、架构合理性
- 是否违反项目约定（与现有代码风格一致）
- 安全/性能问题

按严重性分级：Critical（阻塞）/ Important（修完再走）/ Minor（记下回头改）。

reviewer 发现 Critical/Important → implementer 修 → re-review，直到 ✅。

**为什么顺序不能换**：spec 不合规时审代码质量是浪费——可能整个方向就错了。先确认做对了，再确认做好了。

## 原则 4：返回后必须独立验证

子代理说"全部完成"不代表真的全部完成。

**最小验证清单：**
1. 读子代理修改的文件的关键部分
2. 检查命名风格是否与项目一致
3. 检查错误处理方式是否与项目约定一致
4. 运行全项目编译检查（`cargo check --workspace` / `npm run build` 等）
5. 对关键逻辑做实际验证（不只编译通过）

**claims → evidence**：子代理报"测试通过"→ 你独立跑一遍看输出。verification-before-completion 理念：声称前必有新鲜证据。

## 处理 implementer 状态

implementer 子代理报告 4 种状态，分别处理：

**DONE** → 进 spec 合规审查。

**DONE_WITH_CONCERNS** → 实现完了但有顾虑。**先读顾虑**。若是正确性/范围顾虑，审查前先处理；若是观察（"这文件变大了"），记下进审查。

**NEEDS_CONTEXT** → 缺了信息。补上缺的上下文，重新 dispatch。

**BLOCKED** → 无法完成。评估卡点：
1. 上下文问题 → 多给上下文，同模型重 dispatch
2. 需要更多推理 → 换更强模型重 dispatch
3. 任务太大 → 拆小
4. plan 本身错了 → 上报用户

**绝不**忽略上报、或不做任何改变就强制同模型重试。子代理说卡住，就是有东西得变。

## 何时用子代理 vs 自己做

| 场景 | 建议 |
|------|------|
| 任务清晰、文件路径已知、修改范围明确 | 用子代理 |
| 需要多处并行修改 | 多个子代理并行 |
| 需要先理解代码才能决定怎么改 | 自己先探索，理解后再决定是否委托 |
| 改动涉及核心架构决策 | 自己做 |
| 只改几行代码 | 自己做，不值得委托开销 |

## 并行 dispatch 多个独立任务

2+ 个**无共享状态、无顺序依赖**的独立任务，并行 dispatch 多个 implementer 子代理。

**红线**：
- 同一批并行子代理**绝不能改同一批文件**（冲突）——并行只用于任务文件集互不相交
- 每个并行子代理 prompt 同样要自包含（context 各自给全）
- 并行子代理各自回齐后，逐个走两阶段审查

## 模型选择（省成本提速）

用每个角色**能胜任的最弱模型**：

- **机械实现**（孤立函数、清晰规格、1-2 文件）→ 快而便宜的模型
- **集成与判断**（多文件协调、模式匹配、调试）→ 标准模型
- **架构/设计/审查** → 最强可用模型

任务复杂度信号：
- 碰 1-2 文件 + 完整规格 → 便宜模型
- 碰多文件 + 集成关注 → 标准模型
- 需设计判断或广代码库理解 → 最强模型

## Red Flags

**绝不**：
- 在 main/master 上开始实现而无用户明确同意
- 跳过审查（spec 合规**或**代码质量，任一都不可跳）
- 带未修问题继续
- 并行 dispatch 改同一批文件的 implementer（冲突）
- 让子代理读 plan 文件（给完整文本而非让它读）
- 跳过场景设定上下文（子代理要知道任务处于整体哪里）
- 忽略子代理的提问（开工前回答清楚）
- spec 合规上接受"差不多"（reviewer 发现问题 = 没完成）
- 跳过 re-review（reviewer 发现问题 → implementer 修 → 再 review）
- 让 implementer 自审取代正式审查（两者都要）
- **spec 合规 ✅ 前开始代码质量审查**（顺序错）
- 任一审查有未决问题就进下一任务

## 示例工作流

```
你：我用子代理驱动开发执行这个 plan。

[读 plan 一次，提取所有任务全文+上下文，建 TodoWrite]

Task 1: Hook 安装脚本
[dispatch implementer，带任务全文+上下文]

Implementer: "开工前——hook 装在 user 级还是 system 级？"
你: "User 级（~/.pi/agent/extensions/）"
Implementer: "明白。实现中..." → 实现+测试+自审+提交，报 DONE

[dispatch spec reviewer]
Spec reviewer: ✅ spec 合规——需求全满足，无多余

[拿 git SHA，dispatch code quality reviewer]
Code reviewer: 优点：测试覆盖好、干净。问题：无。Approved.
[标记 Task 1 完成]

Task 2: 恢复模式
[dispatch implementer]
Implementer: [无问题，进行] → 报 DONE

[dispatch spec reviewer]
Spec reviewer: ❌ 问题：缺进度上报（规格要"每 100 项报告"）；多了 --json 标志（没要求）
[implementer 修]
Spec reviewer: ✅ 现在合规

[dispatch code quality reviewer]
Code reviewer: 问题(Important): 魔法数 100
[implementer 提 PROGRESS_INTERVAL 常量]
Code reviewer: ✅ Approved
[标记 Task 2 完成]
```

## 与其他 skill 的分工

- **tdd-cycle**：子代理实现时应遵循 TDD 循环
- **systematic-debugging**：子代理卡 bug 时遵循系统化调试
- **test-discipline**：审查子代理测试质量（防弱断言）
- 子代理也应知道 **evidence-based-proposal**（提方案前先验证）

## 参考

- Prompt 编写规范：[references/prompt-patterns.md](references/prompt-patterns.md)
- 两阶段审查模板：[references/review-templates.md](references/review-templates.md)
