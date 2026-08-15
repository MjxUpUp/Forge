# 环节审查：数据库设计（database）

针对数据库设计阶段产物（migrations、schema 定义、索引设计文档）的审查清单。与 code-review-gate 的代码级审查互补不替换——**只审显式设计产物文件**，代码 diff 走 code-review-gate。

**规范来源**：SQL Style Guide · 阿里 Java 手册 DB 篇 · Flyway 最佳实践
**核心维度**：范式 / 索引 / 迁移可逆

---

## 1. 迁移可逆性

- [ ] 每条迁移有 up/down（可回滚）
- [ ] DOWN 迁移不丢数据（ALTER COLUMN 类型变更有数据迁移策略）
- [ ] 索引/表删除前确认无应用引用
- [ ] DROP TABLE/DROP DATABASE 有事务保护

## 2. 索引策略

- [ ] 外键索引显式命名（非自动生成名）
- [ ] 复合索引顺序合理（等值条件在前，范围条件在后）
- [ ] 无过度索引（写放大 > 读收益）
- [ ] 大表分页用游标/seek 而非 OFFSET

## 3. 字段设计

- [ ] 字段类型跨层一致（DB integer ≠ API string）
- [ ] 不使用 TEXT 存小字符串（用 VARCHAR）
- [ ] 时间字段用 TIMESTAMPTZ（非 TIMESTAMP）
- [ ] 软删除字段有索引（is_deleted）

## 4. 破坏性操作防护

- [ ] 无 WHERE 的 DELETE/UPDATE 标记为高风险
- [ ] DROP/TRUNCATE 在事务内
- [ ] GRANT ALL 限定到具体表而非整个 schema
- [ ] 生产直连有超时和限制

---

## 确定性规则（机械可检）

| 规则 | 检测方式 | 来源 |
|------|----------|------|
| 迁移无 DOWN | 扫描 migration 文件无 `-- DOWN` 标记 | Flyway 最佳实践 |
| 无 WHERE 的 DELETE/UPDATE | 正则扫描 `DELETE\s+FROM|UPDATE.*SET` 后无 WHERE | SQL 安全清单 |
| DROP 无复核 | 扫描 `DROP\s+(TABLE|DATABASE|INDEX)` | 破坏性 SQL |

## 与大厂规范的映射（方向，非条文）

- **Flyway** → 迁移命名、可回滚、版本控制
- **SQL Style Guide** → 命名规范、缩进、关键字大写
- **阿里 Java 手册 DB 篇** → 索引规范、字段类型、建表约定

---

**与其他审查的分工**：
- 数据库设计产物审查 → 本 checklist（审 migrations/schema 文件）
- 代码实现质量 → `review-checklist.md`（审代码 diff 中的 SQL）
- API 设计审查 → `phase-api.md`（审接口定义）

**数据来源**：Flyway 迁移最佳实践、SQL Style Guide、阿里 Java 开发手册。
