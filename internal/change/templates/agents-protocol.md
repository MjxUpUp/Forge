> 本段由 forge 维护（`forge change agents` 同步）。块内手改会在下次同步被覆盖；
> 团队自己的约定请写在标记外。

## change 流程（给 agent 的短协议）

这个仓库用 `spec/` 记录交付过程：每个需求一个 change 目录，产物随 git 走。
换机器、换 agent、换 IDE 都照这套目录干活。产物本身可以直接读；`verify` /
`status` / `gate` 要跑 forge 二进制——没装就只能手工照模板写产物，跑不了这三条命令。

## 目录

- `spec/forge.yaml` — 仓库声明「我用这套产物」。它同时是激活判据：clone 下来就有。
  `forge change init` 默认写 `hooks: off`：只用产物、不开 hook 执法；要门禁把它改成 `on`。
  安全底线（hazard-guard / mcp-scan / skill-scan）随这个开关：默认 audit-only，写 `on`
  才执法（执法时不受契约 profile 收窄），但仍受机器全局档位限制（`FORGE_PROFILE=lite` 时只接线 hazard-guard）。
- `spec/changes/<id>/goal.md` — 目标契约五段：Objective / Why / Constraints / Out of Scope / Check；填完删掉每段的 `forge:fill` 标记。
- `spec/changes/<id>/proposal.md` — 为什么做、怎样算解决。
- `spec/changes/<id>/design.md` — 怎么改、为什么这么改。
- `spec/changes/<id>/tasks.md` — 实施步骤；判据只写 acceptance 的围栏编号（多个用「和/或/、」分隔）。
- `spec/changes/<id>/acceptance.md` — 可执行判据（```bash 围栏）。
- `spec/changes/<id>/evidence/<UTC 时间戳>/` — verify 产出的证据（stdout/stderr/exit + summary.json）。
- `spec/changes/archive/<YYYYMMDD>-<id>/` — 归档。

## 一个 change 的生命周期

1. 一次性初始化：`forge change init` 声明契约（生成 `spec/forge.yaml`）并同步本托管块；契约已存在时用 `forge change agents` 重新同步。
2. `forge change new <id> --title "..."` 生成五份产物（含 `goal.md`）。
3. 依次填 goal → proposal → design → tasks → acceptance，并删掉每份文件里的 `forge:fill` 标记。
   启动任务时用 `forge task start --change <id>` 把 goal 钉成内容哈希；启动后改 goal.md 会让 `task-implement` BLOCKED（改目标等于改判据，重开任务）。
   goal 没填完时 `task start --change` 会报错（读不出五段）；漂移后的恢复：还原 goal.md，或 `forge task abort` 后重新 `task start --change` 钉新哈希。
   老 change 没有 goal.md 时 `--change` 会报错：先补 goal.md，或改用 `forge task start --ref <ref>`（不钉契约；implement 只记 advisory）。
4. `forge change verify <id>` 实跑 acceptance；全部通过才封印证据。
5. 先勾 tasks.md 的进度、再跑 verify、然后提交（证据指纹含 tasks.md，勾选或提交后再改文件都会让证据失效）。归档用 `git mv spec/changes/<id> spec/changes/archive/$(date +%Y%m%d)-<id>`（八位日期 + 连字符 + change id；`archive/` 目录由 init 建好）。
6. 之后再改被验证的文件、或改 acceptance，证据自动失效（`forge change list` 显示「证据已失效」），必须重跑 verify。
7. 工作区有未提交改动时，证据会记下这些文件的 sha256；干净工作区 verify 则只绑 commit
   （summary 里没有 files 字段，靠 HEAD 移动即失效来兜）。list 会显示绑定的 commit。
   之后又改了代码或文档，证据自动失效；只提交证据本身不算。

## 回环：下一步由 status 给，不由自己挑

循环里跑 `forge change status <id>`，只执行它输出的 next steps：

1. 产物没补齐 → 补哪些文件、删哪些标记，它都说清；
2. 没验证过 → `forge change verify <id>`；
3. 判据或工作区变过 → 证据失效，重跑 verify；
4. 上次没通过 → 先读证据，再改实现或判据，再重跑；
5. acceptance 的判据全通过（`forge change verify` 封印成功）→ 归档。

逐节点推进用 `forge change gate <id> --task <N>`：只跑 tasks.md 里那条任务引用的
判据（`（判据：acceptance #N）`），节点证据不封印整个 change。前置条件是那条任务
存在、**没勾选**、且写了判据编号；勾了 `- [x]` 再跑 gate 会被拒绝（勾选是进度记录，
不是判据）。fast change 没有 tasks.md，用整条 verify。

status 若报 `blocked`，说明同一原因（每次 verify/gate 的第一条失败命令）连续失败
3 次。此时 next steps 不再给任何可执行命令——那是给人的判断点；恢复命令在
`handoff` 里，先读证据再改判据或改实现，不要在循环里换个说法继续重试。

## 快速通道（fast）

小的、明显不需要设计的改动可以用 `forge change new <id> --fast`：

- 只要求 `goal.md`、`proposal.md` 与 `acceptance.md`（省的是 design/tasks，不是目标与判据）；
- 必须带 `--by-human`：先问「这次走快速通道吗」，人明确同意才能建；agent 不得自批；
- 判据、证据、封印、失效规则完全一样，summary 里会标 `fast: true`；
- 周界（比如 ≤3 个文件、≤50 行）是团队约定，写在这里而不是让机器猜——
  阈值一判就假，比例可以统计，合适与否由 review 看。

## 红线

- 判据必须能被命令证明；写不出命令的验收项不算完成。
- acceptance 里不能用管道 `|`、`||`、后台 `&`、`set +e`/`set +o errexit`、`eval`，
  也不能再调用一层 shell —— `bash -c`、`bash -lc`、`/bin/sh -c`、`bash.exe -c`、
  用 `${IFS}` 拼参数都算：verify 在解析期就直接拒绝（不是靠 pipefail 兜），
  因为这些写法都能把失败变成 0。这是启发式检查，不是沙箱——它认不出的写法
  （别家解释器、变量拼命令、脚本内部）不保证被拦，所以判据要短、直白、能被 review 看懂。
- 「已验证」这个结论只能由 `forge change verify` 产出。手写的 summary.json 缺实跑输出文件，
  review 时应视为可疑——verify 只检查摘要是否自洽（有判据哈希、有实跑记录），
  它无法证明这份摘要真的来自一次执行。
- 没装 forge 也能写这些产物、照样可审计；差别是产不出封印。
- 保留名：change id 不能用 `archive`、`evidence`。
- 失败的证据别删：删掉最新那份，列表会回到上一次封印，看起来像一直是通过的。
- 机器只能检查摘要自洽（checks 全 0、有 acceptance 哈希）；摘要是不是 verify 产出的、判据有没有真在检验，看 review 与证据目录里的文件。
- 判据检查拦的是常见写法；判据是否真的证明了这个 change，只能由 review 判断——别把「verify 通过」当成「这件事做对了」。