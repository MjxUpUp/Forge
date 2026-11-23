# 测试账本：真相源与四态

## 要解决的问题

仓库里"有没有测试"和"测试有没有真的跑过"是两个问题。前者可以数文件，
后者只有执行记录能回答。这条边界不清时，最坏的情况是：测试早就被删了、
被 `t.Skip` 了、或者换了名字，而报告里仍然显示"有测试"。

本仓的口径只有一条：**没有执行记录 = 不知道跑没跑 = orphaned**。不把"看起来
有测试"当成通过。

## 四态

| 状态 | 判据 | 该做什么 |
|---|---|---|
| live | 源码里有声明，且 `MaxAge`（默认 7 天）内有 pass/fail 记录 | 无事 |
| orphaned | 源码里有声明，但没有足够新的记录 | 跑一次并 `forge test record`，或者接受它是"没人跑过" |
| skipped | 源码里调用了 `t.Skip*` | 写清理由；再由人在 `spec/test-skips.yaml` 审批 |
| phantom | 记录里有、源码里已无声明 | 测试被删/改名了——留痕，别让它从账本里消失 |

## 记录格式

执行记录是 JSONL，一行一条，由 `forge test record` 从 `go test -json` 事件流
生成：

```json
{"package":"github.com/MjxUpUp/Forge/internal/impact","test":"TestCollect","action":"pass","at":"2026-11-23T11:00:00Z"}
```

只保留 `pass` / `fail` / `skip` 且带测试名的事件。损坏行被跳过并计数——一行
坏数据不应该吞掉整本账。

## Skip 的两件事

- **理由**：`t.Skip("为什么")` 的参数必须是非空字符串字面量。`t.Skip()` 和
  `t.Skip(msg)`（变量）都算没写理由——账本判不了变量的值，就不替它背书。
- **审批**：理由写得再好也只是作者自述。`spec/test-skips.yaml` 里由人登记
  `approved_by` 之后才算合法跳过：

```yaml
skips:
  - test: github.com/MjxUpUp/Forge/internal/e2e.TestNeedsDevice
    reason: 需要真实设备，CI 上没有
    approved_by: lead
    at: 2026-11-23
```

`forge test ledger --strict` 对「缺理由」「未审批」「phantom」三类判失败；
不加 `--strict` 时只报告（第一次接入时全仓都是 orphaned，那不该是一次红灯）。

## 增量变异探针

`forge test probe --since <rev>` 只取该 rev 之后 diff 命中的非测试 Go 源文件，
复用任务级抽样引擎的算子与判定，回答"这次改的那几行，测试抓得住吗"。文件
来源由 git 回答，不由 agent 自报——这是它和任务内抽样的唯一区别。

## 条件跳过不算「被跳过」

第一版把「源码里有 `t.Skip*`」一律记成 skipped，结果是 51 条平台/环境闸门
（`if runtime.GOOS != "windows" { t.Skip(...) }`、`if testing.Short()`、
`exec.LookPath("git")`）全被标成「未审批的跳过」，把真信号埋在噪声里——
和「137 条死命令」同一类错误。

现在 `skipInfo` 用 AST 判断 Skip 出现的位置：

- 出现在 `if` / `for` / `switch` / `select` 等控制结构体内 → **条件跳过**
  （平台上照常跑，只在特定环境不发生），单列计数，不进 skipped、不进违规；
- 出现在函数体顶层（无条件）→ **真跳过**，仍要理由与审批。

条件按执行记录落 live / orphaned：它在别的机器上跑过，账本照样认。

