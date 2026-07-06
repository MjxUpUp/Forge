# 环节审查：API 设计（api）

针对 API 设计阶段产物（OpenAPI/Swagger 规范、接口定义文档、proto 定义、API 设计文档）的审查清单。与 code-review-gate 的代码级审查互补不替换——**只审显式设计产物文件**，代码 diff 走 code-review-gate。

**规范来源**：Google API Design Guide · Microsoft REST Guidelines · OpenAPI Spec · JSON:API
**核心维度**：一致 / 版本 / 兼容 / 契约

---

## 1. 版本管理

- [ ] 破坏性变更（删除字段、改类型、改语义）必带**版本号**或迁移路径
- [ ] URL 中不嵌入版本号时，有明确的**版本策略**（header versioning / prefix）
- [ ] 废弃 API 有标记（Deprecated 标记 + 替代接口指引）

## 2. 命名一致

- [ ] **无动词在 URL 中**（RESTful 用 HTTP 方法表达操作，URL 应是名词资源）
- [ ] 资源命名一致（`/users` 不混用 `/user` 和 `/users`）
- [ ] 集合用复数，单数资源用单数
- [ ] 参数命名直觉（`page_size` 而非 `pageSize` 全文一致；全文 snake_case 或 camelCase 统一）
- [ ] 错误码命名一致（`error_code` 全文统一格式，非随机命名）

## 3. 状态码语义正确

- [ ] 200 = 成功，201 = 创建，204 = 无内容成功
- [ ] 400 = 客户端错误，401 = 未认证，403 = 已认证无权限，404 = 不存在
- [ ] 422 = 语义正确但不可处理（如校验失败），非 400
- [ ] 500 = 服务端错误，502/503 = 网关/服务不可用
- [ ] 不滥用 200 返回所有情况（成功/失败混用同一状态码）

## 4. 参数设计

- [ ] **分页统一**（有标准分页参数：page/page_size 或 cursor；非每个接口自定）
- [ ] **排序统一**（有标准排序参数：sort/order；非每个接口自定）
- [ ] **过滤统一**（有标准过滤参数：filter/query params；非每个接口自定）
- [ ] 可选参数有默认值说明
- [ ] 必填参数明确标记（required / 非 optional）
- [ ] 参数类型跨层一致（DB 整数 ≠ API 字符串，需明确转换）

## 5. 错误模型

- [ ] 错误响应格式统一（如 `{ "error": { "code": "...", "message": "..." } }`）
- [ ] 错误信息对客户端有可操作性（非 "Internal error"）
- [ ] 错误码全局唯一，可查文档
- [ ] 幂等操作明确标识（GET/PUT/DELETE 天然幂等；POST 需说明）

## 6. 兼容性

- [ ] 新增字段非破坏性（不要求客户端重发所有字段）
- [ ] 删除/重命名字段有过渡期（不立即删除）
- [ ] 公开 API 必有文档（OpenAPI/Swagger/设计文档）
- [ ] 内部 API 与外部 API 有明确区分

---

## 确定性规则（机械可检）

| 规则 | 检测方式 | 来源 |
|------|----------|------|
| 公开 API 无文档 | `file_exists` 对应 OpenAPI/Swagger 文件 | Google API Design |
| 破坏性变更无版本 | 正则扫描 "delete\|remove\|drop\|rename" 后无 "v\d" 或 "migration" | REST 版本规范 |
| URL 含动词 | 正则 `/(get|post|put|delete|list|create|update|remove)\b` 在资源路径段 | RESTful 约定 |
| 状态码滥用 | 扫描 "200" 后跟 "error\|fail\|exception" | HTTP 语义标准 |

## 与大厂规范的映射（方向，非条文）

- **Google API Design Guide** → URL 命名、HTTP 方法使用、分页/排序/过滤统一
- **Microsoft REST Guidelines** → 错误模型、版本策略、幂等性
- **OpenAPI Spec** → 契约完整性（参数、响应、错误格式）
- **JSON:API** → 错误响应格式、关系字段、过滤/排序标准

---

**与其他审查的分工**：
- API 契约设计审查 → 本 checklist（审 OpenAPI/proto/接口定义文档）
- API 实现代码质量 → `review-checklist.md` 第 9 节（审代码实现）
- 需求设计产物审查 → `phase-requirement.md`（审 PRD/需求文档）

**数据来源**：Google API Design Guide、Microsoft REST API Guidelines、OpenAPI Specification 3.1、JSON:API 1.1。
