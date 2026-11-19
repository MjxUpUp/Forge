# 入口诚实：可执行判据

change: `entry-honest`

判据都可以直接复制到终端执行；需要观察输出的那几条把期望写成围栏后面的子串。

## 1. 构建

```bash
go build ./...
```

## 2. 三个核心包（next 决策、change 活动度、task complete）

```bash
go test -v ./internal/taskpipeline/ ./internal/change/ ./internal/clitask/ -count=1 :: ok
```

## 3. CLI 包（next 命令面与协议文案）

```bash
go test -v ./internal/cli/ -count=1 :: ok
```

## 4. 文档一致性

```bash
go run ./cmd/forge docs lint README.md docs/reference/cli.md
```

## 5. gofmt

```bash
sh scripts/checks/gofmt-clean.sh
```

## 6. 死代码

```bash
sh scripts/checks/deadcode-zero.sh
```

## 7. 已删命令不能回流

```bash
sh scripts/checks/no-removed-checks.sh
```
