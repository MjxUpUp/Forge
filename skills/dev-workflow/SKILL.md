---
name: dev-workflow
description: "开发工作流编排：Plan Mode 先规划后执行。Use when: 开始新功能、规划开发阶段、用户需要指导下一步、项目初始化检测技术栈、用户说\"plan mode\"\"/goal\"\"出计划\"\"先规划再写代码\"时。SKIP: 已有明确实现计划直接执行（用 agent-delegation）、只需调试（用 systematic-debugging）、提技术方案（用 evidence-based-proposal）、执行期纪律自检（用 implementation-discipline）、单步小改（直接做，不需要编排）、用户已明确说了'做X'且X简单直接（不触发编排）。"
metadata:
  pattern: inversion + pipeline
  domain: development-process
  composes: agent-delegation, systematic-debugging, tdd-cycle, test-discipline, evidence-based-proposal, session-continuity, architecture-decision-record
---

# 开发工作流 — Plan Mode 先规划后执行

从创意到交付的编排器。**核心纪律：Plan Mode 是强制 gate——先出详细计划，再动手。不规划就不执行。**

**"不测试、不验证，就不能进入下一步。"**

## 初始化 — 项目检测

首次在项目中使用时，检测项目技术栈。检查以下文件确定语言和工具链：

| 检测文件 | 技术栈 | 测试命令 | 检查命令 | 构建命令 |
|---------|--------|---------|---------|---------|
| `Cargo.toml` | Rust | `cargo test` | `cargo clippy -- -D warnings` | `cargo build` |
| `package.json` | Node/TS | `npm test` 或 `pnpm test` | `npm run lint` | `npm run build` 或 `pnpm build` |
| `go.mod` | Go | `go test ./...` | `golangci-lint run` | `go build ./...` |
| `pyproject.toml` / `requirements.txt` | Python | `pytest` | `ruff check .` 或 `flake8` | — |
| `pom.xml` / `build.gradle` | Java/Kotlin | `mvn test` 或 `gradle test` | `mvn verify` | `mvn package` |
| `*.sln` / `*.csproj` | C#/.NET | `dotnet test` | `dotnet format --verify-no-changes` | `dotnet build` |

**混合项目**：检测所有层，每层独立验证。检测结果记住用于后续阶段。

---

## 阶段 1 — 需求澄清（Inversion Gate）

**⛔ 不确定就必须问用户，不能自己判定。不要猜用户意图。Must Gate。**

**⛔ 防"太简单不需要澄清"**：简单项目也要过澄清，可以短（一句话规格），但不跳。未澄清就开工 = 违规。"太简单"是最常见的偷懒借口。

1. 理解用户真正想要什么："这解决什么问题？"
2. 如果任务范围模糊，问："多大的工作量？预期结果是什么？"
3. 对照下方路由表判断工作类型
4. **向用户确认**："我的理解是 X，准备进 Plan Mode 出详细计划，可以吗？"

| 工作类型 | 判定条件 | 后续路径 |
|---------|---------|---------|
| 新功能/重大变更（多文件/跨会话/需设计） | 用户说"新功能""做X功能"，X 涉及多步骤 | → 阶段 2 Plan Mode |
| 架构决策 | 涉及技术选型 | → `architecture-decision-record` |
| 提技术方案 | 用户说"方案""怎么设计" | → `evidence-based-proposal` |
| Bug 修复 | 用户说"修bug""这个报错" | → `systematic-debugging` |
| 延续之前的工作 | 用户说"继续""上次那个" | → `session-continuity` |
| **以下不触发本 skill** | 用户已明确指令（"改X行""加Y字段"） | **不走 dev-workflow** |

**⛔ 不确定属于哪类 → 回问用户。不要猜。** 确认后进入阶段 2。

---

## 阶段 2 — Plan Mode（强制规划 Gate）

**⛔ 不产出计划就不执行。Plan Mode 是强制 gate，不是建议。**

对于需要 Plan Mode 的任务（新功能/重大变更），**必须按以下 4 个维度产出详细计划，每个维度有验收标准**：

### 2.1 代码考古（理解现状）

**目标**：搞清楚现有代码的布局，避免闭门造车、重复实现。

| 考古项目 | 怎么做 | 验收标准 |
|---------|--------|----------|
| 相关模块定位 | `grep` 关键词定位已有文件/函数/类型；读关键文件的头部和类型定义 | ☐ 列出所有相关文件路径 + 一句话说明每个做什么 |
| 可复用资产 | 搜索现有类似功能的实现、已有 util/helper、项目中是否已有同类模式 | ☐ 明确标出哪些可以直接复用、哪些需要修改后复用 |
| 约束与依赖 | 检查 Cargo.toml/package.json 的已有依赖、现有 trait 接口、数据库 schema | ☐ 列出 3 项关键约束（依赖版本/接口签名/数据模型限制） |
| 历史踩坑 | `git log --oneline -20 <相关文件>` 查近期改动，看是否有回滚/反复修改 | ☐ 如果有反复修改的记录，标注为高风险区 |

### 2.2 技术设计

**目标**：给出清晰到能直接编码的设计，不是模糊方向。

| 设计项目 | 做什么 | 验收标准 |
|---------|--------|----------|
| 模块划分 | 新功能放哪里？新增几个文件？每个文件职责一句话 | ☐ 每个文件有明确职责、不超过 1 句话 |
| 数据流 | 数据从哪进、经过哪些层、到哪出？画简单流程 | ☐ 至少覆盖 3 层（入口→处理→输出） |
| 接口定义 | 函数签名/public API/trait 定义，含参数和返回值类型 | ☐ 每个接口有签名、有一个使用示例 |
| 错误处理策略 | 怎么处理错误？用 Result/Option？panic？自定义 Error 类型？ | ☐ 每个关键函数标注了错误处理方式 |
| 与现有代码的接触面 | 在哪里调用新代码？改动哪些已有文件？ | ☐ 每个已有文件的改动范围 ≤ 10 行 |

### 2.3 测试策略

**目标**：不是"写测试"三个字，而是"测什么、怎么测、怎么算过"。

| 测试项目 | 做什么 | 验收标准 |
|---------|--------|----------|
| 单元测试 | 测哪些函数/方法？核心逻辑路径 | ☐ 每个 public 函数有 1+ 个测试用例 |
| 集成测试 | 测哪些跨模块链路？需要启动什么服务？ | ☐ 关键端到端链路 1 条有测试 |
| 边界 case | 空输入、最大/最小值、权限拒绝、并发冲突 | ☐ 至少 3 个边界 case 有测试 |
| 回归保护 | 改动会不会破坏已有功能？怎么验证？ | ☐ 全量测试套件通过（不能只跑新增的） |

### 2.4 代码评审标准

**目标**：写代码前先定"什么样的代码算合格"，不是为了审查而审查。

| 评审维度 | 检查什么 | 验收标准 |
|---------|---------|----------|
| 命名与风格 | 是否与项目已有命名一致？是否有魔法数字？ | ☐ 0 个魔法数字（全部提取为常量）、命名与项目 convention 一致 |
| 错误处理 | Error 是否被无声吞掉？`unwrap`/`expect` 是否有理由？ | ☐ 0 个未注释的 `unwrap`/`expect` |
| 测试质量 | 断言是真测试还是 mock 表演？是否测了行为而非实现？ | ☐ 参考 `test-discipline`：0 个弱断言 |
| 安全性 | 是否有注入/越权/密钥泄露？ | ☐ 0 个 security 类 Critical 问题 |
| 性能 | 是否有 N+1 查询/不必要的大循环/未释放资源？ | ☐ 关键路径时间复杂度 ≤ O(n log n) |

### 2.5 实现计划输出

把以上 4 个维度的结论，**压缩成 TodoWrite 可执行的 3-8 步**，每步包含：

```
Task N: [任务名]
- 涉及文件: [路径]
- 做什么: [一句话]
- 依赖: [前序 Task ID]
- 验收: 具体命令 + 预期输出（见下方格式）
- 测试: [对应 2.3 的哪个测试用例]
```

**验收标准格式（必须含 Run + Expected）**：
```
Run: cargo test --test integration
Expected: PASS
```
不允许写"编译通过""测试 OK"——必须具体命令 + 具体预期输出。

**forge 项目把验收标准变成不可伪造的实跑证据**：`forge task start` 时用 `--accept "Run :: Expected"`（可重复）把每条验收标准持久化进任务，`forge task verify-acceptance` 实跑每条 Run、比对 Expected、回填结果并记 `checklog:acceptance`（deterministic——forge 自己跑看结果，不可伪造）。本格式直接对应 `--accept` 的 `Run :: Expected` 串：上方例子 → `--accept "cargo test --test integration :: PASS"`。把 plan 里的验收标准从"文本里飘着"变成"实跑留痕的 spec-as-gate"，对冲 agent 自述"满足验收"却没真跑的盲区。

**⛔ No Placeholders 禁令表**：
以下模式 **出现即不合格**，计划被打回重写：
- "TBD" / "TODO" / "implement later" / "稍后补"
- "add appropriate error handling" / "add validation" / "加错误处理"（无具体代码）
- "Write tests for the above"（不给具体测试代码）
- "Similar to Task N" / "和 Task N 类似"（必须重复完整代码——读者可能跳着读）
- 只说"做什么"不给代码/命令的 step
- 引用未在任何 Task 里定义的类型/函数/方法

**🤖 Self-Review（计划产出后，自己过 3 项再呈现给用户）**：
1. **Spec 覆盖度**：阶段 1 确认的需求/范围，每个都能点到具体 Task 吗？漏的补。
2. **Placeholder 扫描**：搜以上禁令表的模式，有的改。
3. **类型一致性**：Task 3 的函数签名在 Task 7 里对得上吗？名字/参数/返回值要跨 Task 一致。

**计划产出必须包含以上 5 个字段 + 过 Self-Review，缺任何一项不得进入阶段 3。**

**门控：计划完整且用户确认后，才进入阶段 3。**

---

## 阶段 3 — 执行

根据阶段 2 的计划执行。路线取决于任务复杂度：

**重大功能（阶段 2 产出完整计划）**：
1. 按 TodoWrite 步骤执行，每步完成后跑对应测试（用 2.5 里的 Run/Expected 命令）
2. `agent-delegation` 子代理驱动实现（含两阶段审查：spec 合规 + 代码质量），**审查不通过不进下一个 Task**
3. 实现遵循 `tdd-cycle`（RED-GREEN-REFACTOR），测试质量用 `test-discipline` 守卫
4. 对照阶段 2.4 的代码评审标准自查
5. 交付前跑 `verification-driver` 做端到端验证

**小型变更（用户已明确具体改动）**：
1. 直接用 TodoWrite 拆分，手动执行
2. 每步完成后跑测试
3. 全部完成后跑完整验证

**⛔ 遇阻即停纪律（RED FLAGS）**：
看到这些信号 → **STOP 问用户，不猜、不硬试**：
- 验证反复失败（同一测试 fail 3+ 次）→ STOP，走 `systematic-debugging`，不堆叠修复
- 遇到 blocker（缺依赖/测试框架没装/指令不清）→ STOP 问用户
- 计划有根本缺口（Task 之间依赖断裂/Task 的代码无法编译）→ STOP 回阶段 2.5 重审计划
- 不清楚某个 Task 怎么做 → STOP 问用户，**不自己脑补**
- 即使用户催"快做"，遇阻也不硬试——乱试的代价 > 停下来问的代价

---

## 阶段 4 — 接力

交付后：
- 记录后续任务和已知问题
- 如分阶段开发，记录下一阶段
- 下次会话使用 `session-continuity` 衔接

---

## Gotchas（高信号）

- **没出 Plan 就开工 = 违规**：阶段 2 是强制 gate。即使用户催"快做"，也要先出概要计划再动手。只有阶段 1 判定为"小型变更/已明确指令"才可跳过
- **代码考古不是走过场**：不考古就设计 = 闭门造车。不读现有代码就写新代码 = 重复造轮子或破坏已有模式
- **验收标准不具体 = 计划不合格**：每个验收标准必须是可判定的（"编译通过"/"curl 返回 201"/"全部 test pass"），不能是"看起来没问题""应该 OK"
- **计划太粗 = 没规划**：3-8 步是硬约束——如果拆不出来 3 步说明不熟悉代码，回阶段 2.1 做更多考古；如果超过 8 步说明任务粒度太大，拆成小任务或考虑分阶段交付
- **混合项目逐层验证**：Rust 后端通过不代表 Vue 前端通过，每层独立验证
- **"应该通过"不是验证**：必须实际运行命令并看到输出。声称前必有新鲜证据

## 参考

- 模式指南：[references/pattern-guide.md](references/pattern-guide.md)
- 验证策略：[references/verification-strategy.md](references/verification-strategy.md)
- 关联 skill：`agent-delegation`（两阶段审查执行）、`tdd-cycle`（TDD 循环）、`test-discipline`（测试质量）、`verification-driver`（端到端验证）、`systematic-debugging`（调试）
