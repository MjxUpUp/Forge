# 设计：goal 契约

change: `task-goal-contract`

## 决策

1. **goal 是 change 的第五份产物**：`spec/changes/<id>/goal.md`，由 `change new` 生成骨架，五段标题固定；填完删掉模板里的 forge:fill 注释标记才算完成。
2. **只存引用不存正文**：TaskState 的 `GoalContract{Path, SHA256, Change}` 只钉仓库相对路径与原始字节 sha256；五段正文归文件所有（单一真相源），state 不复制。
3. **启动即校验**：`task start --change <id>` 读 `goal.md`、校验五段完整，再落 state；`--goal-file <path>` 走同一条解析（仓库内路径），与 `--goal` 自由文本互斥。
4. **实现门禁校验漂移**：`task gate task-implement` 重算 sha256；不一致或文件读不到 → BLOCKED，提示还原文件或重开任务。无契约的老任务只记 `goal-contract` advisory（observation，不进证据分桶）。
5. **fast 需人批**：`change new --fast` 必须同时给 `--by-human`，否则报错并提示「先问用户」；省的是 design/tasks，不省 goal 与 acceptance。
6. **老 change 兼容**：`goal.md` 缺失不阻断 verify；存在但未填完则拒绝（DocProblems 统一处理）。

## 数据结构

```go
// internal/change/goal.go
type GoalSection struct{ Key, Title, Body string }
type Goal struct{ Sections []GoalSection }
func ParseGoal(content string) (*Goal, error)
func LoadGoal(root, id string) (*Goal, sha256, relPath string, err error)

// internal/tasktypes
type GoalContract struct {
    Path   string `json:"path"`
    SHA256 string `json:"sha256"`
    Change string `json:"change,omitempty"`
}
```

## 段落解析

- 只认 `## ` 二级标题；键做小写+去空格归一，支持中文别名（目标/原因/约束/范围外/检查）。
- 每段 trim 后非空，且不含 forge:fill 注释标记；缺段/空段/残留标记都返回带段落名的错误。
- 哈希对文件原始字节计算（不做换行归一）——跨平台一致性依赖仓库把行尾钉成 LF（本仓 `.gitattributes` 的 `* text=auto eol=lf`）；用 forge 的他仓若开 `autocrlf=true` 且无 attributes，未改动的 goal.md 也可能哈希不符而误 BLOCKED（恢复=重开任务重新钉哈希）。

## 门禁位置

`internal/taskpipeline/executor.go` 的 `checkImplement`：在「确有代码改动」之后、产物链之前调用 `checkGoalContract`。放在改动校验后，避免空改动重试刷日志；放在产物链前，让 goal 漂移先于其它噪音暴露。

## 兼容与回滚

- `--goal`、`--plan-file` 行为不变；新 flag 不改变老脚本。
- 删除 `goal.md` 等价于改判据：已启动任务 BLOCKED；新任务自动生成骨架。
- checklog 新名 `goal-contract` 未进 evidence 白名单，失败不会把证据强度拉低到误报（observation 语义）。

## 测试策略

- `internal/change`：五段解析、缺段、残留标记、中文别名、缺文件、哈希稳定性；`DocProblems` 对「无 goal.md 的老 change 放行」「有但未填完拒绝」两向。
- `internal/taskpipeline`：无契约 advisory、未变通过、漂移 BLOCKED、文件被删 BLOCKED。
- `internal/cli`：`change new` 生成 goal.md、`--fast` 缺 `--by-human` 被拒且不留目录、带 `--by-human` 成功。
- `internal/clitask`：`--change` 与 `--goal-file` 互斥、仓外路径与符号链接逃逸被拒、缺 change 时读 goal 报错、两个 flag 确实注册。