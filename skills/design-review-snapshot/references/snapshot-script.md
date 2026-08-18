# Playwright 抓 SPA + computed style 内联化脚本骨架

本文件是 `design-review-snapshot` 阶段 1B/1C 的脚本骨架。直接 copy 改路由配置即可用。

## 目录

- [依赖](#依赖)
- [完整脚本（snapshot-script.ts）](#完整脚本snapshot-scriptts)
- [使用](#使用)
- [进阶处理（按需加）](#进阶处理按需加)
- [质量自检（导入 Pixso 前必过）](#质量自检导入-pixso-前必过)

## 依赖

```bash
npm i -D playwright
npx playwright install chromium
```

## 完整脚本（snapshot-script.ts）

```typescript
import { chromium } from 'playwright';
import * as fs from 'fs/promises';
import * as path from 'path';

// === 配置：改成你的项目 ===
const BASE_URL = 'http://localhost:1420';  // vite dev 或本地静态 server
const OUTPUT_DIR = './html-pages';          // 输出目录（导入 Pixso 前的中间产物）
const ROUTES: Array<[string, string]> = [
  // [路由路径, 输出文件名（带序号，Pixso 导入后做页面标签）]
  ['/', '01-home'],
  ['/settings', '02-settings'],
  ['/chat', '03-chat'],
  // Tauri 项目：如果是多窗口而非多路由，改成抓各窗口的 URL
];

const WAIT_UNTIL: 'networkidle' = 'networkidle';  // SPA 等 networkidle 确保异步完成
const RENDER_SETTLE_MS = 1500;                     // networkidle 后再等 1.5s 让动画/字体稳定

async function snapshot() {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },   // 桌面应用默认尺寸；移动端改 375x812
    deviceScaleFactor: 2,                       // 高清截图，字体/边框更清晰
  });

  await fs.mkdir(OUTPUT_DIR, { recursive: true });

  for (const [route, name] of ROUTES) {
    const page = await context.newPage();
    console.log(`[${name}] 抓取 ${route} ...`);

    try {
      await page.goto(`${BASE_URL}${route}`, { waitUntil: WAIT_UNTIL, timeout: 30000 });
      await page.waitForTimeout(RENDER_SETTLE_MS);

      // 可选：关闭 modal/tooltip 等干扰元素
      // await page.evaluate(() => {
      //   document.querySelectorAll('.modal-overlay, .tooltip').forEach(el => el.remove());
      // });

      // 核心：computed style 内联化（见下方函数）
      await page.evaluate(inlineComputedStyles);

      // 抓处理后的 HTML
      const html = await page.content();
      const outPath = path.join(OUTPUT_DIR, `${name}.html`);
      await fs.writeFile(outPath, html, 'utf-8');

      const sizeKB = Math.round(Buffer.byteLength(html, 'utf-8') / 1024);
      const flag = sizeKB > 95 ? ' ⚠️ 超 95KB 需分块' : '';
      console.log(`  ✓ ${outPath} (${sizeKB} KB${flag})`);
    } catch (e) {
      console.error(`  ✗ ${name} 失败:`, e instanceof Error ? e.message : e);
    } finally {
      await page.close();
    }
  }

  await browser.close();
  console.log(`\n完成。输出在 ${OUTPUT_DIR}/，下一步用 pixso_import.py 导入。`);
}

/**
 * 关键函数：把每个元素的 computed style 内联到 style 属性。
 * 必须在浏览器内执行（page.evaluate），拿得到 getComputedStyle。
 *
 * 为什么这么做：code_to_design 解析 class CSS 靠正则，会丢继承。
 * 内联化后每个元素自带完整样式，Pixso 解析最准。
 */
const inlineComputedStyles = () => {
  // 只保留视觉相关属性（全保留会让 HTML 膨胀过头）
  const VISUAL_PROPS = [
    'display', 'position', 'top', 'right', 'bottom', 'left', 'z-index',
    'width', 'height', 'min-width', 'max-width', 'min-height', 'max-height',
    'margin', 'padding', 'border', 'border-radius',
    'flex-direction', 'flex-wrap', 'justify-content', 'align-items', 'align-self',
    'flex', 'flex-grow', 'flex-shrink', 'flex-basis', 'gap',
    'grid', 'grid-template', 'grid-area', 'grid-column', 'grid-row',
    'background', 'background-color', 'background-image',
    'color', 'font-family', 'font-size', 'font-weight', 'font-style',
    'line-height', 'letter-spacing', 'text-align', 'text-decoration',
    'box-shadow', 'opacity', 'transform',
    'overflow', 'white-space', 'text-overflow',
  ];

  const elements = document.querySelectorAll('*');
  elements.forEach((el) => {
    const computed = window.getComputedStyle(el);
    const styleParts: string[] = [];
    VISUAL_PROPS.forEach((prop) => {
      const value = computed.getPropertyValue(prop);
      // 跳过默认值/空值，减小体积
      if (value && value !== 'auto' && value !== 'normal' && value !== 'none' && value !== '0' && value !== '0px') {
        styleParts.push(`${prop}: ${value}`);
      }
    });
    if (styleParts.length) {
      (el as HTMLElement).setAttribute('style', styleParts.join('; '));
    }
    // 清掉 class（已内联，class 在 Pixso 侧无用且干扰）
    el.removeAttribute('class');
  });

  // 把 <style> 块和 <link rel=stylesheet> 移除（样式已内联，避免重复解析）
  document.querySelectorAll('style, link[rel="stylesheet"]').forEach((n) => n.remove());
};

snapshot().catch(console.error);
```

## 使用

```bash
# 1. 起你的项目（vite dev 或静态 server）
npm run dev  # 确认 BASE_URL 对应端口对

# 2. 改脚本顶部的 ROUTES 配置（你要抓的路由）

# 3. 跑
npx tsx snapshot-script.ts

# 4. 检查输出
# - 每个文件 < 95KB（超了的标 ⚠️，进分块流程）
# - 浏览器打开 html-pages/01-home.html 视觉与原页面一致

# 5. 导入 Pixso（确认桌面端 MCP 在跑）
python3 /path/to/pixso-design-skill/scripts/pixso_import.py ./html-pages
```

## 进阶处理（按需加）

### 分块（单页超 95KB）
```typescript
// 在 inlineComputedStyles 后，如果 html 超 95KB，按顶层区域拆：
if (Buffer.byteLength(html) > 95000) {
  const sections = await page.evaluate(() => {
    // 按主要 section 拆（如 header/main/footer 或 [data-section]）
    const roots = document.querySelectorAll('body > *');
    return Array.from(roots).map((el, i) => ({
      index: i,
      html: `<body>${(el as HTMLElement).outerHTML}</body>`,
    }));
  });
  // 每个 section 单独存一个文件，导入时挂到同一 parentId 不同子节点
  for (const s of sections) {
    await fs.writeFile(path.join(OUTPUT_DIR, `${name}_part${s.index}.html`), s.html);
  }
}
```

### 多状态快照（loading / loaded / error）
```typescript
const STATES = [
  { name: 'loading', setup: async (page) => { /* 拦截 API 让它 pending */ } },
  { name: 'loaded', setup: async (page) => { /* 正常加载 */ } },
  { name: 'empty', setup: async (page) => { /* mock 空数据 */ } },
];
// 每个 state 抓一份，方便审核不同态
```

### Tauri 项目特殊处理
Tauri 没有 URL，要先确认能独立 Web 构建：
```bash
# 如果 vite.config 独立可用
npm run build  # 产出 dist/
npx vite preview --port 4173  # 起静态 server
# 然后改 BASE_URL = 'http://localhost:4173'，正常跑脚本
```
如果 Tauri 深度依赖 Rust IPC（文件系统/通知/系统调用），Web 构建会缺功能——抓出来的快照只能反映"静态 UI 壳"，需告知审核者这点限制。

## 质量自检（导入 Pixso 前必过）

| 检查项 | 方法 |
|---|---|
| 视觉还原 | 浏览器打开 html，对比原页面，布局/色值/字体一致 |
| 无空白内容 | 没有未渲染的 `<div id="root">` 空壳 |
| 文件大小 | 单文件 ≤95KB（超了分块） |
| 样式内联 | 打开 html 查看源码，元素带 `style` 属性，无外部 CSS 依赖 |
| 无动态残留 | 没有loading 骨架/空数据态（除非要审这些） |
