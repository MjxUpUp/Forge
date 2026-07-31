---
name: ai-generated-ui-review
description: "审查 AI 工具（v0/Bolt.new/Lovable/Replit/Cursor）生成的前端 UI 代码，拦'原型即生产'伪命题——查可维护性税/安全债/DRY 违反/设计系统脱节/a11y 缺失/供应链风险。Use when: 审查 AI 生成的代码、v0 生成的能不能用、Lovable/Bolt 产出能上生产吗、AI 生成 UI 检查、vibe coding 审查、这个 AI 写的前端能合并吗、AI 生成代码安全审查时。SKIP: 人类手写代码审查（用 frontend-code-review）、通用 AI 作弊指纹扫描（断言弱化等，用 code-review-gate 轨道 A）、Rust 代码（用 rust-code-review）、纯调研 AI 工具（用 research-workflow）。"
metadata:
  pattern: reviewer + gate
  domain: frontend
  severity-levels: block,fix,suggest
  composes: [ai-ui-generation-workflow, frontend-code-review]
---

# AI 生成 UI 审查

审查 AI 工具（v0/Bolt/Lovable/Replit/Cursor）生成的前端 UI 代码，拦"原型即生产"伪命题。AI 生成代码有**结构性问题**（非偶发 bug）：可维护性税、安全债、反 DRY、设计系统脱节——这些是 AI 生成范式的固有代价，不是改几个 bug 能解决的。

**核心立场（Addy Osmani, Google Chrome）**：
> "treat every AI-generated snippet as if it came from a junior developer."

## 与其他审查 skill 的分工（必读，避免重复）

| 场景 | 用哪个 skill |
|---|---|
| 人类手写前端代码 | `frontend-code-review`（前端专属深度） |
| 通用 AI 作弊指纹（断言弱化/错误吞没/假重构/类型抑制） | `code-review-gate` 轨道 A |
| **AI 生成 UI 的结构性问题**（本 skill） | DRY 违反 / 安全债 / 设计系统脱节 / a11y 缺失 / 供应链 |
| Rust 代码 | `rust-code-review` |
| 生成方法论（怎么用 AI 工具生成好） | `ai-ui-generation-workflow`（本 skill 是审查它的产出） |
| 建 design token（修 AI 产出的 token 抽取） | `design-system-workflow` |
| 生成时规范 | `frontend-feature-development` |

**叠加使用**：AI 生成的代码 → 先 `code-review-gate` 轨道 A 查通用作弊 → 再本 skill 查 AI 生成特有问题 → 再 `frontend-code-review` 查前端规范。

## 流程

### 步骤 1：确认来源 + 范围

- 是哪个 AI 工具生成的？（v0/Bolt/Lovable/Replit/Cursor——各有已知问题模式）
- 有 spec.md 吗？（没有 = vibe coding，风险翻倍，重点查）
- 有 diff/PR → 审变更；整份生成代码 → 全审

### 步骤 2：加载 6 类可维护性评估清单

加载 [references/maintainability-checklist.md](references/maintainability-checklist.md) 获取完整清单。

### 步骤 3：6 类评估（核心）

#### 1. DRY 违反（AI 生成的头号问题）

**Lovable 官方文档自己承认**："Create a component specifically for [role X] and **do not reuse shared components** unless clearly scoped."——这是 AI 生成反 DRY 的厂商自认。

检查：
- 相同/近似组件重复出现（ButtonA/ButtonB/ButtonC 各自实现）→ **block**（抽公共组件）
- 内联样式/逻辑重复 ≥3 次 → fix
- 同一图标/常量在多处硬编码 → fix

**arXiv 2603.28592 数据**：302,579 个 AI 生成 commit 含 484,366 个技术问题，**89.3% code smell，22.7% 存活**。

#### 2. 安全债（生产化必查，最高优先）

**Georgia Tech Vibe Security Radar**：AI 代码相关 CVE 从 2026-01 的 6 个涨到 74 个；OX.Security：~62% AI 代码有可利用漏洞、45% 触发 OWASP Top 10。

检查（命中任一 = **block**，不可合并）：
- **API key / 密钥前端暴露**：搜 `process.env` 误用、硬编码 key、`.env` 进 client bundle
- **数据库无认证**：直连 DB 无 auth 中间件
- **SQL 拼接 / 无输入校验**：未用 zod/参数化查询
- **BOLA（越权）**：Lovable 曾出现 BOLA 暴露 48 天才修
- **CORS 全开**：`Access-Control-Allow-Origin: *` 配合凭证

**真实事故**：某创始人用 Cursor 全 AI 生成 SaaS，上线两天发现 API key 暴露在前端、数据库无认证。

#### 3. 设计系统脱节

AI 生成倾向硬编码而非走 token：
- 硬编码色值/间距（`#5e6ad2` / `16px`）而非 `var(--color-*)` → fix
- 未复用项目已有组件（自己造 Button 而非用 `@/components/ui/button`）→ fix
- 与项目 design language 不一致（字体/圆角/阴影风格）→ fix

→ 修 token 抽取用 **design-system-workflow**。

#### 4. a11y 缺失（AI 生成普遍缺）

AI 生成 UI 普遍忽略 a11y：
- `<div onClick>` 处理交互 → block
- 动效无 reduced-motion → block（WCAG 2.2 SC 2.3.3）
- 缺键盘导航/ARIA → fix
- 对比度不足 → fix

→ 详细 a11y 检查用 **frontend-code-review** 维度 1。

#### 5. shadcn registry 供应链风险

若 AI 生成代码引入第三方 registry 组件：
- 非可信 registry 来源 → **block**（RCE 注入攻击面，DEV.to "Risk of Registry Injection Attacks with shadcn"）
- 未审查的 `npx shadcn add` 来源 → block
- 只用官方/可信 registry，企业自建

#### 6. 可维护性税量化（纵向指标）

对 AI 生成的模块跑量化指标：
- **重复率**：相同/近似代码块占比（>15% 警告）
- **圈复杂度**：单组件 > 10 警告，> 20 block
- **包大小**：引入依赖是否必要（Framer Motion 125KB / Spline runtime 544KB 不可 tree-shake）
- **存活技术债**：arXiv 研究 22.7% AI 技术债存活——标记后必须修，不能"先留着"

### 步骤 4：产出结构化审查

```markdown
## AI 生成 UI 审查摘要

**生成工具**：[v0/Bolt/Lovable/Replit/Cursor]
**有无 spec**：[有/无——无则标"vibe coding，高风险"]
**总发现数**：N（X block、Y fix、Z suggest）

### Block（不可合并，必须修复）
1. `api/route.ts:15` — API key 硬编码在前端 bundle — [安全债，立即移除]
2. `components/UserCard*.tsx` — 5 个近似 UserCard 各自实现 — [DRY 违反，抽公共]

### Fix（应当修复）
1. `Button.tsx:8` — 硬编码 #5e6ad2 — [换 var(--color-brand-primary)]

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
| "人工看过了没大问题" | 人工看是弱校验；跑 6 类量化指标（重复率/圈复杂度/grep 密钥）才叫审过 |
| "跑起来能点就算过" | 功能能跑 ≠ 适合生产（arXiv 2508.14727）；必跑 SAST + 6 类清单 |
| "v0 生成的应该没问题" | v0 质量退化已被社区反映；生成后必跑评估，不靠品牌信任 |
| "安全问题上线再查" | 74 CVE / BOLA 48 天都是上线后被发现的；上线前 grep + SAST 必跑 |

## Red Flags（我在 rationalize 的信号）

- 无 spec.md 就让 AI 生成（vibe coding）
- 把 AI 生成代码当"成品"而非"起点"
- 跳过安全审查（API key/认证/SQL）
- 不查 DRY 违反就合并
- 引入第三方 registry 不审查来源
- 用"AI 生成的"当借口跳过 a11y

## 易错点（Gotchas）

- **Lovable 的"不要复用组件"是厂商承认缺陷**：不是建议你照做，是暴露它生成的代码结构性反 DRY；查法：跑 `jscpd src/ --threshold 15`，重复率 >15% 警告 / >25% 阻断（maintainability-checklist 类 1）
- **v0 UI 质量可能退化**：Vercel Community 反映 "UI quality in v0 gotten worse"；查法：不能靠"看完觉得还行"——跑步骤 3 的 6 类评估（DRY 重复率/安全 grep/设计系统脱节 grep/a11y/registry 来源/圈复杂度）拿量化指标
- **"通过功能测试" ≠ 适合生产**：arXiv 2508.14727 明确——AI 代码即使通过功能测试也不适合生产。查法：跑 SAST 扫描（Veracode/Endor Labs/Cycode 或免费 `gitleaks`/`semgrep`）+ 按 maintainability-checklist 6 类逐项打分，不靠"看起来能跑"
- **Escape.tech 数据**：1400 个 AI 生成应用发现 2000+ 严重漏洞；Bolt/Replit 同病
- **Cursor 全 AI 生成 SaaS 事故**：上线两天暴露 API key + 无认证——真实案例，不是理论

## 参考

- 6 类可维护性评估清单完整版（含检测命令）：[references/maintainability-checklist.md](references/maintainability-checklist.md)
- arXiv 2603.28592《Debt Behind the AI Boom》：https://arxiv.org/html/2603.28592v1
- Vibe Security Radar：https://www.ox.security/blog/vibe-coding-security
