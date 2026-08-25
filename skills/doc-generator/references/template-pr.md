# PR 描述模板

## 变量列表

**必填**：
- `motive`：为什么改（用户可见的问题/需求，一句话；不写"重构了代码"这类无动机描述）
- `approach`：方案要点（改了什么、关键取舍；不复述 diff——读者看 diff 本体）
- `verification`：怎么验证的（实跑的命令/测试/手工步骤 + 结果）

**选填**：
- `scope`：影响范围（哪些模块/接口受影响，破坏性变更必填）
- `rollback`：回滚方式（默认"revert 此 PR"）
- `links`：关联 issue/任务/设计文档

## 章节结构

```
## 动机

{motive；问题对用户的影响，或引用 issue 原文}

## 方案

{approach；关键设计点与取舍用要点列出，>3 点考虑"是否该拆 PR"}

## 验证

{verification；实跑命令用反引号包裹 + 结果（通过/失败数/截图链接）}

## 影响范围与回滚

{scope + rollback；无破坏性变更可写"无破坏性变更，revert 即回滚"}
```

## 风格示例

> ## 动机
>
> `forge task complete` 在无验收标准的任务上静默跳过 pre-flight（#123），导致验收声明形同虚设。
>
> ## 方案
>
> - complete 前显式校验每条 acceptance 的快照 commit
> - 逃生舱走既有 override 机制，不新开 env
>
> ## 验证
>
> - `go test ./internal/taskpipeline/ -run TestAcceptance` 通过（12 cases）
> - 手工：声明 2 条验收 → complete 被拒 → 实跑后放行

动机一段讲清、方案不复述 diff、验证给实跑命令与结果——三段之外不加"总结"章节。
