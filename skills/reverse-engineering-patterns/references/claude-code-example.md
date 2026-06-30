# Claude Code 逆向实例

## 源码

`E:\source\extracted\` — 从 claude-code `cli.js.map` 逆向还原的 TypeScript 源码。

## 核心循环: query.ts

AsyncGenerator 循环：
```
用户输入 → 压缩检查 → API 调用 → 工具执行 → 循环或终止
```

关键模式：
- **State 对象**: 跨迭代的可变状态封装在单个类型中
- **不可变参数分离**: systemPrompt、userContext 等永不重新赋值
- **Token 预算追踪**: budgetTracker + taskBudget 独立跟踪

## 7 个功能域分析

### 1. 上下文管理（4 层压缩）

| 层级 | 文件 | 作用 | 开销 |
|------|------|------|------|
| Snip | snipCompact.ts | 删除僵尸/过时消息 | 零 API 调用 |
| Microcompact | microCompact.ts | 缓存编辑，不重调 API | 零 API 调用 |
| Context Collapse | contextCollapse/ | 只读投影，折叠旧对话 | 极轻量 |
| Auto Compact | autoCompact.ts | LLM 总结整个上下文 | 需要 API 调用 |

### 2. 工具编排

`partitionToolCalls()` 分区：
- 只读工具（Grep、Glob、Read）→ 并行执行（最大并发 10）
- 写操作工具（Edit、Write、Bash）→ 串行执行

### 3. 安全系统

三层：
- Zod schema 验证（`z.strictObject()`）
- PermissionMode + 规则（alwaysAllow/Deny/Ask）
- 沙箱执行（Bash 在沙箱中运行）

### 4. 成本追踪

按模型追踪：token（input/output/cache）、时间（API/工具）、代码（行增删）、Web（搜索次数）。

### 5. 会话管理

消息历史写文件、成本状态跨会话保存、`compact_boundary` 标记压缩边界、`matchSessionMode()` 恢复模式。

### 6. Coordinator/Worker

- Coordinator 不写代码，只分解任务
- Worker 完全自主，有完整工具集
- 任务 4 阶段：Research → Synthesis → Implementation → Verification
- 核心原则："Never delegate understanding"

### 7. Prompt 编写

- Worker 看不到 Coordinator 对话，prompt 必须自包含
- 必须包含：文件路径、行号、具体要改什么
- 禁止 "based on your findings" 类偷懒委托

## 借鉴到目标项目

| 功能 | 借鉴内容 | 适用场景 |
|------|---------|---------|
| 分层上下文压缩 | 4 层递进压缩 | 本地模型 32K 窗口极易溢出 |
| 工具并行化 | 只读/写分离并行 | 提升响应速度 |
| 成本追踪 | UsageMetrics 类型 | 为 Compensator 优化提供数据 |
| Bash 安全 | 命令白名单/破坏性检测 | 工具执行安全底线 |
| 会话持久化 | compact_boundary 标记 | 会话恢复体验 |
| 多代理编排 | 任务 4 阶段流程 | 等模型能力到位后启用 |
