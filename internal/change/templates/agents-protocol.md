> 本段由 forge 维护（`forge change agents` 同步）。块内手改会在下次同步被覆盖；
> 团队自己的约定请写在标记外。

## 开始之前（硬规则）

- **任何代码改动之前，先跑 `forge next`**，把它当建议：通常执行它给出的下一步；一句话小修可直接 task start，并在标题里写清意图。
- **记录改动：建议先立 change**：任务是一等入口，随时可以开工；change 是记录单元，
  既能在开工前立，也能中途补。建议的顺序是 `forge change new <id>` → 填产物 →
  `forge change phase <id>` 看还缺什么 → `forge task start --ref <ref> --change <id>`
  把 goal 钉成哈希。**不立 change 也允许开工**（`task start` 不带 `--change`）；
  只有你显式带 `--change` 时，那个 change 必须已经 ready。
- 一句话小修可以加 `--kind task`：它仍是任务（走三道门禁），只是向人和审计说明
  「这次没有立 change」。它不是绕过门禁的开关。
- **不要为了让门禁放行而临时改产物**：阶段机只读文件，伪造 goal/design 会留在 diff 里。
- 一次任务只做一件事，做完依次跑 `forge task gate task-implement`、`forge task verify-acceptance`
  （实跑验收）、`forge task gate task-verify`，然后 `git commit` → 只读子 agent 审查 diff →
  `forge review pass` →（含 `.md` 时）`forge task doc-review` → `forge task gate task-complete`
  → `forge task complete`。缺 review pass 会在 task-complete 前 HARD stop。
- **门禁是命令，不是 hook**：没有后台拦截（可选的 git pre-commit 除外），上面每一条
  都要你自己跑；不跑就等于没有门禁。
- 刻意的一次性小改：先 `forge task wild "<说明>"` 申报，再改；不要用「顺手」绕过门禁。
- 「这次不用走 forge」也是合法选择，但要由人明确说出来；agent 不得自行决定跳过。

## change 流程（给 agent 的短协议）

这个仓库用 `spec/` 记录交付过程：每个需求一个 change 目录，产物随 git 走。
换机器、换 agent、换 IDE 都照这套目录干活。产物本身可以直接读；`verify` /
`status` / `gate` 要跑 forge 二进制——没装就只能手工照模板写产物，跑不了这三条命令。

## 目录

- `spec/forge.yaml` — 仓库声明「我用这套产物」。它同时是激活判据：clone 下来就有。
  `forge change init` 默认写 `hooks: off`；这个字段现在只是为了兼容旧文件而保留，
  forge 从 1.85 起没有 hook 执法层，改它不会换来拦截。门禁靠 agent 显式跑
  `forge task …` / `forge change …` 执行。
- `spec/changes/<id>/goal.md` — 目标契约五段：Objective / Why / Constraints / Out of Scope / Check；填完删掉每段的 `forge:fill` 标记。
- `spec/changes/<id>/clarify.md` — 边界与非目标 + `人确认：<UTC>`（`forge change clarify <id> --by-human --note "…"` 生成）。
- `spec/changes/<id>/proposal.md` — 为什么做、怎样算解决。
- `spec/changes/<id>/design.md` — 怎么改、为什么这么改。
- `spec/changes/<id>/tasks.md` — 实施步骤；判据只写 acceptance 的围栏编号（多个用「和/或/、」分隔）。
- `spec/changes/<id>/acceptance.md` — 可执行判据（```bash 围栏）。
- `spec/changes/<id>/evidence/<UTC 时间戳>/` — verify 产出的证据（stdout/stderr/exit + summary.json）。
- `spec/changes/archive/<YYYYMMDD>-<id>/` — 归档。

## 一个 change 的生命周期

1. 一次性初始化：`forge change init` 声明契约（生成 `spec/forge.yaml`）并同步本托管块；契约已存在时用 `forge change agents` 重新同步。
2. `forge change new <id> --title "..."` 生成五份产物（含 `goal.md`）。
   新 change 会留一个 `.goal-required` 标记：它让 goal.md 成为必填——删掉 goal.md 不再算「老 change」，verify 会直接拒绝。
3. 依次填 goal → proposal → design → tasks → acceptance，并删掉每份文件里的 `forge:fill` 标记。
   填完用 `forge change phase <id>` 确认走到 `ready`；缺 clarify.md 时先问用户边界与非目标，
   再 `forge change clarify <id> --by-human --note "…"` 记录（agent 不得自批）。
   启动任务时用 `forge task start --change <id>` 把 goal 钉成内容哈希；启动后改 goal.md 会让 `task-implement` BLOCKED（改目标等于改判据，重开任务）。
   goal 没填完时 `task start --change` 会报错（读不出五段）；漂移后的恢复：还原 goal.md，或 `forge task abort` 后重新 `task start --change` 钉新哈希。
   老 change 没有 goal.md 时 `--change` 会报错：先补 goal.md，或改用 `forge task start --ref <ref>`（不钉契约；implement 只记 advisory）。
4. `forge change verify <id>` 实跑 acceptance；全部通过才封印证据。
5. 先勾 tasks.md 的进度、再跑 verify、然后提交（证据指纹含 tasks.md，勾选或提交后再改文件都会让证据失效）。归档用 `git mv spec/changes/<id> spec/changes/archive/$(date +%Y%m%d)-<id>`（八位日期 + 连字符 + change id；`archive/` 目录由 init 建好）。
6. 之后再改被验证的文件、或改 acceptance，证据自动失效（`forge change list` 显示「证据已失效」），必须重跑 verify。
7. 工作区有未提交改动时，证据会记下这些文件的 sha256；干净工作区 verify 则只绑 commit
   （summary 里没有 files 字段，靠 HEAD 移动即失效来兜）。list 会显示绑定的 commit。
   之后又改了代码或文档，证据自动失效；只提交证据本身不算。

## Task 流程（门禁在命令里）

任务是把「一次改动」变成可审证据的容器；change 是记录单元、可以后补——
任务本身才是一等入口，Task 门禁是强制的：

1. `forge task start --ref <分支名> --title "…" --accept "run :: expected"` 起任务
   （不带 `--change` 也能开工；一句话小修加 `--kind task` 只是标记，不放松任何门禁）。
   验收标准最迟在 verify-acceptance 前登记：推荐开工时用 `--accept` 一次登记；
   事后补登用 `forge task accept "run :: expected" --ref <分支名>`（会标 manual 层，交付单会披露）。
   `--accept` 的 run **按空白切分后直接 exec，不经 shell**——引号、管道、`&&`
   都不生效，写成无歧义的 argv。expected 是**子串匹配**（strings.Contains，不是正则）；
   空 expected（`::`）只校验退出码，task start 会提示一次。
   有测试能力的仓库，考卷至少要有一条测试类命令（`go test ./...` 之类），
   否则 `task complete` 会以「考卷无测试类命令」拒绝。
2. 想留可评审记录时，把 change 钉成契约：`forge change new <id>` → 填产物 →
   `forge change phase <id>` 到 ready → `forge task start --ref <分支名> --change <id>`
   （`--change` 要求 change 已到 `ready`，并把 goal.md 内容哈希钉进 state；没有
   goal 契约时也可以用 `--goal-file` 或 `--goal`）。已开着的任务不能换契约——重开任务。
3. 干活。真要做的是「改代码」而不是「写文档」时，implement 门禁会核对确有改动。
4. `forge task gate task-implement` → `forge task verify-acceptance`（实跑考卷并记确定性证据）
   → `forge task gate task-verify`（配对测试、cheat-scan 等；多数是 advisory，复发才升硬）。
   BLOCKED 先读它给的证据，不要换个说法重试。complete 会硬查验收已实跑且未过期。
   `task gate task-complete` 与 `task complete` 同口径：同一 `CheckDocGate`——缺记录/记录过期/得分 <75/未决 Critical/L1 硬失败/证据完整性校验失败时
   gate 就 BLOCKED，不会先绿灯再在 complete 打回。
5. 改动里含 `.md` 时先过文档闸：`forge docs lint <文件>`。
6. 提交、再审查、再记录、再完结——顺序是：`git commit` → 派只读子 agent 审查当前 diff →
   `forge review pass --note "..."` →（含 `.md` 时）`forge task doc-review --passed pass --score <N> --round <N> --reviewer <评审者 id>`（≥75 才过；
   纯文档改动同样要这一步）→ `forge task gate task-complete` → `forge task complete`。
   `review pass` 是 task-complete 的硬前置，不是可选项；`doc-review` 绑定记录时的 HEAD，
   必须在 commit 之后记录（先记再提交会让 complete 因 HEAD 前移要求重录）。
   产出者不能自检：文档评审要派独立会话/只读子 agent，自己评的分不被采纳。
   审查后改源码会因快照不一致被拒（只能复审；或 `--acknowledge-changes` 自担、留审计）。
7. `forge task report --ref <分支名>` 出交付单（任务完成后必须带 `--ref`，
   不带会报 no active task）；单里「文档回检」行会披露 L2 分数/轮次/评审者，
   未记录评审者或自查时标「自查（独立性未校验）」。
8. 卡住或方向错了：`forge task abort`（只清 state，不动代码）；逃生舱 `task override`
   只用于确实不适用的门禁：验证类（`--test-coverage`/`--acceptance-gate`/`--artifact-chain`/
   `--skill-decisions`/`--doc-gate`）会降证据强度并留审计，节奏类（`--work-activity`）只审计，
   别当常规手段。

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

- 只要求 `goal.md`、`proposal.md` 与 `acceptance.md`，外加 `clarify.md` 的人确认
  （省掉的是 design/tasks，不是目标、边界与判据）；
- 必须带 `--by-human`：先问「这次走快速通道吗」，人明确同意才能建；agent 不得自批；
- 判据、证据、封印、失效规则完全一样，summary 里会标 `fast: true`；
- 周界（比如 ≤3 个文件、≤50 行）是团队约定，写在这里而不是让机器猜——
  阈值一判就假，比例可以统计，合适与否由 review 看。

## 三个出口的边界

- **正常 change**（默认）：五份产物 + 全套门禁。
- **fast**：goal + proposal + acceptance + clarify，必须人批（`--by-human`）。
- **不走 forge**：由人明确说「这次不用走 forge」，那就只做 git 提交；不要建了 change
  又空着，也不要事后补记成 fast。agent 不得自行选择这个出口。

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
