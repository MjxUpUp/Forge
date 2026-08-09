---
name: reverse-engineering-patterns
description: "源码架构分析与模式提取方法论。Use when: 需要从现有项目中提取设计智慧时、分析开源项目架构时、为项目参考其他系统的设计模式时。SKIP: 已有清晰文档的项目、用户直接给出设计方案时。"
metadata:
  pattern: pipeline
  domain: architecture
  steps: 3
  triggers: [{"event":"UserPromptSubmit","keywords":["看懂这段","理解这个代码","这段代码是","代码是怎么","怎么实现的","how does this work","读一下源码","trace the code"],"cooldown":300}]
---

# 源码逆向工程方法论

从 Claude Code 源码逆向（Coordinator/Worker 多代理、4 层上下文压缩，实例见 references/claude-code-example.md）中提炼的系统化方法。

## 阶段 1：定位核心循环

任何软件系统都有一个核心循环——主事件循环、请求处理循环、消息处理循环。找到它，就找到了理解整个系统的钥匙。

**查找策略：**
1. 从入口文件开始（`main.ts`、`index.ts`、`app.ts`）
2. 找到主循环结构（`while`、`for await`、递归调用）
3. 识别循环中的状态对象——它记录了跨迭代的可变状态

**侦察命令：**
```bash
ls src/ lib/ app/ 2>/dev/null                          # 顶层结构，找入口候选
rg -ln "for await|while \(true\)|async function\*" src/ | head -10   # 定位主循环结构
rg -n "interface \w*State|type \w*State" src/ | head -10             # 找跨迭代状态对象
```

**输出格式：**
```
核心循环: <文件路径>
循环结构: <AsyncGenerator / while / 事件驱动>
状态对象: <State 类型包含哪些字段>
每次迭代: <输入 → 处理 → 输出的流程>
```

## 阶段 2：按功能域分区

将系统拆成独立的功能域，每个域单独分析。

**常见功能域：**

| 功能域 | 关注点 | 典型文件 |
|--------|--------|---------|
| 上下文管理 | 消息压缩、历史裁剪、token 预算 | compact/、context/ |
| 工具编排 | 并行/串行执行、权限检查、结果收集 | tools/、orchestration/ |
| 安全系统 | 输入验证、权限控制、沙箱隔离 | security/、permission/ |
| 成本追踪 | token 计数、性能指标、状态持久化 | cost/、metrics/ |
| 会话管理 | 持久化、恢复、模式切换 | session/、storage/ |
| 多代理编排 | 任务分解、结果综合、Worker 管理 | coordinator/、agent/ |

**对每个域，提取：**
- **What**: 这个域做什么（一句话）
- **How**: 怎么做的（关键机制）
- **Design Pattern**: 用了什么设计模式（State、Strategy、Chain of Responsibility 等）

**侦察命令：**
```bash
find src -maxdepth 2 -type d | sort                    # 顶层目录 = 功能域候选
for d in src/*/; do echo "$d $(find "$d" -type f | wc -l) 个文件"; done   # 各域规模排序，先大后小
rg -ln "<域名关键词，如 compact>" src/                  # 关键词反查某域涉及哪些文件
```

## 阶段 3：参考分析

对每个有参考价值的模式，输出：

```markdown
### <模式名称>

**What**: <一句话描述>

**关键机制**:
- <机制 1>
- <机制 2>

**Why 值得参考**: <解决什么问题、在目标项目中有什么对应需求>

**How to apply**:
- <具体步骤 1>
- <具体步骤 2>
- <需要注意的约束>
```

**参考原则：**
- 只参考解决实际问题的模式，不参考"看起来优雅"的模式
- 每个参考必须回答 "目标项目中什么场景用得上"
- 如果目标项目的技术栈不同，说明如何适配

**侦察命令：**
```bash
rg -n "<机制关键词>" src/ -A 5 | head -40   # 找机制实现点，读上下文确认 How
rg -c "<机制关键词>" src/ | sort -t: -k2 -rn | head   # 出现频率判断它是核心路径还是边缘代码
```

## 分层降级理念

从 Codex 逆向中提炼的最核心设计理念：

**分层降级**——从上下文压缩到工具执行到错误恢复，每一步都先尝试轻量方案，不行再升级。

```
零成本方案（纯逻辑处理）
  ↓ 不行
低开销方案（缓存、折叠）
  ↓ 不行
中开销方案（LLM 摘要、单次调用）
  ↓ 不行
高开销方案（重新执行、全量重算）
```

这个理念可以应用到：上下文管理、错误处理、性能优化、能力补偿。

## Gotchas

- **不要从入口直接读所有文件**: 从核心循环出发，按功能域逐步展开，避免淹没在细节中。
- **区分框架代码和业务逻辑**: 框架代码（路由注册、依赖注入）是噪音，业务逻辑（核心循环、状态管理）是信号。
- **注意构建产物 vs 源码**: `.js.map` 文件可以还原源码结构，但变量名可能被混淆。
- **版本差异**: 逆向分析的是特定版本，最新版本可能已有变化。记录分析的版本号和时间。

## 参考

- Claude Code 逆向实例：[references/claude-code-example.md](references/claude-code-example.md)
