# 本仓库的 change 协议

本仓库用 `spec/` 声明工作流，产物跟代码一起提交。装不装 forge 都能照这套目录干活——
差别只在 `forge change verify` 会替你实跑判据并落证据，手做时要自己跑同样的命令并留下输出。

## 目录

- `spec/forge.yaml` — 契约（版本与档位）。它的存在即声明本仓库受这套协议约束。
- `spec/changes/<id>/` — 一个 change 的四份产物：`proposal.md`、`design.md`、`tasks.md`、`acceptance.md`。
- `spec/changes/<id>/evidence/` — 证据，由 `forge change verify` 生成；不要手写、不要改。
- `spec/changes/archive/<YYYYMMDD>-<id>/` — 已交付的 change，人工确认后挪进来。

## 循环

0. 新仓库先 `forge change init`（或手写一份 `spec/forge.yaml`）。
1. `forge change new <id> --title "..."` 建骨架。
2. 先写 proposal：为什么做、备选方案、取舍。人看过之后再写 design。
3. design 定结构与风险，再拆 tasks；每条 task 都要带一条能跑出对错的验证命令。
4. 把判据写进 `acceptance.md`：一个 ```bash 围栏一条命令（多步用 `&&`），写不成命令的验收不是判据。
5. `forge change verify <id>` 实跑并落证据。失败就改，改完重跑。
6. 人工检查项由人确认，归档由人决定——这两件事命令不代劳。

## 红线

- 不要手写或修改 `evidence/`。forge 不做密码学防伪，做的是防遗忘、防漂移；伪造证据与改 `.git` 历史同性质。
- 不要为了让验证变绿而放宽 `acceptance.md`。要改判据，先在 proposal 里说明理由。
- 判据变了，或同一 commit 下工作区内容变了，旧证据自动失效（stale），必须重跑。
  已提交后再改 HEAD 不再判 stale——那属于新 change，由人决定是继续还是归档。