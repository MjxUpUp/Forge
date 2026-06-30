# DB/SQL 破坏性操作审查清单

AI 生成**迁移脚本**、**数据修复脚本**、**运维 SQL** 时高频出现的**破坏性** DDL/DML。与 [review-checklist.md](review-checklist.md) 第 5 维度的 SQL **注入**（字符串拼接）正交——这里聚焦"语法合法但会摧毁数据/权限"的操作。一次失误清空生产库、误授全库权限，回滚成本极高（甚至不可逆）。

**铁律：命中以下任一模式 → 直接 `block` 级，必须确认有备份/回滚/限定了范围才能放行。**

`category` 归 `security`（不新增 sql-safety 类，保持与 [severity-rubric.md](severity-rubric.md) + subagent-contract 的维度正交——破坏性 SQL 本质是安全/数据完整性问题）。

---

## 1. DROP DATABASE / TABLE / SCHEMA

**指纹**：迁移或脚本里出现 `DROP DATABASE` / `DROP TABLE` / `DROP SCHEMA`（含经 `mysql -e` / `psql -c` / `sqlite3` 执行的）。

**示例**：
```diff
+ -- 迁移脚本"清理旧表"
+ DROP TABLE users;
+ DROP TABLE orders;
```
```python
+ cursor.execute(f"DROP SCHEMA IF EXISTS {tenant_schema}")
```

**为什么是问题**：`DROP` 删的是**结构和全部数据**，非 `DELETE` 的行级。无备份 = 永久丢失。AI 常在"重构表结构"时直接 DROP+CREATE，而非 ALTER——丢了数据还觉得"只是改了结构"。`DROP COLUMN` 同理（删列即删该列所有数据）。

**检测方法**：
- 搜 `DROP DATABASE` / `DROP TABLE` / `DROP SCHEMA` / `DROP COLUMN`（大小写不敏感）
- 命中即问：有 `IF EXISTS` 之外的备份吗？能改用 `ALTER TABLE` / `RENAME` 吗？是迁移的 `up`，那对应的 `down`（重建表+恢复数据）在哪？

---

## 2. TRUNCATE

**指纹**：`TRUNCATE TABLE` / `TRUNCATE`（清空表保留结构）。

**示例**：
```diff
+ -- "重置测试数据"
+ TRUNCATE TABLE sessions;
```
```sql
-- 运维脚本误连生产
TRUNCATE audit_log;
```

**为什么是问题**：`TRUNCATE` 比 `DELETE` 快（不记逐行日志），但**不可回滚**（多数 DB 是 DDL 语义，立即提交），且**绕过外键/触发器**。AI 常用它"快速清表"，误连环境或漏 `WHERE` 就清空生产。与 `DELETE` 不同，`TRUNCATE` 不能加 `WHERE`——出现就是清空全表。

**检测方法**：
- 搜 `TRUNCATE`：每处都要确认目标表 + 执行环境
- 问：这是测试库还是生产？有 `WHERE` 需求却用了 `TRUNCATE`（应该 `DELETE WHERE`）吗？

---

## 3. DELETE / UPDATE 无 WHERE

**指纹**：`DELETE FROM t` / `UPDATE t SET ...` 不带 `WHERE` 子句（全表操作）。

**示例**：
```diff
+ -- "删除过期记录"
+ DELETE FROM events;
+ -- 忘了 WHERE created_at < ...
```
```python
+ # AI 拼接，condition 变量为空时退化成全表更新
+ stmt = f"UPDATE products SET price = {new_price}"
+ if where: stmt += f" WHERE {where}"
```

**为什么是问题**：无 `WHERE` 的 `DELETE` 清空全表，无 `WHERE` 的 `UPDATE` 改全表。AI 生成时常把 `WHERE` 放在条件分支或拼接里，变量为空时静默退化成全表操作。这是经典"运维事故"根因。

**检测方法**：
- 对每条 `DELETE`/`UPDATE`：有 `WHERE` 吗？`WHERE` 是硬编码常量还是可空变量？
- 动态拼接 SQL 时，确认 `WHERE` 子句不可能为空字符串（加 assertion/默认拒绝）
- hazard-guard hook 已对裸 `DELETE...FROM`/`UPDATE...SET` 无 `WHERE` 做拦截预警（详见 on-demand-guards），审查时复核语义边界

---

## 4. GRANT ALL / TO PUBLIC

**指纹**：`GRANT ALL` / `GRANT ... TO PUBLIC`（过度授权）。

**示例**：
```diff
+ -- "让应用能访问"
+ GRANT ALL ON *.* TO 'app'@'%';
```
```sql
GRANT SELECT, INSERT, UPDATE, DELETE ON production.* TO PUBLIC;
```

**为什么是问题**：`ALL` 权限含 `DROP`/`ALTER`/`GRANT OPTION`，应用账号不该有。`TO PUBLIC` 授权给所有用户（含未来新建的）。AI 生成权限脚本时常"给足权限省事"，违反最小权限原则——一旦应用账号泄露，攻击者能 `DROP` 整库。

**检测方法**：
- 搜 `GRANT ALL` / `TO PUBLIC`：几乎总是过度授权
- 问：应用账号真的需要 `DROP`/`ALTER` 吗？能否只 `SELECT/INSERT/UPDATE/DELETE`？`PUBLIC` 是不是该换成具体角色？

---

## 5. 生产环境直连（hardcoded prod DSN）

**指纹**：代码/脚本里硬编码生产数据库连接串（`prod-db.internal` / 含真实密码的 DSN）。

**示例**：
```diff
+ # "数据修复脚本"
+ conn = psycopg2.connect("host=prod-rds.internal dbname=prod user=admin password=S3cret!")
```
```python
+ # AI 直接连生产跑迁移
+ engine = create_engine("mysql://root:pass@10.0.0.5:3306/prod")
```

**为什么是问题**：三重风险——① 凭证泄露（提交进 git = 永久暴露，即使后续删）；② 绕过环境隔离（开发脚本直连生产，误操作无沙箱）；③ 无审计（直连不走发布流程，没有 review/回滚）。AI 写"快速修复脚本"时常直接连生产。

**检测方法**：
- 搜连接串里的 `prod`/`production`/内网 IP/真实密码
- 任何硬编码 DSN 都要 `block`：改用环境变量 + 明确的"只连非生产"断言
- 同时触发 review-checklist 第 5 维度的"硬编码密钥"（L72）

---

## 6. 不可逆迁移（无 down / rollback）

**指纹**：迁移只有 `up`（正向），无对应 `down`（回滚）；或含 `DROP COLUMN` / `ALTER TYPE`（不可逆改类型）等无法回滚的操作。

**示例**：
```diff
+ def up():
+     op.drop_column('users', 'legacy_field')   # 删列=删数据，down 无法恢复
+ # 无 down()：部署后无法回滚
```

**为什么是问题**：部署后发现问题，无 `down` = 无法回滚到上一版本，只能前向修复（高风险）。`DROP COLUMN`/改类型/删表的迁移，即使写了 `down` 也恢复不了数据（结构能重建，数据没了）。AI 生成迁移时常只写 `up`。

**检测方法**：
- 每个迁移：有 `down`/`rollback` 吗？
- `up` 含 `DROP COLUMN`/`DROP TABLE`/改类型 → `down` 即使存在也是假的（数据回不来），标记 `block`，要求分步迁移（先停用→备份→删除）
- 参考 review-checklist 第 9 维度"破坏性变更有迁移路径"（L130）

---

## 7. 未事务化多语句

**指纹**：多条 DML（INSERT/UPDATE/DELETE）顺序执行，未包在单个事务里；任一条失败留下部分应用的脏数据。

**示例**：
```diff
+ # "转账：扣A加B"
+ db.execute("UPDATE accounts SET balance = balance - 100 WHERE id = 1")
+ db.execute("UPDATE accounts SET balance = balance + 100 WHERE id = 2")
+ # 第二条失败 → 钱凭空消失（A扣了B没加）
```

**为什么是问题**：多步操作无事务 = 部分失败留中间态。数据一致性被破坏，且难发现（每条单独看都成功）。AI 写"批量更新"常忽略事务边界。

**检测方法**：
- 多条写操作在同一逻辑单元 → 必须包 `BEGIN/COMMIT`（或 ORM 的 `transaction()`）
- 有 `ROLLBACK` 的错误路径吗？还是失败后静默继续？
- 跨服务/跨库的"事务"用 saga/补偿事务，不是 DB 事务

---

## 与 SQL 注入的分工（不要重复查）

- **本清单**：查**破坏性**操作（语法合法但摧毁数据/权限/结构）——`category=security`，`block` 级。
- **review-checklist.md L73（无 SQL 字符串拼接）**：查**注入**（用户输入拼进 SQL 导致越权读写）——同样 `security`，`block` 级。

两者正交：一条 SQL 可以"用参数化查询（无注入）+ 但 TRUNCATE 了生产表（破坏性）"。审查时都要查。

---

## 检测要点速查

| 模式 | grep 关键词 | 默认级别 |
|---|---|---|
| DROP DATABASE/TABLE/SCHEMA/COLUMN | `DROP (DATABASE\|TABLE\|SCHEMA\|COLUMN)` | block |
| TRUNCATE | `TRUNCATE` | block |
| DELETE/UPDATE 无 WHERE | `DELETE FROM` / `UPDATE.*SET` 无 `WHERE` | block |
| GRANT ALL / TO PUBLIC | `GRANT ALL` / `TO PUBLIC` | block |
| 生产直连 | 连接串含 `prod`/内网IP/真实密码 | block |
| 不可逆迁移 | 迁移无 `down` / 含 `DROP COLUMN` | block |
| 未事务化多语句 | 多条 DML 无 `BEGIN/COMMIT` | block |

`hazard-guard` hook（on-demand-guards 自动挡）已对 DROP/TRUNCATE/GRANT ALL/无 WHERE DELETE 这几类做**执行前拦截**（HITL 确认）。本清单是**代码审查**层——在提交前发现这些问题，不依赖 hook 运行时拦。
