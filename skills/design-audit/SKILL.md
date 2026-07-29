---
name: design-audit
description: "飞书设计文档→代码落地审计：判断设计文档里的功能在代码里落地了几个（LANDED/PARTIAL/MISSING 三态判决）。Use when: 给飞书 wiki/docx 设计文档链接要对比代码实现程度时、判断功能 landing 状态时、做设计 vs 实现 gap 分析时、说\"看下实现程度\"\"对比设计稿\"\"哪些没做\"\"landing 了吗\"\"缺口分析\"时。SKIP: 单个功能实现（用 frontend-feature-development）、提交前 diff 审查（用 code-review-gate）、整项目批量审查编排（用 review-batch）、把代码反向导成设计图（用 design-review-snapshot）、纯调研不对比代码（用 research-workflow）、整项目验收编排（用 project-acceptance）。"
metadata:
  pattern: pipeline
  domain: quality-assurance
---

# 设计文档 → 代码落地审计

解决"设计文档说做 10 个功能，代码到底落地了几个"的反复手动对比。每次给飞书 wiki 链接都要手工让 agent 读文档+搜代码+做判断——本 skill 把它固化成标准 gap report 流程。

> **前置依赖**：Step 1 读飞书设计文档依赖 `lark-doc` skill（非 canonical，需单独安装）。**未安装 lark-doc 时降级**：请用户直接粘贴设计文档文本/导出 markdown，跳过飞书读取直接进 Step 1 的 feature 提取。后续 Step 2-4（搜证/判决/报告）纯代码操作，不依赖飞书。

## 核心原则

**你不是设计稿的复读机，也不是代码的辩护律师。** 你的价值是**中立判决**：设计说要 X，代码里有 X 的证据吗？有→LANDED，部分有→PARTIAL，没有→MISSING。每条判决必须附**文件:行**证据，不靠印象。

## 审计流程

### Step 1: 提取设计 feature 清单（从飞书文档）

用 `lark-doc` skill 读取飞书设计文档（wiki/docx），提取所有可验证的 feature/设计点：

- 每个 feature 必须是**可在代码中搜索的具象点**，不是模糊描述
- 差：`"良好的用户体验"` → 好：`"登录页有表单校验，错误提示 inline 显示"`
- 差：`"性能优化"` → 好：`"列表用虚拟滚动，>1000 项不卡"`

提取后产出 `{feature清单}`，每条编号。**清单必须经用户确认**再进 Step 2——避免审计了一堆用户不关心的点。

### Step 2: 逐项搜证（在代码库）

对 feature 清单每一项，在代码库搜证据：

- **搜索策略**：先按 feature 的关键名词 grep（组件名/函数名/路由名/CSS class），找到候选文件后再 read 确认
- **证据标准**：不是"有相关代码"，而是"代码确实实现了这个 feature 的核心行为"
  - `"登录表单校验"` → 找到校验函数 + 调用处 + 错误提示渲染，才算 LANDED
  - 只找到 `<form>` 但无校验逻辑 → PARTIAL
- **搜不到不代表 MISSING**：可能命名不同。换 2-3 个近义词/技术术语再搜，仍无才判 MISSING

### Step 3: 三态判决 + gap report

每个 feature 给一态：

| 状态 | 判据 |
|---|---|
| ✅ **LANDED** | 代码有完整实现，行为符合设计描述 |
| 🟡 **PARTIAL** | 代码有部分实现（如 UI 有但逻辑缺，或主路径有但边界没处理） |
| ❌ **MISSING** | 代码中找不到任何相关实现 |

输出 gap report（markdown 表格）：

```markdown
## 设计落地审计 — <项目> @ <日期>

| # | Feature (设计要求) | 状态 | 证据 (文件:行) | Gap 说明 |
|---|---|---|---|---|
| 1 | 登录表单 inline 校验 | ✅ LANDED | `src/Login.tsx:42` validate() | — |
| 2 | 列表虚拟滚动 | 🟡 PARTIAL | `src/List.tsx` 无虚拟化 | UI 在但未接 react-window |
| 3 | 暗色主题切换 | ❌ MISSING | — | 无 theme provider |

### 汇总
- LANDED: X / N
- PARTIAL: Y / N（列缺口）
- MISSING: Z / N（列缺失）
```

### Step 4: 确认 gap → 进入实现

把 gap report 给用户。用户确认后：
- MISSING 项 → 进入实现（用 frontend-feature-development / fullstack-feature 等）
- PARTIAL 项 → 补全缺口
- **不要跳过确认直接实现**——用户可能调整优先级或判定某些 MISSING 是有意为之

## 决策树

```
拿到飞书设计链接
├─ 是 wiki/docx 吗？→ 是：用 `lark-doc` 读
│                   → 否（sheets/base 等）：先用 `lark-doc` 提 token 再切对应 skill
├─ feature 清单提取后
│  ├─ 清单 ≥20 条？→ 太多，问用户聚焦哪几个模块再审计
│  └─ 清单清晰？→ 进 Step 2
├─ 搜证时某 feature 搜不到
│  ├─ 换 2-3 近义词仍无？→ MISSING
│  └─ 找到了但行为不全？→ PARTIAL
└─ gap report 出来后
   ├─ MISSING 过半？→ 可能项目刚起步，报告"整体处于早期"，别逐条列
   └─ 用户要直接实现 MISSING？→ 走对应实现 skill
```

## Gotchas（高信号）

### 问题: 把"有相关代码"当 LANDED
**现象**: feature 是"登录校验"，搜到 `<input>` 就判 LANDED，实际无校验逻辑
**解决**: 证据必须是**核心行为**的实现，不是相关名词的出现。`<input>` 是 PARTIAL（UI 在逻辑缺），有 validate() 才 LANDED

### 问题: 设计文档 feature 太模糊无法搜证
**现象**: 设计写"用户体验好""性能优"，无法在代码搜索
**解决**: Step 1 提取时就把模糊描述**转译成可搜证的具象点**。转译不了就标 `[模糊，需用户澄清]` 跳过，不硬判

### 问题: 漏判——搜不到就报 MISSING，实际是命名不同
**现象**: 设计说"购物车"，代码里叫 `CartService`/`cart-store`/`useCart`，只搜"购物车"中文报了 MISSING
**解决**: 搜证时同时用中英文 + 技术术语（组件名/函数名/hook 名/CSS class）。仍无才 MISSING

### 问题: 审计完不确认就实现
**现象**: 出 gap report 直接开始写 MISSING 的功能，结果用户说"那个本来就不做"
**解决**: Step 4 必须等用户确认。MISSING ≠ 一定要做——可能是有意砍掉的需求

## 与其他 skill 的分工

- `lark-doc`：读飞书设计文档（本 skill 的 Step 1 调用）
- **code-review-gate**：提交前 diff 审查（单次变更），本 skill 是设计 vs 代码的全量对比
- **review-batch**：整项目并行审查编排（无设计文档参照），本 skill 是有设计文档作锚点的审计
- **design-review-snapshot**：反向，把代码导成设计图给设计审核；本 skill 是正向，设计文档审代码
