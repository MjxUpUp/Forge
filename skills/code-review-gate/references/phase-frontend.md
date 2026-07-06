# 环节审查：前端设计（frontend）

针对前端设计阶段产物（组件、页面、路由、状态管理设计）的审查清单。与 code-review-gate 的代码级审查互补不替换——**只审显式设计产物文件**，代码 diff 走 code-review-gate。

**规范来源**：WCAG 2.1 · Airbnb JS · React 最佳实践 · 阿里前端规范
**核心维度**：组件 / 状态 / a11y / 性能

---

## 1. 组件设计

- [ ] 组件职责单一（不混合 UI 渲染 + 业务逻辑 + 数据获取）
- [ ] Props/Inputs 最小化（不传递多余参数）
- [ ] 受控组件一致（表单输入有 value + onChange）
- [ ] 无 key=index（列表渲染用稳定唯一 key）

## 2. 状态管理

- [ ] 状态就近（组件内用 useState，不用全局管）
- [ ] 全局状态有明确 scope（不把所有状态放 store）
- [ ] 异步状态有 loading/error 处理
- [ ] 状态不可变更新（不直接修改 state 对象）

## 3. 无障碍（a11y）

- [ ] img 必有 alt 属性
- [ ] button 有 aria-label（图标按钮）
- [ ] 表单字段有 label 关联
- [ ] 键盘导航可用（tab order 合理）
- [ ] 色彩对比度符合 WCAG AA

## 4. 性能

- [ ] 无内联密钥（CSS-in-JS 中用 CSS 变量）
- [ ] 大列表有虚拟滚动（>100 项）
- [ ] 图片懒加载（懒加载 viewport 外图片）
- [ ] SSR/CSR 取舍有记录

---

## 确定性规则（机械可检）

| 规则 | 检测方式 | 来源 |
|------|----------|------|
| img 无 alt | 正则扫描 `<img` 后无 `alt=` | WCAG 2.1 |
| key=index | 正则扫描 `key={index}` 或 `key={i}` | React 最佳实践 |
| 内联密钥 | 扫描 `style=.*color.*` 内联样式中的颜色值 | 性能最佳实践 |

## 与大厂规范的映射（方向，非条文）

- **WCAG 2.1** → 无障碍标准（alt、aria-label、对比度）
- **Airbnb JS** → React 组件模式、props 设计
- **React 最佳实践** → 状态管理、性能优化
- **阿里前端规范** → 命名约定、目录结构

---

**与其他审查的分工**：
- 前端设计产物审查 → 本 checklist（审组件/页面文件）
- 代码实现质量 → `review-checklist.md`（审代码 diff）
- API 设计审查 → `phase-api.md`（审接口定义）

**数据来源**：WCAG 2.1 无障碍标准、Airbnb JavaScript/React Style Guide、React 官方最佳实践。
