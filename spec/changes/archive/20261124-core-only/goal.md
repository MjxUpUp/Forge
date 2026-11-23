# goal：core-only

## Objective

把 forge 收敛成 3.0 的一层：只留 change / task / review / docs / impact / test / compat / next / status 与 init / on / off / version。
其余旧时代能力（hook 宿主层之后残留的命令组、死检查名、artifactchain / testcoverage 死接线、历史计划文档）一律删除，
删完 go build ./... 通过、核心包测试全绿、forge docs closure drift 为 0。

保留命令面（白名单）：init、on、off、version、next、status、change、task、review、docs、impact、test、compat。
删除命令面：config、conventions、harness、migrate、node、policy、project（含 adopt/export/import/sync）、
registry（含 audit/gc/rekey/remove）、skill、update、workspace（含 depcycle）、worktree、dashboard、health、trace、
enforcement、skills、bridge、trust、act（含 rebuild/remigrate）、eval（含 suite/friction/oracle/rule-ledger/validity）、
gate、verify。

同时删除：checklog 里 18 个无生产者的宿主时代检查名；internal/artifactchain 与 loopedge 的 chain.yaml 读取；
internal/taskpipeline/testcoverage.go 的 task-complete 兜底；docs/plans、docs/surveys 与自述「已删除/已被取代」的 design 文档。

## Out of Scope

- 不改 change/task/review/docs/impact/test/compat 的核心语义与门禁判定。
- 不改 3.0 已定的评分/证据/红先绿机制。
- 不重写剩余命令的实现，只删与修编译/测试连带。
- 不删 docs/adr、CHANGELOG.md、spec/changes/archive（决策与变更记录，不是残留）。
- 不动 git 历史；删掉的内容在历史里仍可取回。

## Check

见 acceptance.md 围栏 #1–#5：编译、死检查名不再存在、旧命令组已消失、核心包测试绿、活文档 drift 为 0。
