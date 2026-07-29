# 双向 Token 校验脚本化清单（阶段 3.5）

本文件是 `design-review-snapshot` 阶段 3.5 的详细操作手册。阶段 3 导入成功后必跑，替代/强化"肉眼对比"。

## 为什么需要这一步

阶段 3 的"导入成功"日志只说明 Pixso 收到了 HTML 并生成了节点，**不保证 token 值被精确还原**。常见失真：

- CSS 继承关系丢失（父元素 `color` 子元素变黑）
- alpha 通道浮点漂移（0.08 → 0.07999...）
- letter-spacing 单位换算错误
- Grid 布局降级为绝对定位
- 光晕/多层阴影合并或丢失

**肉眼对比的盲区**（必须用 `get_node_dsl` 自动读回）：
- 1 位色差（`#08090a` vs `#090a0b`）肉眼难辨
- 0.005em 字距偏差（32px 时 = 0.16px）肉眼难辨
- hairline border alpha 0.08 vs 0.06 肉眼难辨

## 操作步骤

### 1. 提取传入 HTML 的 token 基线

从阶段 1 产出的 HTML 里 grep 关键 token（建议脚本化）：

```bash
# 从 HTML 提取所有内联 style 的 token 值
grep -oE '(color|background-color|border|box-shadow|font-family|font-size|font-weight|letter-spacing|border-radius):\s*[^;"]+' page.html \
  | sort -u > /tmp/baseline_tokens.txt
```

### 2. 读回 DSL

调用 Pixso MCP（不传 guid，读当前选中节点）：

```
get_node_dsl({})  # 读回刚导入的节点树
```

返回 JSON 含完整节点树：`fillPaints` / `strokePaints` / `effects` / `fontFamily` / `fontSize` / `letterSpacing` / `cornerRadius` / `autoLayout` / `nodeText` 等。

### 3. 逐项填精度表

对照基线与读回值，按维度判定（容差见下）：

| 维度 | HTML 基线示例 | DSL 读回示例 | 容差 |
|---|---|---|---|
| 背景色 | `#08090a` | `fillPaints[0].color: {r:8,g:9,b:10}` | rgb 各通道 ±1 |
| 文字色 | `#e8e8f0` | `fillPaints[0].color: {r:232,g:232,b:240}` | ±1 |
| Border | `rgba(255,255,255,0.08)` | `strokePaints[0].color.alpha:0.0799...` | alpha ±0.01 |
| 字体 | `Inter` | `fontFamily:"Inter"` | 精确匹配 |
| 字号 | `14px` | `fontSize:14` | 精确 |
| 字重 | `font-weight:500` | `fontWeight:500` | 精确 |
| 字距 | `letter-spacing:-0.02em`（32px 时） | `letterSpacing:-0.64` | 换算后 ±0.1px |
| 圆角 | `border-radius:8px` | `cornerRadius:8` | 精确 |
| 阴影偏移 | `box-shadow:0 4px ...` | `effects[0].offset:{x:0,y:4}` | 精确 |
| 阴影半径 | `...16px...` | `effects[0].radius:16` | 精确 |
| 阴影颜色 alpha | `rgba(0,0,0,0.1)` | `effects[0].color.alpha:0.1` | ±0.01 |
| Flex gap | `gap:20` | `autoLayout.itemSpacing:20` | 精确 |
| Flex padding | `padding:28` | `autoLayout.paddingTop/Bottom/Left/Right:28` | 精确 |
| 文字内容 | `Agent 工作流总览` | `nodeText:"Agent 工作流总览"` | 精确（含中文） |
| Glow（光晕） | `box-shadow:0 0 8px rgba(94,106,210,0.6)` | `effects[0]:DROP_SHADOW offset(0,0) radius:8 color(94,106,210,0.6)` | 全匹配 |

### 4. 自动化比对脚本（可选，推荐）

```python
import json, re

def parse_html_tokens(html: str) -> dict:
    """从 HTML style 提取 token"""
    tokens = {'colors': set(), 'fonts': set(), 'sizes': set(), 'shadows': set()}
    for m in re.finditer(r'style="([^"]+)"', html):
        style = m.group(1)
        for cm in re.finditer(r'(?:background-)?color:\s*(#[0-9a-fA-F]{3,8}|rgba?\([^)]+\))', style):
            tokens['colors'].add(cm.group(1))
        for fm in re.finditer(r'font-family:\s*([^;]+)', style):
            tokens['fonts'].add(fm.group(1).split(',')[0].strip().strip("'\""))
        for sm in re.finditer(r'box-shadow:\s*([^;]+)', style):
            tokens['shadows'].add(sm.group(1))
    return tokens

def parse_dsl_colors(dsl: dict) -> set:
    """从 DSL 递归提取所有颜色"""
    colors = set()
    def walk(node):
        for paint in node.get('fillPaints', []) or []:
            c = paint.get('color', {})
            colors.add((c.get('r'), c.get('g'), c.get('b'), round(c.get('a', 1), 2)))
        for child in node.get('childNode', []) or []:
            walk(child)
    for root in dsl.get('pixTreeNodes', []):
        walk(root)
    return colors

# 主流程
html_tokens = parse_html_tokens(open('page.html').read())
dsl = json.loads(open('dsl.json').read())
dsl_colors = parse_dsl_colors(dsl)

# 比对（rgb 容差 ±1）
def color_match(html_color, dsl_rgb, tol=1):
    # html_color 转 rgb 后比对 dsl_rgb
    ...

# 输出精度表
```

### 5. 判定通过/不通过

**全部通过**（所有维度在容差内）→ 进阶段 4 用户审核

**部分超容差**：
- 颜色超容差 → 多半 computed style 没内联全（阶段 1.1 重抓）
- 字距超容差 → 检查 HTML 是否用了 em 单位（Pixso 按当前字号换算）
- 阴影丢失 → Pixso 对多层阴影合并支持弱，单层阴影精确保留，多层要人工补
- Grid 降级 → 接受（结构性限制，见下）

## 已知弱点处理

### Grid 布局（结构性限制，非快照质量问题）

CSS `grid-template-columns:1fr 1fr 1fr` 在 Pixso **无原生网格对应**，被转成绝对定位的并列 Frame（如 left:1 / 312 / 622）。

**判定策略**：
- 不要求"是真正的网格"
- 只校验"视觉位置一致"：各 Frame 的 left 值间距均匀（如都是 ~311px 间距）
- 容差：间距 ±5px（Pixso 自动布局计算有微小误差）

**告知用户**：编辑 Grid 区域时不能像 Auto Layout 自动重排，需手动移动。这是 Pixso vs CSS Grid 的能力差，不是快照质量问题。

### 多层阴影（部分丢失）

CSS 的 `box-shadow: 0 0 0 1px rgba(0,0,0,0.08), 0 2px 2px rgba(0,0,0,0.04)`（Vercel 风格 stacked shadow）在 Pixso 里**可能合并或丢失部分层**。

**判定**：
- 单层阴影：精确保留
- 多层阴影：读回的 `effects` 数组可能少于传入，记录"丢失了第 N 层"
- 处理：告知用户在 Pixso 里手动补丢失的阴影层

### backdrop-filter（完全丢失）

CSS `backdrop-filter: blur(24px)`（glassmorphism）在 Pixso 无等价物，导入后**完全消失**。

**判定**：不校验（已知无法还原），告知用户手动补 glass 效果。

## 通过后的归档

精度表 + 已知弱点清单一起归档，作为本次快照的质量凭证：

```
snapshot-2026-06-21/
├── page.html           # 传入的 HTML
├── dsl.json            # 读回的 DSL
├── verify-report.md    # 精度表 + 弱点清单
└── status.txt          # 通过/部分通过/重做
```

下次审核者看 `verify-report.md` 就知道这份快照的可信度边界（哪些精确、哪些是降级还原）。
