# HANDOFF 标准格式（跨会话/跨工具交接）

> 本文件是 HANDOFF 格式的唯一真相源，session-continuity 与 cross-tool-context 均引用此处，不各自复制。

会话结束或切换工具时，写一份结构化 HANDOFF 到项目根 `HANDOFF.md`（或 `AI_CONTEXT.md` 的 `## Current Handoff` 节），让下一个会话/工具能冷启动续做。**统一格式，不要每次手抄不同结构**：

```markdown
# HANDOFF — <项目名> @ <YYYY-MM-DD HH:MM>

## 当前任务
- 主线：[一句话当前在做什么]
- 进度：[做到哪一步 / 完成度]

## 调用栈（恢复时按序读）
1. <文件:行> — <这个文件当前状态/为什么重要>
2. <文件:行> — <同上>

## 已修改未提交
- <文件> — <改了什么，是否验证过>

## 待验证项
- [ ] <编译/测试/端到端验证还没跑的>

## 已知问题/阻塞
- <问题> — < workaround 或状态>

## 下一步
1. <恢复后第一件事>
```

**关键纪律**：
- HANDOFF 是给**冷启动的下一个会话**看的，不是给自己备忘——写清楚"为什么"不只写"是什么"
- 跨工具交接（A 工具→B 工具）时，双方都读写同一份 `AI_CONTEXT.md`，见 **cross-tool-context** skill
- HANDOFF 不是永久文档，任务完成后可删或归档到 `docs/session-log.md`
