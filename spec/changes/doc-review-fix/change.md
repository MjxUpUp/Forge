# 改动：独立评审发现的文档事实修正

## 改动

按第二个模型的评审表逐条核实后修正：

1. README：质量检查范围补 `forge task verify-acceptance`；删「不被质量评分追踪」的旧口径；evals 行改标历史；60 秒示例补 `--agent`；`profile` 示例改 mini；pre-commit 句说明 forge 不安装 hook、脚本需手动启用。
2. docs/design/positioning.md：验收入口补 change 侧 `forge change verify`。
3. docs/design/task-tree-guard.md：删「评分/capped」旧语义，改证据档位封顶；修正「每个任务都有活锚」的绝对断言。
4. docs/wedge-demo-anti-false-green.md：「唯一来源」限定为 change 的结论。
5. docs/design/compat-commitments.md：宿主层移除版本改 1.85；命令面「只增不删」注明自 3.0.1 起的范围；§二 列名改「代表位点」并对已移除位点加注。
6. docs/compliance/standards-crosswalk.md：compat 锚点不再宣称只增不删；toollog 标为历史数据；bundle 导入验签标已移除。
7. READMEs/README.zh-CN.md：3.0 时间线修正（宿主层在 1.85）、产物数量表述统一。
8. npm/README.md：命令表补 version/next/off/on/review 或注明节选。

## 影响

- 只影响文档，无代码行为变化；`docs closure` 的活文档集合包含这些文件，改错会立刻显示漂移。
- 不改变任何判据、门禁或快照。

## 验证

- `forge docs closure --strict`（drift 0）
- `forge docs lint` 逐份（0 硬失败）
- `go test ./internal/capability ./internal/docsconsistency ./internal/change ./internal/cli`
