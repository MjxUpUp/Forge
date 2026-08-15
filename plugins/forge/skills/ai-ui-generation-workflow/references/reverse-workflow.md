# 反向路径：代码 → 设计图供审核

> 本文件是 `ai-ui-generation-workflow` 模板 D 的反向路径细节，从 SKILL.md 拆出（progressive disclosure）。Pixso 反向的端到端 SOP（含 token 双向校验）见 `design-review-snapshot`。

双向能力**真实存在但有硬限制**，常用于把现有前端反向导入设计工具供用户评审/批注。

## 按设计工具选反向工具

| 设计工具 | 反向工具 | 输入 | 质量 |
|---|---|---|---|
| **Figma** | **html.to.design**（Builder.io 旗下，付费插件最成熟）或 **Builder.io HTML to Design** | URL / HTML / Chrome 扩展捕获 | 多 viewport + dark/light 主题，近原品质 |
| **Figma（开源）** | **Yueyin-Tql/htmlToFigma**（MCP server，对标 html.to.design） / **sergcen/html-to-figma** / **Floristeady/html-to-figma**（含 Cursor 集成） | URL / HTML/CSS，Puppeteer 渲染 | Flexbox → Auto Layout，SVG/字体自动转 |
| **Pixso** | **官方 Pixso MCP `code_to_design`**（本地 18 工具之一） + `pixso_import.py` 批量脚本（出自外部仓库 jiaweiwei1961/pixso-design-skill 的 `scripts/pixso_import.py`） | HTML 字符串 / HTML 目录 / ZIP | 需处理 95KB 分块，输出扁平图层 |

> **外部仓库 fallback**：`jiaweiwei1961/pixso-design-skill` 是社区 0★ 封装（非官方，2026-08 快照），用前先验证仓库可用；不可用走通用路径——自写最小分块脚本（按顶层区域拆 HTML、每片 ≤95KB 分次 `code_to_design` 挂同一 parentId 下不同子节点）或直接 HTTP 调本地 MCP，流程不依赖该仓库。

## 通用硬限制（反向路径的核心约束）

1. **只吃 HTML，不吃 React/Vue 源码**——反向工具全部要求静态 HTML。React/TSX 组件必须先 build 成静态 HTML 才能导入。
   - 纯静态页（落地页/文档站）：直接抓 HTML 顺畅
   - 动态 SPA：先用 Playwright/Puppeteer 跑各路由抓快照，或 `vite build` + 静态渲染
   - **Tauri 桌面应用（如 DevWorkBench 类）最麻烦**：要用 Tauri webview 截图或独立 Web 构建再抓 HTML
2. **Pixso MCP 请求大小限制 ~95KB**（超 101KB 返回 HTTP 413）——复杂页面需 `pixso_import.py`（外部仓库 jiaweiwei1961/pixso-design-skill 的 `scripts/pixso_import.py`）自动分批（会话过期自动刷新 + 重试 3 次）；仓库不可用时按上方 fallback 自写分块
3. **转换质量取决于 HTML 语义化**——内联 style 最准；class-based CSS 靠正则提取会丢部分继承关系
4. **输出是扁平化图层，不是交互组件**——导入后是 Frame/Text/Rectangle，不是设计工具的 Component/Variant，要手动封装才可复用

## 完整反向工作流（React/Tauri 项目 → 设计图审核）

```
现有 React/TSX 代码
  ↓ vite build + 静态渲染（或 Playwright 抓路由 HTML）
静态 HTML 文件（每路由一个）
  ├─ Figma: html.to.design 插件粘贴 URL/HTML，或 htmlToFigma MCP
  └─ Pixso: pixso_import.py ./html-pages（jiaweiwei1961/pixso-design-skill `scripts/pixso_import.py`，自动分批+打标签+垂直排列；不可用按 fallback 自写分块）
设计工具设计稿（可编辑）
  ↓ 用户在设计工具里批注/审核/调整细节
审核反馈 → 代码改造
```

**关键判定**：反向是"评审/快照"用途，不是"代码→设计→再回代码"的循环。导入的设计图是当前代码的静态映射，后续改动仍以代码为准、定期重新快照重新导入。
