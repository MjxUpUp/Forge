# 本仓库的 change 流程（给 agent 的短协议）

这个仓库用 `spec/` 记录交付过程：每个需求一个 change 目录，产物随 git 走。
换机器、换 agent、换 IDE 都照这套目录干活——不需要先装什么。

## 目录

- `spec/forge.yaml` — 仓库声明「我接受这套流程」。它同时是激活判据：clone 下来就有。
- `spec/changes/<id>/proposal.md` — 为什么做、怎样算解决。
- `spec/changes/<id>/design.md` — 怎么改、为什么这么改。
- `spec/changes/<id>/tasks.md` — 实施步骤；判据只写 acceptance 的围栏编号。
- `spec/changes/<id>/acceptance.md` — 可执行判据（```bash 围栏）。
- `spec/changes/<id>/evidence/<UTC 时间戳>/` — verify 产出的证据（stdout/stderr/exit + summary.json）。
- `spec/changes/archive/<YYYYMMDD>-<id>/` — 归档。

## 一个 change 的生命周期

1. 一次性初始化：`forge change init`（生成 `spec/forge.yaml` 与 `AGENTS.md`）。
2. `forge change new <id> --title "..."` 生成四份产物。
3. 依次填 proposal → design → tasks → acceptance，并删掉每份文件里的 `forge:fill` 标记。
4. `forge change verify <id>` 实跑 acceptance；全部通过才封印证据。
5. 提交。归档用 `git mv spec/changes/<id> spec/changes/archive/YYYYMMDD-<id>`。
6. 之后再改被验证的文件、或改 acceptance，证据自动失效（`forge change list` 显示「证据已失效」），必须重跑 verify。
7. 证据会记下当时改动的每个文件的内容摘要，并在 list 里显示绑定的 commit。
   之后又改了代码或文档，证据自动失效；只提交证据本身不算。

## 红线

- 判据必须能被命令证明；写不出命令的验收项不算完成。
- acceptance 里不能用管道 `|`、`||`、后台 `&`、`set +e`、再套一层 `bash -c`——
  verify 在解析期就直接拒绝（不是靠 pipefail 兜），因为这些写法都能把失败变成 0。
- 「已验证」这个结论只能由 `forge change verify` 产出：手写 summary.json 没有实跑记录与工作区摘要，不算证据。
- 没装 forge 也能写四份产物、照样可审计；差别是产不出封印。
- 保留名：change id 不能用 `archive`、`evidence`。