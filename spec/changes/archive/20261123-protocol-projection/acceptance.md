# 验收判据：agent 投递面生成化：协议块由 compat 命令树生成

change: `protocol-projection`

这里的每个 ```bash 围栏是一条可执行判据，`forge change verify` 会按出现顺序编号（#1、#2……）并依次实跑。
围栏内以 `set -eo pipefail` 执行；首个非零即停。

## 判据

```bash
go test ./internal/agentproto -count=1
```

```bash
go test ./internal/agentproto -run TestAnnotationsMatchTree -count=1
```

```bash
go test ./internal/agentproto -run TestTemplateHasNoDeadCommand -count=1
```

```bash
go test ./internal/change -count=1
```

```bash
go run ./cmd/forge change agents --check
```

## 先失败证明（--accept-red）

用 #1：`go test ./internal/agentproto -count=1`。当前实测输出 `stat E:\Forge\internal\agentproto: directory not found` / `FAIL ./internal/agentproto [setup failed]`，是**真红**。

不用 #2–#4 当红判据：`go test -run <尚未存在的测试名>` 会打印 `no tests to run` 并退出 0，那会变成假绿（3.0 删掉的那类启发式的镜像错误）。

## 人工检查项（可选）

- [ ] 重同步后的托管块读起来仍像给人/agent 看的说明，而不是命令清单倾倒（生成物可读性靠 review，机器判不了）。
