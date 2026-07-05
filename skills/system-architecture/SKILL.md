---
name: system-architecture
description: "系统架构强制规范：服务拆分（单体/模块化/微服务）/ Bounded Context（DDD）/ C4 模型（System Context/Container/Component/Code）/ ADR 模板 / 12-Factor App 云原生。Use when: 设计新系统、拆分微服务边界、画架构图、写/审 ADR、决定单体 vs 模块化 vs 微服务、评估技术栈生死抉择时。SKIP: 单个 API 设计（用 api-design 类——backend-development）/ 写组件级代码（用 frontend-development 或 backend-development）/ 部署 CI/CD（用 release-readiness）。"
metadata:
  pattern: tool-wrapper
  domain: architecture
  steps: 6
  composes: [architecture-decision-record, evidence-based-proposal, dev-workflow, requirement-clarification, backend-development, database-design]
---

# 系统架构规范

> **本 skill 不重复**: 单 API 设计 → `backend-development`；数据 schema → `database-design`；ADR 流程（写记录）→ `architecture-decision-record`；CI/CD → `release-readiness`。本 skill 解决"按 SOP 做系统级架构决策 + 画图 + 拆服务"的纪律，覆盖 AWS Well-Architected 框架 + DDD + C4 + 12-Factor App 整合。

## 1. 决策树（架构路径）

```
任务是什么？
├─ 新系统从零设计 → §2.1 7 步架构设计流程
├─ 现有系统拆分（单体 → 模块化/微服务）→ §2.2 服务边界识别
├─ 微服务边界争议 → §2.3 Bounded Context 映射（DDD strategic）
├─ 画架构图给团队/外部 → §2.4 C4 模型
├─ 重要技术选型争议 → §2.5 ADR 模板
├─ 12-factor 合规审计 → §2.6 云原生原则清单
└─ 跨服务契约纠纷 → §2.7 集成模式（同步/异步/事件）
```

## 2. 7 路径规范

### 2.1 新系统设计 7 步（顺序）

1. **需求建模**：用户故事 + 业务规则（**不要**上来就选技术栈）
2. **战略 DDD**：识别 Bounded Context（业务域边界）+ Context Map（域关系）
3. **战术 DDD**：每个 Context 内聚合根 + 实体 + 值对象 + 领域事件
4. **架构决策**：单体/模块化/微服务/事件驱动？ADR 模板记录
5. **C4 模型画图**：System Context（外部用户/系统）→ Container（应用/数据库/服务）→ Component（容器内部模块）→ Code（关键类）
6. **12-factor 应用层**：每个 Container 是否符合 12 原则（codebase/deps/config/backing services/build/release/run/processes/port binding/concurrency/disposability/dev-prod parity/logs/admin processes）
7. **评审 + 上 ADR**：架构评审 → ADR 是真相源（非 Slack/Notion）

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

### 2.3 Bounded Context 映射（DDD Strategic）

每个 Context = 一致性边界 + 团队边界 + 数据所有权边界。

**识别步骤**：
1. 画领域模型图（实体 + 关系）
2. 找 Ubiquitous Language（领域专家和开发者同义词汇）
3. 概念冲突处 = Context 边界（如"账户"在支付=余额，在内容=订阅）
4. Context Map 标 Context 间关系：
   - **Customer-Supplier**：上游/下游（明确依赖）
   - **Conformist**：下游顺应上游模型（不抗辩）
   - **Anti-Corruption Layer (ACL)**：下游有转换层隔离（防污染）
   - **Shared Kernel**：两 Context 共享子集（要明确边界）
   - **Separate Ways**：无关系（独立演进）
   - **Open-Host Service**：标准化协议（API/事件 schema 公开）
5. Context Map 是**动态文档**——每个 Context 团队 owner 协作维护

### 2.4 C4 模型画图（必备）

| 层 | 颗粒度 | 工具例 | 受众 |
|---|---|---|---|
| **System Context** | 整个系统 + 外部用户/系统 | Structurizr / draw.io + C4 plugin | 所有 stakeholder |
| **Container** | 应用/数据库/消息队列等 | draw.io + C4-PlantUML | 内部工程师 + 架构师 |
| **Component** | Container 内的主要模块/类 | IDE 工具图 / 手动 | 模块 owner |
| **Code** | 类图/ER 图（UML） | UML 类图 | 深度 review |

**规则**：
- 每张图给**单一受众**（避免一图全塞）
- 元素**不超过 10 个**（多就拆图）
- **实线 = 同步**（HTTP/gRPC），**虚线 = 异步**（消息/事件）
- 关系**标技术** + **方向**（A → B "REST API"）

### 2.5 ADR 模板（重要决策必写）

```markdown
# ADR-NNNN: <决策标题>

**Status**: Proposed / Accepted / Deprecated / Superseded
**Date**: YYYY-MM-DD
**Deciders**: <谁拍板>
**Supersedes**: ADR-NNNN (如有)

## Context（背景 + 约束）
<业务/技术约束、为什么现在要做这个决策>

## Decision（决策）
<选了 X，不选 Y/Z：清晰陈述选择>

## Consequences（后果）
- Positive: <收益>
- Negative: <代价/技术债>
- Neutral: <不变的>

## Alternatives Considered（备选 + 否决理由）
- Y 方案：<为什么否>
- Z 方案：<为什么否>

## Compliance（合规性）
<这条决策如何与 12-factor / DDD / Well-Architected 对齐>
```

**ADRs 都是 immutable**——改用新 ADR + "Supersedes" 链接。

### 2.6 12-Factor App 云原生审计清单

| 因素 | 通过条件 | 失败症状 |
|---|---|---|
| **Codebase** | 一份代码库多 deploy（不是分叉） | 多个 repo 同步分支 |
| **Dependencies** | 显式声明（package.json/go.mod/lock 文件） | 隐式 OS 包依赖 |
| **Config** | 环境变量存（不进代码） | 写死的 URL/key 出现代码 |
| **Backing services** | DB/缓存/队列当附加资源（URL 连） | DB ip 写进代码 |
| **Build/Release/Run** | 三阶段严格分（build=代码→artifact，release=artifact+config，run=启动） | CI 直接跑代码（跳过 artifact）|
| **Processes** | 无状态（任何进程可被替换） | Session 写本地文件 |
| **Port binding** | 自包含（应用自带 HTTP 端口） | 依赖外部 web server 注入 |
| **Concurrency** | 进程模型横向扩（不是靠单进程线程池） | 单进程扛 QPS |
| **Disposability** | 快速启动 + 优雅 SIGTERM | 启动 > 1min / 杀进程丢数据 |
| **Dev/Prod parity** | 持续 deploy + 同一 backing service（用 docker compose） | dev 用 SQLite/prod 用 PG |
| **Logs** | stdout/stderr 流式（不是写文件） | 日志写 `/var/log/app.log` |
| **Admin processes** | 一次性 REPL/脚本（不是常驻 daemon） | "脚本 daemon" |

### 2.7 集成模式决策（跨服务）

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

- [ ] ADR 写了（重大决策）
- [ ] C4 图 4 层至少 System Context + Container
- [ ] 12-factor 12 项逐项过
- [ ] Bounded Context Map 标注 team owner
- [ ] 服务边界清晰到团队级（Conway's Law）
- [ ] 集成模式（同步/异步）有 ADR 支撑
- [ ] 非功能性需求（性能/可用/合规）在架构层明确
- [ ] `forge review pass`（架构 review）通过
- [ ] 文档同步（README/ADR index）

## 5. Gotchas（实操易错点）

**G1**: 拆微服务没拆团队 → "分布式单体"（通信开销 > 收益）。预防：Conway's Law 先重组团队再拆。

**G2**: 跨服务共享 DB → 故障级联 + schema 锁死。预防：CAP 定理认输，每个服务 own data + 通过事件最终一致性。

**G3**: 12-factor 应用但 dev/Prod DB 不同 → 配置漂移 bug。预防：docker compose 起同 DB（即使是 SQL Server，dev 也跑 docker）。

**G4**: C4 画到 Container 就不画 Component → 新人不懂模块边界。预防：Component 层一定画（即使是简化版）。

**G5**: 架构图没标方向/技术 → 读者猜。预防：每条关系标 [A → B via REST/HTTP]。

**G6**: ADR 不更新（被新决策覆盖但未写新 ADR）→ 历史丢失。预防：superseded 链接强制（这是 §2.5 的 immutable 设计）。

**G7**: "上分布式事务" → 全局锁变系统瓶颈。预防：Saga 或最终一致性，不强求 ACID 跨服务。

**G8**: 服务数量爆炸（50+ 微服务）→ 运维灾难。预防：定期合并低 QPS 服务（AWS Well-Architected：start simple, evolve）。

## 6. 提交前必跑

```bash
# 架构相关自审（不阻塞但提示）
forge auto-build

# 架构评审（提交重要决策前必跑）
forge skills audit --skill=architecture-decision-record

# C4 图 diff（如果改了架构图）
forge skills validate --skill=c4-model

# ADR 索引更新检查
forge review pass                            # 触发 code-review-gate
```

不过 → §4 自查清单补足；过 → commit + 通知相关团队。

## 7. 与其他 skill 的协作

- **战略层**：`architecture-decision-record` — 本 skill 的 §2.5 是它的具体模板
- **战术层**：`backend-development` + `database-design` — 用本 skill 拆好服务/边界后，应用层做
- **可观测**：`resilience-and-observability` — 本 skill 的集成模式（同步/异步）+ 它的 SLO/error budget
- **安全**：`secure-coding` — 跨服务信任边界 + OWASP 实践
- **可读性**：`maintainability-and-readability` — 微服务粒度不要超 Cyclomatic 复杂度

## 8. AWS Well-Architected 6 支柱落地表（本 skill 集成视角）

| AWS 支柱 | 在本 skill 中的体现 |
|---|---|
| Operational Excellence | Runbook + monitoring + 持续 deploy（→ resilience-and-observability） |
| Security | 跨服务认证/最小权限/threat model（→ secure-coding） |
| Reliability | 多 AZ 部署 + 故障隔离 + SLO（→ resilience-and-observability） |
| Performance Efficiency | C4 验证热点路径 + 缓存策略（→ backend-development §2.5） |
| Cost Optimization | 服务粒度（不过度拆）+ 资源 right-sizing |
| Sustainability | 简单为先 + 减 compute overhead（参见 G8） |

## 9. 多云/多技术栈适配

| 场景 | 推荐参考 |
|---|---|
| AWS 云原生 | AWS Well-Architected + 12-factor + Amazon Builder's Library |
| Azure | Azure Architecture Center + Well-Architected 同源 |
| 多云/可移植 | 12-factor + Open Application Model (OAM) |
| On-prem | 原则不变（DDD + C4 仍适用），但 12-factor cloud-specific 部分需调整 |

## 参考

- 完整 references 进 `references/`（AWS W-A pillar 详细 checklist / C4 模板 / ADR 范例 / Bounded Context 实战案例）
- 调研权威源：[AWS Well-Architected](https://aws.amazon.com/architecture/well-architected) / [Google SRE](https://sre.google/workbook) / [12factor](https://12factor.net) / [DDD 战术](https://learn.microsoft.com/en-us/azure/architecture/microservices/model/tactical-domain-driven-design) / [OWASP](https://owasp.org/Top10/2021/A00_2021_Introduction) / [Web Vitals](https://web.dev/articles/vitals/)
- 写法参照 `skill-authoring-standard`
