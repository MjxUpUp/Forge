---
name: backend-development
description: "后端开发强制规范：API 设计 / service 层 / 鉴权 / 数据校验 / 错误处理 / 性能 / 测试 / 可观测。Use when: 写 API endpoint/service 层、设计 schema 业务层、加鉴权/中间件、写 e2e 测试、排查性能瓶颈、debug 后端 bug、写后端任务给 agent 时。SKIP: 数据库 schema/迁移（用 database-design）/ 纯 UI（用 frontend-development）/ 部署/CI（用 release-readiness）。"
metadata:
  pattern: tool-wrapper
  domain: backend
  composes: [code-review-gate, test-discipline, tdd-cycle, integration-test-architecture, verification-driver]
---

# 后端开发规范

> **本 skill 不重复**: 数据库 schema/迁移 → `database-design`；性能 e2e → `integration-test-architecture`；CI/CD → `release-readiness`；架构 ADR → `architecture-decision-record`。本 skill 解决"按 SOP 写出/改后端代码"的工作流纪律，覆盖多语言（Rust/Node/Go/Python 通用 + 跨 stack 适配引用）。

## 1. 决策树（后端开发路径）

```
任务是什么？
├─ 新 API endpoint → §2.1 REST/GraphQL/gRPC 设计 7 步
├─ 改现有 endpoint → §2.2 改不破坏（contract-stable）
├─ 加鉴权/中间件 → §2.3 鉴权决策 + 中间件顺序
├─ 业务逻辑层 → §2.4 service / repo 分层
├─ 性能/排查 → §2.5 性能自检 + 可观测必做项
├─ 测试 → §2.6 测试策略（unit/integration/e2e/contract）
└─ bug 修复 → §2.7 排查 SOP（systematic-debugging + 此 §7）
```

## 2. 7 路径规范

### 2.1 新 API endpoint 7 步

1. **确定资源模型 + URL**：名词复数（`/users` 不是 `/getUsers`），HTTP 方法语义（GET/POST/PUT/PATCH/DELETE）
2. **定义契约**：request/response schema（OpenAPI/Protobuf/JSON Schema）+ 错误码
3. **数据校验**：边界值 + 长度 + enum + 格式（拒绝"裸类型"——所有入参必须校验）
4. **鉴权**：每 endpoint 显式声明权限要求（不靠"隐藏"安全）
5. **错误处理**：统一错误结构（code + message + detail），不泄内部
6. **测试**：unit（业务逻辑）+ integration（HTTP）+ e2e（真实路径）+ contract（契约不变）
7. **可观测**：log/trace/metric（§2.5）+ 提交前 `code-review-gate`

### 2.2 改现有 endpoint — 不破坏契约

**契约 stable 原则**：写出去的字段不删、不改语义、改字段类型前发 deprecation notice。
```bash
# 改前查当前使用情况
grep -rn "v1/users" src/
```
**禁止**：silent break（rename 字段不加 `@deprecated`、删字段不留 redirect）。

### 2.3 鉴权 + 中间件顺序（铁律）

```
中间件栈从外到内（express/gin/axum 同理）：
1. Tracing/Logging          → 最早，记录完整上下文
2. Request ID               → 跨服务关联
3. CORS                     → 浏览器跨域必需
4. Rate limit / Quota       → 资源保护
5. 鉴权（auth）              → 通过才有权进
6. 业务鉴权（authz/policy）  → 角色+资源级权限（区别 auth）
7. Schema 校验              → 入参验完再进 service
8. Service 业务             → 不重复 auth/authz（上层已做）
9. DB / 外部依赖            → 仅 service 调
10. Error handler / Response → 最外兜
```

**反模式**：
- 在 service 层查 req.user 鉴权（应在上层做）
- 中间件顺序错（auth 在 schema 校验后 → 未授权用户能跑 schema 解析开销）
- 自定义鉴权逻辑（用成熟库：JWT/OAuth2/PASETO + 标准中间件）

### 2.4 Service / Repo 分层

```
handler/      → 参数解析 + 调 service + 返回 HTTP（薄）
service/      → 业务逻辑编排（事务边界、跨 repo 调用）
repo/         → 单表/单领域的数据访问（不写业务）
domain/       → 实体 + 业务规则（纯函数优先，不依赖框架）
```

**铁律**：
- service 不返 HTTP 类型（http.Request 之类的）
- repo 不做 join 跨表（join 进 service 层组装）
- domain 不 import framework（可纯单测）
- handler 不写业务逻辑（只解析 + 调 service + 序列化）

### 2.5 性能 + 可观测必做项

**性能自检**：
- [ ] 慢查询 log：`query_time > 100ms` warn
- [ ] N+1 检测（用 dataloader / batch）
- [ ] 缓存层（hot path），失效策略明确
- [ ] 分页/limit（list endpoint 必须有上限）
- [ ] 超时链路（HTTP 2s / DB 1s / Redis 100ms）

**可观测必做**（不靠运气 debug）：
- [ ] trace（OpenTelemetry/W3C trace context）
- [ ] structured log（JSON，request_id 全链路）
- [ ] metric（latency histogram + error rate + QPS）
- [ ] health check（`/health` liveness + `/ready` readiness，区别依赖是否就绪）

### 2.6 测试策略

| 层 | 工具 | 测什么 |
|---|---|---|
| unit | Go test / pytest / vitest | 纯函数、domain 规则 |
| integration | httptest / supertest | 整 endpoint + 真 DB（testcontainers） |
| contract | pact / spectral | API 契约对消费者不破坏 |
| e2e | Playwright / k6 / ghz | 真实链路、性能 |
| chaos | chaos-mesh / toxiproxy | 依赖故障容忍 |

**铁律**：不测 SQL 拼装（用 DB driver testcontainers），不 mock service 层（mock 会掩盖真 bug），按层测层。

### 2.7 Bug 排查 SOP

1. **systematic-debugging** skill 跑：复现→定位→假设→验证
2. **查 trace**：找 trace_id 看慢在哪、错在哪
3. **查 metric**：CPU/IO/连接数/限流
4. **复现 minimum**：剥到单 endpoint/单 query 复现
5. **root cause**：归类（数据/逻辑/并发/外部依赖）
6. **修 + test**：回归 test + contract test
7. **写 post-mortem + skill**：跨任务复现 → 改 SKILL.md / 加新 skill

## 3. 负向约束 + 替代方案

| 不要做 ❌ | 应该做 ✅ |
|---|---|
| `any` 类型 / 裸 string | 严格 schema（pydantic/zod/serde） |
| 把 user_id 拼接 URL（`/users/${id}`） | 路径参数 + 鉴权校验所属 |
| catch (e) { console.log(e) } | 结构化错误 + 监控上报 |
| secret 写代码里 / config | 环境变量 + vault（如 HashiCorp Vault） |
| 自定义鉴权/jwt | 用成熟库（jose/jwt-go/Authlib） |
| "信任"客户端传的任何字段 | 重新校验（不信任前端的字段） |
| 同步阻塞主流程（HTTP call DB）| 异步/批/缓存 |

## 4. Post-Generation 自查清单

- [ ] 文件 < 200 行（不超 300）
- [ ] 错误统一处理（不每 endpoint 自己捉）
- [ ] 无 `panic` / `process.exit` 冒到顶层
- [ ] 无硬编码 secret / token / URL
- [ ] 无未处理的 error（`if err != nil` 路径有处理）
- [ ] API 契约文档同步（OpenAPI/JSON Schema）
- [ ] unit + integration 测试覆盖率 ≥ 80%
- [ ] `forge review pass` 通过

## 5. Gotchas（实操易错点）

**G1**: DB 连接未释放 → 连接池耗尽。预防：withTx / defer close + 测试用连接数监控。

**G2**: 时区错位 → 时间错乱。预防：DB 全 UTC + 应用层时区转换 + 测试覆盖 DST 边界。

**G3**: secret 进 git → 撤销轮换代价。预防：.env.example + `git-secrets` pre-commit hook（扫历史用 `trufflehog` / `gitleaks`）。

**G4**: race condition → 偶发线上 bug 难复现。预防：所有 mutable shared state 过 transaction / lock，并发测试（t.Parallel + race detector）。

**G5**: 大 JSON 序列化在 hot path → CPU 飙。预防：proto / msgpack + batch + 流式。

**G6**: 鉴权 token 不 refresh → 用户莫名 401。预防：refresh 流程 + 监控失效比例。

## 6. 提交前必跑

```bash
# 1. 静态（编译 + vet）
go build ./... && go vet ./...
# 或 cargo build / tsc --noEmit / ruff check

# 2. 测试（含 race + 覆盖率）
go test -race -cover ./...
# 或
pytest --cov=src --cov-fail-under=80

# 3. Lint + 安全扫描
golangci-lint run
# 或
ruff check + bandit

# 4. API 契约对消费者
forge review pass                       # 触发 code-review-gate
```

不过 → §4 自查清单补足；过 → commit。

## 7. 与其他 skill 的协作

- **数据层**：`database-design` — schema 迁移必走
- **测试层**：`integration-test-architecture` — integration/e2e 完整覆盖
- **审查层**：`code-review-gate` — 提交前必过
- **安全**：`on-demand-guards` — 按需启 hazard/hardening 检查
- **错误排查**：`systematic-debugging` 主导，本 skill §2.7 是 dev-specific 补充
- **契约对外部**：`verification-driver` — 跨服务契约断言

## 8. 多语言适配（按 stack 选）

| Stack | 必跑 lint | 必跑 test | 推荐工具链 |
|---|---|---|---|
| Go | golangci-lint + go vet | go test -race -cover | sqlc + sqlx |
| Rust | clippy + cargo fmt | cargo test --all-features | sqlx + diesel |
| Node | eslint + tsgo | jest/vitest --coverage | prisma / drizzle |
| Python | ruff + mypy | pytest --cov | sqlalchemy + alembic |
| 多语言栈各自 stack-selection skill 选型 | | | |

## 参考

- 完整 references 进 `references/`（HTTP code 规范/rbac matrix/perf pattern）
- 写法参照 `skill-authoring-standard`
