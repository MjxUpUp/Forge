# 设计模式快速参考

基于 [Thariq 的文章](https://x.com/trq212/article/2033949937936085378) 和 Google 的 5 种 Agent Skill 模式。

## Thariq 的 9 种技能类型

| 类型 | 说明 | 我们的对应技能 |
|------|------|-------------|
| 1. Library & API Reference | 库/CLI/SDK 使用指南 | dev-lookup（库/API/CLI 单点检索） |
| 2. Product Verification | 测试和验证代码 | **融入 dev-workflow 的验证门控** |
| 3. Data Fetching & Analysis | 连接数据和监控 | （按需创建） |
| 4. Business Process & Automation | 自动化重复工作流 | dev-workflow, session-continuity |
| 5. Code Scaffolding & Templates | 生成脚手架代码 | doc-generator（文档脚手架）；栈特定脚手架已剥离到全局 |
| 6. Code Quality & Review | 代码质量和审查 | code-review-gate, review-batch, test-discipline |
| 7. CI/CD & Deployment | 部署和持续集成 | （按需创建） |
| 8. Runbooks | 故障排查手册 | （按需创建） |
| 9. Infrastructure Operations | 基础设施运维 | （按需创建） |

## Google 的 5 种模式

| 模式 | 关键特征 | 使用场景 |
|------|---------|---------|
| **Tool Wrapper** | `references/` 目录按需加载 | 加载领域知识 |
| **Generator** | 模板 + 风格指南 | 生成一致输出 |
| **Reviewer** | 审查清单 + 严重性分级 | 质量检查 |
| **Inversion** | 分阶段提问，硬性门控 | 先收集需求 |
| **Pipeline** | Diamond Gates，步骤间确认 | 多步骤流程 |

## 组合规则

- **Inversion + Pipeline**：先问后做，然后逐步执行
- **Pipeline + Reviewer**：每个 Pipeline 以质量检查结尾
- **Generator + Inversion**：填模板前先收集变量
- **Tool Wrapper + Reviewer**：加载标准，然后对照检查

## Thariq 的核心建议

1. **不要说显而易见的事** — 只写 Agent 不知道的
2. **易错点是最高价值内容** — 从真实失败中积累
3. **渐进式披露** — 技能 = 文件夹，不只是 markdown
4. **避免过度限制** — 给灵活度，让 Agent 适应不同情况
5. **描述 = 触发条件** — 不是摘要
6. **记忆** — 在技能目录中存储数据用于跨会话
7. **脚本优于散文** — 给可组合的代码
8. **按需 Hooks** — 仅在调用时激活

## 技能质量检查清单

- [ ] 描述包含触发场景（"当...时使用"）
- [ ] SKILL.md 正文不超过 500 行
- [ ] 有来自真实失败的易错点章节
- [ ] 使用渐进式披露（references/ 存放详情）
- [ ] 关键步骤间有验证门控
- [ ] 不硬编码特定语言——通过组合适配
- [ ] Pipeline 技能包含 Reviewer 步骤
