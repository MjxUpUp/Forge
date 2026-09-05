# Pixso 反向导入检查清单（95KB 分块 / 样式处理 / 内容冻结）

本文件是 `design-review-snapshot` 阶段 2 的细节。阶段 1 产出的原始 HTML 不能直接丢进 Pixso MCP，必须先过以下处理。

## 目录

- [处理流程（顺序不可换）](#处理流程顺序不可换)
- [2.1 computed style 内联化（质量关键）](#21-computed-style-内联化质量关键)
- [2.2 重复样式去重](#22-重复样式去重)
- [2.3 95KB 分块](#23-95kb-分块)
- [2.4 动态内容冻结](#24-动态内容冻结)
- [处理后自检清单（进阶段 3 前必过）](#处理后自检清单进阶段-3-前必过)
- [常见坑](#常见坑)

## 处理流程（顺序不可换）

```
原始 HTML（阶段 1 产出）
  ↓ 2.1 computed style 内联化（体积膨胀，但质量最稳）
  ↓ 2.2 重复样式去重（把高频重复的内联 style 抽成 class + <style> 块）
  ↓ 2.3 95KB 分块（单页超限按区域拆）
  ↓ 2.4 动态内容冻结确认（选对状态）
处理后的 HTML（阶段 3 导入 Pixso）
```

**顺序为什么不能换**：内联化必须最先（保证质量基线）→ 去重压缩体积（为分块做准备）→ 分块处理超限 → 内容冻结是人工确认。换顺序会导致质量塌或返工。

---

## 2.1 computed style 内联化（质量关键）

### 为什么必须做
`code_to_design` 解析 class-based CSS 靠正则提取 `<style>` 块 + 匹配 class。这套机制会丢：
- **继承关系**：父元素 `color: red`，子元素没显式声明 → 子元素在 Pixso 里变黑色
- **媒体查询**：`@media (max-width: 768px)` 整段失效
- **伪类**：`:hover` `:focus` `:active` `::before` `::after` 全丢（只能抓当前态）
- **CSS 变量**：`var(--color-primary)` 在 Pixso 侧无定义 → 解析失败
- **级联优先级**：`!important` 和 specificity 计算在正则解析里不可靠

内联化（把 `getComputedStyle` 结果写进每个元素的 `style`）绕开所有这些问题——每个元素自带最终生效的样式值。

### 代价
HTML 体积膨胀 3-10 倍（每个元素带完整 style）。所以必须配合 2.2 去重 + 2.3 分块。

### 脚本见 [snapshot-script.md](snapshot-script.md) 的 `inlineComputedStyles` 函数。

### 属性白名单（控制膨胀）
全保留所有 CSS 属性会让 HTML 巨大且 Pixso 解析慢。只保留视觉相关属性（snapshot-script.md 里的 `VISUAL_PROPS`）：
- 布局：display/position/flex/grid/margin/padding/width/height
- 视觉：background/color/border/border-radius/box-shadow/opacity
- 文字：font-*/line-height/letter-spacing/text-*
- 溢出：overflow/white-space/text-overflow

跳过：`auto`/`normal`/`none`/`0`/`0px` 等默认值。

---

## 2.2 重复样式去重

### 场景
列表项（`<li>` 卡片）、按钮（`<button>`）、表单项——这些元素内联化后 style 完全相同，重复几十次。

### 处理
`pixso_import.py` 的 `extract_styles` 自动做：
1. 扫描所有元素的 `style` 属性
2. 相同 style 串 hash 去重
3. 生成 `<style>` 块，每个唯一 style 分配一个 class（如 `.s1`/`.s2`）
4. 元素的 `style` 替换成 `class="s1"`

自写脚本可参照实现，或直接用 `pixso_import.py`（它已含此逻辑）。

### 效果
典型列表页去重后体积降 40-70%，配合分块基本能压进 95KB。

---

## 2.3 95KB 分块

### 硬限制（实测）
| 阈值 | 值 |
|---|---|
| 安全上限 | ~99 KB |
| 推荐值 | 95 KB |
| 失败阈值 | 101 KB（返回 HTTP 413 PayloadTooLargeError） |

### 分块策略

**多页场景**（推荐用 `pixso_import.py`，自动处理）：
- 脚本扫目录 → 累加文件大小 → 满 95KB 切一批 → 调 MCP 导入这批 → 下一批
- 会话过期（HTTP 401）自动刷新 session 重试，最多 3 次

**单页超限**（需自写逻辑）：
- 按顶层区域拆：`body > *` 的每个直接子元素单独成块
  ```
  <body>
    <header>  → part0.html
    <main>    → part1.html（可能还超，再按 main > * 拆）
    <footer>  → part2.html
  </body>
  ```
- 每块单独调 `code_to_design`，`parentId` 指向目标 Frame 的不同子节点
- 导入后在 Pixso 里手动拼回（或接受分层结构）

**极端情况**（单区域仍超限）：
- 剥离大图：`<img src="data:base64...">` 换成占位符 `<img src="placeholder.png">`
- 剥离 SVG：复杂 SVG 导出单独文件，导入后手动贴回
- 拆 DOM 树：按子树深度递归拆，直到每块 < 95KB

### 自写分块脚本骨架
```python
MAX_SIZE = 95000

def chunk_page(html_str: str, name: str) -> list[tuple[str, str]]:
    """返回 [(part_name, html), ...] 每块 < 95KB"""
    if len(html_str.encode('utf-8')) <= MAX_SIZE:
        return [(name, html_str)]
    
    # 按 body 直接子元素拆
    import re
    body_match = re.search(r'<body[^>]*>(.*?)</body>', html_str, re.DOTALL)
    if not body_match:
        return [(name, html_str)]  # 拆不动，原样返回（导入会失败，人工介入）
    
    children = re.findall(r'<(\w+)[^>]*>.*?</\1>', body_match.group(1), re.DOTALL)
    parts = []
    for i, child_html in enumerate(children):
        part = f'<body>{child_html}</body>'
        parts.append((f'{name}_part{i}', part))
    return parts
```

---

## 2.4 动态内容冻结

### 问题
快照是某时刻状态。SPA 有多种态，抓哪个决定审核看到什么：
- loading 骨架（无审核价值）
- 空数据（适合审"空状态设计"）
- 错误态（适合审"错误处理"）
- 已加载典型数据（最常用，审主流程）
- 满数据/极端数据（审边界/性能）

### 默认抓"典型已加载态"
```typescript
// Playwright 等 networkidle + 额外 1.5s，确保异步完成
await page.goto(url, { waitUntil: 'networkidle' });
await page.waitForTimeout(1500);

// 可选：mock 数据保证一致性（避免抓到随机数据）
await page.route('**/api/**', route => {
  route.fulfill({ json: FIXED_TEST_DATA });
});
```

### 多态审核
如果要审多个态，每个态单独抓一份：
```typescript
const STATES = ['loading', 'loaded', 'empty', 'error'];
for (const state of STATES) {
  await mockState(page, state);  // 你的 mock 逻辑
  await snapshot(page, `${name}-${state}`);
}
```
导入 Pixso 后同一路由的多个态并排，审核者可对比。

### 干扰元素清理
抓前移除会挡住主内容的元素：
```typescript
await page.evaluate(() => {
  document.querySelectorAll('.modal-overlay, .tooltip, .toast').forEach(el => el.remove());
});
```

---

## 处理后自检清单（进阶段 3 前必过）

| 检查项 | 方法 | 不过的后果 |
|---|---|---|
| 视觉还原 | 浏览器打开 html，对比原页面 | Pixso 里错位/丢色 |
| 无空壳 | html 有实际 DOM 内容，不是 `<div id="root">` | Pixso 里空白 |
| 单文件 ≤95KB | `wc -c *.html` | 导入返回 413 |
| 样式无外部依赖 | 断网打开 html 仍正常显示 | Pixso 解析失败（class 无定义） |
| 无残留动态态 | 没有 loading 骨架/空数据（除非要审） | 审核者看到无意义状态 |
| 字体可用 | html 不依赖未安装的 web font | Pixso 字体替换错乱 |
| 图片可访问 | `<img>` 是 base64 或绝对 URL | Pixso 里破图 |

---

## 常见坑

- **`getComputedStyle` 抓不到伪类**：`:hover` 等交互态只能抓当前默认态。要审交互态需手动触发（`await page.hover('.btn')` 再抓）或单独标注
- **Flexbox 转 Pixso Auto Layout 大致对， Grid 支持弱**：复杂 Grid 导入后可能错位，Pixso 的 Auto Layout 不完全等价 CSS Grid
- **`box-shadow` 多层在 Pixso 里可能合并/丢失**：Vercel 风格的 stacked shadow（多层 4-12% black）导入后可能变单层
- **`backdrop-filter`（glassmorphism）在 Pixso 里无等价**：模糊效果导入后消失，需手动补
- **OKLCH/color-mix 颜色**：getComputedStyle 返回的是浏览器解析后的 rgb/rgba（不是原始 oklch），导入 Pixso 是 rgb 值——色彩管理上会丢宽色域，但视觉一致
- **base64 大图撑爆体积**：单个 hero 图 base64 可能 200KB+，必须剥离或压缩
