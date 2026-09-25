# cross-tool-context · Forge 集成

仅 forge 项目适用。skills 零反向依赖契约（CONVENTIONS §13）下，本 skill 的 forge 集成知识由 forge 侧维护（原 references/forge-integration.md 迁入）。

## 双向锚定命令组（主路径，替代 AI_CONTEXT.md 纪律约定）

跨工具接续 = 各工具把信息写进同一个 forge task（持久化在用户级 DataDir `~/.forge/projects/<key>/`，不随会话/压缩丢失），任意工具 resume 即拉回：

- **开工前**：`forge task resume [--ref <ref>]` 拉回结构化上下文（目标/决策/发现/下一步/阻塞 + 门禁进度 + git 已改未提交）。
- **做了决策**：`forge task decide --content "..." --by [当前工具]`。
- **发现问题**：`forge task finding --content "..." --source [当前工具] --evidence file:line`（来源工具自动记录）。
- **跨工具锚定**：`forge task attach --ref <ref> --tool <当前工具>` 把当前工具的 session 锚定到 task——任意工具 resume 即知"谁参与过、用什么工具"。

forge task 是结构化真相源（可查询、抗压缩丢失、跨工具双向锚定）。`forge task context` 可导出 markdown 视图，供无 forge 的工具/人类阅读。

## 从 AI_CONTEXT.md 迁移

AI_CONTEXT.md 是纯纪律约定（无工具强制，长期易漂移）；项目引入 forge 后应把 AI_CONTEXT.md 的存量条目迁入 task（decide/finding 各归其位），文件退役或仅留指针。
