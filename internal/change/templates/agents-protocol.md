# 本仓库的 change 流程（给 agent 的短协议）

这个仓库用 `spec/` 记录交付过程：每个需求一个 change 目录，产物随 git 走。
换机器、换 agent、换 IDE 都照这套目录干活——不需要先装什么。

## 目录

- `spec/forge.yaml` — 仓库声明「我用这套产物」。它同时是激活判据：clone 下来就有。
  `forge change init` 默认写 `hooks: off`：只用产物、不开 hook 执法；要门禁把它改成 `on`。
  安全底线（hazard-guard / mcp-scan / skill-scan）不受这个开关和契约 profile 影响，
  但仍受机器全局档位限制（`FORGE_PROFILE=lite` 时只接线 hazard-guard）。
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
7. 工作区有未提交改动时，证据会记下这些文件的 sha256；干净工作区 verify 则只绑 commit
   （summary 里没有 files 字段，靠 HEAD 移动即失效来兜）。list 会显示绑定的 commit。
   之后又改了代码或文档，证据自动失效；只提交证据本身不算。

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
- 没装 forge 也能写四份产物、照样可审计；差别是产不出封印。
- 保留名：change id 不能用 `archive`、`evidence`。
- 失败的证据别删：删掉最新那份，列表会回到上一次封印，看起来像一直是通过的。
- 机器只能检查摘要自洽（checks 全 0、有 acceptance 哈希）；摘要是不是 verify 产出的、判据有没有真在检验，看 review 与证据目录里的文件。
- 判据检查拦的是常见写法；判据是否真的证明了这个 change，只能由 review 判断——别把「verify 通过」当成「这件事做对了」。