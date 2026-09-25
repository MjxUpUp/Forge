# 设计模式（metadata.pattern 取值）

| 模式 | 用途 | 特征 |
|------|------|------|
| `pipeline` | 逐步执行流程 | 有明确步骤，每步有门控 |
| `reviewer` | 质量检查 | 检查清单，按严重性分级 |
| `gate` | 决策门控 | 必须满足条件才能继续 |
| `tool-wrapper` | 领域知识封装 | 易错点 + 模式 + 参考 |
| `inversion` | 先问后做 | 以向用户提问开始 |
| `routing + fallback` | 按类型路由 + 降级链 | 路由表 + 失败终止点 |
| `reference` | 经验/踩坑记录 | 单一项目实录、语言无关规则 + 坑清单，无流程无门控 |

合法取值与组合规则（`+` 组合、每段须合法）的机器校验定义见 references/validation-rules.md R7。
