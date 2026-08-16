---
name: design-review-snapshot
description: "把现有前端项目（React/Vue/Tauri SPA）反向导出成 Pixso 设计图供用户审核批注的工程化工作流——生成 HTML 快照、处理 Pixso 95KB 分块与样式内联化、经本地 Pixso MCP code_to_design 导入、审核反馈回流代码。Use when: 用户要把现有前端代码/项目导成设计图审核、把 SPA 反向导入 Pixso、做设计评审快照、给设计/用户看设计稿批注、Tauri 应用导出设计图、问'怎么把现在的界面变成设计图给设计看'、'把代码反向导入 Pixso'时。SKIP: 设计图转代码正向（用 ai-ui-generation-workflow）、选设计工具（用 frontend-stack-selection）、审查已生成代码（用 ai-generated-ui-review）、建 design token（用 design-system-workflow）。"
metadata:
  pattern: inversion + pipeline + gate
  domain: frontend
  composes: [ai-ui-generation-workflow, frontend-feature-development, frontend-aesthetics-execution]
  triggers: [{"event":"UserPromptSubmit","keywords":["导成设计图","导出设计图","反向导入","设计评审快照","变成设计图","给设计看"],"cooldown":600}]
---

# 设计审核快照：前端项目反向导入 Pixso

把现有前端代码反向变成 Pixso 设计图，让用户/设计在 Pixso 里批注审核，反馈回流代码。**核心立场：反向是"评审快照"，不是"代码→设计→代码"循环——导入的是当前代码的静态映射，后续改动以代码为准，定期重新快照。**

## 铁律：反向必须用本地 Pixso MCP

**云端 MCP（`pixso.cn/api/mcp/mcp`）6 个工具全是读取（getCode/getNodeDSL/get_*），没有 `code_to_design`。** 反向导入只能用**本地 MCP**（`http://127.0.0.1:3667/mcp`），需 Pixso 桌面端运行。

→ 开工前确认 Pixso 桌面端已启动并开了 MCP server，否则整个流程跑不通。

## 阶段 0 — 确认范围（Inversion 门控）

**动键盘前必须确认 4 件事，缺一不做：**

1. **项目类型**：纯静态站（落地页/文档）/ Web SPA（React/Vue）/ **Tauri 桌面应用**？决定阶段 1 抓 HTML 的方式
2. **目标页面清单**：要反向哪些路由/窗口？全量还是抽样？（全量反向成本高，建议先关键页面）
3. **Pixso MCP 状态**：本地 MCP server 是否在 `127.0.0.1:3667` 跑通？目标 Pixso 文件 + parentId（挂载节点）准备好了？
4. **审核目的**：设计审视觉 / 验收问题核对 / 给非技术干系人看？决定快照精度（视觉审要 computed style 全保留，验收核对只要布局）

→ 用户没说清就问，不要猜。尤其 Tauri 项目要特别确认能否独立 Web 构建（否则抓 HTML 极难）。

## 阶段 1 — 生成 HTML 快照（Pipeline，按项目类型分）

**所有反向工具只吃静态 HTML，不吃 React/Vue/Tauri 源码。** 先把目标页面变成静态 HTML 文件。

### 1A. 纯静态站 / 文档站
直接抓：每个页面 `curl -sL URL > page.html`，或 `wget --mirror`。

### 1B. Web SPA（React/Vue/Next 等）
用 **Playwright 抓渲染后的 DOM + computed style 内联化**（脚本骨架见 [references/snapshot-script.md](references/snapshot-script.md)）。**前置依赖**：需 Node 环境，首次运行 `npx playwright install chromium` 自动装无头浏览器（需联网下载）；整个 skill 还需 Pixso 桌面端跑本地 MCP（见铁律段），二者缺失则无法反向导入。
- 启动无头浏览器 → 导航到路由 → 等待网络空闲 + 关键元素渲染
- 抓 `document.documentElement.outerHTML`
- **遍历每个元素，把 `getComputedStyle` 的关键属性内联到 `style` 属性**（这一步是质量关键，见阶段 2）
- 每个路由存一个 HTML 文件，命名带序号（`01-home.html`、`02-settings.html`）

⚠️ 别用 `vite build` 直接产出的 `dist/index.html`——SPA 的 dist 是空壳 `<div id="root">`，没渲染内容。必须用 Playwright 跑渲染。

### 1C. Tauri 桌面应用（最难）
Tauri 没有"网页 URL"可抓，三条路：
- **路径 1（推荐）：独立 Web 构建** —— 项目通常能用 `vite build` 跑出 Web 版（Tauri 只是套壳），构建后用 Playwright 抓 `dist/index.html` 的本地服务
- **路径 2：开发模式抓** —— `vite dev` 起本地 server（如 `localhost:1420`），Playwright 抓各窗口对应路由
- **路径 3：webview 截图**（保底）—— Tauri 的 webview 可截图，但拿到的是图片不是可编辑设计图，只能当参考贴入

**门控**：阶段 1 产出的每个 HTML 文件，在浏览器打开能还原原页面的视觉（布局/色值/字体大致对），才进阶段 2。打开是空白/错乱 → 渲染没抓全，回去补等待条件或 computed style。

## 阶段 2 — 处理 Pixso 限制（质量门控）

Pixso MCP `code_to_design` 有硬限制，阶段 1 的原始 HTML 直接丢进去会失败或质量差。逐项处理：

### 2.1 样式内联化（最关键）
`code_to_design` 解析 class-based CSS 靠正则，会丢继承关系（父元素 `color` 子元素不继承、媒体查询失效、伪类丢失）。**把 computed style 内联到每个元素的 `style` 属性**，质量最稳。脚本骨架见 references/snapshot-script.md 的 `inlineComputedStyles`。

代价：HTML 体积膨胀（每个元素都带完整 style），所以要先于 2.2 的分块。

### 2.2 95KB 分块
Pixso MCP 单次请求 >101KB 返回 HTTP 413，推荐 ≤95KB。复杂页面内联化后必超限。用 `pixso_import.py`（jiaweiwei1961/pixso-design-skill）自动分批，或自写分块：
- 单页超限 → 按顶层区域拆成多个 HTML 片段，分次 `code_to_design` 挂到同一 parentId 下不同子节点
- 多页 → 自动分批（~95KB/批），会话过期自动刷新

> **外部仓库 fallback**：`jiaweiwei1961/pixso-design-skill` 是社区 0★ 封装（非官方），用前先验证仓库可用（clone 后跑通一次导入）；不可用走通用路径——自写最小分块脚本（按顶层区域拆 HTML、每片 ≤95KB 分次 `code_to_design` 挂同一 parentId 下不同子节点）或直接 HTTP 调本地 MCP，流程不依赖该仓库。

### 2.3 重复样式去重
内联化后大量重复 `style`（按钮/卡片列表项）。`pixso_import.py` 的 `extract_styles` 自动去重合并成 `<style>` 块 + class 引用，减小体积。自写脚本可参照。

### 2.4 动态内容冻结
快照是某时刻状态——loading 态、空数据、异步未完成的内容要决定抓哪个态。建议抓"典型已加载态"，避免空白骨架。

**门控**：每页 HTML ≤95KB（或已分块），样式全内联或去重，浏览器打开视觉与原页面一致。过了才进阶段 3。

## 阶段 3 — 导入 Pixso（本地 MCP）

确认 Pixso 桌面端运行 + MCP server 在 3667 端口 + 目标文件/parentId 就绪。

### 3A. 单页/少量页面：直接调 `code_to_design`
```bash
# 用 pixso-cli.py（jiaweiwei1961/pixso-design-skill 封装）
python3 scripts/pixso-cli.py import ./01-home.html --parent 100:1
```
或直接 HTTP 调本地 MCP（参数：`html` 字符串或 `zipPath` + `parentId`）。

### 3B. 多页批量：用 pixso_import.py
```bash
python3 scripts/pixso_import.py ./html-pages
# 自动分批 + 会话刷新 + 重试 3 次 + 样式去重 + 页面打标签 + 垂直排列
```

**门控**：导入完成后，必须跑**阶段 3.5 双向 token 校验**（自动化读回 DSL 比对），不能只凭"导入成功"日志或肉眼对比。肉眼会漏细微色差/字距/alpha 偏差。详见阶段 3.5。

## 阶段 3.5 — 双向 token 校验（自动化，必跑）

**这是比"肉眼对比"严格 10 倍的门控**。用 `get_node_dsl` 读回刚导入的节点，与阶段 1 传入 HTML 的 token 值逐项比对，生成精度表。人眼分辨不出的偏差（1 位色差、0.005em 字距、alpha 0.02）会被这一步抓出来。

### 校验维度（逐项对照）

| 维度 | 传入 HTML | 读回 DSL | 判定 |
|---|---|---|---|
| 颜色（背景/文字/border） | `#08090a` / `rgba(255,255,255,0.08)` | `fillPaints.color rgb(8,9,10)` / `strokePaints alpha:0.08` | rgb/alpha 值精确匹配（±1 容差）|
| 字体 + 字号 + 字重 | `Inter 14px weight:500` | `fontFamily:"Inter" fontSize:14 fontWeight:500` | 全匹配 |
| 字距（letter-spacing） | `-0.02em`（32px 时） | `letterSpacing:-0.64`（=32×-0.02）| 换算后精确（容差 ±0.1px）|
| 圆角 | `border-radius:8px` | `cornerRadius:8` | 精确 |
| 阴影 | `0 4px 16px rgba(0,0,0,0.1)` | `DROP_SHADOW offset(0,4) radius:16 color alpha:0.1` | 偏移/半径/颜色全匹配 |
| Flex 布局 | `display:flex gap:20 padding:28` | `autoLayout: VERTICAL, itemSpacing:20, padding:28` | 转 Auto Layout 后值匹配 |
| Grid 布局 | `grid-template-columns:1fr 1fr 1fr` | **无原生对应**，转绝对定位 Frame | ⚠️ **已知弱点**，见下 |
| 文字内容（中文/数字/符号） | nodeText 原文 | `nodeText` 读回 | 精确（含中文）|
| 光晕/glow | `box-shadow:0 0 8px rgba(94,106,210,0.6)` | `DROP_SHADOW radius:8 offset(0,0) color(94,106,210,0.6)` | **连光晕都能还原**（实测）|

### 操作步骤

1. 记录阶段 1 传入 HTML 的关键 token（色值/字体/字距/阴影/圆角）—— 建议直接从 HTML 里 grep 出 `style` 里的值
2. 调 `get_node_dsl`（不传 guid，用当前选中）读回刚导入的节点树
3. 逐项填精度表（传入 vs 读回 vs 判定）
4. 容差外的项 → 回阶段 1/2 调快照（通常根因是 computed style 没内联全）

### 已知弱点（Grid，诚实告知）

CSS `grid-template-columns` 在 Pixso **无原生对应**，会被转成绝对定位的并列 Frame（left:1 / 312 / 622）。视觉一致但编辑时不能像 Auto Layout 自动重排。**判定策略**：Grid 布局只校验"视觉位置一致"（各 Frame 的 top/left 间距均匀），不要求"是真正的网格"——这是 Pixso 的结构性限制，非快照质量问题。

### 通过标准

- 颜色/字体/字距/阴影/圆角/文字内容：**全部在容差内**
- Grid 布局：视觉位置一致即可（接受绝对定位转化）
- 任一关键维度超容差 → 不进阶段 4 审核，回阶段 1/2 修

→ 完整脚本化比对清单见 [references/snapshot-verify.md](references/snapshot-verify.md)

## 阶段 4 — 用户审核 + 反馈回流

### 4.1 组织审核
- 导入的页面在 Pixso 里垂直排列、带标签（`01-Home`/`02-Settings`）
- 告知审核者：这是**当前代码的静态快照**，不是设计提案——请批注"哪里不对/哪里要改"
- 用 Pixso 的批注/评论功能收集反馈

### 4.2 反馈回流代码
- 每条批注 → 转成代码改造任务（不是改设计图，是改源码）
- 改造完成后 → 重新跑阶段 1-3 出新快照 → 下一轮审核
- **循环终止条件**：审核者确认"代码现状符合预期"，不是"设计图改漂亮了"

### 4.3 快照版本管理
每次快照打 tag（`snapshot-2026-06-21-pre-redesign`），保留历史供对比。Pixso 文件用版本/分支管理，避免覆盖。

## Common Rationalizations（堵借口）

| 借口 | 现实 |
|---|---|
| "用云端 MCP 方便，不用开 Pixso" | 云端 6 工具全是 get，没有 code_to_design；反向必须本地 MCP + 桌面端运行 |
| "直接把 TSX 扔进 MCP" | code_to_design 只吃 HTML/ZIP，不认 React/Vue；必须先 Playwright 抓静态 HTML |
| "vite build 的 dist 直接导" | SPA 的 dist 是空 `<div id="root">`，没渲染内容；必须无头浏览器跑渲染 |
| "一次性导整个应用" | 95KB 限制 + 会话超时；分批 + 分页，关键页面优先 |
| "导入成功就好了" | 日志成功 ≠ token 还原；必须跑阶段 3.5 双向校验（get_node_dsl 读回比对），肉眼还会漏细微色差/字距 |
| "设计图改好再导回代码" | 反向是快照评审，不是双向同步；改代码 + 重新快照，不要在设计图上改 |
| "抓 loading 态也行" | 动态内容冻结要选典型态；空骨架/加载中没审核价值 |

## Red Flags（我在 rationalize 的信号）

- 没确认 Pixso 桌面端开没开 MCP 就开工
- 用云端 MCP 配置尝试反向（会失败）
- 跳过 Playwright 直接用 vite build 产物
- 不做 computed style 内联化（质量会塌）
- 导入后不跑阶段 3.5 双向校验就交付审核
- 把设计图当"正确答案"改，忘了它是代码的快照

## Gotchas

- **云端 MCP 无反向能力**：6 工具（getCode/getNodeDSL/get_local_styles/get_variable_sets/get_variables/get_variants）全是读取；`code_to_design` 只在本地 18 工具集。这是 Pixso MCP 设计取舍——云端只读保安全，写入要在本地受控环境
- **95KB 是 HTTP body 上限**：含 base64 图片/SVG 的页面极易超限；先剥离大图（用占位）或分块
- **`getComputedStyle` 内联化的伪类陷阱**：`:hover`/`:focus`/`:active` 的样式抓不到（只能抓当前态），快照是"默认态"，交互态要单独抓或人工补
- **Flexbox/Grid 转 Pixso**：Pixso 的 Auto Layout 对标 Flex，但 Grid 支持弱；复杂 Grid 布局导入后可能错位，需手动调
- **字体替换**：快照里的 web font 在 Pixso 可能缺失，导入前确认字体已安装或可替换
- **Tauri 项目要先验证 Web 构建可行性**：有些 Tauri 项目深度依赖 Rust 后端 IPC，Web 构建后功能缺失（如文件系统/通知），快照只能反映 UI 静态态——跟用户说清这点
- **会话过期**：本地 MCP session 有时效，批量导入时 `pixso_import.py` 自动刷新（401 触发），自写脚本要处理
- **parentId 错位**：导入挂错节点会把页面塞进错误的 Frame，导入前确认 parentId 是目标画布/Frame

## 与其他 skill 的分工

- **正向（设计→代码）** → `ai-ui-generation-workflow`（含 Pixso MCP 正向 18 工具 + Figma Builder.io）
- **反向审核反馈后的代码改造** → `frontend-feature-development`（按批注改代码时遵守生成规范）
- **改造涉及视觉风格调整** → `frontend-aesthetics-execution`
- **改造完提交前审查** → `frontend-code-review`
- **token 体系同步** → `design-system-workflow`

## 参考

- Playwright 抓 SPA + computed style 内联化脚本骨架：[references/snapshot-script.md](references/snapshot-script.md)
- 95KB 分块 + 样式去重 + 动态内容冻结细节：[references/snapshot-checklist.md](references/snapshot-checklist.md)
- Pixso MCP 本地工具 API（code_to_design 等）：jiaweiwei1961/pixso-design-skill `references/api-reference.md`
- 批量导入脚本：jiaweiwei1961/pixso-design-skill `scripts/pixso_import.py`
- **完整脚本化比对清单**：[references/snapshot-verify.md](references/snapshot-verify.md)（阶段 3.5 的详细操作 + 已知 Grid 弱点处理）
