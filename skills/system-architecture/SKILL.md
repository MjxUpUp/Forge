---
name: system-architecture
description: "系统架构强制规范：服务拆分（单体/模块化/微服务，带阈值信号）/ Bounded Context（DDD strategic）/ 跨服务集成模式（同步/异步/事件）。Use when: 设计新系统、拆分微服务边界、画架构图、写/审 ADR、决定单体 vs 模块化 vs 微服务、评估技术栈生死抉择时。SKIP: 单个 API 设计（用 backend-development）/ 写组件级代码（用 frontend-feature-development 或 backend-development）/ 部署 CI/CD（用 release-readiness）。"
metadata:
  pattern: tool-wrapper
  domain: architecture
  composes: [architecture-decision-record, evidence-based-proposal, dev-workflow, requirement-clarification, backend-development, database-design]
  triggers: [{"event":"UserPromptSubmit","keywords":["系统架构","架构设计","服务拆分","微服务","单体还是","high-level design","系统设计","拆服务","服务怎么拆","画架构图"],"cooldown":300}]
---

# 系统架构规范

> **本 skill 不重复**: 单 API 设计 → `backend-development`；数据 schema → `database-design`；ADR 流程与模板 → `architecture-decision-record`（本 skill 不自带模板）；CI/CD → `release-readiness`；C4/12-factor/SOLID 等通识 → 官方原文，本 skill 不复述。本 skill 只留带阈值的判断规则和决策树。

## 1. 决策树（架构路径）

```
任务是什么？
├─ 新系统从零设计 → §2.1 架构设计流程
├─ 现有系统拆分（单体 → 模块化/微服务）→ §2.2 拆分 5 信号（≥2 才拆）
├─ 微服务边界争议 → §2.3 Bounded Context 识别
├─ 跨服务契约纠纷 → §2.4 集成模式（同步/异步/事件）
└─ 重要技术选型争议 → architecture-decision-record（ADR 模板唯一真相源）
```

## 2. 路径规范

### 2.1 新系统设计流程（顺序）

1. **需求建模**：用户故事 + 业务规则（**不要**上来就选技术栈）
2. **战略 DDD**：识别 Bounded Context（业务域边界）+ Context Map（§2.3）
3. **战术 DDD**：聚合根/实体/值对象 + 应用分层（handler 薄只解析 / service 编排事务与跨 repo / repo 单表不写业务 / domain 纯函数不依赖框架）
4. **架构决策**：单体/模块化/微服务/事件驱动 → 重大决策记 ADR（走 `architecture-decision-record`）
5. **画图沟通**：一张图只给一个受众；元素 ≤ 10 个（多就拆图）；实线 = 同步、虚线 = 异步；每条关系标技术 + 方向（A → B "REST API"）
6. **云原生合规**：12-factor 逐项对照官方清单（https://12factor.net，此处不复制）
7. **评审 + ADR 落盘**：ADR 是真相源（非 Slack/Notion）

### 2.2 服务拆分——单体的"什么时候拆"

**铁律**：**不要预防性拆**（Hickey: "if your app is a monolith, don't start it as a microservices"）

```
触发拆分的真信号（满足 ≥ 2 才拆）：
├─ 团队 ≥ 50 人 + 编译/部署时间瓶颈（compile time > 10min）
├─ 单个团队无法独立 deploy 全部（merge conflict 频繁）
├─ 不同模块的扩缩容规律差异大（10x ~ 100x）
├─ 故障隔离需求强（一个模块挂不能拖垮全部）
└─ 业务边界清晰到能独立定义 team boundaries

未触发 → 保持单体或模块化单体（modular monolith）：
- 模块边界清晰但代码同进程
- 后期 extract microservices 是个明确方向（不是当下）
- 避免分布式单体（distributed monolith = 通信开销 > 收益）
```

### 2.3 Bounded Context 识别（DDD Strategic）

每个 Context = 一致性边界 + 团队边界 + 数据所有权边界。

1. 画领域模型图（实体 + 关系）
2. 找 Ubiquitous Language（领域专家和开发者同义词汇）
3. 概念冲突处 = Context 边界（如"账户"在支付=余额，在内容=订阅）
4. Context 间关系（Customer-Supplier / Anti-Corruption Layer / Shared Kernel 等）按 DDD 标准标记，此处不展开
5. Context Map 是**动态文档**——每个 Context 团队 owner 协作维护

### 2.4 集成模式决策（跨服务）

```
跨服务通信？
├─ 同步（HTTP/gRPC）
│   ├─ 紧耦合请求/响应（订单创建要库存确认）→ REST / gRPC
│   ├─ 实时查询（用户信息）→ GraphQL / REST
│   └─ 性能敏感（毫秒级）→ gRPC + protobuf
├─ 异步（消息/事件）
│   ├─ 最终一致性（30s 内）→ Kafka / RabbitMQ / SQS
│   ├─ 实时事件通知（"用户注册成功" 通知）→ Pub/Sub / EventBridge
│   └─ 跨域编排（订单支付失败补偿）→ Saga 模式
└─ 混合
    ├─ 命令同步 + 事件异步 = "Outbox 模式"
    └─ 读同步 / 写异步 = "CQRS"
```

**反模式**：
- 同步链路过长（5+ 调用 = 延迟 + 故障扩散）
- 同步通信做实时通知（应该异步事件）
- 事件总线同步阻塞（queue 不能阻塞 send 端）

## 3. 负向约束 + 替代方案

| 不要做 ❌ | 应该做 ✅ |
|---|---|
| 上来就拆微服务（预防性拆） | 单体起步，触发信号出现再拆 |
| 跨服务共享数据库 | 每个服务 own data + 通过 API/事件交换 |
| 同步链路过长 | Saga / Event-driven + 最终一致性 |
| "我们用 Kafka 就 scalable 了" | 评估吞吐/延迟/运维成本，**消息中间件不是银弹** |
| 1 个 Context 跨多团队 | 拆分 Context（每 Context 一团队） |
| 微服务没有 team boundary | Conway's Law 反推：先有 team，再拆 service |
| "我们要用 Blockchain" | 业务问题先问"为什么需要不可篡改" |
| "Kubernetes 解决一切" | 先评估运维能力，再选编排 |

## 4. Post-Generation 自查清单

- [ ] ADR 写了（重大决策，走 `architecture-decision-record`）
- [ ] 架构图按受众分层（元素 ≤10，关系标技术 + 方向）
- [ ] Bounded Context Map 标注 team owner
- [ ] 服务边界清晰到团队级（Conway's Law）
- [ ] 集成模式（同步/异步）有 ADR 支撑
- [ ] 非功能性需求（性能/可用/合规）在架构层明确
- [ ] 文档同步（README/ADR index）

## 5. Gotchas（实操易错点）

**G1**: 拆微服务没拆团队 → "分布式单体"（通信开销 > 收益）。预防：Conway's Law 先重组团队再拆。

**G2**: 跨服务共享 DB → 故障级联 + schema 锁死。预防：每个服务 own data + 通过事件最终一致性。

**G3**: dev/Prod backing service 不同 → 配置漂移 bug。预防：docker compose 起同 DB（即使是 SQL Server，dev 也跑 docker）。

**G4**: 架构图没标方向/技术 → 读者猜。预防：每条关系标 [A → B via REST/HTTP]。

**G5**: ADR 不更新（被新决策覆盖但未写新 ADR）→ 历史丢失。预防：Superseded 链接强制（ADR immutable，改用新 ADR 取代）。

**G6**: "上分布式事务" → 全局锁变系统瓶颈。预防：Saga 或最终一致性，不强求 ACID 跨服务。

**G7**: 服务数量爆炸（50+ 微服务）→ 运维灾难。预防：定期合并低 QPS 服务（start simple, evolve）。

## 6. 提交前必跑

架构 skill 的交付物是文档/图，不是代码——无编译步骤：

```bash
# 1. ADR 索引更新检查：docs/adr/README.md 含新 ADR 条目
# 2. 架构 review
# 提交前审查：code-review-gate 门控（宿主有审查盖章机制时由其标记已审）
```

不过 → §4 自查清单补足；过 → commit + 通知相关团队。

## 7. 与其他 skill 的协作

- **ADR**：`architecture-decision-record` — ADR 流程与模板唯一真相源，本 skill 不复述
- **战术层**：`backend-development` + `database-design` — 用本 skill 拆好服务/边界后，应用层做
- **可观测**：`resilience-and-observability` — 本 skill 的集成模式（同步/异步）+ 它的 SLO/error budget
- **安全**：`secure-coding` — 跨服务信任边界 + OWASP 实践
- **审查**：`code-review-gate` — 提交前审查门禁

## 参考

- 调研权威源：[AWS Well-Architected](https://aws.amazon.com/architecture/well-architected) / [Google SRE](https://sre.google/workbook) / [12factor](https://12factor.net) / [DDD 战术](https://learn.microsoft.com/en-us/azure/architecture/microservices/model/tactical-domain-driven-design)
- 写法参照 `skill-authoring-standard`
