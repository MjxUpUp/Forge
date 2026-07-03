---
name: skill-authoring-standard
description: "Skill 编写规范。Use when: 创建新 skill 时、修改现有 skill 时、编写 skill 的 description 字段时、组织 skill 目录结构时、验证 skill 质量时。SKIP: 不涉及 skill 文件编写的工作。"
metadata:
  pattern: tool-wrapper
  domain: skill-authoring
---

# Skill 编写规范

基于 Anthropic Agent Skills 官方标准 + Google 5 种设计模式 + TDD 创作法 + 实际编写经验。

## Frontmatter 规范

```yaml
---
name: kebab-case-name        # 必须与目录名一致，^[a-z][a-z0-9-]*$
description: "≥80字符的触发器描述  # 见下方 description 规范"
metadata:
  pattern: <pattern-name>    # 可选：pipeline / reviewer / gate / tool-wrapper / inversion
  domain: <domain-name>      # 可选：领域标签
  steps: <number>            # 可选：步骤数
  composes: <skill-list>     # 可选：组合的其他 skill（本地 skill 名，勿引外部 plugin: 命名空间）
---
```

### name 规则

- 必须是 kebab-case：`my-skill-name`
- 必须与目录名（即 skill id）一致
- 用动名词或核心洞察命名：✅ `condition-based-waiting` ❌ `async-test-helpers`；✅ `systematic-debugging` ❌ `debug-techniques`

## description 规范（最关键字段，也是最容易写错的）

description 是**触发器**，不是摘要。模型用它判断是否加载这个 skill。

### CSO 反直觉铁律：description 绝不能总结 workflow

**实测发现**：description 若总结了 skill 的流程，模型会**跟着 description 走、跳过 skill 正文**。

> 实测案例：description 写 "code review between tasks"，模型只做 **1 次**审查，即便正文流程图明确要求 **2 次**（spec 合规 + 代码质量）。改为只写触发条件 "Use when executing implementation plans with independent tasks" 后，模型才读完整正文并执行两阶段审查。

**陷阱**：总结 workflow 的 description 创造了模型会走的捷径，skill 正文成了被跳过的文档。

```yaml
# ❌ 错：总结了 workflow——模型可能照此跳过正文
description: Use when executing plans - dispatches subagent per task with code review between tasks

# ❌ 错：太多流程细节
description: Use for TDD - write test first, watch it fail, write minimal code, refactor

# ✅ 对：只写触发条件，不写 workflow
description: Use when executing implementation plans with independent tasks in the current session

# ✅ 对：只写触发条件
description: Use when implementing any feature or bugfix, before writing implementation code
```

### 必须包含

- `Use when:` + 触发场景列表（用用户会说的话，含错误信息/症状/工具名关键词）
- `SKIP:` + 排除场景列表（提供 skill 间互斥，指向正确替代）

### 内容原则

- 宁可多触发，不要少触发（"pushy" style）

### Pushy 原则（anthropics/skill-creator 官方依据）

Anthropic 官方在 skill-creator 中明确指出：**Claude 倾向于 undertrigger skill（该用时不用）**。对抗方法是 description 写得积极：

> 官方原话："Make the skill descriptions a little bit 'pushy'."

**Pushy 的具体手法**——列举用户可能说的近义表达，即使用户没明说该 skill 名字：

```yaml
# ❌ 不 pushy（undertrigger 风险）
description: How to build a dashboard.

# ✅ pushy（积极列举触发场景）
description: How to build a dashboard. Make sure to use this skill whenever the user mentions dashboards, data visualization, metrics, or wants to display any kind of data, even if they don't explicitly ask for a 'dashboard.'
```

**和 CSO 铁律的关系（互补不冲突）**：
- CSO 铁律管"不能写什么"：不总结 workflow
- Pushy 管"应该怎么写"：积极列举触发场景防 undertrigger
- 合起来：**积极列举触发场景，但不总结 workflow**

### 其他内容原则

- ≥ 80 字符
- 关键词覆盖：错误信息（"E0432"、"race condition"）、症状（"flaky"、"hanging"）、工具名——模型会搜这些词
- 第三人称（注入 system prompt）
- 用描述问题的词，不用技术 jargon（除非 skill 本身技术特定）
- **绝不总结 skill 的流程或 workflow**

**差：** `帮助管理 Rust trait`
**好：** `Rust trait 适配器模式。Use when: 为已有类型实现外部 trait 时、创建 newtype wrapper 时、遇到孤儿规则冲突时。SKIP: 定义新 trait（直接定义即可）、纯数据结构设计。`

## 写 skill = TDD（最重要的创作理念）

**写 skill IS Test-Driven Development applied to process documentation.**

写测试用例（带子代理的压力场景）→ 看它失败（无 skill 的基线行为）→ 写 skill（文档）→ 看测试通过（子代理遵守）→ 重构（堵漏洞）。

**核心原则：没看过子代理在无 skill 时失败，你不知道这 skill 教得对不对。**

### TDD 映射

| TDD 概念 | Skill 创作 |
|---|---|
| 测试用例 | 带子代理的压力场景 |
| 生产代码 | skill 文档（SKILL.md） |
| 测试失败（RED） | 无 skill 时子代理违规（基线） |
| 测试通过（GREEN） | 有 skill 时子代理遵守 |
| 重构 | 堵漏洞同时保持遵守 |
| 先写测试 | 写 skill **前**先跑基线场景 |
| 看它失败 | 记录子代理用的确切 rationalization |
| 最小代码 | 写针对那些违规的 skill |

**Iron Law：没有先失败测试就不写 skill**（对新建和**编辑**都适用）。先写 skill 再测？删掉重来。编辑 skill 不测？同样违规。

### 四类 skill 的测试法

| 类型 | 例子 | 测试法 | 成功标准 |
|---|---|---|---|
| 纪律强制型 | TDD、systematic-debugging | 压力场景（时间+沉没成本+疲惫多重施压），记 rationalization 加显式反驳 | 最大压力下仍遵守 |
| 技术型 | root-cause-tracing、委托 prompt | 应用场景（能正确应用技术）+ 变体（处理边界）+ 缺信息测试 | 正确应用到新场景 |
| 模式型 | 减复杂度、信息隐藏 | 识别场景（认出何时适用）+ 应用 + 反例（知道何时不适用） | 正确识别何时/如何用 |
| 参考型 | API 文档、命令参考 | 检索场景（能找到对的信息）+ 应用 + 缺口测试（常见用例覆盖） | 找到并正确应用参考信息 |

## 内容组织

### 渐进式加载

```
metadata（~100 词，始终加载）
  → SKILL.md body（< 500 行，按需加载）
    → bundled resources（按需读取 references/）
```

SKILL.md 超过 500 行时，把详细内容拆到 `references/`。

### Token 效率（频繁加载的 skill 尤其关键）

problem：getting-started 和频繁引用的 skill 加载进**每个**会话，每个 token 都算数。

目标字数：
- getting-started 流程：<150 词
- 频繁加载 skill：<200 词
- 其他 skill：<500 词（仍要精炼）

手法：
- 详细移到 `--help` 或 references，正文用 "详见 references/xxx.md"
- 用交叉引用而非重复（"用 [其他skill] 的流程" 而非复制 20 行）
- 压缩示例（一个优秀示例胜过多个平庸的）
- 消除冗余

### 内容结构

```markdown
# Skill 标题

一句话说明 + 核心原则。

## When to Use / When NOT to Use

## 核心步骤 / 规则
<主体内容>

## Common Rationalizations（纪律型 skill 必备——堵借口的最高信号）
| 借口 | 现实 |
|---|---|

## Red Flags（纪律型 skill 必备——"我在 rationalize 的想法"自检清单）

## 易错点（Gotchas）
<从实际失败中积累——最高信号>

## 与其他 skill 的分工
SKIP 指向 + 互补指向

## 参考
- [描述](references/file.md)
```

### Gotchas 是最高信号内容

Gotchas 比最佳实践更有价值，因为它来自实际失败。每个 gotcha：
- **问题**：什么情况下会出错
- **现象**：出错时看到什么
- **解决**：怎么避免或修复

## 目录结构

```
skill-name/
├── SKILL.md                    # 主文件，≤ 500 行
└── references/                 # 按需加载的详细内容
    ├── examples.md             # 示例
    ├── checklist.md            # 检查清单
    ├── gotchas.md              # 详细易错点
    └── ...
```

**自包含 skill**：所有内容内联（无 references）
**带可复用工具**：SKILL.md + 可执行脚本/模板
**带重型参考**：SKILL.md 概述 + references/*.md（API 文档、600+ 行参考）

### 单文件 vs 目录

- 整个内容能装下、无重型参考 → 单 SKILL.md
- 有可复用工具代码或 >500 行参考 → 目录 + references

## 设计模式

| 模式 | 用途 | 特征 |
|------|------|------|
| `pipeline` | 逐步执行流程 | 有明确步骤，每步有门控 |
| `reviewer` | 质量检查 | 检查清单，按严重性分级 |
| `gate` | 决策门控 | 必须满足条件才能继续 |
| `tool-wrapper` | 领域知识封装 | 易错点 + 模式 + 参考 |
| `inversion` | 先问后做 | 以向用户提问开始 |
| `routing + fallback` | 按类型路由 + 降级链 | 路由表 + 失败终止点 |

## 交叉引用其他 skill

用 skill 名 + 明确要求标记：
- ✅ `**REQUIRED: 必须用 systematic-debugging 跑 Phase 1**`
- ✅ `互补：test-discipline 管测试质量，本 skill 管测试顺序`
- ❌ `See skills/testing/test-discipline/SKILL.md`（不清楚是否必需）
- ❌ `@skills/...`（force-load，烧上下文）

**勿引外部 plugin: 命名空间**（如 `some-plugin:skill-name`）——pi 等环境没装对应 plugin 会悬空。引用本地 skill 名（如 systematic-debugging、tdd-cycle）。

## 验证（新建/修改后）

- [ ] name 与目录名一致且 kebab-case
- [ ] description ≥ 80 字符且包含 Use when + SKIP
- [ ] **description 只写触发条件，不总结 workflow**（CSO 铁律）
- [ ] SKILL.md ≤ 500 行（超了拆 references）
- [ ] 有易错点（Gotchas）部分
- [ ] 纪律型 skill 有 Red Flags + Rationalization 表
- [ ] 超长详细内容拆到了 references/
- [ ] 跑过 `forge skills validate` + `forge skills audit` 校验（R1-R9 规范 + 19 条安全规则，forge 自带，无需外部脚本）
- [ ] **TDD：跑过基线测试**（无 skill 时子代理怎么失败），针对失败写的 skill
- [ ] **防注水：跑过 `skill-anti-degradation-check.sh`**（扫描弱措辞/弱门控/无命令 checklist）

## 防注水自检（避免 skill 写得比实际做法松）

新建或修改 skill 后，强制跑一次防注水扫描，检出三类"skill 声称有校验但无具体可执行方法"的注水：

| 类型 | 特征 | 示例 | 修复 |
|---|---|---|---|
| 弱校验措辞 | "人工校验/肉眼对比/大致核对/应该通过" | `生成后必人工校验` | 换成可执行命令 + 量化阈值（`跑 jscpd 重复率 >15% 警告`）|
| 门控无方法 | 有"门控/必跑/强制"关键字但附近无具体命令/工具 | `门控：导入后验证还原度` | 补命令（`用 get_node_dsl 读回，跑 snapshot-verify check`）|
| checklist 无命令 | `- [ ] 编译是否通过` 无配套命令 | `- [ ] 编译是否通过` | 每项配具体命令 + 通过标准（`cargo build --all-targets, exit 0`）|

### 命令

```bash
# 全仓扫描（CI / 提交前必跑）
bash references/skill-anti-degradation-check.sh

# 扫描单个 skill（开发时自检）
bash references/skill-anti-degradation-check.sh <skill-name>
```

### 退出码

- `0` = 干净，无注水点
- `1` = 发现可疑项，需逐项检查（脚本优先检出不漏检，少量 false positive 需 human review 最终判定）

### 已知 false positive 模式（脚本会标记但 human reviewer 判定为正向引用）

- Rationalization 表格中的反例引用（`| "xxx 应该通过" |`——这是在堵借口，不是注水）
- "核心原则/红线"段中作为反例的弱措辞（"**红线**：用'应该通过'代替实际运行"——这是在说不能用它）
- Inversion/Pipeline 门控（"阶段 0 确认前不要进阶段 1"——不需要工具）

脚本标记后 human reviewer 逐项判定，以上三类直接标"正向引用，跳过"即可。

### 脚本文件

[references/skill-anti-degradation-check.sh](references/skill-anti-degradation-check.sh) — 三类检测的完整实现，可直接在 CI 或 pre-commit hook 中调用。

## 参考

- Anthropic 官方 skill 规范：[references/anthropic-spec-notes.md](references/anthropic-spec-notes.md)
- CSO 实证发现：description A/B 测试
- 防注水扫描脚本：**references/skill-anti-degradation-check.sh**（本次 review 沉淀，与 TDD 基线测试同为 skill 质量门控）
- 真实注水案例（project-acceptance 维度 3 / code-review-gate 步骤 5）→ 本文件"防注水自检"段的修复手法对应
