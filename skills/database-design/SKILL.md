---
name: database-design
description: "数据库 schema / migration / 索引 / 查询优化 / ORM 选型强制规范。Use when: 设计新 schema、写 migration、加索引、调慢查询、选 ORM/查询库、写数据库相关 ADR 时。SKIP: 应用层业务逻辑（用 backend-development）/ 表单/UI（用 frontend-development）/ 后端 API 设计（用 backend-development）。"
metadata:
  pattern: tool-wrapper
  domain: database
  composes: [integration-test-architecture, verification-driver, backend-development, code-review-gate]
---

# 数据库设计规范

> **本 skill 不重复**: 应用层业务逻辑（service/repo 分层）→ `backend-development` §2.4；性能 e2e → `integration-test-architecture`；API 契约 → `backend-development`。本 skill 解决"按 SOP 设计/迁移/调优 schema"的工作流纪律，覆盖 SQL（PostgreSQL/MySQL/SQLite）+ NoSQL（MongoDB/DynamoDB 适配）。

## 1. 决策树（数据库开发路径）

```
任务是什么？
├─ 新 schema 设计   → §2.1 schema 设计 7 步
├─ 加表/加列/加索引 → §2.2 加不破坏（在线/迁移兼容）
├─ 改现有 schema    → §2.3 migration 策略（expand/contract + 回滚）
├─ 慢查询调优       → §2.4 查询优化决策树（索引/重写/分页/缓存）
├─ ORM/查询库选型    → §2.5 ORM 选型决策
├─ 数据迁移（大表/跨库）→ §2.6 数据迁移 SOP（双写/分批/回滚）
└─ 备份/灾备        → §2.7 备份策略（决定你 schema 演进的安全网）
```

## 2. 7 路径规范

### 2.1 新 schema 设计 7 步

1. **领域建模**：先画实体 + 关系（不用 ORM，直接 ER 图/白板）
2. **范式 vs 反范式决策**：OLTP 默认 3NF；OLAP/读重则可反范式
3. **主键选型**：UUID vs auto-increment vs ULID vs 雪花 ID（按业务场景选）
4. **外键 + 引用完整性**：业务上必有关系的表，外键 + index（**不要全信应用层维护**）
5. **索引规划**：查询 pattern 决定索引（不是"全表都有"——索引有写入代价）
6. **审计 + 软删除**：必加 `created_at` `updated_at` `deleted_at`（除非强需求）
7. **数据迁移与回滚**：新 schema 必带回滚 SQL（§2.3）

### 2.2 加表/加列/加索引 — 不破坏存量

**三阶段**（强制）：

```
1. Expand       → 加新表/新列，老代码不读
   deploy       → 老数据继续写入，迁新数据可选
2. Migrate      → 数据回填（如历史行补默认值）
   deploy       → 持续
3. Contract     → 改读路径用新列/删老列
   deploy       → 老列可下个版本 drop
```

**禁止**："加列同时改应用读"（一步到位 = 部署回滚困难）。

### 2.3 Migration 策略（expand/contract + 回滚）

```
每个 migration 必须显式写：
├─ up.sql     → expand/contract 步骤
├─ down.sql   → 回滚反向（**不省略**）
├─ risk       → 锁表?长事务?大表 DDL?
├─ rollback   → 验证 down 在生产跑过（staging 必跑）
└─ approval  → 大表改 schema 必 DBA/schema owner 双 sign
```

**风险分级**：
- 🟢 低: 加索引（CONCURRENTLY）、加列（带 default）
- 🟡 中: 加 NOT NULL 列（需 backfill）、改类型（cast 转换）
- 🔴 高: drop 列、改主键、rename 表（生产请错峰发版）

### 2.4 慢查询优化决策树

```
SQL 跑超过 100ms？
├─ EXPLAIN ANALYZE 看执行计划
│   ├─ Seq Scan（大表）→ 缺索引？
│   ├─ Index Scan 但 cost 高 → 索引选择性差？
│   ├─ Hash Join 卡 → 数据倾斜？
│   └─ Sort 卡 → 索引排好序？
├─ 是否回表过多？
│   ├─ 是 → 索引覆盖（covering index）含 select 列
│   └─ 否 → 业务能否拆查询
├─ 是否 N+1？
│   ├─ 是 → join / IN / dataloader
│   └─ 否 → 单查询已最优
├─ 数据量？
│   ├─ < 1000 行 → 不用优化（cache 即可）
│   ├─ 1000-100 万行 → 索引 + 改写
│   └─ > 100 万行 → 考虑分表/归档/分库
└─ 是否要实时？
    ├─ 是 → 索引 + cache
    └─ 否 → 离线 + 物化视图 + 批处理
```

### 2.5 ORM/查询库选型决策

```
语言 → 优先选
├─ Go     → sqlc（编译期 SQL 校验） > sqlx（手写 SQL） > GORM（全 ORM）
├─ Rust   → sqlx（宏校验） > diesel（重型）
├─ Node   → drizzle（轻量 schema-first） > prisma（重生成）
├─ Python  → sqlalchemy 2.x（typed） > django ORM

查询场景决定补工具：
├─ 复杂聚合 → 物化视图 + 触发器
├─ 全文搜索 → pg_trgm / Elasticsearch
├─ 地理查询 → PostGIS
└─ 时序数据 → TimescaleDB / InfluxDB
```

**反 ORM**：
- 不用 ORM 做必胜性能关键路径（手写 SQL + connection pool 更可控）
- 不用 ORM 做事务含外部依赖（DB 事务不能含 HTTP/queue——拆 saga）

### 2.6 大数据迁移 SOP

```
迁移目标 = 100 GB+ / 1 亿行+ / 跨库
├─ 评估：单机/分批/在线？
├─ 选工具：mydumper/pgloader/DMS/Liquibase/AWS DMS
├─ 试运行：源/目标 count + checksum 抽样
├─ 双写期：新老库同步写（应用层做，触发器可补）
├─ 数据校验：全量 row count + 关键字段 hash
├─ 切读：先 1% 灰度 → 100%（必须有开关回滚能力）
└─ 废弃老库：保留 ≥ 1 季度备份
```

### 2.7 备份 + 灾备策略

| 数据等级 | 备份频率 | RPO | RTO | 备份存储 |
|---|---|---|---|---|
| 关键（金融/订单）| 实时 | 秒级 | 分钟级 | 跨区域多副本 |
| 重要（用户数据）| 每 15 分 | 15 分 | 1 小时 | 跨区域 |
| 一般（日志/分析）| 每日 | 小时级 | 天级 | 单区域 |

**铁律**：
- 备份全链路验证恢复（不演练 = 没有备份）
- schema migration 不破坏老备份的可恢复性

## 3. 负向约束 + 替代方案

| 不要做 ❌ | 应该做 ✅ |
|---|---|
| 用 ORM 写 N+1（loop 调 find） | 一次 join / IN 查询 / dataloader |
| SELECT * | 只取需要的列（省 IO / 带宽） |
| 字符串存 enum | enum/int + 应用层 enum 映射 |
| 软删 + UPDATE + SELECT 都带 `where deleted_at is null` 漏写 | middleware 强制加（软删框架） |
| 数字 precision 不足 | 用 `decimal` / `numeric` 而非 `float` |
| 文本用 VARCHAR(255) 一刀切 | 按业务定大小（email/url/description 各不同） |
| 不用索引 JOIN 大表 | 必查 EXPLAIN；索引覆盖 |
| 业务上无索引也能跑 | 必查缺失索引 + EXPLAIN |
| `BIGSERIAL` 全用 | UUID（多机器/不暴 ID 进度）适合分布式 |

## 4. Post-Generation 自查清单

- [ ] schema 设计 ER 图与 ADR 同步
- [ ] migration 双向（up/down）齐全
- [ ] 索引 EXPLAIN 验证命中
- [ ] 软删字段 + 中间件强制
- [ ] 时区策略（DB UTC + 应用层 timezone）
- [ ] 备份 + 恢复演练 schedule
- [ ] permission/grant 限定（不 root 跑应用）
- [ ] 跨字符集/排序规则统一（utf8mb4_unicode_ci / en_US.UTF-8）
- [ ] 字段 `NOT NULL` + `DEFAULT` 显式
- [ ] 审计字段（created_at/updated_at/deleted_at/created_by）齐全
- [ ] review pass 通过（含 schema diff review）

## 5. Gotchas（实操易错点）

**G1**: 大表加索引无 `CONCURRENTLY` → 锁表 30 分。预防：PostgreSQL `CREATE INDEX CONCURRENTLY`，MySQL 在线 schema 工具（gh-ost/pt-online-schema-change）。

**G2**: enum 改值失败 → 老应用读新 enum decode 错。预防：版本化（`status_v1`/`status_v2`）+ 双 enum 过渡期。

**G3**: 软删真删难辨 → 误恢复。预防：`deleted_at` 索引 + 删除前 migration script 审计 + 真删走工具不靠 `DELETE`。

**G4**: 时区错位（`TIMESTAMP WITHOUT TIME ZONE`）→ 全球用户 bug。预防：DB 全 `TIMESTAMPTZ` + 应用 UTC。

**G5**: `BIGINT` 转 `INT` 数据丢失 → 静默。预防：大字段严禁降级，必须新加字段。

**G6**: 索引加太多 → 写性能下降 + 磁盘涨。预防：每加一索引算代价（写放大）；EXPLAIN 证明必要。

**G7**: 外键 cascade 误删 → 数据雪崩。预防：软删 + 真删分两步，cascade 用 `ON DELETE SET NULL` 不 `CASCADE`。

**G8**: N+1 ORM 默认 → 性能塌方。预防：N+1 检测测试 + ORM config 强制 preload。

## 6. 提交前必跑

```bash
# 1. schema diff
forge review pass                       # 触发 schema-diff review

# 2. migration dry-run（staging）
forge skills eval --skill=database-design
# 或手动跑：
psql -f up.sql --dry-run
psql -f down.sql --dry-run

# 3. EXPLAIN 必查
psql -c "EXPLAIN ANALYZE <query>"
# 或
mysql -e "EXPLAIN <query>"

# 4. perf 回归
forge integration-test --scope=db
```

不过 → §4 自查清单补足；过 → commit + DBA review（大表）。

## 7. 与其他 skill 的协作

- **应用层**：`backend-development` — repo/service 用本 skill 设计的 schema
- **测试**：`integration-test-architecture` + `verification-driver` — 数据契约断言
- **性能**：`performance` 主题不属于任何单一 skill——本 §2.4 是 dev-specific
- **安全**：`on-demand-guards` — 数据库 hardening / SQL 注入检查
- **错误排查**：`systematic-debugging` + 本 §2.4
- **跨服务契约**：`verification-driver`

## 8. SQL 与 NoSQL 适配参考

| 类型 | 适合 | ORM 建议 |
|---|---|---|
| **PostgreSQL** | 复杂查询/事务/全文/地理 | sqlc / sqlx |
| **MySQL** | 读多写少/InnoDB 引擎 | drizzle / typeorm |
| **SQLite** | 嵌入式 / 简单应用 | peewee / GORM |
| **MongoDB** | schema-less / 文档 / 时间序列 | prisma / mongoose（避免全模型） |
| **DynamoDB** | 极致水平扩展 / 简单 query | aws-sdk v3（直接 + PartiQL，少用 ORM） |
| **Redis** | 缓存 / session / 简单 KV | go-redis / ioredis（不适合主存储）|

## 参考

- 完整 references 进 `references/`（SQL 方言差异/索引基数规则/迁移案例）
- 写法参照 `skill-authoring-standard`
