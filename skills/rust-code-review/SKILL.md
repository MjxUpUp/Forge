---
name: rust-code-review
description: "Rust 代码结构化审查。Use when: 审查 Rust 代码 PR 时、检查 Rust 代码变更时、做合并前检查时、用户要求 Rust code review 时。特别关注异步 Rust、unsafe 代码和多 crate workspace 模式。SKIP: 非 Rust 代码（用内置 code-review）、只需要快速检查时（用内置 code-review --low）、只做格式化或 lint 建议时。"
metadata:
  pattern: reviewer
  domain: code-review
  severity-levels: error,warning,info
---

# Rust 代码审查

为 Rust 项目提供结构化代码审查。加载审查清单，应用于代码，产出按严重性分级的发现。

## 流程

### 步骤 1：确定范围

确定审查什么：
- 如果提供了 PR/diff → 只审查变更的文件
- 如果提供了文件路径 → 审查该文件
- 如果"审查全部" → 聚焦自上次提交以来修改的 `src/` 文件

### 步骤 2：加载清单

加载 [references/review-checklist.md](references/review-checklist.md) 获取完整审查清单。

### 步骤 3：应用清单

对每个被审查的文件，检查每条适用规则。对每个发现：

- **行号**：问题所在位置
- **严重性**：`error`（合并前必须修复）、`warning`（应当修复）、`info`（建议考虑）
- **规则**：违反了清单的哪一项
- **原因**：解释**为什么**是问题，不只是说**什么**有问题
- **修复**：给出具体的修正代码

### 步骤 3.5：严重性升级规则（不能凭感觉定级）

以下命中条件**直接定 error**，不需权衡（合并前必须修）：

| 命中条件 | 严重性 | 理由 |
|---|---|---|
| `unsafe` 块无 `// SAFETY:` 注释 | error | unsafe 是安全合约，无注释 = 无法审查 |
| `.unwrap()` / `.expect()` 在非测试代码且错误路径不可控 | error | 生产环境 panic 风险 |
| `.clone()` 在热路径（循环内 / async 内 / 高频调用）| error | 性能 + 设计问题 |
| 跨 `.await` 持有 `MutexGuard` / 非 Send 的引用 | error | 编译可能过但运行时死锁/UB |
| 错误被静默吞掉（`let _ = result;` / `.ok();` 无日志）| error | 隐藏故障 |
| 测试断言弱化（`assert!(result.is_ok())` 不查值 / `t.Fatal→t.Log`）| error | 虚假测试信心 |
| 无界 channel / 集合增长无上限 | error | 内存爆炸风险 |
| 硬编码密钥 / 密码 / token | error | 安全漏洞 |
| 命令注入（`Command::new(user_input)`）| error | 安全漏洞 |

以下定为 **warning**（应修不阻断）：函数圈复杂度 >10、重复代码块、命名不清、缺文档注释、未处理的 `Result`（但已传播）、过度抽象。

以下定为 **info**（建议）：风格偏好、可简化的语法、非关键性能优化。

**门控**：发现上述 error 条件直接定 error，不许"降级为 warning 因为……"。rationalization 见下方表。

### 步骤 4：产出结构化审查

```markdown
## 代码审查摘要

**审查文件数**：N
**总发现数**：N（X 个 error、Y 个 warning、Z 个 info）

### Error（必须修复）
1. `file.rs:42` — [描述] — [修复]

### Warning（应当修复）
1. `file.rs:88` — [描述] — [修复]

### Info（建议考虑）
1. `file.rs:15` — [描述] — [建议]

### 评分：N/10
[评分理由]

### 优先改进建议 Top 3
1. [最有影响力的改进]
2. [第二有影响力的]
3. [第三有影响力的]
```

### 步骤 5：迭代

如果用户修复了问题，只重新审查变更部分。

**变更界定（不凭记忆）**：

```bash
# 有 PR/分支：对比目标分支
git diff origin/main...HEAD --name-only -- '*.rs'

# 本地未提交：看工作区 + 暂存区
git diff HEAD --name-only -- '*.rs'

# 只看本次审查后用户改的：上次审查时记 git SHA，现 SHA 对比
git diff <上次审查时的 SHA> --name-only -- '*.rs'
```

**追踪发现解决状态**：上次审查的每个发现记行号 + 规则，重审时逐个核对：
- 已修：从发现列表移除
- 未修：保留并升级严重性（用户选择不修需明示说明理由）
- 新增：本次 diff 引入的新问题

**门控**：迭代不能凭"看一眼差不多"——必须拿 `git diff --name-only` 明确文件清单，对每个变更文件重跑清单。

## 易错点

- **不要在审查中纠结风格**：如果有 `rustfmt` 或 clippy 规则能处理，那就不是审查发现——是工具配置问题。
- **检查测试质量，不只是覆盖率**：通过但不断言有意义行为的测试比没有测试更糟（虚假信心）。
- **异步 Rust 需要额外审查**：查找缺失的 `.await`、跨 `.await` 持有的 `MutexGuard`、无界 channel 增长。
- **`unsafe` 不是天生坏的**：但每个 `unsafe` 块需要 `// SAFETY:` 注释解释为什么是安全的。
- **审查错误处理路径**：正常路径通常能工作。审查出错时会发生什么——是否记了日志？传播了？还是被静默吞掉了？

## Common Rationalizations（塔借口）

| 借口 | 现实 |
|---|---|
| "unsafe 这里应该没问题" | 无 `// SAFETY:` 注释 = 无法审查，直接 error；有注释但理由不充分也是 error |
| "`.unwrap()` 这个场景不会 panic" | 生产代码不靠"应该不会"；用 `?` 或明确处理 |
| "这个 error 我降成 warning 吧" | 命中步骤 3.5 升级规则的不得降级；降级需明示理由并记在审查报告 |
| "迭代看一眼差不多" | 凭眼看会漏；`git diff --name-only` 拿文件清单逐个重跑清单 |
| "跨 await 持锁应该不会死锁" | 编译可能过但运行时 UB；跨 await 持 MutexGuard 直接 error |
| "测试过了就行不管断言" | 弱断言测试比没测试更坏（虚假信心）；断言弱化直接 error |

## 参考

- 完整清单：[references/review-checklist.md](references/review-checklist.md)
- 测试断言守卫：**test-discipline**（防 t.Fatal→t.Log 等弱化）
- 通用 AI 作弊指纹：**code-review-gate** 轨道 A
