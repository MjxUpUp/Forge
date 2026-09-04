# 决策树完整版：什么算 Ready 可放行

```
Start
  │
  ├── 任一 Mandatory（M1-M7）未过？
  │     └── YES → 🚫 NO-GO. 列出阻断项 + 不通过怎么办的方案，回到修复。
  │              禁止"差不多能发先发了再说"。
  │
  ├── M6 仅人工核对通过（无 docs-consistency-guard 守卫）？
  │     └── YES → ⚠ GO-WITH-RISK（强制，不能升 GO）。
  │              即使其他 M 全过、R 全过——M6 无自动化守卫 = 文档一致性是弱校验（§防注水自检 点名的反模式）。
  │              "已知风险"段注明"M6 无自动化守卫，人工核对 N 表"，并要求下次发布前建立守卫，不能长期依赖人工。
  │
  ├── 全部 Mandatory 过（M6 经守卫测试绿）+ 全部 Recommended 过？
  │     └── YES → ✅ GO. 产出 checklist.md，按发布流程执行。
  │
  └── 全部 Mandatory 过（M6 经守卫测试绿）+ 部分 Recommended 未过？
        │
        ├── 未过的 Recommended 项影响范围可控（用户无感知 / 有降级路径）？
        │     └── YES → ⚠ GO-WITH-RISK.
        │              在发布说明"已知风险"段逐项记录：
        │              - 未过项 / 影响范围 / 降级策略 / 触发回滚的条件
        │              并取得干系人 explicit ack（不是默认同意）。
        │
        └── 未过的 Recommended 项影响用户数据/安全/收入？
              └── YES → 🚫 NO-GO. 升级为阻断（Recommended 不等于可忽略）。
```

**禁止模糊结论**：不说"差不多能发""应该没问题""先发了看看"。给明确的 GO / NO-GO / GO-WITH-RISK 三档之一，附阻断项或风险项。
