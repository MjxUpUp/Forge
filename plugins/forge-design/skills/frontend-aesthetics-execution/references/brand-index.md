# 品牌 DESIGN.md 实例库：发现方式、用法与完整索引（74，按领域；slug 即目录名）

SKILL.md 阶段 1.5 的完整参考材料。主文件只留 pattern ↔ instance 的用法说明，细节全部在此。

## 品牌资产库与发现方式

**品牌资产库**：VoltAgent/awesome-design-md 仓库的 `design-md/<slug>/DESIGN.md`（74 个真实品牌，`git pull` 更新——引用路径而非搬文件）。发现方式：环境变量 `DESIGN_MD_ROOT` 指向仓库克隆根，或查找当前工作区/常用代码目录下的 `awesome-design-md`；未克隆时先 `git clone https://github.com/VoltAgent/awesome-design-md`；仓库不可用或品牌未命中时 fallback 到 SKILL.md 阶段 1 的 6 种通用风格模板。

## 用法

1. 用户点名品牌 → 列 `design-md/` 目录拿 **slug**（注意带点 / 连字符的目录名），或查下方完整索引 → `Read` 该 `DESIGN.md`
2. 从其 front matter 取 `colors`（hex）/ `typography`（px + weight + letterSpacing）/ `rounded` / `spacing` / `components` 的精确值
3. 套 SKILL.md 阶段 1 对应模板的**结构**，用 DESIGN.md 的精确值替换模板占位值
4. hex → OKLCH 转换走 **design-system-workflow**（见下方格式 gap）

## 6 模板 ← 代表品牌

| SKILL.md 阶段 1 模板 | 代表品牌（slug） |
|---|---|
| A 暗色精致 SaaS | `linear.app` · `vercel` · `raycast` · `cursor` · `superhuman` · `warp` · `resend` |
| B Liquid Glass | `apple` |
| C Neo-brutalism | `dell-1996` · `nintendo-2001` |
| D Bento 模块化 | `meta` · `playstation` |
| E Dopamine 高饱和 | `spotify` · `binance` · `figma` · `slack` |
| F Human Touch 编辑暖色 | `notion` · `stripe` · `airbnb` · `cal` · `mastercard` |

> 未命中 6 模板的品牌（`ferrari`/`bugatti`/`lamborghini` 极致黑金汽车、`wired`/`theverge` 编辑媒体、`ibm` Carbon 等）直接 `Read` DESIGN.md 取值，**不强行套模板**。

## 格式 gap（DESIGN.md 不能直接当 token 消费）

- DESIGN.md front matter 是 **hex + 自定义 YAML**（`colors`/`typography`/`rounded`/`spacing`/`components`，用 `{colors.primary}` 引用），**不是 DTCG tokens.json**——不能直接喂 Style Dictionary
- hex → **OKLCH** 转换、YAML → DTCG `$type/$value` 重组走 **design-system-workflow** 阶段 3。以 `linear.app/DESIGN.md` 为例：20+ 色 token、13 级 typography、8 级 rounded、8 级 spacing——分钟级手活，无一键工具；`components` composite 段（按钮/卡片）按 design-system-workflow 阶段 2 拆解或限定 Web 用
- `preview.html` / `preview-dark.html`：README 声称每个站点有，**仓库实际不存在**（只在 getdesign.md 网站）——只能用 DESIGN.md 文本，别声称能看预览
- 字体多为品牌私有（Linear Display / Stripe Sans / Apple SF Pro）——DESIGN.md 自带 fallback 与开源替代（Inter / Geist），用替代值即可

## 完整品牌索引（74，按领域）

来源：VoltAgent/awesome-design-md 仓库 `design-md/<slug>/DESIGN.md`；也可以直接列 `design-md/` 目录拿 slug（slug 即目录名）。

- **AI & LLM**：`claude` · `cohere` · `elevenlabs` · `minimax` · `mistral.ai` · `ollama` · `opencode.ai` · `replicate` · `runwayml` · `together.ai` · `voltagent` · `x.ai`
- **Developer Tools**：`cursor` · `expo` · `lovable` · `raycast` · `superhuman` · `vercel` · `warp`
- **Backend / DB / DevOps**：`clickhouse` · `composio` · `hashicorp` · `mongodb` · `posthog` · `sanity` · `sentry` · `supabase`
- **Productivity / SaaS**：`cal` · `intercom` · `linear.app` · `mintlify` · `notion` · `resend` · `slack` · `zapier`
- **Design / Creative**：`airtable` · `clay` · `figma` · `framer` · `miro` · `webflow`
- **Fintech / Crypto**：`binance` · `coinbase` · `kraken` · `mastercard` · `revolut` · `stripe` · `wise`
- **E-commerce**：`airbnb` · `meta` · `nike` · `shopify` · `starbucks`
- **Media / Consumer**：`apple` · `hp` · `ibm` · `nvidia` · `pinterest` · `playstation` · `spacex` · `spotify` · `theverge` · `uber` · `vodafone` · `wired`
- **Automotive**：`bmw` · `bmw-m` · `bugatti` · `ferrari` · `lamborghini` · `renault` · `tesla`
- **Retro Web**：`dell-1996` · `nintendo-2001`
