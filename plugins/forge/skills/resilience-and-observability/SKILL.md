---
name: resilience-and-observability
description: "韧性与可观测性强制规范：SLO/Error Budget (Google SRE) / 4 golden signals / RED+USE / Trace propagation / 结构化日志 / 告警哲学（multi-window burn rate）/ Circuit Breaker / Retry / Bulkhead / Backpressure / Rate Limit / Timeout chain。Use when: 设计服务 SLO、设置 Grafana dashboard、写告警规则、加熔断限流、调试跨服务性能、review 故障树、postmortem 时。SKIP: 单 API 设计（用 backend-development）/ 单 DB schema（用 database-design）/ 纯安全编码（用 secure-coding）。"
metadata:
  pattern: tool-wrapper
  domain: operations
  composes: [systematic-debugging, backend-development, integration-test-architecture, code-review-gate, verification-driver]
  triggers: [{"event":"UserPromptSubmit","keywords":["重试","retry","限流","熔断","错误处理","告警规则","可观测","grafana","SLO"],"cooldown":300}]
---

# 韧性与可观测性规范

> **本 skill 不重复**: 单 service 实现 → `backend-development`；纯 DEBUG 方法 → `systematic-debugging`；CI/CD 告警 → `release-readiness`。本 skill 只留带数值的规则与决策点；Google SRE/RED/USE 通识不复述。

## 1. 决策树

```
任务是什么？
├─ 设定服务 SLO → §2.1 SLO 4 步（user journey → SLI → SLO → error budget）
├─ 设计告警 → §2.2 alert philosophy（multi-window / burn-rate）
├─ 加韧性模式 → §2.3 7 模式决策树（circuit breaker / retry / bulkhead ...）
├─ 选可观测栈 → §2.4 三支柱（OTel-first）
├─ 跨服务故障排查 → §2.5 trace propagation + structured logging
└─ Postmortem → §2.6 blameless 要点
```

## 2. 路径规范

### 2.1 SLO 设计 4 步（用户旅程驱动）

1. **User Journey**：列出 3-5 个核心用户路径（"下单"/"登录"/"搜索"），每个 journey 选 1-2 个指标
2. **SLI 选型**：用 4 golden signals（Latency / Traffic / Errors / Saturation），**不用**自创衍生聚合
3. **SLO 数值**：用户可忍受的失败率（典型 99% / 99.9% / 99.99%，30 天窗口停机约 7.2h / 43min / 4.3min）
4. **Error Budget**：100% - SLO = 失败率配额（烧光 → 暂停 feature 上线，专 reliability）

例：下单 API "从 cart 到 confirmation" → SLI = HTTP 200 且 < 500ms → SLO 99.9%（30 天）→ budget = 0.1% × 10^6 = 1000 failed reqs/月。

**铁律**：SLO 1 个指标 1 个窗口（不要套餐）；数值靠用户合理期望非运营拍脑袋；太严 = 零 budget = 无法 dev。

### 2.2 Alert 哲学（multi-window multi-burn-rate）

每个 SLO 至少配两级（Google SRE 推荐）：

```
- 1h long window × 14.4× burn rate → fast-burn page（high priority）
- 6h short window × 6× burn rate → page
- 3d/72h × 1× burn rate → slow-burn ticket（不 page）
目的：高 burn rate 才 page（防误报）+ 多时间窗（防瞬时掩盖漏报）
```

**反模式**：CPU > 80% 告警（饱和度非可用性信号）；裸阈值告警（"5xx > 100" 不知烧光速度）；> 10 page/周 = alert fatigue。

### 2.3 Resilience 7 模式决策树

**1. Circuit Breaker**（熔断，隔离故障下游）——跨进程调用用，本地内存调用不用。阈值：5 次失败 / 10s → open（30s）→ half-open（5s）→ close。工具：resilience4j（Java）/ Polly（.NET）/ opossum（Node）/ gobreaker（Go）/ Sentinel / Envoy outlier detection（网格层）。**不要用 Hystrix**（2018 年起维护模式）。

**2. Retry**（重试，处理瞬时失败）——只对幂等操作 + 瞬时失败；非幂等（重复扣款）/ 长延迟（>1s 业务）不用。规则：3 次重试 + 指数 backoff + jitter（100ms × 2^n × random ± 20%），必须配 max retries + circuit breaker（防 retry storm）。

**3. Bulkhead**（舱壁，隔离线程/连接池）——多下游共用资源时用（防一个慢下游拖死全部）；单下游不用。工具：resilience4j Bulkhead / 连接池与线程池限数。

**4. Rate Limit**（限流，保护资源）——算法：token bucket（允许 burst）/ leaky bucket（平滑）/ fixed window（简单）。工具：Sentinel / resilience4j / nginx / envoy / AWS WAF。

**5. Backpressure**（背压）——生产者 > 消费者（队列堆积）时：消费 ack 慢则减拉取 + 队列上限 + dead letter。工具：Kafka max.in.flight / RabbitMQ prefetch / SQS reserved concurrency。

**6. Timeout chain**（超时链）——所有同步跨服务调用必配。规则：**总 timeout > 各 hop timeout 之和**（A→B 1s + B→C 800ms → 总 ≥ 2s）。实现：context deadline。

**7. Graceful degradation**（优雅降级）——非核心功能失效时核心仍可用：fail-open + circuit breaker 配合（如推荐挂了搜索仍能用）。

### 2.4 可观测三支柱（OTel-first）

**OpenTelemetry 是 vendor-neutral 共同支柱**——所有 SDK 上报 OTel 格式，后端可换（Jaeger/Tempo/Loki/Prometheus/DataDog...），不写 vendor-specific API。方法学：service-level 用 RED（Rate/Errors/Duration）+ latency，host-level 用 USE（Utilization/Saturation/Errors）；最小集合 = 4 golden signals。

**反模式**：日志当 metrics 用；"metrics 够了"忽略 trace；vendor 锁定 API。

### 2.5 Trace propagation 跨服务

- 每跳透传 W3C Trace Context：`traceparent: 00-{16-byte trace-id}-{8-byte span-id}-{flags}`（HTTP/gRPC 库自动传播；Kafka 走消息 header，异步消息 encode 进 payload）
- **Sampling**：head-based（入口决策）+ tail-based（出口按真实 latency）混合——短期 100% + 长期 1% stored；关键路径保 100%
- 日志必须结构化（JSON + request_id 全链路 + trace_id 自动注入）

### 2.6 Blameless Postmortem 要点

模板字段：Timeline（检测/缓解/解决时间）+ Root cause（5 why / fault tree）+ Impact（SLO 烧光 + 业务损失）+ What went well/poorly + Action items（**每条 owner + deadline**）。blameless = 找系统漏洞非个人过失。

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

## 4. Post-Generation 自查清单

- [ ] 每个 service 有 ≥1 SLO（用户旅程驱动）
- [ ] SLO 配 multi-window burn-rate 告警（至少 2 窗口）
- [ ] 4 golden signals 全部 metrics
- [ ] Trace 跨服务 propagation（W3C traceparent）
- [ ] 结构化日志（JSON，request_id 全链路）
- [ ] 韧性模式覆盖关键路径（circuit breaker / retry / timeout）
- [ ] 提交前过 code-review-gate 审查（宿主有审查盖章机制时由其标记已审）

## 5. Gotchas（实操易错点）

**G1**: SLO 太严（99.99%）→ error budget 太小 → 难 ship feature。预防：基于真实用户期望。

**G2**: 告警基于阈值（"99% CPU"）→ alert fatigue。预防：SLO burn rate 告警（multi-window）。

**G3**: 日志无结构化 → grep 不到。预防：JSON log + request_id 全链路 + trace_id 自动注入。

**G4**: Trace sampling 100% → 存储爆炸。预防：head-based 5% + tail-based 关键路径 100%。

**G5**: 重试未配 circuit breaker → retry storm（雪崩）。预防：retry 必有 breaker + max retries ≤3。

**G6**: Timeout 缺失 → 慢 downstream 拖死 upstream。预防：context deadline + 每 hop 超时（总 > hop 之和）。

**G7**: Bulkhead 漏配线程池大小 → 默认太小雪崩。预防：load test 验证饱和点。

## 6. 提交前核对

韧性/可观测的交付物多为配置与规则文件，无可执行编译步骤：按 §4 自查清单逐项人工核对 + 确认告警规则文件存在（`grep -rn "burn_rate\|slo" <rules 目录>`）+ code-review-gate 门控。不过 → §4 补足；过 → commit + 通知 oncall。

## 7. 与其他 skill 的协作

- **服务级**：`backend-development` — 可观测指针回到本 skill
- **可观测栈验证**：`integration-test-architecture` — chaos test 验证韧性
- **调试**：`systematic-debugging` — Postmortem 阶段主导
- **架构**：`system-architecture` — 集成模式（同步/异步）影响韧性设计

## 参考

- 调研权威源：[Google SRE Book](https://sre.google/sre-book/table-of-contents/) / [Google SRE Workbook](https://sre.google/workbook) / [Microsoft Azure Reliability](https://learn.microsoft.com/en-us/azure/architecture/framework) / [OpenTelemetry Docs](https://opentelemetry.io/docs/) / [AWS W-A Reliability Pillar](https://wa.aws.amazon.com/well-architected-pillar-framework.html)
- 写法参照 `skill-authoring-standard`
