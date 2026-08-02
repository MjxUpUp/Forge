---
name: ai-generated-ui-review
description: "审查 AI 工具（v0/Bolt.new/Lovable/Replit/Cursor）生成的前端 UI 代码，拦'原型即生产'伪命题——查安全债/供应链风险/可维护性税量化/来源风险四个 AI 生成独有块。Use when: 审查 AI 生成的代码、v0 生成的能不能用、Lovable/Bolt 产出能上生产吗、AI 生成 UI 检查、vibe coding 审查、这个 AI 写的前端能合并吗、AI 生成代码安全审查时。SKIP: 人类手写代码审查（用 frontend-code-review）、通用 AI 作弊指纹扫描（断言弱化等，用 code-review-gate 轨道 A）、DRY/a11y/Design Token 等通用前端维度（用 frontend-code-review）、Rust 代码（用 rust-code-review）、纯调研 AI 工具（用 research-workflow）。"
metadata:
  pattern: reviewer + gate
  domain: frontend
  severity-levels: block,fix,suggest
  composes: [frontend-code-review]
---

# AI 生成 UI 审查

审查 AI 工具（v0/Bolt/Lovable/Replit/Cursor）生成的前端 UI 代码，拦"原型即生产"伪命题。AI 生成代码有**结构性问题**（非偶发 bug）：安全债、供应链风险、可维护性税——这些是 AI 生成范式的固有代价，不是改几个 bug 能解决的。

**核心立场（Addy Osmani, Google Chrome）**：
> "treat every AI-generated snippet as if it came from a junior developer."

## 与其他审查 skill 的分工（必读，避免重复）

| 场景 | 用哪个 skill |
|---|---|
| 人类手写前端代码 | `frontend-code-review`（前端专属深度） |
| 通用 AI 作弊指纹（断言弱化/错误吞没/假重构/类型抑制） | `code-review-gate` 轨道 A |
| DRY / 设计系统脱节 / a11y 等通用维度 | `frontend-code-review` + `code-review-gate` 轨道 B（本 skill 不重复，见步骤 3 指针） |
| **AI 生成 UI 的独有问题**（本 skill） | 安全债 / 供应链 / 可维护性税量化 / 来源风险 |
| Rust 代码 | `rust-code-review` |
| 生成方法论（怎么用 AI 工具生成好） | `ai-ui-generation-workflow`（本 skill 是审查它的产出） |
| 建 design token（修 AI 产出的 token 抽取） | `design-system-workflow` |
| 生成时规范 | `frontend-feature-development` |

**叠加使用**：AI 生成的代码 → 先 `code-review-gate` 轨道 A 查通用作弊 → 再本 skill 查 AI 生成独有的四块 → 再 `frontend-code-review` 查前端规范。分级只表达处理顺序——叠加 gate 时 block 以下（fix/suggest）也必须逐条显式回应，裁决见 code-review-gate 步骤 3「叠加专项审查的输出协议」。

## 流程

### 步骤 1：确定范围 + 风险升档信号

- 有 diff/PR → 审变更；整份生成代码 → 全审
- **风险升档信号（不是硬前置，确认不到不阻断）**：能确认生成工具（v0/Bolt/Lovable/Replit/Cursor）则按各自已知问题模式加权；无 spec.md = vibe coding，风险翻倍，所有发现升一级看待、判定按"无 spec"行执行（块 4）

### 步骤 2：加载清单

加载 [references/maintainability-checklist.md](references/maintainability-checklist.md) 获取完整清单（4 个独有块 + 3 个通用维度指针 + 判定矩阵）。

### 步骤 3：4 个独有块评估（核心）

**通用维度不重复查（指针，各一行）**：
- **DRY 违反**（AI 生成头号问题——[Lovable 官方文档自认](https://docs.lovable.dev/) "do not reuse shared components unless clearly scoped"；[arXiv 2603.28592](https://arxiv.org/abs/2603.28592) 302,579 个 AI 生成 commit 中 89.3% 为 code smell）→ code-review-gate 轨道 B「可维护性」+ 清单「通用维度指针」节的量化阈值
- **设计系统脱节**（硬编码色值/间距、未复用项目组件）→ frontend-code-review 维度 4「Design Token 一致性」
- **a11y 缺失**（`<div onClick>`、动效无 reduced-motion）→ frontend-code-review 维度 1「a11y」

#### 块 1：安全债（生产化必查，最高优先）

[Georgia Tech Vibe Security Radar](https://news.research.gatech.edu/2026/04/13/bad-vibes-ai-generated-code-vulnerable-researchers-warn)：AI 代码相关 CVE 累计确认 74 个（2026-03 单月 35 个，为 1 月的 6 倍）；[OX.Security](https://www.ox.security/blog/vibe-coding-security)：~62% AI 代码有可利用漏洞、45% 触发 OWASP Top 10。

检查（命中任一 = **block**，不可合并）：
- **API key / 密钥前端暴露**：搜 `process.env` 误用、硬编码 key、`.env` 进 client bundle
- **数据库无认证**：直连 DB 无 auth 中间件
- **SQL 拼接 / 无输入校验**：未用 zod/参数化查询
- **BOLA（越权）**：[Lovable BOLA 暴露 48 天才修](https://thenextweb.com/news/lovable-vibe-coding-security-crisis-exposed)
- **CORS 全开**：`Access-Control-Allow-Origin: *` 配合凭证

**真实事故**：某创始人用 Cursor 全 AI 生成 SaaS，上线两天发现 API key 暴露在前端、数据库无认证。

#### 块 2：shadcn registry 供应链风险

若 AI 生成代码引入第三方 registry 组件：
- 非可信 registry 来源 → **block**（RCE 注入攻击面，DEV.to "Risk of Registry Injection Attacks with shadcn"）
- 未审查的 `npx shadcn add` 来源 → block
- 只用官方/可信 registry，企业自建

#### 块 3：可维护性税量化（纵向指标）

对 AI 生成的模块跑量化指标：
- **重复率**：相同/近似代码块占比（>15% 警告、>25% block，`jscpd` 检测）
- **圈复杂度**：单组件 > 10 警告，> 20 block
- **包大小**：引入依赖是否必要（Framer Motion 125KB / Spline runtime 544KB 不可 tree-shake）
- **存活技术债**：[arXiv 2603.28592](https://arxiv.org/abs/2603.28592) 22.7% AI 技术债存活——标记后必须修，不能"先留着"

#### 块 4：来源风险（vibe coding 定级）

- 无 spec.md 的 vibe coding → 风险翻倍：所有发现升一级看待，判定矩阵按"无 spec"行执行
- 把 AI 产出当"成品"而非"起点"本身即是 Red Flag
- [arXiv 2508.14727](https://arxiv.org/abs/2508.14727)：AI 代码即使通过功能测试也不等于适合生产——必跑 SAST + 清单逐项查，不靠"看起来能跑"

### 步骤 4：产出结构化审查

```markdown
## AI 生成 UI 审查摘要

**生成工具**：[v0/Bolt/Lovable/Replit/Cursor/未知]
**有无 spec**：[有/无——无则标"vibe coding，高风险"]
**总发现数**：N（X block、Y fix、Z suggest）

### Block（不可合并，必须修复）
1. `api/route.ts:15` — API key 硬编码在前端 bundle — [安全债，立即移除]

### Fix（应当修复）
1. `components/Form.tsx` — secrets 打进 console.log — [安全债 fix]

### Suggest
1. 引入 Framer Motion 125KB 仅用 fade-in — [建议换 CSS transition]

### 可维护性指标
- 重复率：18%（⚠️ 超 15%）
- 最大圈复杂度：14（⚠️）
- 新增依赖：3 个（2 个必要）

### 判定
[✅ 可合并（修完 block）/ ⚠️ 需改造后合并 / ❌ 重写（vibe coding 无 spec + 多 block）]
```

### 步骤 5：迭代 + 生产化指引

block/fix 修复后重审。若判定"需改造"或"重写"，指引走 **ai-ui-generation-workflow** 阶段 3（生产化改造 5 步）。

## Common Rationalizations（堵借口）

| 借口 | 现实 |
|---|---|
| "v0 生成的，质量有保证" | 89.3% code smell；当 junior 代码审查 |
| "API key 在 env 里就安全" | Next.js client bundle 会打进 `NEXT_PUBLIC_*`；查是否误用 |
| "Lovable 说不用复用，那就听它的" | 那是承认它反 DRY；你要手动抽公共组件 |
| "安全以后再加" | vibe coding 安全债正在显性化（74 CVE）；上线前必查 |
| "重复几处无所谓" | 89.3% code smell 主因就是重复；AI 生成的头号问题 |
| "AI 写得快，审查浪费时间" | 22.7% 技术债存活；不审查 = 永久负担 |
| "人工看过了没大问题" | 人工看是弱校验；跑量化指标（重复率/圈复杂度/grep 密钥）才叫审过 |
| "跑起来能点就算过" | 功能能跑 ≠ 适合生产（[arXiv 2508.14727](https://arxiv.org/abs/2508.14727)）；必跑 SAST + 清单 |

## Red Flags（我在 rationalize 的信号）

- 无 spec.md 就让 AI 生成（vibe coding）
- 把 AI 生成代码当"成品"而非"起点"
- 跳过安全审查（API key/认证/SQL）
- 不查 DRY 违反就合并
- 引入第三方 registry 不审查来源
- 用"AI 生成的"当借口跳过 a11y

## 易错点（Gotchas）

- **Lovable 的"不要复用组件"是厂商承认缺陷**：不是建议你照做，是暴露它生成的代码结构性反 DRY；查法：跑 `jscpd src/ --threshold 15`，重复率 >15% 警告 / >25% block（阈值见清单块 3）
- **v0 UI 质量可能退化**：Vercel Community 反映 "UI quality in v0 gotten worse"；查法：不能靠"看完觉得还行"——跑步骤 3 的 4 块评估（安全 grep/registry 来源/重复率/圈复杂度）拿量化指标
- **"通过功能测试" ≠ 适合生产**：[arXiv 2508.14727](https://arxiv.org/abs/2508.14727) 明确——AI 代码即使通过功能测试也不适合生产。查法：跑 SAST 扫描（Veracode/Endor Labs/Cycode 或免费 `gitleaks`/`semgrep`）+ 按清单 4 块逐项查，不靠"看起来能跑"
- **[Escape.tech 数据](https://escape.tech/blog/methodology-how-we-discovered-vulnerabilities-apps-built-with-vibe-coding/)**：1400 个 AI 生成应用发现 2000+ 严重漏洞；Bolt/Replit 同病
- **Cursor 全 AI 生成 SaaS 事故**：上线两天暴露 API key + 无认证——真实案例，不是理论

## 参考

- 审查清单完整版（4 个独有块含检测命令 + 通用维度指针 + 判定矩阵）：[references/maintainability-checklist.md](references/maintainability-checklist.md)
- arXiv 2603.28592《Debt Behind the AI Boom》：https://arxiv.org/abs/2603.28592
- arXiv 2508.14727《Assessing the Quality and Security of AI-Generated Code》：https://arxiv.org/abs/2508.14727
- Georgia Tech Vibe Security Radar：https://news.research.gatech.edu/2026/04/13/bad-vibes-ai-generated-code-vulnerable-researchers-warn
- Vibe Security（OX.Security）：https://www.ox.security/blog/vibe-coding-security
