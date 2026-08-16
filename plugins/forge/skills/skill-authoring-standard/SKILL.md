---
name: skill-authoring-standard
description: "Skill 编写规范。Use when: 创建新 skill 时、修改现有 skill 时、编写 skill 的 description 字段时、组织 skill 目录结构时、验证 skill 质量时。SKIP: skill 在实际任务中漏检/误触发的系统性改进与回归验证（用 skill-evolution）、会话经验沉淀的载体路由（用 session-retrospective）、skill 路由机制的配置与排查（用 skill-routing）、不涉及 skill 文件编写的工作。"
metadata:
  pattern: tool-wrapper
  domain: skill-engineering
  triggers: [{"event":"PreToolUse","match":"Write|Edit","when":"skill_file_touched","reason":"编辑 SKILL.md 时须遵守编写规范（description/结构/行数/决策记录）","cooldown":0}]
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
  domain: skill-engineering
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

```yaml
# ❌ 错：总结了 workflow——模型可能照此跳过正文
description: Use when executing plans - dispatches subagent per task with code review between tasks

# ✅ 对：只写触发条件，不写 workflow
description: Use when executing implementation plans with independent tasks in the current session
```

### 必须包含

- `Use when:` + 触发场景列表（用用户会说的话，含错误信息/症状/工具名关键词）
- `SKIP:` + 排除场景列表（提供 skill 间互斥，指向正确替代）

### Pushy 原则（anthropics/skill-creator 官方依据）

Anthropic 官方在 skill-creator 中明确指出：**Claude 倾向于 undertrigger skill（该用时不用）**。对抗方法是 description 写得积极：

> 官方原话："Make the skill descriptions a little bit 'pushy'."

**Pushy 的具体手法**——列举用户可能说的近义表达，即使用户没明说该 skill 名字：

```yaml
# ❌ 不 pushy：description: How to build a dashboard.
# ✅ pushy：description: How to build a dashboard. Make sure to use this skill whenever
#   the user mentions dashboards, data visualization, metrics, or wants to display any
#   kind of data, even if they don't explicitly ask for a 'dashboard.'
```

**和 CSO 铁律的关系（互补不冲突）**：CSO 管"不能写什么"（不总结 workflow），Pushy 管"应该怎么写"（积极列举触发场景）——合起来：**积极列举触发场景，但不总结 workflow**。

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

**四类 skill 的测试法**（纪律强制型/技术型/模式型/参考型各自的测试形态与成功标准）：见 [references/testing-patterns.md](references/testing-patterns.md)。

## 内容组织

### 渐进式加载

metadata（~100 词，始终加载）→ SKILL.md body（< 500 行，按需加载）→ bundled resources（references/ 按需读取）。SKILL.md 超过 500 行时，把详细内容拆到 `references/`。

### Token 效率（频繁加载的 skill 尤其关键）

problem：getting-started 和频繁引用的 skill 加载进**每个**会话，每个 token 都算数。

目标字数：getting-started 流程 <150 词；频繁加载 skill <200 词；其他 skill <500 词（仍要精炼）。

手法：详细移到 `--help` 或 references（正文用 "详见 references/xxx.md"）；用交叉引用而非重复；压缩示例（一个优秀示例胜过多个平庸的）；消除冗余。

### 内容结构 / 目录结构 / 单文件 vs 目录

SKILL.md 章节骨架模板、skill 目录布局、单文件与目录形态的选择标准：见 [references/structure-template.md](references/structure-template.md)。

### Gotchas 是最高信号内容

Gotchas 比最佳实践更有价值，因为它来自实际失败。每个 gotcha：
- **问题**：什么情况下会出错
- **现象**：出错时看到什么
- **解决**：怎么避免或修复

## 设计模式

`metadata.pattern` 的合法取值与适用场景（pipeline / reviewer / gate / tool-wrapper / inversion / routing + fallback / reference）：见 [references/design-patterns.md](references/design-patterns.md)。

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
- [ ] 跑过 `forge skills validate` + `forge skills audit` 校验（R1-R17 规范 + 22 条安全规则，forge 自带，无需外部脚本）。R1-R17 每条规则的文本定义：见 [references/validation-rules.md](references/validation-rules.md)
- [ ] **TDD：跑过基线测试**（无 skill 时子代理怎么失败），针对失败写的 skill
- [ ] **防注水：跑过 `skill-anti-degradation-check.sh`**（扫描弱措辞/弱门控/无命令 checklist）
- [ ] **rubric ≥75 分**：机器校验过后，按四维质量 rubric（简洁性/可操作性/工作流清晰度/渐进披露，各 0-25 分）人工或 LLM 评审打分，总分 <75 不予合并。锚点判据与裁决表：见 [references/quality-rubric.md](references/quality-rubric.md)

## 防注水自检（避免 skill 写得比实际做法松）

新建或修改 skill 后，强制跑一次防注水扫描，检出三类"skill 声称有校验但无具体可执行方法"的注水（弱校验措辞 / 门控无方法 / checklist 无命令）。

```bash
bash references/skill-anti-degradation-check.sh              # 全仓扫描（CI / 提交前必跑）
bash references/skill-anti-degradation-check.sh <skill-name> # 扫描单个 skill
```

三类注水的特征/示例/修复、退出码、已知 false positive 模式：见 [references/anti-degradation.md](references/anti-degradation.md)。

## 参考

- Anthropic 官方 skill 规范：[references/anthropic-spec-notes.md](references/anthropic-spec-notes.md)
- R1-R17 校验规则文本定义：[references/validation-rules.md](references/validation-rules.md)
- 四维质量评分 rubric（机器判不了的质量维度，总分 <75 为合并阻断线）：[references/quality-rubric.md](references/quality-rubric.md)
- 外部分类法映射（Thariq 9 类型 / Google 5 模式 → 本库 skill）：[references/skill-pattern-guide.md](references/skill-pattern-guide.md)（外部分类视角；本库 pattern 合法值以 design-patterns.md + R7 为准）
- CSO 实证发现：description A/B 测试
- 防注水扫描脚本：**references/skill-anti-degradation-check.sh**（本次 review 沉淀，与 TDD 基线测试同为 skill 质量门控）
