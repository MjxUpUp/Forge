# 改动

## 改什么

- `internal/taskpipeline/acceptance.go`：`runAndJudgeCriterion` 里新增「选择器未匹配任何测试」判定，
  抽成纯函数 `RunSelectorMatchedNothing(run, output)` 便于单测；命中即把 exitOK 判负并追加一行说明。
- `internal/clitask/task_redfirst_decl.go`（新文件）：新增 `CheckRedFirstDeclared`，行为类任务（改动集含源码扩展名）
  且 `RedAttempts` 为空且未豁免时返回阻断错误；豁免字段 `RedFirstWaived` 由 `task start --red-first-waived "<理由>"` 写入状态。
- `internal/clitask/task_complete.go`：在既有 `CheckRedFirst` 前调用新检查。
- `internal/clitask/task_report.go`：交付单新增「豁免登记（理由：…）」分支——豁免不能和「未登记」
  渲染成同一句话，否则显式豁免退化成静默跳过（配套 `TestBuildTaskReport_WaiverDisclosed`）。
- `internal/cli/review.go`：`--criteria-reviewed` 缺省从 `-1` 改为「未提供即报错」，
  并给出「0 表示一条都没核对」的说明。
- 对应单测三处 + 一处回归。

## 影响面

- 文档：`docs/reference/cli.md` 与协议模板 `internal/change/templates/agents-protocol.md` 提到 review pass 时补一句「必须声明核对判据数」。
- 测试：新增 taskpipeline / clitask / cli 三处单测；`go test ./...` 全包回归。
- 兼容：老任务没有 CriteriaReviewed 记录时，task-complete 不追溯（只对新评审生效）；
  `--accept-red` 豁免走显式 flag，不改默认豁免面。

## 怎么验证

acceptance.md 四条：两条红判据先在实现前失败，实现后转绿；一条 CLI 单测；一条三包全跑。

## 已知风险

- B1 只覆盖 task 侧的 acceptance 执行（`internal/taskpipeline/assertion_judge.go`）。`forge change verify`
  走 `internal/change/verify.go:333` 自己的 `sh -c` 执行器，同样只看退出码，`-run` 空匹配在那里仍会封印通过。
  本批不动它是为了守住 clarify 的边界；下一批（B2）应把同一条判定接到 change 侧执行器上，并补一条夹具。
- 「行为类」判定靠扩展名白名单（.go/.ts/.tsx/.js/.jsx/.py/.rs/.java/.kt），新增语言需要同步该表——
  漏掉的语言会退回「不阻断」，这是显式的覆盖边界，不是遗漏。
