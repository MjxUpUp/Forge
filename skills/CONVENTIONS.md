# Skills 仓库编写规范 (CONVENTIONS)

本文件是本仓库所有 Skill 的**唯一编写规范**，整合自：Anthropic Agent Skills 官方标准、Google ADK 五种设计模式、skill-creator/skill-judge 元 skill 方法论、SkillForge 自进化范式，以及本机半年实践沉淀。`forge skills validate` + `forge skills audit` 的质量校验以此为准。

---

## 1. 仓库定位

Agent Skill 是**模块化的领域知识资产包（bundle）**：自然语言指令 + 元数据 + 可选资源（脚本/模板/参考）。MCP 给 agent 提供"手"（工具），Skill 提供"操作手册"（怎么用工具）。Skill 的本质是**用文件系统结构 + 文本决策树替代运行时服务**，零基础设施依赖。

## 2. 适用边界

- ✅ 适合：半自动化重复流程、领域知识/专家经验导向、上下文有限需按需加载
- ❌ 不适合：简单任务（靠模型泛化即可）、100% 确定性流程（直接写代码自动化）、职责高度单一（塞 system prompt 即可）

## 3. 文件结构

```
skill-name/                # 目录名 = skill id = frontmatter.name
├── SKILL.md               # 主文件，≤ 500 行
└── references/            # 按需加载的详细内容（模板/规范/示例/详细 gotcha）
    └── ...
# 可选
├── scripts/               # 确定性操作的执行脚本（能脚本化就不靠模型推理）
└── assets/                # 模板文件、示例产物
```

## 4. Frontmatter 规范（机器校验项）

```yaml
---
name: kebab-case-name          # [必填] 必须与目录名一致，正则 ^[a-z][a-z0-9-]*$
description: "≥80 字符的触发器" # [必填] 见 §5
metadata:
  pattern: <pattern-name>      # [必填] 见 §6，六选一或组合
  domain: <domain-name>        # [可选] 领域标签
  steps: <number>              # [可选] pipeline 步骤数
  composes: [skill-list]       # [可选] 组合的其他 skill
---
```

## 5. description 规范（最关键字段，触发器不是摘要）

模型靠 description 判断是否加载该 skill。必须包含：
- **一句话核心功能**（这是什么）
- `Use when:` + 触发场景列表（用用户会说的话，不用技术术语）
- `SKIP:` + 排除场景（提供 skill 间互斥，指向正确替代 skill）

**硬性指标**（`forge skills validate` 校验）：
- 长度 ≥ 80 字符
- 必须出现 `Use when`（大小写不敏感）
- 必须出现 `SKIP`
- 宁可多触发（"pushy"），不要少触发

**差**：`帮助管理 Rust trait`
**好**：`Rust trait 适配器模式。Use when: 为已有类型实现外部 trait 时、创建 newtype wrapper 时、遇到孤儿规则冲突时。SKIP: 定义新 trait（直接定义即可）、纯数据结构设计。`

## 6. 设计模式（metadata.pattern，Google ADK 五模式 + Forge gate 扩展）

| pattern | 用途 | 特征 | 何时用 |
|---------|------|------|--------|
| `tool-wrapper` | 按需加载领域知识 | SKILL.md 指向 references/，不塞满 system prompt | 技术栈规范、领域知识封装 |
| `generator` | 填空式文档生成 | 模板 + 风格指南 + 主动问缺失变量 | 报告/文档/API 文档生成 |
| `reviewer` | 质量审查 | 检查清单独立维护，按严重性分组 | 代码审查、规范检查 |
| `inversion` | 先问后做 | "DO NOT start until..." 硬 gate，串行提问 | 需求不明确、复杂方案设计 |
| `pipeline` | 带检查点的多步工作流 | 严格顺序，每步有 diamond gate | 多阶段内容生产、迁移 |
| `gate` | 硬门控拦截（Forge 扩展） | mandatory 项不过即阻断，每项配可执行命令 + 量化通过标准 | 提交前审查、发布 readiness、按需安全护栏 |

前五种源自 Google ADK；`gate` 是 Forge 扩展（门控拦截，区别于 reviewer 的"审查清单"——gate 强制阻断、reviewer 仅发现）。模式可组合：`inversion + pipeline`、`generator + reviewer`、`reviewer + gate`。组合时主模式写在前。

## 7. 内容设计四件套（高信号实践）

1. **决策树替代模糊判断**：skill 的核心价值是封装专家判断。分支处用树形结构（"当 X 失败时，因为 Y，尝试 Z"），不让模型自行决策。这是 skill-judge D1/D8 的明确加分项。
2. **负向约束配替代方案**：说"不能做什么"时同步给"那应该怎么做"（few-shot）。否则模型会自己找个错法。
3. **执行后自查清单**：产出规范类 skill 加 Post-Generation Review，把"我觉得做完了"变"我验证过做完了"。决策树收敛（多选一），自查发散（一验多）。
4. **Gotchas（易错点）**：来自实际失败的内容比最佳实践更高信号。每条含 问题/现象/解决。

## 8. 渐进式披露（Progressive Disclosure）

三层信息成本递增，按需加载：
- 第一层：frontmatter（~100 词，始终加载，靠 description 决定是否触发）
- 第二层：SKILL.md 正文（≤ 500 行，按需加载，总-分结构：核心规则→展开约束）
- 第三层：references/ 完整资源（需要时才读）

设计 skill 时先想清楚：哪些必须在 SKILL.md，哪些下沉 reference。

## 9. 双重验证机制

- **内部自查**（运行时护栏）：skill 执行时模型对照清单自检（规范符合性 + 覆盖完整性）
- **外部 eval**（开发期标准线）：真实提示词跑 有/无 skill 对比，迭代循环（评估→修改→重跑→再评估）
  - 测试用例：2-3 个真实用户会说的话，足够复杂以保证触发
  - description 优化：单独用 should-trigger / should-not-trigger 样本测召回精度

## 10. 质量校验清单（`forge skills validate` + `forge skills audit` 自动检查）

- [ ] name 与目录名一致且 kebab-case
- [ ] description ≥ 80 字符且含 `Use when` + `SKIP`
- [ ] metadata.pattern 存在且为六种之一（或组合）
- [ ] SKILL.md ≤ 500 行（超限拆 references）
- [ ] 有决策树/自查/Gotchas 之一（高信号内容）

## 11. 自进化（SkillForge 范式，目标态）

失败 case 是改进燃料。理想闭环：执行→四维度失败分析（Knowledge/Tool/Clarification/Style）→聚类→ReAct 诊断（映射失败到 skill 缺陷位置）→ Minimal Modification 修复。改一处解决一类，防止"改了 A 坏了 B"。

## 12. Skills Loop 闭环（实测机制，与 §11 目标态对照）

37 个 canonical skill + 6 个 Go 包族（`skillscanonical` / `skillgen` / `skillsdist` / `skillsqa` / `skillsfm`(frontmatter YAML 解析，**不是**用度聚合) / `skillseval`）构成 8 阶段 loop：[1] Authoring → [2] Canonical Resolve → [3] Project Generate → [4] Distribute → [5] Usage in Loop → [6] Audit + Track → [7] Eval → [8] Feedback → 回 [1]。Forge 自身定位（README:17-25）= 给 coding agent loop 补验证/状态/学习三层，**不替代循环**。

### 强制路由（skill-routing 实测强度）

| Agent | 机制 | 强度 | 备注 |
|---|---|---|---|
| **Claude Code** | UserPromptSubmit hook `additionalContext` | 中 | 不能改写输入，只能注入/阻断 |
| **Cursor** | `rules/*.mdc` alwaysApply | 软 | 纯 prompt |
| **Codex** | AGENTS.md 文字注入 | 软（最弱） | Codex 不读 SKILL.md，只能文字 |

> **硬强制（input transform）已无支持 agent**：曾仅 pi 能在用户输入阶段改写成 `/skill:name <原文>` 强制展开 skill，pi 已退出专精名单（commit 34c68b8）。当前 3 家 agent 都做不到改写输入——这是机制上限，不是配置问题。

**改路由 = 改 `skills/skill-routing/routes.json` 一个文件** + `forge skills adapters --apply` 重分；详见 `skill-routing/SKILL.md` §路由表源解析。

### 工具（已实现，非新写）

`forge skills {list,install,audit,validate,usage,adapters,eval}` 命令族已完整。**`forge skills usage [--top N] [--json] [--undertrigger]` 是 skill 用度 dashboard 的现成形态**——读 `~/.forge/research/skill-usage.jsonl` 出 hot + undertrigger 候选。**不是新写，是文档化普及**。

### 实测验证（2026-07-05，本机）

| 项目 | ConfigDir | DataDir（`~/.forge/projects/<hash>/`） | 活循环 |
|---|---|---|---|
| E:\Forge | ✅ | ✅ 含 act/checklog/experience/reviews | ✅ |
| E:\AgentWorld | ✅ | ⚠️ 空（init 后未跑 task） | ❌ |
| E:\DevWorkbench | ✅ | ⚠️ 空（init 后未跑 task） | ❌ |

`s/skill-usage.jsonl` 实测：185 条 / 39 unique / top 5 = research-workflow(48) / web-search-bridge(35) / fact-research(8) / evidence-based-proposal(8) / agent-delegation(8)。**研究类 skill 占比 ~45%** 反映"写代码前先调研"的项目画像。

### 已知 gap（诚实记录，未来触发再评估）

- **G1** `[8] Feedback → [1] Authoring` 自动反哺弱：现靠 spec author review 改 SKILL.md → forge release 推到用户；无自动化 hook
- **G2** 2/3 项目 init 但 DataDir 空：产品 adoption 问题，不是 loop 工程缺陷
- **G3** skill-routing 强 vs 软差异 = 各 agent 机制上限（不可绕过）；新 agent 出现时再评估
- **G4** 长期 undertrigger skill 用 `forge skills usage --undertrigger` 半年级 review
- **G5** 本仓库 `~/.gitignore_global` 防 `docs/`/`设计文档/` 误提交——本节是该架构 truth 的单点归档（无需单立 docs/）
