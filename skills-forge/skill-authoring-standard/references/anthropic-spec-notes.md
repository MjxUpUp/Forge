# Anthropic Agent Skills 规范要点

基于 anthropics/skills 仓库、内部实践博文、Google "5 Design Patterns for AI Agents" 文章整理。

## 触发机制

Claude Code 的 skill 触发判断基于 `name` + `description` 字段：
- 如果 description 太短或缺少触发上下文，skill 无法被正确加载
- 如果 description 缺少 SKIP，可能在不该加载时加载

## Description 设计模式

### Pushy Style（推荐）
宁可多触发，不要少触发。误触发的代价（多读几百行文本）远低于漏触发的代价（用户得手动调用）。

### 互斥设计
skill 之间通过 SKIP 实现互斥：
- `code-review-gate` 的 SKIP: `整个项目验收（用 project-acceptance）`
- `project-acceptance` 的 SKIP: `单文件代码审查（用 code-review-gate）`

### 触发词选择
用用户会说的话来写，不要用技术术语：
- "添加新功能" 而不是 "实现 feature module"
- "测试失败" 而不是 "assertion violation"
- "改 bug" 而不是 "defect remediation"

## 渐进式加载层级

1. **metadata**（~100 词）：始终加载，用于触发判断
2. **SKILL.md body**（< 500 行）：触发后加载，核心内容
3. **bundled resources**：按需读取，详细参考

每一层都应该独立有用——只看 metadata 能知道什么时候用，只看 body 能完成基本工作，看 references 能处理复杂情况。

## 常见错误

1. **description 当摘要写**："管理开发工作流的技能" ← 这不是触发器
2. **name 不一致**：目录名 `code-review-gate` 但 name 字段写 `code-review`
3. **SKILL.md 太长**：超过 500 行说明需要拆分
4. **缺少 Gotchas**：最容易犯的错没有记录
5. **过度抽象**：一个 skill 想覆盖太多场景，应该拆成多个
