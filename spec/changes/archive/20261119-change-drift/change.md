# Change：spec 漂移检测

## 改动

1. `internal/change/drift.go`：`DeclaredPaths`（只认「## 改动」段里带 `/` 或 `.` 的反引号 token，跳过列表序号）、`Drift`（base 取该 change 目录首次提交，diff base..HEAD + `git status --porcelain`，排除 `spec/changes/**`）。
2. `internal/cli/change_drift.go`：`forge change drift <id> [--json]`，advisory 输出，退出码恒 0（除读取失败）。
3. `docs/reference/cli.md`：增一行。

## 影响

- 命令面 +1（compat 重钉）；不触门禁判定。
- 已归档 change 也能跑（Load 支持 archive）。

## 验证

- `TestDeclaredPaths`：只取改动段路径、跳过命令名与序号。
- `TestDrift_ReportsUndeclaredAndUntouched`：临时仓库真跑，未声明文件进 undeclared、声明未改进 untouched。
