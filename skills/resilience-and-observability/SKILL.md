---
name: resilience-and-observability
description: "韧性与可观测性强制规范：SLO/Error Budget (Google SRE) / 4 golden signals / RED+USE / Trace propagation / 结构化日志 / 告警哲学（multi-window burn rate）/ Circuit Breaker / Retry / Bulkhead / Backpressure / Rate Limit / Timeout chain。Use when: 设计服务 SLO、设置 Grafana dashboard、写告警规则、加熔断限流、调试跨服务性能、review 故障树、postmortem 时。SKIP: 单 API 设计（用 backend-development）/ 单 DB schema（用 database-design）/ 纯安全编码（用 secure-coding）。"
metadata:
  pattern: tool-wrapper
  domain: resilience
  composes: [systematic-debugging, backend-development, integration-test-architecture, code-review-gate, verification-driver]
---

# 韧性与可观测性规范

> **本 skill 不重复**: 单 service 实现 → `backend-development`；纯 DEBUG 方法 → `systematic-debugging`；CI/CD 告警 → `release-readiness`。本 skill 解决"按 SOP 设计生产级韧性 + 可观测"——把 Google SRE + 经典韧性模式 + 可观测工程系统化。

## 1. 决策树

```
任务是什么？
├─ 设定服务 SLO → §2.1 SLO 4 步（user journey → SLI → SLO → error budget）
├─ 设计告警 → §2.5 alert philosophy（multi-window / burn-rate）
├─ 加韧性模式 → §2.6 7 模式决策树（circuit breaker / retry / bulkhead ...）
├─ 选可观测栈 → §2.3 三支柱（metrics/log/trace）选型
├─ 跨服务故障排查 → §2.7 trace propagation + structured logging
└─ Postmortem → §2.4 blameless 模板
```

## 2. 5 路径规范

### 2.1 SLO 设计 4 步（用户旅程驱动）

**SLO = Service Level Objective**（用户视角的可靠性目标）

1. **User Journey**：列出 3-5 个核心用户路径（"下单"/"登录"/"搜索"），每个 journey 选 1-2 个指标
2. **SLI 选型**：用 4 golden signals（Latency / Traffic / Errors / Saturation），**不用**衍生聚合（如 QPS 自创指标）
3. **SLO 数值**：用户可忍受的失败率（典型 99% / 99.9% / 99.99% = "three nines" = 月停机 43min / 4.3min / 26s）
4. **Error Budget**：100% - SLO = 失败率配额（用这配额做 vs feature work 的决策）

```
例子：电商下单 API
- User journey: "从 cart 到 confirmation"
- SLI: request successful (HTTP 200) latency
- SLO: 99.9% requests < 500ms（30 天窗口）
- Error budget: 0.1% × 10^6 reqs = 1000 failed reqs / 月
- 如果烧光 budget → 暂停 feature 上线，专 reliability
```

**铁律**：
- SLO 1 个指标 1 个窗口（不要 SLO 套餐）
- SLO 必须靠用户合理期望（不是"99.99% 给运营拍"）
- SLO 太严 = 零 error budget = 不可能 dev（要权衡开发速度）

### 2.2 4 golden signals + RED + USE

```
黄金信号（Google SRE）= 任何服务的最小可观测集合：
├─ Latency      响应时间（成功 + 失败分开）
├─ Traffic      请求量（QPS / connections）
├─ Errors      失败率（5xx / 异常 / 业务失败）
└─ Saturation   饱和度（CPU / memory / queue depth）

RED（Tom Wilkie）：
├─ Rate         每秒请求数
├─ Errors       失败数
└─ Duration    请求耗时

USE（Brendan Gregg，资源视角）：
├─ Utilization 资源使用率
├─ Saturation   排队/饱和
└─ Errors       资源错误

组合：service-level = RED + golden lat；host-level = USE
```

### 2.3 三支柱选型

| 支柱 | 用途 | 工具（按规模） |
|---|---|---|
| **Metrics** | 聚合数字（QPS / 延迟 / 错误率）| Prometheus / VictoriaMetrics / Cortex / Mimir / Thanos / CloudWatch / DataDog |
| **Logs** | 离散事件（含 stack trace）| Loki / Elasticsearch + Kibana / Splunk / CloudWatch Logs / Datadog |
| **Traces** | 单请求跨服务路径 | Jaeger / Zipkin / Tempo / OpenTelemetry Collector / DataDog APM |

**共同支柱**：OpenTelemetry（OTel）作为 vendor-neutral 标准——所有 SDK 上报 OTel 格式，后端可换。

**反模式**：
- 日志当 metrics 用（"这次日志 ERROR 多=故障" 不可靠）
- 仅"metrics 够"忽略 trace（故障排查卡死）
- 用 vendor-specific API（换 vendor 重写全栈）

### 2.4 Blameless Postmortem（故障复盘）

```
模板（每条 incident 必填）：
├─ Timeline（精确时间 / 检测时间 / 缓解时间 / 解决时间）
├─ Root cause（5 why 或 fault tree）
├─ Impact（用户感知：SLO 烧光多少 + 业务损失）
├─ What went well（我们哪些做好了 = blameless culture）
├─ What went poorly（流程/工具/沟通失败）
├─ Action items（每条 owner + deadline）
└─ Lessons learned（写进 skill 或 ADR）
```

**铁律**：
- blameless = 找系统漏洞，非个人过失
- 5 个 why 比 1 个 why 深（根因不在最浅层）
- 行动项必须有 owner + deadline（无 owner = 没承诺）

### 2.5 Alert 哲学（multi-window multi-burn-rate）

**Google SRE 推荐**：**multi-window**（多时间窗）+ **burn rate**（烧光速度）

```
合理告警公式：
- Page rate (1h) = error_budget_burn_rate × hour_window_factor
  - 1h 烧光 14.4× budget（1h 内）→ 立即 page
  - 6h 烧光 6× budget → page
  - 3d 烧光 1× budget → ticket（不 page）

每个 SLO 必须配至少：
- 1h long window × 14.4× burn rate → fast-burn 告警（high priority）
- 6h short window × 6× burn rate → medium
- 24h or 72h × 1× burn rate → slow-burn ticket

目的：避免误报（高 burn rate 才 page）+ 漏报（多时间窗防止瞬时掩盖）
```

**反模式**：
- CPU > 80% 告警（饱和度非直接可用性信号）
- 阈值告警（"500 > 100" 不可知 SLO 烧光速度）
- 告警太多（> 10 page/周 = alert fatigue）

### 2.6 Resilience 7 模式决策树

**模式 1: Circuit Breaker**（熔断）—— 隔离故障服务
- 用：调用外部/downstream 服务（不可控）
- 不：用：本地内存调用（同进程）
- 阈值：5 次失败 / 10s → open（30s），half-open（5s），close
- 工具：resilience4j / Hystrix / Polly / sentinel

**模式 2: Retry**（重试）—— 处理瞬时失败
- 用：幂等 + 瞬时失败（网络抖动）
- 不：用：非幂等操作（重复扣款） / 长延迟（>1s 业务）
- 规则：3 次重试 + 指数 backoff + jitter（100ms × 2^n × random ± 20%）
- 必须配：max retries + max elapsed + circuit breaker（防 retry storm）

**模式 3: Bulkhead**（舱壁）—— 隔离线程/连接池
- 用：多下游共用资源（线程池被一个慢下游拖死）
- 不：用：单下游（无隔离必要）
- 工具：resilience4j Bulkhead / Hystrix ThreadPool / 连接池限数

**模式 4: Rate Limit**（限流）—— 保护资源
- 用：API 网关 / 防滥用 / 资源稀缺
- 算法：token bucket（允许 burst）/ leaky bucket（平滑）/ fixed window（简单）
- 工具：Sentinel / resilience4j / nginx / envoy / AWS WAF

**模式 5: Backpressure**（背压）—— 上游被下游拖
- 用：生产者 > 消费者（如消息队列堆积）
- 模式：流量控制（消费者 ack 慢则减少拉取）+ 队列上限 + dead letter
- 工具：Kafka + max.in.flight / RabbitMQ + prefetch / SQS + reserved concurrency

**模式 6: Timeout chain**（超时链）—— 防慢调用拖死
- 用：所有跨服务调用（同步）
- 规则：**总 timeout > 各 hop timeout 之和**（A→B 1s + B→C 800ms = 总 2s）
- 实现：context deadline / OpenTelemetry trace propagation timeout

**模式 7: Graceful degradation**（优雅降级）—— 部分功能失效时还能用
- 用：非核心功能（如"推荐"挂了，搜索仍能用）
- 模式：fail-open（功能空但可用）+ circuit breaker 配合
- 示例：广告系统挂了 → 主站仍能浏览（无广告）

### 2.7 Trace propagation 跨服务

**OpenTelemetry trace context**：每跳服务透传 `traceparent` + `tracestate` headers

```
W3C Trace Context 格式：
- traceparent: "00-{trace-id}-{span-id}-{flags}"
- 16-byte trace-id（跨服务关联）
- 8-byte span-id（单 hop）

实现：
- HTTP/gRPC 自动传播（库支持）
- Kafka：消息 header 透传
- 异步消息：encode/decode trace context 进 payload
```

**Sampling**：
- Head-based（决策在入口）
- Tail-based（决策在出口，看真实 latency）
- 100% sample 短期 + 1% 长期 stored（混合）

## 3. 负向约束 + 替代方案

| 不要做 ❌ | 应该做 ✅ |
|---|---|
| "服务挂了重启就好" | 找根因（5 why） + blameless postmortem |
| CPU > 80% 自动告警 | SLO 烧光率告警（multi-window） |
| 日志 + grep 排查故障 | Trace propagation + 结构化日志 |
| 重试所有失败（无限） | 限次数 + 指数 backoff + circuit breaker |
| 一处故障拖垮全栈 | Bulkhead 隔离 + 服务降级 |
| "我们用 Redis 当消息队列" | 专用 broker（Kafka / SQS / RabbitMQ） |
| 没有 SLO 直接上 K8s | 先 SLO + 可观测，后扩缩容 |
| "我们监控所有 metrics" | SLO 关键 metric subset（4 golden signals） |

## 4. Post-Generation 自查清单

- [ ] 每个 service 有 ≥1 SLO（用户旅程驱动）
- [ ] SLO 配 alert（multi-window 至少 2）
- [ ] 4 golden signals 全部 metrics
- [ ] Trace 跨服务 propagation（W3C traceparent）
- [ ] 结构化日志（JSON，request_id 全链路）
- [ ] Dashboard 包含 SLO + golden signals + saturation
- [ ] 韧性模式覆盖关键路径（circuit breaker / retry / timeout）
- [ ] Runbook + Oncall rotation 文档化
- [ ] blameless postmortem 模板可用
- [ ] `forge review pass` 通过（韧性 review）

## 5. Gotchas（实操易错点）

**G1**: SLO 太严（99.99%）→ error budget 太小 → 难 ship feature。预防：基于真实用户期望（"三个 nines = 行业 baseline"）。

**G2**: 告警基于阈值（"99% CPU"）→ alert fatigue。预防：SLO burn rate 告警（multi-window）。

**G3**: 日志无结构化（"error happened somewhere"）→ grep 不到。预防：JSON log + request_id 全链路 + trace_id 自动注入。

**G4**: Trace sampling 100% → 存储爆炸。预防：head-based 5% + tail-based 关键路径 100%。

**G5**: 重试未配 circuit breaker → 失败时重试 storm（雪崩）。预防：retry 必有 breaker + max retries 限（≤3）。

**G6**: Timeout 缺失 → 慢 downstream 拖死 upstream。预防：context deadline + 每 hop 超时（总 > hop 之和）。

**G7**: Bulkhead 漏配线程池大小 → 默认太小雪崩。预防：load test 验证饱和点 + 自动扩线程池。

**G8**: Postmortem 找责任人 → blameless 文化坏。预防：模板强调"系统漏洞"非"个人过失"。

## 6. 提交前必跑

```bash
# 静态检查（韧性相关）
forge skills validate --skill=resilience-and-observability

# 自动测试
forge integration-test --scope=resilience   # chaos test（如配置）

# 可观测 stack 联通测试
curl -X POST $COLLECTOR_OTLP/v1/traces -d @trace.json
# 验证 trace 出现 + service.name 正确

# SLO 配置检查
# Grafana: rate(sli_errors_total[5m]) / rate(sli_total[5m]) > 0.001 → alert

# 提交前必过
forge review pass                            # code-review-gate
```

不过 → §4 自查清单补足；过 → commit + 通知 oncall。

## 7. 与其他 skill 的协作

- **服务级**：`backend-development` — §2.6 韧性模式落地 + 同步/异步选型
- **API 设计**：`api-design` 类（在 backend-development §2.1）— 错误码 + Retry-After 头
- **可观测栈**：`integration-test-architecture` — chaos test 验证韧性
- **调试**：`systematic-debugging` — Postmortem 阶段主导
- **架构**：`system-architecture` — §2.6 集成模式 + service decomposition 影响韧性设计

## 8. SRE / 阿里 / Google 实践融合

| 来源 | 关键贡献 | 集成到本 skill |
|---|---|---|
| Google SRE Book | SLO/Error Budget + blameless | §2.1 + §2.4 |
| Google SRE Workbook | multi-window burn rate | §2.5 |
| AWS Well-Architected | Reliability + Operational Excellence pillar | §2.5 + §3 自查 |
| Brendan Gregg (USE) | 资源视角方法 | §2.2 |
| Tom Wilkie (RED) | 服务视角方法 | §2.2 |
| Azure Architecture Center | 韧性模式分类 | §2.6 |

## 参考

- 完整 references 进 `references/`（Google SRE 第 4 章 / Pattern: Circuit Breaker 论文 / chaos-mesh 用例 / OTel config 范例 / SLO 模板）
- 调研权威源：[Google SRE Book](https://sre.google/sre-book/table-of-contents/) / [Google SRE Workbook](https://sre.google/workbook) / [Microsoft Azure Reliability](https://learn.microsoft.com/en-us/azure/architecture/framework) / [OpenTelemetry Docs](https://opentelemetry.io/docs/) / [AWS W-A Reliability Pillar](https://wa.aws.amazon.com/well-architected-pillar-framework.html)
- 写法参照 `skill-authoring-standard`
