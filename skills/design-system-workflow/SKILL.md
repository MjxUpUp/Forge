---
name: design-system-workflow
description: "Design Token 与设计系统建设端到端工作流：Figma/Pixso/PenPot → DTCG 2025.10 → Style Dictionary → CSS/iOS/Android，含 OKLCH 双主题与 shadcn registry 治理。Use when: 建/管 design token、设计工具到代码 token 同步、OKLCH 主题切换、shadcn registry 集成、多端 token 流转、composite token 处理、token 命名与 design system 治理时。SKIP: 选组件库（frontend-stack-selection）、纯视觉审美执行（frontend-aesthetics-execution）、写普通组件（frontend-feature-development）、查 Tailwind/@theme API（dev-lookup）、迁移旧设计系统/换色板（design-system-migration）。"
metadata:
  pattern: pipeline + gate
  domain: frontend
  composes: [frontend-stack-selection, frontend-feature-development, frontend-code-review]
---

# Design System 与 Token 工作流

Design Token 是设计系统的基石——把设计决策（色值/间距/字号/阴影/动效）编码成可跨平台流转的变量。本 skill 管 token 从设计侧（Figma / Pixso / PenPot）到工程侧（CSS/iOS/Android）的端到端 pipeline，以及 shadcn registry 等组件分发机制的治理。

## 设计工具中立（2026 实证，选你团队用的）

| 工具 | token 来源 | 集成方式 | 适合 |
|---|---|---|---|
| **Figma** | Variables + **Tokens Studio 插件**（最成熟现成链路） | Tokens Studio 导出 DTCG JSON → Style Dictionary | 海外团队、有 Figma 预算 |
| **Pixso** | **原生 Design Tokens + Variables**（2.0 起，官方 release） | ① Plugin API 自建导出 ② **官方 Pixso MCP**（18 工具本地 / 6 工具云端）直接读 token ③ JovanHsu/pixso-node-exporter 导节点 | 国内团队、免费、AI coding 原生 |
| **PenPot** | Tokens Studio 原生支持 + 开源自托管 | 同 Figma 路径 | 要开源自托管 |

**关键**：Tokens Studio 官方支持 Figma/PenPot/Framer/VS Code，**不支持 Pixso**——但 Pixso 有原生 tokens + 官方 MCP，不依赖 Tokens Studio 也能跑通链路。

## 铁律：token-only 是底线

**所有色值/间距/圆角/阴影/字号/动效曲线必须从 design token 取，禁止硬编码。** 这是 frontend-feature-development 的生成时约束，也是 frontend-code-review 的事后检查项。没有 token 的项目，先建 token 再写组件。

## 阶段 0 — 确认 token 成熟度（Inversion）

**建/改 token 前先确认：**
1. **设计侧来源**：Figma 有没有用 Variables/Tokens Studio 规范化？Pixso 有没有用原生 Design Tokens / Variables？还是散落在图层样式里？
2. **现有 token 状态**：项目里有没有 CSS 变量/Tailwind @theme/StyleX 主题？命名规范吗？
3. **目标平台**：Web only，还是含 iOS/Android/小程序多端？
4. **token 粒度**：global token（裸值如 `#5e6ad2`）+ alias token（语义如 `--color-brand-primary`）+ component token（如 `--button-bg`）三层都要吗？

→ 没确认就建 = token 体系混乱、无法维护。

## 阶段 1 — DTCG 端到端流转链路（标准 pipeline）

**W3C Design Tokens Format Module 2025.10** 于 2025-10-28 发布首个稳定版（Community Group Report，非正式 W3C 标准，但已是事实标准）。端到端链路：

```
设计工具 (设计侧)
  │  Figma: Tokens Studio 插件 → 导出 DTCG JSON
  │  Pixso: 原生 tokens → Plugin API 导出 / 或走 Pixso MCP 直读
  │  PenPot: Tokens Studio → 导出 DTCG JSON
  ▼
Git (单一真相源)
  │  tokens.json 推到仓库（设计→工程的契约）
  ▼
Style Dictionary (转换器)
  │  + @tokens-studio/sd-transforms 适配器
  ▼
多端产物
  ├── Web: CSS 变量 (tokens.css)
  ├── iOS: plist / Swift 常量
  └── Android: XML resources
```

### Pixso 特殊路径（不依赖 Tokens Studio）

Pixso 有原生 design tokens，无需 Tokens Studio，两条路：

**路径 A：自建导出脚本**（最可控，token 流转工程化）
- 用 Pixso Plugin API（`pixso.variables.getLocalVariablesAsync()` 等）读 Variables
- 在插件里转成 DTCG 2025.10 JSON 格式
- 推 Git → Style Dictionary 消费（与 Figma 路径合流）
- 参考实现：JovanHsu/pixso-node-exporter（导节点数据的开源插件）

**路径 B：官方 Pixso MCP 直读**（AI coding 工作流，最省事）
- 本地：Pixso 桌面端跑 MCP server（端口 3667），18 个工具含 `get_variables`/`get_variable_sets`/`get_local_styles`
- 云端：`https://pixso.cn/api/mcp/mcp`，6 个核心工具，需 Personal Access Token，**不开 Pixso 也能用**
- Cursor/Claude/Cline/Windsurf 原生 MCP 配置（见 ai-ui-generation-workflow）
- 适合 AI agent 直接消费 token 生成代码，跳过 JSON 中间件

### 最小可行配置

**tokens.json**（DTCG 2025.10 格式）：
```json
{
  "color": {
    "brand": {
      "primary": { "$type": "color", "$value": "#5e6ad2" }
    }
  },
  "space": {
    "4": { "$type": "dimension", "$value": "1rem" }
  }
}
```

**sd.config.mjs**（Style Dictionary + sd-transforms）：
```javascript
import { register } from '@tokens-studio/sd-transforms';
register(sd); // 适配 DTCG 格式

export default {
  tokens: ['./tokens.json'],
  platforms: {
    css: {
      transformGroup: 'tokens-studio',
      buildPath: './src/styles/',
      files: [{ destination: 'tokens.css', format: 'css/variables' }]
    }
  }
};
```

**门控**：跑 `style-dictionary build` 生成 tokens.css，提交到仓库。设计工具改 token → 推 JSON → CI 跑 Style Dictionary → PR 改 CSS 变量。（Pixso MCP 路径下，token 经 agent 直读，CI 同步需配合路径 A 的导出脚本。）

## 阶段 2 — composite token 跨平台落差（最大坑）

**shadow / gradient / border 等 composite token 在原生平台（iOS/Android）无等价物**——这是 DTCG 落地的最大落差。

| Token 类型 | Web (CSS) | iOS | Android |
|---|---|---|---|
| color | ✅ CSS 变量 | ✅ UIColor | ✅ color resource |
| dimension | ✅ rem/px | ⚠️ 需 pt 转换 | ⚠️ 需 dp/sp |
| shadow (composite) | ✅ box-shadow | ❌ 需手写 CALayer | ❌ 需 elevation XML |
| gradient (composite) | ✅ linear-gradient | ❌ 需 CAGradientLayer | ❌ 需 XML shape |
| typography (composite) | ✅ font shorthand | ⚠️ 需拆解 | ⚠️ 需拆解 |

**应对**：composite token 在 Style Dictionary 配 transform 拆解成平台原语；或限定 composite token 只在 Web 用，原生端单独维护。

## 阶段 3 — OKLCH 双主题切换（色彩工程化）

**OKLCH + color-mix + Tailwind v4 已是生产默认色彩栈。** Tailwind v4 所有 color token 用 `oklch()` 表示。

**双主题运行时切换代码模板**（可直接 copy）：
```css
@theme {
  /* 亮色（默认）— 用感知亮度 L=0~1 */
  --color-bg: oklch(99% 0 0);
  --color-fg: oklch(20% 0 0);
  --color-brand-primary: oklch(62% 0.19 264);
}

[data-theme="dark"] {
  --color-bg: oklch(15% 0 0);      /* 不是灰度反色，是系统性调暗 */
  --color-fg: oklch(95% 0 0);
  --color-brand-primary: oklch(70% 0.19 264); /* 提亮保持感知一致 */
}
```

**⚠️ sRGB fallback + 对比度退化**：OKLCH 在 sRGB 设备会被浏览器自动 gamut mapping，**静默改变亮度**，致对比度退化。dark/light 双主题必须在真实 sRGB 设备复测 WCAG 对比度（≥4.5:1）。

**color-mix 运行时混色**（W3C CSS Color Level 5）：
```css
--button-hover: color-mix(in oklch, var(--color-brand-primary) 85%, black);
```

## 阶段 4 — shadcn registry 集成与治理

shadcn/ui 的 copy-in 模式（组件源码作为分发单位）需特殊治理。**核心：设计工具（Figma/Pixso/PenPot）↔ Tailwind token 必须 strict one-to-one mapping。**

### registry.json schema（三层）
```json
{
  "name": "my-registry",
  "items": [
    {
      "name": "custom-button",
      "type": "registry:component",
      "registryDependencies": ["button"],   // 依赖的其他 registry 项
      "dependencies": ["@radix-ui/react-slot"],  // npm 依赖
      "files": [{ "path": "button.tsx", "type": "registry:component" }]
    }
  ]
}
```

### monorepo/CI 集成（参照 OpenStatus 实战）
- registry 托管两种：**静态**（JSON + 文件托管在域名）或**动态**（API 按需生成）
- monorepo（Turborepo）：每个 package 一个 registry 子项；CI 跑 `shadcn diff` 检测漂移
- **安全风险**：第三方 registry 绕开 npm 审计，有 RCE 注入攻击面（DEV.to "Risk of Registry Injection Attacks with shadcn"）——只用可信 registry，企业自建

### token 对齐纪律（设计工具 ↔ code）
- 设计工具 Variables 命名 = CSS 变量命名（strict 1:1）——Figma Variables / Pixso 原生 tokens / PenPot Variables 同理
- 不允许设计师用"临时色"，所有色必须先入 token
- 定期跑 token 漂移检测（设计工具 export vs 仓库 tokens.json diff）——Pixso 可用 Plugin API 或 MCP `get_variables` 自动化

## 阶段 5 — 大规模治理（Meta 级选 StyleX）

数千人共改同一 CSS 仓库的场景，Tailwind 的 utility 滥用会失控。**Meta 用 StyleX** 的治理逻辑：
- 原子化 CSS + build-time 静态生成（无运行时）
- **"last wins" 确定性合并**：多人改同一样式，合并结果可预测
- 类型安全 + collision-free atomic CSS

**判据**：团队 <50 人用 Tailwind + CSS 变量够；>100 人共改设计系统，评估 StyleX。StyleX guidelines "super restrictive" 是规模代价。

## Common Rationalizations（堵借口）

| 借口 | 现实 |
|---|---|
| "token 以后再建，先写组件" | 没有 token 的组件全是硬编码，重构要 grep 全仓库；先建 token |
| "设计工具和代码 token 差不多对就行" | 漂移会累积，最终设计稿和代码两个世界；strict 1:1 mapping |
| "OKLCH 太复杂，用 hex 算了" | OKLCH 感知亮度一致，dark/light 主题切换才不崩；Tailwind v4 已默认 |
| "composite token 原生端不管了" | 那原生端就会出现设计不一致；要么 transform 拆解，要么限定 Web 用 |
| "shadcn registry 随便加第三方" | 有 RCE 攻击面；只用可信 registry，企业自建 |
| "我们项目小，不用三层 token" | global + alias 两层最低；component token 按需加 |

## Red Flags（我在 rationalize 的信号）

- 组件里出现硬编码色值/间距
- 设计工具 token 和代码 token 命名不一致
- dark 主题是"灰度反色"而非 OKLCH 系统调色
- 第三方 registry 不审查就用
- composite token 直接丢给原生端不拆解

## Gotchas

- **DTCG 2025.10 是 Community Group Report**：非正式 W3C 标准，工具实现可参考但无强制合规；仍是事实标准
- **容器查询的"想用 vs 真用"落差**：36%/43% 开发者放 reading list，实际用 7%/<1%；别为用而用
- **Figma 原生 Variables vs Tokens Studio**：Figma 2024 推原生 Variables 后，部分团队绕开 DTCG JSON 直接走"Figma Variables → 导出 CSS"；两套并存注意选型
- **Pixso 无 Tokens Studio 但有原生 tokens + 官方 MCP**：别误以为 Pixso 做不了 token 工作流——原生 Design Tokens（2.0 起）+ Plugin API + MCP（18 本地工具 / 6 云端工具）三条路，AI coding 场景下甚至比 Figma 的 Tokens Studio 更顺（MCP 是 Cursor/Claude 原生协议）
- **Pixso 官方 MCP 是客户端能力**：本地 18 工具需 Pixso 桌面端运行（端口 3667）；云端 6 工具走 `pixso.cn/api/mcp/mcp` 需 Personal Access Token，不开应用也能用
- **Style Dictionary 是 Amazon 维护**（周下载 177 万），是 token 流转的事实标准；别自己写转换器
- **shadcn copy-in 后追上游**：上游修 bug 你不会自动得到；定期 `shadcn diff` 同步

- **Stitch DESIGN.md（awesome-design-md）不是 DTCG tokens.json**：`frontend-aesthetics-execution` 引来的品牌 DESIGN.md 用 hex + 自定义 YAML front matter（`{colors.primary}` 引用语法），**不能直接喂 Style Dictionary**。转换是手工/脚本活：① 每个 hex 按阶段 3 转 OKLCH；② `colors`/`typography`/`spacing`/`rounded` 的 YAML 重组为 DTCG `$type`/`$value`（color / dimension / typography）；③ `components` composite 段（按钮/卡片）按阶段 2 拆解或限定 Web 用。以 `linear.app/DESIGN.md` 为例：20+ 色 token、13 级 typography、8 级 rounded、8 级 spacing——分钟级手活，无一键工具。`preview.html` 仓库不存在，校验只能读文本。

## 与其他 skill 的分工

- 选组件库 + CSS 架构（Tailwind vs StyleX vs vanilla-extract）→ **frontend-stack-selection**
- 组件实现时取 token → **frontend-feature-development**（阶段 1.2）
- 审查 token 一致性 → **frontend-code-review**
- AI 生成组件的 token 抽取 → **ai-ui-generation-workflow**（阶段 3 步骤 1）

## 参考

- W3C DTCG 2025.10 规范：https://www.designtokens.org/TR/2025.10
