# 环节审查：后端设计（backend）

针对后端设计阶段产物（services/ domain/ 业务逻辑文件、领域模型、状态机设计）的审查清单。与 code-review-gate 的代码级审查互补不替换——**只审显式设计产物文件**，代码 diff 走 code-review-gate。

**规范来源**：DDD(Evans) · Clean Architecture · Twelve-Factor · 阿里 Java 手册
**核心维度**：分层 / 领域 / 状态 / 事务

---

## 1. 分层架构

- [ ] 分层清晰（controller → service → repository → domain），无跨层调用
- [ ] 依赖方向正确（高层依赖低层抽象，不反向）
- [ ] 无循环依赖（模块间依赖构成 DAG）
- [ ] 全局中间件可配置（非硬编码）

## 2. 领域模型

- [ ] 领域边界清晰（Bounded Context 不互相渗透）
- [ ] 领域对象有不变量保护（构造函数/ setter 保证）
- [ ] 贫血模型 vs 充血模型选择一致
- [ ] 值对象不可变（Value Object）

## 3. 事务与状态

- [ ] 事务边界清晰（@Transactional 范围合理，不包围 HTTP 调用）
- [ ] 状态机完备（所有状态转换有定义，无非法转换）
- [ ] 并发幂等（重试/重复请求不产生副作用）
- [ ] 错误类型跨层一致（不混用 panic/err/返回码）

## 4. 配置与硬编码

- [ ] 配置从外部注入（env/config file），不写死在代码中
- [ ] 密钥/密码/连接串从环境变量或密钥管理读取
- [ ] 超时/重试参数可配置

---

## 确定性规则（机械可检）

| 规则 | 检测方式 | 来源 |
|------|----------|------|
| 硬编码密钥 | 正则扫描 `"AKIA[0-9A-Z]{16}"` / `"mongodb://"` / `"mysql://"` 在代码字面量中 | 安全最佳实践 |
| 无 WHERE 的 DELETE/UPDATE | 正则扫描 `DELETE\s+FROM` / `UPDATE\s+\w+\s+SET` 后无 WHERE | SQL 安全清单 |
| 事务包围 HTTP | 扫描 `@Transactional` 内是否有 `http\.\|HTTP\.` 调用 | DDD/事务最佳实践 |

## 与大厂规范的映射（方向，非条文）

- **DDD(Evans)** → 领域边界、Bounded Context、充血模型
- **Clean Architecture** → 依赖方向、分层架构
- **Twelve-Factor** → 配置外部化、后台进程
- **阿里 Java 手册** → 事务注解使用、异常处理规范

---

**与其他审查的分工**：
- 后端设计产物审查 → 本 checklist（审 services/domain 文件）
- 代码实现质量 → `review-checklist.md`（审代码 diff）
- API 设计审查 → `phase-api.md`（审接口定义）

**数据来源**：Eric Evans《Domain-Driven Design》、Robert Martin《Clean Architecture》、Twelve-Factor App、阿里 Java 开发手册。
