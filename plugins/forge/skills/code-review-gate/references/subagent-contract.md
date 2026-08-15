# 子 agent 预设契约

## 目录

- [为什么必须独立上下文](#为什么必须独立上下文)
- [规模 → 1 还是 2](#规模-1-还是-2)
- [契约 1：`cheat-detector`（轨道 A — AI 作弊指纹）](#契约-1cheat-detector轨道-a-ai-作弊指纹)
- [契约 2：`eng-reviewer`（轨道 B — 传统工程规范）](#契约-2eng-reviewer轨道-b-传统工程规范)
- [输出 schema（两个契约共用）](#输出-schema两个契约共用)
- [派发方式](#派发方式)
- [约束（硬性）](#约束硬性)

code-review-gate 的双轨审查由**独立只读子 agent** 执行，而非主 agent 自审。本文件定义两个预设契约（`cheat-detector` / `eng-reviewer`）+ 共用输出 schema + 派发方式。

定位：子 agent 预设是 **skill 内部契约**，不是 Forge 分发层——不写进各 agent 的私有 agent 目录（如 `.claude/agents/`），而由主 agent 读本文件后用所在 agent 的子任务机制注入。这样保持 Forge 的多 agent 中立（Claude Code / codex / cursor / windsurf 各自派生，契约相同）。

## 为什么必须独立上下文

主 agent 刚写完代码，带着"它是对的"的确认偏误去审 = 自己批改自己的作业。最危险的单行作弊就在这盲区：

- `@ts-ignore` / `eslint-disable` 加上去时主 agent 知道"这是临时绕过"，自审会放过
- 空 catch 是主 agent 自己写的"先这样"，自审觉得"以后再补"
- 删掉的断言是主 agent 为了让测试过删的，自审不会觉得自己在作弊

独立子 agent **看不到主 agent 的实现理由**，只看到 diff——它只问"这行代码本身是否构成作弊/缺陷"。这是规模无关的底线：**哪怕改一行也派 1 个独立子 agent**。

> ⚠️ **"独立上下文"不是"只读 diff 行"**。独立指不继承主 agent 的实现理由/对话历史（防确认偏误），**不是**把审查局限在 diff 的 +/- 行。改名 / 删除 / 改签名的符号，子 agent 必须用 `Grep`/`Glob` 主动查全仓调用方（**含 gitignored 文件**）+ 数据流下游——调用方常在 diff 之外（其他文件、生成的代码、配置、docs 示例）。只看 diff 行 = 把审查降级成"这行字符对不对"，丢掉"改动是否真的有效、波及面是否都更新了"。

## 规模 → 1 还是 2

规模只决定并行数，不决定是否派子 agent：

| 变更规模 | 派几个 | 怎么分 |
|---|---|---|
| 小（<100 行 / 单文件） | 1 个 | 一个子 agent 跑双轨 A+B |
| 大（≥100 行 / 多文件） | 2 个并行 | `cheat-detector`（A）+ `eng-reviewer`（B），独立上下文 + 交叉验证 |

大变更拆两个是因为：单一上下文同时盯 11 类指纹 + 8 个维度会顾此失彼；并行专精 + 交叉去重比一个子 agent 全包信号更强。

---

## 契约 1：`cheat-detector`（轨道 A — AI 作弊指纹）

- **职责**：只扫 11 类 AI 作弊指纹（详见 [ai-cheat-patterns.md](ai-cheat-patterns.md)）。命中任一即必须解决，这是最高信号。
- **工具**：`Read` / `Grep` / `Glob`（只读，**禁止 Edit/Write/Bash 写操作**）
- **不看**：设计模式、可读性、命名、性能——这些归 `eng-reviewer`，本契约聚焦作弊
- **重点扫**：diff 的删除行（`-` 侧）、新增的 ignore/disable、改名的符号是否更新了所有调用点（**必须 `Grep` 全仓 `oldName` 含 gitignored 文件**——调用方常在 diff 之外，diff 里没改 ≠ 没漏改）、断言数量净变化、空 catch
- **输入**：主 agent 注入 ① diff 范围（`git diff` 输出或文件路径）+ ② **改动符号清单**（被改名/删除/改签名的 export·函数·类型·常量 old→new）。子 agent 据 ② 主动 `Grep` 全仓调用点（含 gitignored），不只扫 diff 行。
- **输出**：见下方 schema，`category` 取 11 类之一

## 契约 2：`eng-reviewer`（轨道 B — 传统工程规范）

- **职责**：按 8 维度审查（详见 [review-checklist.md](review-checklist.md)）：正确性 & 逻辑 / 错误处理 / 可维护性（SRP·DRY·YAGNI）/ 设计模式 / 安全 / 性能 / 测试有效性 / 可读性。
- **工具**：`Read` / `Grep` / `Glob`（只读）
- **不看**：AI 作弊指纹（那是 `cheat-detector` 的职责，避免重复）
- **重点**：跨层数据类型一致性、错误路径（非快乐路径）、依赖真实性、安全反模式（硬编码密钥/SQL 拼接/缺输入验证/缺限流）、破坏性 SQL（DROP/TRUNCATE/无 WHERE DELETE/GRANT ALL/生产直连/不可逆迁移，见 [sql-safety-checklist.md](sql-safety-checklist.md)）、过度工程（重造轮子/单实现抽象/引依赖做几行事/投机灵活性/死代码）——用 delete-list 格式给可删清单 + 替代方案，结束给 `net: -N lines possible`，见 [over-engineering-checklist.md](over-engineering-checklist.md)
- **输入**：主 agent 注入 diff 范围 + 改动符号清单（同 cheat-detector）。eng-reviewer 额外 `Grep` 数据流下游（DB → struct → API → JWT claims 跨层类型一致性，见 [review-checklist.md](review-checklist.md)）。
- **输出**：见下方 schema，`category` 取 8 维度之一

---

## 输出 schema（两个契约共用）

子 agent 必须以下面的 JSON 结构返回（主 agent 据此合并去重、生成发现清单）：

```json
{
  "findings": [
    {
      "file": "path/to/file.go",
      "line": 42,
      "category": "assertion-strip | error-swallow | no-op-fix | fake-refactor | coverage-erosion | test-relaxation | type-suppression | mock-of-hallucination | comment-only-fix | exception-rethrow-lost-context | dead-branch-insertion | correctness | error-handling | maintainability | design | security | performance | test-validity | readability",
      "problem": "具体问题（引用代码片段）",
      "why": "为什么是问题（结合上下文背景分析，不是贴标签）",
      "resolution": "解决方向（修复，或结合背景论证为何不需修）"
    }
  ],
  "summary": "一句话结论"
}
```

- `category`：`cheat-detector` 只用前 11 个作弊类；`eng-reviewer` 只用后 8 个维度类——两者正交，便于主 agent 交叉去重
- 每条 finding 必须含 **位置 + 问题 + 背景分析 + 解决方向** 四要素，缺一不可（**不分级**：无 severity / verdict 字段——所有发现一视同仁，都必须解决）

---

## 派发方式

主 agent 读本契约后，按所在 agent 的机制注入 prompt：

**Claude Code**（Task tool）：
```
Task(subagent_type="general-purpose", prompt="<契约职责> + <schema> + <diff 范围> + <改动符号清单>")
```
prompt 里注入：本契约的「职责/工具/不看/重点」+ 输出 schema + 当前 diff + **改动符号清单**（改名/删除/改签名的符号 old→new，子 agent 据此 Grep 全仓调用点含 gitignored）。不预设 `.claude/agents/*.md`（那是 Claude Code 私有，Forge 不污染）。

**codex / cursor / windsurf**：用各自子任务/子 agent 机制，prompt 注入相同契约。Forge 的多 agent hook 配置已就绪（见 agentbridge），审查触发点（Stop hook / task-complete 门禁）跨 agent 一致。

**通用**：任何支持"派只读子任务 + 结构化输出"的 agent 都可套用本契约——这正是契约独立于 agent 私有目录的原因。

---

## 约束（硬性）

1. **只读**：子 agent 禁止修改代码。审查与修复分离——主 agent 拿到 findings 后自己改，避免子 agent "边审边改"妥协。
2. **独立上下文**：子 agent 不继承主 agent 的实现理由/对话历史（防确认偏误——这是本机制存在的全部价值），但**必须主动用 `Grep`/`Glob` 查波及面**：拿到改动符号清单后 grep 全仓调用方/下游。"独立"≠"只读 diff 行"——只读 diff 行会把改名漏改调用方的致命 bug 当成"diff 里看着对"放行。
3. **结构化输出**：必须返回 schema JSON，不返回散文。主 agent 靠 JSON 合并去重、生成发现清单。
4. **不越界**：`cheat-detector` 不评设计、`eng-reviewer` 不扫作弊指纹——各司其职，主 agent 合并时交叉去重。

审查闭环：子 agent 返回 → 主 agent 合并去重 + 生成发现清单 → 逐项解决（修复或结合背景论证不需修） → **全部解决后运行 `forge review pass`**（满足 task-complete 门禁前置 / 解除 Stop hook 拦截）。
