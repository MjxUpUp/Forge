# change：protocol-projection

一句话边界：给 agent 投递面（AGENTS.md 托管块）加两样机器可查的东西——块头的 `proto/sha` 指纹，以及一节由命令树生成的「命令面」清单——并用测试锁住「块里不许出现命令树里没有的动作」；散文照旧手写。

## 改动

- 新增 `internal/agentproto`（**不叫 internal/protocol：该包已存在，是 `protocol.yml` 配置加载器**）：
  - `Render(cmds []Command, version string) (string, error)`：散文部分取自现有模板，末尾追加 `## 命令面（生成）`；块体首行插 `<!-- forge:proto version=<v> sha=<body 的 sha256 前 12 位> -->`。
  - `annotations.go`：command → 一句话；**只给被散文教学的命令写**，其余不出现（未注解数量进 `--check` 报告当债）。
  - 双向校验：注解了命令树里没有的命令 → 报错；散文模板里提到的每个 `forge …` 不在命令树里 → 报错。
- `internal/change`：`EnsureAgentsBlock` 增加命令面入参（tree 由调用方注入，保持 change 不 import cli）；保留 begin/end 标记与 CRLF 处理逻辑不动。
- `internal/cli/change.go`：`forge change agents` 用 `compat.BuildSnapshot`（rootCmd 已由 `internal/cli/root.go:50` 注册给 docsconsistency）取命令面；新增 `--check`（只算不写，有漂移非 0）与 `--print`（打印块体）。
- `internal/clitask`：`task start` 在块头 sha 与当前二进制算出的 sha 不一致时打一条 advisory（不阻断）。
- 文档：本仓 AGENTS.md 托管块重同步（会多出「命令面（生成）」一节）；`docs/reference/cli.md` 补 `--check/--print` 一行。

## 影响

- 所有声明了 `spec/forge.yaml` 的仓库：下次 `forge change agents` 后块尾多一节生成清单、块头多一行指纹。
- **实现期已核实的依赖方向**：agentproto 只吃「命令路径 + flag 名」这个纯数据，不 import cli；clitask 通过 docsconsistency 的命令树回调拿树，因此 advisory 不需要 cli 参与。
- 文档影响：本仓 AGENTS.md、`docs/reference/cli.md`；compat 快照面 1 会因 `--check/--print` 变化。
- 无 state/schema 变更。

## 验证

- 单测：确定性（同输入两次字节一致）、注解表 ↔ 命令树双向一致、散文里无死命令、块头带 version 与 sha。
- `forge change agents --check` 重同步后 drift=0；删一条注解后报出；把散文里一条命令改成不存在的名字必须非 0。

## 已知风险（实现前记下，避免开工后才发现）

- `go test <pkg> -run <TestName>` 在「测试尚未存在」时退出码是 **0**（no tests to run）。所以**判据不能拿「新写的测试名」当先失败证明**——它 fake-green。A 批的红判据用「新包目录不存在」：`go test ./internal/agentproto -count=1` 现在真的是 `directory not found`。
