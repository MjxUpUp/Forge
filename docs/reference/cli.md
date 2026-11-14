# 命令参考（完整）

> 这是从根 README 拆出来的**完整**命令表：每个命令、flag、逃生舱与门禁语义都在这里。
> 根 README 只保留高频速查，避免新用户一上来就被成百行命令细节挡住。
>
> 命令、flag 与语义以 `forge <命令> --help` 的实测输出为准；代码与本文不一致时以二进制为准。
> 本文按实测维护：顶层命令表覆盖用户面向的全部命令（cobra 自动生成的 `completion`
> 与 `help` 不单列；`forge verify`、`forge enforcement` 这类不带子命令的叶子也在表内）。

---

<details>
<summary><b>🔧 项目管理</b></summary>

| 命令 | 说明 |
|------|------|
| `forge init [--profile mini\|standard\|full]` | 在仓库里声明 forge 契约：**只写仓库内产物**——`spec/forge.yaml`（`version: "1"`、profile 缺省 mini、`hooks: off`）、`spec/changes/`（含 `archive/`）、AGENTS.md 的 forge 托管块，并按默认规则维护已存在的入口指针（已有 `CLAUDE.md` 追加指针、已有 `.cursor/rules/` 目录补 `forge.mdc`）；**不写** `~/.forge`（不登记注册表、不建 protocol.yml/runtime state、不写 hooks 资产），也不写 `~/.claude`/`~/.codex`；**没有宿主管道**：forge 不装 hook、不改 `~/.claude/settings.json`、不写插件或用户级设置，质量检查只在 `forge task gate` / `forge change verify` 进程内跑。仓库已有 `spec/forge.yaml` 时只重同步托管块与指针，不覆盖契约。`--project` 已废弃为 no-op（显式传入时打印警告）；`--mode`/`--fresh` 为隐藏 no-op，旧脚本仍可跑；`--agents` 为隐藏 no-op（阶段 D 起没有宿主接线，传入即打印废弃警告并忽略） |
| `forge status [--json] [--system]` | 查看项目状态（任务管道 + 质量信号）；`--system` 跑系统级健康检查（~/.forge、PATH、孤儿 hook、skills manifest） |
| `forge verify [--run-tests] [--regression] [--scenario <name>] [--collect-golden <dir>]` | 项目级验证入口：默认只检查仓库契约（`spec/forge.yaml` 存在且 version/profile/hooks 合法，输出档位与规则计数）；`--run-tests` 探测并实跑项目测试命令（go/Cargo/npm/pytest，未探测到只报告），结果记 checklog `test-run` 证据；`--regression/--scenario` 的 E2E 场景仍指向已删除的宿主层（`forge data-dir`、hooks 副本），当前不可用；`--collect-golden` 是从已完成任务采集 golden case 的开发工具 |
| `forge change init [--profile mini\|standard\|full]` / `new <id> [--title <标题>] [--fast]` / `list [--json]` / `verify <id> [--timeout 10m]` / `status <id> [--json]` / `gate <id> --task <N> [--timeout 10m]` / `check [--staged] [--json]` / `agents [--pointers claude|cursor|all|none]` | 仓库内 change 生命周期（不要求 forge 接管）：init 写 `spec/forge.yaml`（默认 `hooks: off`）、`spec/changes/`（含 `archive/`），同步 AGENTS.md 的 forge 托管块（缺文件整份写、无块追加、有块就地替换，标记外不动），并维护已存在的入口指针（已有 `CLAUDE.md` 追加指针、已有 `.cursor/rules/` 目录补 `forge.mdc`）；new 生成模板（`--fast` 只生成 proposal + acceptance）；list 从文件系统推导阶段与证据状态；verify 实跑 acceptance 判据并封印证据；status 给下一步；gate 只跑某个节点的判据；check 检查每个未归档 change 的证据是否成立（CI 与 pre-commit 共用的闸门，不合格退出码 2）；agents 单独重同步 AGENTS.md 的托管块（`<!-- forge:begin -->`…`<!-- forge:end -->`），并维护 `CLAUDE.md` / `.cursor/rules/forge.mdc` 指针（`--pointers` 点名；缺省只维护已存在的 `CLAUDE.md`，`.cursor/rules/` 目录存在时补 `forge.mdc`；详见文末补充）。详见文末「change 子命令补充」 |
| `forge update [--apply]` | 检查并更新到最新版本（按安装通道分流：npm 安装自动检测包管理器 npm/pnpm/yarn，查 npm registry 并打印对应更新命令——npm 包不可变，原地替换会被下次 install 还原，故不代下载，可用 `FORGE_NPM_REGISTRY` 覆盖 registry；GitHub Release/手动安装从 GitHub 下载自替换）；npm 通道加 `--apply` 直接代跑包管理器安装命令并自验新版本（Windows 下二进制被占用时只打印命令） |
| `forge off [--all] [--commit]` 或 `forge on` | 按项目退出/恢复接管（Project Policy Layer）：`off` 把当前项目（git 根）置为 declined——只改注册表条目（含决策来源/时间审计），不改任何宿主配置（没有 hook 层可静默），`forge init` 拒绝（退出不被任何默认路径重置）；`--all` 一键全退；`--commit` 额外在仓库根写 `.forge-decline` 团队声明（committed 后对所有协作者让位，deny-wins）；`on` 是 declined→managed 的唯一恢复通道（清 `.forge-decline` 声明文件；从未 init 的项目提示先 `forge init`）；仓库已有 `spec/forge.yaml` 且当前是 managed 时，`on` 直接提示「已由仓库契约声明接管」并返回（不写注册表）；若是 declined（`forge off`、`policy yield` 或 clone 到带 `.forge-decline` 的仓库），则照常恢复为 managed（契约仍在，不要求先 init；若随后提示补全接线，再跑一次 `forge init`）。状态存注册表条目（含决策来源/时间审计），`forge status` 退出码即「是否 managed」 |
| `forge config get/set compat <版本>` | 兼容基线声明（指纹分流 v1，GODEBUG go 行声明的轻量版）：声明"行为按哪个版本的默认走"（v1 为声明性锚点 + 落后提醒，暂不分派行为——完整分流机制见承诺表"刻意不做"）；`forge status` 显示兼容基线行与落后 minor 数；遥测回读（逃生舱使用聚合）见 `forge eval dashboard`；`--raw` 只输出值供脚本消费。原 `takeover` 三档偏好随 init-suggest 一并移除 |
| `forge policy state` / `forge policy yield` | 接管策略快查/外来 harness 让位：`state` 打印当前目录三态（managed\|declined\|unknown，退出码恒 0，供脚本分流）；判据与 `forge status` 共用同一套解析（`resolveFrom`）：有 `spec/forge.yaml` 的仓库报 managed，不再因为没进全局注册表而报 unknown。例外是该仓库处于 declined（`forge off`、`policy yield` 或 clone 到带 `.forge-decline` 的仓库）——declined 优先于契约，这里会如实报 declined，`forge status` 则以非零退出码报「已退出接管」（`resolveFrom` 两个入口只差这一点：`status` 把 declined 当失败，`policy state` 把它当状态）。另注意仓库级 `hooks` 字段与 `forge off` 是两件事：`hooks` 现在只是契约里的兼容字段（合法值 `on`/`off`，缺省 `off`，`forge status` 会把它显示出来；宿主层已删除，没有东西消费它），`forge off` 才是成员资格退出；`yield` 检测外来 harness 高置信信号（`.specify/`、`.claude/commands`、`.claude/settings.json` 含 hooks/permissions、`.cursor/rules`——单一真相源 internal/harnessdetect），命中即让位（declined, by=foreign-harness）并打印说明，`forge on` 显式覆盖 |
| `forge conventions init [--force]` | 项目规范档案（conventions-profile）：机械扫描本仓库**已声明**的规范——AGENTS.md/CLAUDE.md/.github/copilot-instructions.md 等规范文件、lint/format 配置、stack 工具链命令——写入用户级档案 `~/.forge/projects/<key>/conventions/`；**没有会话注入通道**：档案只是用户级文件，摘要要自己 `forge conventions show` 看（同一份档案跨宿主共享）；摘要的「提取要点」节由 agent 代码考古增补，重跑 init 保留提炼内容（`--force` 才重建骨架）；规范源文件变化后指纹翻转，show 提示重扫；task-verify 会提醒「档案声明的 lint 命令本任务未跑」（advisory，提醒与 checklog 审计行经 `FORGE_CONVENTIONS_LINT=disable` 一并静默） |
| `forge conventions show` | 查看规范档案：stack/lint/test/build 命令、规范声明文件与 lint 配置清单、fingerprint 与过期状态（STALE=源已漂移，重跑 `forge conventions init` 刷新）、摘要全文 |
| `forge conventions learn <rule>` | 纠正增量写回（conventions-profile）：用户/审查指出规范违规时当场把该规则写进摘要提取要点节（一字不差去重、替换待提取占位、超 15 行预算警告——展示按 15 行截断，全文仍在 summary.md）——纠正离开会话上下文进持久档案，下次 `forge conventions show` 即生效 |
| `forge migrate [--dry-run] [--force]` | 把旧 `.forge/` runtime state（tasks/gates/checklog/toollog/act/sessions/quarantine/active-task-ref 等）迁到用户级 DataDir（`~/.forge/projects/<key>/`）——升级到 runtime state 外迁版本后的迁移路径；未改过的 `.forge/protocol.yml` 由 autoSync 自动迁 DataDir，用户改过的保留为团队共享覆盖层；迁移的 task 文件落地即剥离外来门禁信号（review/验收/评分/完成/逃生舱/generic 须本机重挣，验收命令带外来标记）；幂等，`--dry-run` 预览，`--force` 覆盖 DataDir 已有同名 |
| `forge registry add [path]` | 把一个项目（默认当前目录）显式登记进全局注册表 `~/.forge/projects.json`——契约随仓库走后 `forge init` 不再自登记，`forge dashboard` 的全局聚合靠这条建立；重复登记幂等。存量漂移用 `forge registry audit`、孤儿数据目录用 `forge registry gc` 修 |
| `forge registry prune` | 精简全局注册表 `~/.forge/projects.json`——移除项目目录已不存在的死路径与重复条目（项目移走/删除/测试残留），原子写回。registry.List 读时惰性精简但只在 `forge dashboard` 启动时触发（启 web 阻塞），本命令给不启 web 的主动清理入口 |
| `forge registry remove <path>` | 按路径移除单条注册表条目（精确匹配、不做前缀；路径形态与大小写归一容忍——8.3 短名/符号链接解析形态/大小写变体命中同一条）。与 prune 分工：prune 只能清**死**路径，活着的伪根（如误注册的用户 home/临时根——此类条目 registry.Add 已有保留根守卫拒绝再入册）只能具名移除 |
| `forge registry gc [--prune]` | 回收孤儿项目数据目录（`~/.forge/projects/<key>/` 下不在注册表内的目录，测试泄漏/项目注销残留的无界增长收口）：默认 dry-run 只报告；`--prune` 执行——空目录树删除，最新文件超过 14 天的非空目录移入 `~/.forge/backups/gc-<时间戳>/`（移动而非删除，可恢复），14 天内有活动的未登记项目数据与已登记项目永不触碰 |
| `forge registry audit [--json]` | 只读一致性审计：key-drift（注册表 key ≠ 当前派生 key 且旧数据目录有载荷，提示 `forge project adopt` 迁移）/ orphan-datadir（数据目录有载荷但注册表无条目，备份壳除外）/ id-collision（两个路径派生同一 key——复制粘贴共享 `.forge-project-id` 的检出器）/ invalid-id（ID 文件格式非法，Key() fail-open 回落的唯一暴露面） |
| `forge registry rekey --from <key> --to <key> [--dry-run]` | 把 from key 的项目数据目录并入 to key（修复身份分裂的存量数据；合并语义与 project import 共享 datamerge） |
| `forge project adopt [--dry-run] [--regenerate]` | 采纳 repo-born 项目 ID：在主 worktree 根生成 `.forge-project-id`（`fpid_<32hex>`，建议 commit 进 git），并把本机数据从路径 key 迁到 ID key（先迁数据再翻身份，复用 rekey 合并语义）+ 注册表同步。另一台机器 `git pull` 拿到 ID 后跑一次 adopt 即对齐——两台机器对同一 clone 推导同一 key，跨机器同步免重映射；`--regenerate` 给共享/污染 ID 换新（其他机器需重跑 adopt 处理 key 不匹配） |
| `forge project export [--out <file>] [--include quarantine,hazards]` | 把项目记录打包为跨机器 bundle（tar.gz + 逐文件 sha256 的 manifest）：allowlist 默认拒绝——只带 tasks/checklog/toollog/sessions/act/stamps(除 hook-deploy)/protocol.yml，quarantine 源码全文与 hazards 命令行等敏感 store 须 `--include` 显式选入，会话锚/sentinel/freeze 等机器本地文件一律排除；manifest 记录来源身份（key/key_mode/project_id）供导入侧 lineage 判定 |
| `forge project import <bundle> [--dry-run] [--untrusted] [--trust-foreign] [--force] [--adopt-id]` | 校验（sha256+版本守卫+路径安全）并合并 bundle 到本机：同 key = 同身份 lineage 默认保留结果字段（评分/完成/门禁历史经单调合并收敛），session 链接恒幽灵化；key 不匹配默认剥离外来门禁信号（`--trust-foreign` 显式放行）；bundle 来自 ID 身份而本机是路径身份时默认拒绝给指引，`--adopt-id` 直接采纳其 ID（本机数据先迁移）；账本 `imports.jsonl` 保证同 bundle 重复导入跳过，jsonl 精确行去重保证重叠导出不重复——双向同步收敛 |
| `forge node show [--json]` | 显示本机节点身份（node-identity：多机器同步的机器归因地基）：`node_id` = ed25519 公钥指纹（`fnode_<32hex>`，身份即公钥——验签即身份证明），密钥对存于用户级 `~/.forge/node.json`（私钥 0600，永不外泄也不进 bundle），`rotation_chain` 轮换证书链格式预留（v1 恒空）；输出只含 node_id + 公钥，绝不打印私钥 |
| `forge project sync <init\|push\|pull\|status>` | 多机器持续同步（git 传输通道）：`init <remote>` 绑定任意 git remote（固定 `forge-sync` 分支，免疫各机默认分支漂移）；`push` 导出 bundle 到 `nodes/<node_id>/<key>/bundle.tar.gz` 并推送（每节点只写自己前缀，git 写权限即权限层；同时写 `.sig` 签名 sidecar）；`pull` 拉取后经标准 `project import` 导入全部他机 bundle（账本幂等，重复 pull 免费；lineage 裁决信任；逐节点容错 + 节点目录形态校验）；`status` 显示绑定与最近操作时间。机器本地绑定存 `DataDir/sync-remote.json`（allowlist 默认拒绝，不随 bundle 旅行），缓存仓库在 `~/.forge/sync-cache/` |
| `forge trust <list\|add\|remove\|require-signed>` | 节点信任 store（TOFU，node-identity §3）：`add <node_id> <pubkey> [--label <备注>] [--profile personal\|team]` 带外核对指纹后登记对端（node_id 与公钥一致性强制校验）；`require-signed on` 切团队档——bundle 必须带有效签名且签名者已登记，否则导入硬拒；签名无效（篡改/公钥不符）任何档位都硬拒。store 存 `~/.forge/trust.json`（0600，永不随 bundle 旅行） |
| `forge workspace create <name>` | 创建空多仓 workspace（用户级清单 `~/.forge/workspaces.json`，与 projects.json 平级；一组共同交付的 forge 项目 key 的逻辑分组，机器本地配置、不进 project sync bundle） |
| `forge workspace add <name> [--path <dir>]` | 把 repo 加入 workspace（默认当前项目，`--path` 指定他仓）；成员按项目 key 引用（path 仅是展示缓存，漂移由 doctor 检出），同一 repo 允许属于多个 workspace |
| `forge workspace remove <name> [--path <dir>]` 或 `forge workspace remove <name> --key <key>` | 从 workspace 移除成员（默认当前项目）；`--key` 逃生口：repo 已删/搬移无法按路径推导 key 时按存储 key 移除 |
| `forge workspace list [--json]` | 列出全部 workspace 及成员（key + 缓存路径） |
| `forge workspace status <name>` | 读侧聚合：按 key 扫描各成员仓活跃任务（ref/gate 进度/branch）；单个成员坏了告警跳过，绝不让整视图空白 |
| `forge workspace doctor [--json]` | 检出清单 drift（全部 advisory 不阻断）：成员 key 未注册 / 缓存路径缺失或与 registry 现路径分叉 / 一 key 属多个 workspace / 空 workspace / 跨仓任务依赖环（dep-cycle，点名完整 key:ref 环序列供人工摘边） |
| `forge worktree janitor` | worktree 生命周期清理：只清理【任务已完成或超期 14d 且工作树干净】的 workspace（脏的只报告——免删除条款，绝不自动删有未提交工作的 worktree）；路径已消失的死锚绑定无条件清除。多任务并发的磁盘上界管理 |
| `forge worktree use [--ref <ref>]` | 把任务重新锚到当前树（错树守护的正当出口）：任务有活 worktree 锚在别的树时，guard（任意已知非测试路径的写入，含 README/docs/.forge 配置）与三个 task gate 都 fail-closed；`use` 在当前目录重建绑定并把 active 指针指过来（有会话身份时才写该指针——Bash 无身份的宿主只迁锚；默认取当前会话解析出的活跃任务；`--ref` 显式指定，解析不到任务或任务已完成都直接报错而不是静默迁移）。旧锚一并解绑，输出实际迁走的锚数 |
| `forge harness init [--from-existing] [--remote <url>] [--yes]` | 建立研发控制面仓库（multi-task-concurrency T6）：把用户级 `~/.forge` 变成私有 git 仓库——tasks/checklog/archive 入库获 git 史，stamps/hazards（信任锚）/workspaces/attribution（机器本地）/会话簿记 gitignore 永不外发。HITL：交互确认仅在终端（TTY）进行——agent 经 Bash 调用无 TTY 被拒（agent 不得代批）；`--yes` 仅脚本化 CI 逃生口；`--from-existing` 给存量 DataDir 做一次基线提交（不重写数据） |
| `forge harness status` | harness repo 状态：未建立 / 本地 / 已连远端 + 近期提交 + 未提交计数 |
| `forge harness push [--yes]` | 推送 harness repo 到私有远端（首推是独立外发 HITL——数据出境清单：tracked 类别 / 永不外发类别 / 远端必须私有；凭据走你自己的 git credential helper，forge 不持有凭据）；后续 push 常规。`--yes` 仅脚本化 CI |
| `forge harness pull` | 从远端拉取（冲突上浮人工解决，机器绝不自动裁决） |
| `forge task finish [--merge-to <branch>] [--keep]` | worktree 任务收尾：验证门禁完成后在主检出合并分支（合并前校验主检出 HEAD 在目标分支上，错配拒绝）→ 清理 worktree → 解绑；`--keep` 合并后保留 worktree。脏工作树拒绝（免删除条款） |

</details>

<details>
<summary><b>🚦 任务管理</b></summary>

| 命令 | 说明 |
|------|------|
| `forge task start --ref <type/desc> --branch` | 创建任务（自动创建分支）；`--depends-on <ref>` 声明上游依赖（task-verify/task-complete 在上游交付前阻断），支持 `<key>:<ref>` 跨仓依赖——key 须为本 repo 所属 workspace 的成员（`forge workspace add`），缺失目标按保守 pending 处理 |
| `forge next [--json]` | 单命令引导：从 git/任务状态推导**恰好一条**下一步命令+理由（无任务有脏树→task start/wild 申报；门禁链→下一步命令：implement→验收实跑→verify→review pass→complete 门→complete）。agent 不自选下一步——push 侧由 `forge gate push` 兜底 |
| `forge task intent "<注记>"` | 追加意图注记（三段工件之 intent 段）：append-only——没有覆写/删除入口，意图历史即决策史，防轮次重写导致的语义漂移（rot） |
| `forge task checklist add/tick/drop` | 操作对账单（checklist 段）：勾选即进度、断点存活，task-complete 硬门禁要求全勾（report 写完不算 done，checkbox 才算） |
| `forge task start --invariant "run :: expected"` | 析出不变量（instrument 段）：声明期校验必须是可执行命令（叙述性约束被拒并指引降级到 checklist/intent），映射进验收标准——机器对账/freshness/complete 前置全覆盖 |
| `forge task start --accept "..." --assert "type:arg :: expected"` | v2 结构化断言（spec-as-gate）：五型机械可判 `exit`（期望退出码）\|`contains`\|`not-contains`（输出子串/反作弊）\|`file-changed`\|`file-untouched`（glob 对任务 diff——freeze 语义的任务级版，保护对象含 .md/.yml 等非源码）；`--assert` 附属于同命令中 preceding `--accept`，file-\* 型可独立出现（无 Run）；regex 刻意排除（ReDoS + 不可判——意见不走 hard）；声明期拒绝叙述性与越形断言 |
| `forge task start --accept-file <yml>` | 批量考卷声明：顶层 `criteria:`，每条 `run/expected/assertions[type/arg/expected]`（键与任务状态 JSON 一致）；与 `--accept` 同层级、按 (Run, Expected, Assertions) 三元组去重合并 |
| `forge eval golden harvest [--since <ref>]` | golden 候选收割（L3）：只读扫描本地已完结任务的验收考卷，机械投影成候选骨架落 `evals/forge/golden/candidates/`（gitignore，永不进 VCS）——候选经实跑探测→翻转 kind→人工策展转正 canonical；canonical 目录零写入（guard 钉死，golden 只进人工策展红线） |
| `forge review llm [--scores <file>]` | LLM 判官臂（L3「可命题」类约束的执法通道）：无 flag 打印独立只读子代理评审派遣说明（design/mock-hallucination 两类 findings，0-100 判分）；`--scores` 校验 JudgeAuditEntry 判分文件落 judge-samples 留档并指向 `forge eval judge-audit` 算 κ（地板 0.6，不达标降级 ADVISORY——判官永不进 hard） |
| `forge task wild "<说明>"` | 野外动作申报：任务管道外的显式留痕出口（比静默绕过诚实、比强制建任务轻）。记会话/分支/HEAD/是否已有任务到 `wild/declarations.jsonl`，累计计数供审计回溯（vNext INV-1 的合法出口之一） |
| `forge task status` | 查看当前任务门禁状态（含已登记产物链产物与漂移/审批态） |
| `forge task list --open [--workspace\|--all-projects] [--json]` | 列出未完成任务供用户挑选要接续哪个：每条带项目、标题、门禁进度、首条下一步、未解决阻塞数、派活状态、最后活动时间。默认当前项目；`--workspace` 扩到当前项目所属多仓 forge workspace 的全部成员（不属于任何 workspace 时回落当前项目）；`--all-projects` 扩到本机全部已接管项目。只读 |
| `forge task list --plan-conversion` | plan-first advisory 转化率报告（先测量再翻转）：已发/发后已补方案/发后仍无方案/未发自带方案四计数 + 转化率——叙事 stage 是否升档由数据决定而非直觉 |
| `forge task artifact --set <stage> --file <path> \| --stdin \| --list [--json] \| --verify \| --approve <stage> --by <人> [--checked <项>] \| --extract \| --check [--file <path>]` | 产物链操作（见 docs/design/artifact-chain-workflow.md）：登记阶段产物（哈希引用，漂移探测；登记 design 产物时其「触点」一节自动写入 PlanScope（手工 `--scope` / `task scope add` 优先），实改超出即报「方案外改动」）/ 查看链态 / 漂移校验（采用需求 ID 的任务附需求覆盖表：已验证/未验证/孤儿）/ 人工审批（human 档经 --by 归因到人、签内容哈希，内容再改即作废；批前打印一屏审批摘要——TL;DR、高危步骤、Open Questions、自上次批准的变化；`--checked` 可重复，记录本次审了什么）/ 从产物提取验收标准（`accept: <cmd> :: <expected>` 行与 \`\`\`accept 围栏编译成验收门禁——产物成为硬门禁的源码；提取的标准盖考卷层级 spec-extract；`accept[REQ-3]:` 前缀或行尾 `#REQ-3` 声明该条验证的需求 ID，两种写法并存取并集）/ 方案事实核查 `--check`（方案中的路径、`file:line`、Go 符号、Go 模块是否真实存在；`new:` 行豁免；advisory）。链与档位（advisory\|rubric\|human\|hard，HARD 只守事实）由 `<DataDir>/schemas/schema.yaml` 声明（缺省默认链 proposal→spec→design→plan 全 advisory 零阻断）；`task start --artifact "<stage>=<path>"` 开工即登记并就地提取验收；complete 前漂移 pre-flight：hard/human 档漂移阻断、advisory/rubric 留痕、漂移一律作废审批；逃生（落审计）`FORGE_ARTIFACT_CHAIN=disable` 或 `forge task override --artifact-chain disable` |
| `forge task chain-init [--force]` | 一键接线产物链执法：写默认链 schema 且 spec 档升 human——spec 里的考卷围栏（accept: 行/```accept 块）从此需 forge task artifact --approve spec --by <审批人> 签字才算定稿，内容再改即作废（业务正确性的签字入口）；已存在 schema 须 --force 显式覆盖；不依赖活跃任务 |
| `forge task mine [--agent <agent>] [--role <role>] [--all-projects] [--blocked] [--json]` | 列出分派给当前/指定 agent 的任务（`--all-projects` 全仓扫描按项目分组；`--blocked` 仅被依赖阻塞的，标注卡在哪环 [status, gate 进度 passed/total]） |
| `forge task gate <gate-id>` | 验证单道任务门禁 |
| `forge task impact --level none\|multi [--repo <key>]... [--note <说明>] [--ref <ref>]` | 声明当前任务的跨仓影响（多仓 workspace 成员的 verify 前置，单仓改动也须显式声明）：`--level none` 改动限定本仓；`--level multi --repo <key>` 波及指定成员 repo（`--repo` 可重复）。默认 advisory（未声明只提醒），protocol.yml 配 `cross_repo_impact: required` 升级为 HARD stop（四段式 WHAT/WHY/HOW/REF 报错），详见 docs/design/multi-repo-workspace.md |
| `forge task verify-acceptance [--ref <ref>] [--trust-foreign]` | 实跑验收标准（task start --accept 登记），记 deterministic 证据；v2 断言逐条判定并记 `acceptance-assert` 证据行（聚合 `acceptance` 行保留）；验收命令来自 task import / .forge migrate（外来标记）时首跑须 `--trust-foreign`（人工审阅命令清单后显式受信，防外来命令串直接执行）；登记了 held-out 保留集（`task start --heldout <file>`）时同时实跑双套件并记 gap——可见全过而保留集挂 = test-generalization gap（SpecBench 形态，`FORGE_HELDOUT=disable` 逃生留痕），task-complete 边界复跑；零登记时自动从 conventions 档案兜底登记默认套件（build/test/lint，考卷层级 conventions——项目默认考卷而非实现者自选），并从 held-out 池自动抽 1 套业务坏天气集入卷（池非空时） |
| `forge task accept <criteria...> [--ref <ref>] [--assert "type:arg :: expected"]` | 补登记验收标准（考卷层级 manual——实现者看过代码后出的题，梯子最弱一级）：格式同 --accept（"run :: expected"，可多条），`--assert` 挂本命令最后一条标准（file-\* 可独立）；锁内合并按 (Run, Expected, Assertions) 三元组去重（已存在的同一检查保留原层级）；已完成任务拒绝补登（考卷在交付前定稿）；交付验收单如实披露 manual 层存在 |
| `forge task report [--ref <ref>] [--json]` | 交付验收单（验收方单页视图）：考卷逐条结果+层级与来源分布、评分与证据强度（deterministic vs agent-claim）、未验证面（UntestedAreas）、残留风险（完成时 open findings）、考卷披露（manual 层告警/held-out 保留集/spec 审批态）、逃生舱库存、审查轮次——附复现入口（verify-acceptance 重跑 / trace 证据链）；逃生债行（每个验证类逃生转为未验证面披露）、自供申辩计数、自审标记（盖章会话=改动生产者时 doc-gate 不消费自录分）；--json 结构化输出 |
| `forge task mutation [--sample N]` | mutation 抽样（机器出题治假绿；作用于当前活跃任务，无 --ref）：对任务改动的 Go 源文件注入变异体（比较/逻辑翻转），逐个原地变异→实跑所在包 go test→立即还原——杀不死变异体的测试是装饰品（断言弱化/空断言无论覆盖率多高都查不出）。判定走 go 退出码契约（测试失败=杀灭/构建失败=无效体/通过=存活逐位点点名）；确定性均匀抽样（默认 3；只变异磁盘上有配对测试的改动文件，FORGE_MUTATION_TIMEOUT 调单变异体超时）；证据落 mutation-sampling deterministic 行（计入证据强度）；advisory 不阻断 |
| `forge task fuzz [--sec N] [--max N]` | fuzz 实跑（作用于当前活跃任务，无 --ref）：发现任务改动包的 Fuzz 目标并按预算执行 Go 原生引擎（默认 30s/目标、最多 3 个）——edge 清单再全也是人枚举的，fuzz 是无记忆的对抗者；失败目标分型：退出码 1=真 crasher（崩溃输入由 go 写进 testdata/，证据即语料，修完 go test 回归），构建失败/预算中断则无 crasher 语料；证据落 fuzz-run deterministic 行；advisory 不阻断 |
| `forge task edgecheck` | edge case 清单（作用于当前活跃任务，无 --ref）：任务改动的每个导出函数 × 五维（基数/值域/时序/环境/故障）机械枚举成可勾选骨架，落 DataDir/edgecases/——机器出骨架、验收前逐项填断言；确定性生成（文件序×声明序） |
| `forge task regression --finding <id> --test <file> \| --none --note <理由>` | 登记 finding 的回归测试绑定（修复自证）：修前红由 finding 作证、修后绿由 --test 指向的本任务改动 _test.go 作证（三重校验：后缀/任务窗口/真含测试函数）；无可测形态走 --none --note 落审计行——`forge task finding --resolve` 的硬前置，没有回归的 fixed 会复发 |
| `forge task heldout-pool --add <file> --name <名> [--tag <组>] \| --list [--tag <组>] \| --draw N [--tag <组>] [--apply] [--ref <任务>]` | held-out 池（独立出题）：把出保留题变成项目资产——业务坏天气集（金额边界/并发/退款链）沉淀进 DataDir/heldout-pool/，任务侧 `--draw N --apply [--ref <任务>]` 名序确定性抽取并入指定/活跃任务保留集（按 Run 去重、保留集计数不清零、落 apply 审计行；verify-acceptance 实跑双套件记 gap）；同名拒绝静默换锚；--add/--list/--draw 是项目级动作不依赖活跃任务；「测试的测试」是人工边界——池里的题必须人出 |
| task-complete 验收登记门禁（自动） | 非 generic 交付任务零验收标准 → 拒绝完成（考卷缺位）；考卷质量下限：有测试能力仓库考卷须含 ≥1 条测试类命令——单条 go build 不构成合格考卷；mutation 复发升硬：Go 改动连续 3 任务零 mutation 证据 → 硬拦（FORGE_MUTATION_GATE=disable 逃生）。考卷层级梯子：spec 产物提取（human 档可签字）＞ start 登记 ＞ conventions 兜底 ＞ manual 补登（验收单披露）。逃生与 freshness 共用舱：`forge task override --acceptance-gate disable` / `FORGE_ACCEPTANCE_GATE=disable`（落审计、降 evidence） |
| `forge task mirror github [--repo owner/name] [--dry-run]` | 分派任务镜像到 GitHub Issues（Forge 台账为主真相、issue 为组织可见面；offered→建 issue 打 forge:状态 label，终态→关闭；映射存 DataDir/mirror-gh.json；经 gh CLI，无 gh 明确报错）——Symphony 验证的组织面入口需求 |
| `forge compat snapshot [--out <file>]` / `forge compat report [--base <ref>] [--json]` | 可执行兼容工件（七面快照：命令树+flags / CheckName roster / 逃生舱 env / 内嵌载荷 sha256 / 序列化 schema 键 / 阻断位点计数 / 外部桥契约）——golden（compat.snapshot.json）入库、PR diff 呈现；report 对比基线：破坏性变更（removed/changed）exit 2、added 提示同步文档（cargo-semver-checks 契约本地化：0=过 / 2=违规 / 1=工具故障）；承诺表见 docs/design/compat-commitments.md |
| `forge config get/set compat <版本>` | 兼容基线声明（指纹分流 v1，GODEBUG go 行声明的轻量版）：声明"行为按哪个版本的默认走"（v1 为声明性锚点 + 落后提醒，暂不分派行为——完整分流机制见承诺表"刻意不做"）；`forge status` 显示兼容基线行与落后 minor 数；遥测回读（逃生舱使用聚合）见 `forge eval dashboard` |
| `forge task watchdog [--stall 45m] [--release]` | 长时任务停滞检测（always-on 治理）：从 checklog/toollog 取未完成任务的最后活动，超阈报停滞（task-stalled advisory，marker 节流每小时一条）；`--release` 清停滞任务租约；顺带展示 token 熔断信号 |
| `forge task doc-review --passed <pass\|fail> --score <N> [--round <R>] [--reviewer <id>] [--critical <tag:发现>] [--co-reviewer <id> --co-score <N>]` | 记录 L2 文档回检证据（输出→回检循环）：按 doc-review skill 四维评审后落档（产出者不能自检）；`--score` 为 0-100 总分、`--round` 轮次（≥3 轮未过升级人工确认）、`--critical` 落 Critical findings（未决阻断 complete；`tag:` 前缀打标供进化判据聚合）、`--co-reviewer/--co-score` 落同家族 borderline 双评记录（gate 不消费，只作分歧率观测）。task-complete 的 doc gate 消费该证据 |
| `forge task finding --content <text> [--tag <枚举>] \| --resolve <id> \| --stats [--source <source>]` | 记录发现/标 fixed/跨任务聚合：`--tag` 打类别标（padding/conclusion/template/false-precision/disclaimer/evidence/style）；`--stats --source doc-review` 按 Tag×Round 跨任务聚合计数——session-retrospective「同类打回 ≥3 次」升级判据的机器来源 |
| task-complete 自报一致性门禁（自动） | checklist 已勾选项里声称执行过的验证类命令（go test/pytest/cargo test 等）与 toollog 实测 Bash 集比对：测试类声称任务全程零匹配 = 虚报进度形态（arXiv 2605.29442）→ 拒绝完成；非测试类差集只留 advisory 痕；toollog 缺失（宿主遥测未接）跳过——区分"无法验证"与"验证通过"；逃生（留痕）`FORGE_SELF_REPORT=disable` |
| `forge docs lint [paths...] [--base <rev>]` | 文档产物 L1 确定性 lint（D1-D11：禁令短语/无证据结论/复述 diff/通过断言无证据/必填章节/结论枚举/篇幅/结论位置/调研文档的来源、信度标注与内部标签——调研三条 D9-D11 在观察期内全是 advisory）；`--base` 改扫该基线以来变更的 .md。exit code：0=通过 2=硬失败。禁令清单单一真相源在 `internal/doclint`，同步渲染进 forge-quality skill |
| `forge eval card [--render]` | 治理披露卡：Forge 占 ETCSOVG 哪四层、门禁/逃生舱/检查器自证与已知盲区清单（缺节 BLOCKED）。评测体系：docs/design/forge-evaluation-system.md |
| `forge eval friction [--corpus <dir>] [--timeout 120s] [--json]` | 摩擦净值实验（脚本化双臂）：诱饵语料在「门禁开 / 既有逃生舱全开」两臂下实跑，量拦截率、残余与检查器开销（ON-OFF 墙钟）；端到端 agent 臂走 FORGE_EVAL_MODEL 通道 |
| `forge eval rule-ledger --record --rule <id> --claim <预测> --probe <命令>` / `--verify --rule <id>` / `--list` | 门禁规则可证伪台账：规则变更带预测声明 + 可执行 probe，verify 实跑落账结论（`evals/rule-ledger.jsonl` 随库提交；损坏行 fail-closed） |
| `forge init --profile <mini\|standard\|full>` | 写进仓库契约的档位（`spec/forge.yaml` 的 `profile`）：mini=最小集（产物与检查都收窄），standard=缺省完整集，full=预留超集位（当前等同 standard）；`lite` 为 mini 的旧名（打印提示后按 mini 写）。它只决定这个仓库的契约档位与检查深度，不写 `~/.forge/profile`，也不改契约里的 `hooks` 兼容字段（缺省 `off`） |
| `forge eval dashboard [--dry-run] [--json]` | 遥测：escape 率/off_churn/自举通过率（Wilson 95% CI + 误用注记；样本低于字典下限只出 INSUFFICIENT）。快照落 `~/.forge/evals/forge/snapshots/` |
| `forge eval golden run [--dir <dir>] [--repeats N] [--rewrite-manifest] [--release] [--json]` | 门禁 golden 标注集重放：precision/fpr（Wilson 区间）+ 确定性重放一致率；用例集指纹钉在 `evals/forge/golden/MANIFEST.sha256`，不符拒绝运行（`--rewrite-manifest` 仅限显式轮换）。用例 `layer: test` 只在 `--release` 时运行，每次给 test 层查询计数 +1 并落 `eval-golden-test-query` 行 |
| `forge eval golden private-init` / `rotate [--max-cases N]` | 私有 golden 子集（0700，永不进 VCS）与季度轮换（oracle 复验 + 最老优先淘汰 + 审计行） |
| `forge eval traps run [--dir <dir>]` | 对抗陷阱重放（14 用例：测试削弱/伪造审计证据/虚假完成 + cheat-scan 七模式族 11 例——type-suppression/error-swallow/dead-branch/comment-only-fix/comment-as-debt/phantom-import/path-assumption，经 gate push --dry-run 推送边界探测；ImpossibleBench 思想）；capture 率与行动清单 |
| `forge eval redteam` | seeded-bug 红队演练：对验证链自身注入五类雷（零验收交付/internal 零引用导出/吞错/类型抑制/heldout gap），逐颗用既有检查函数判定拦截——escaped 或设施故障即如实披露（exit 非 0 供 CI 门禁）；与 traps 互补：traps 回放过去，redteam 注入现在 |
| `forge eval oracle run [--suite evals/oracle] [--arm both\|scanner\|acceptance] [--repeats N] [--image <img>] [--json] [--out <md>]` | 变更级沙箱考卷（v0，docs/design/sandbox-oracle.md）：每条用例在临时 git 仓库里回放一次变更——扫描臂调与被测门禁同一函数 `ScanCheatPatternsRange` 量 cheat 召回/类别准确率/误报（known_fp 不进分母），验收臂在 `docker run --network none` 里先跑可见测试（OFF）再挂 held-out（ON）量逃逸率；套件指纹（用例 sha256）写进报告可复算，`--repeats` 检出确定性漂移（flips）。实测 v0：召回 86%（6/7，唯一漏网是无机械签名的语义 stub——盲区被记录而非漏测）、误报 0/3、逃逸 100%→0% |
| `forge eval judge-audit --scores <file>` | 判分器受审：重放极差 + 与人工标注 Cohen's κ；κ<0.6 该判分器 BLOCKED 决策降级 ADVISORY。分数可带 `pair_original`/`pair_swapped`（a\|b\|tie）做成对位置交换审计，翻转率 >10% 判 high systematic bias |
| `forge eval resume-drill [--dir <dir>]` | 接续演练：脚本化断点续做断言（仅回归对比，绝对值不外宣） |
| `forge eval run --manifest <file> --profile <off\|gates-only\|full> --model <m> [--repeats N] [--wallclock <d>] [--forge-ref <ref>] [--variants a,b,c]` | 端到端基准运行（四元组 scorecard + pass^k 曲线 + 预算截断披露；`--forge-ref` 标注被测 forge 版本，四元组之一）；真实执行需 `FORGE_EVAL_SMOKE=1`，否则确定性 scripted 替身。开跑前 preflight：带 `test_cmd` 的容器任务须声明 `oracle_cmd`，oracle 臂须全过、只跑判分的 do-nothing 臂须全败，否则 BLOCKED exit 2（`FORGE_EVAL_PREFLIGHT=skip` 逃生，证据封顶 Weak）；`leakage_cmd` 声明时 leakage 分数与 pass@1 并列，高分任务列入需复核。`--variants` 声明 ≥3 个语义等价 prompt 变体时计入 CI 的扰动方差源 |
| `forge eval decompose --manifest <file> --models <a,b> [--profiles off,gates-only,full] [--repeats N] [--variants a,b,c]` | 方差分解大体检：HV̄/MV̄ + 排名翻转数 + η²_p + 三档差值（full−off 整体贡献 / full−gates-only 注入层 / gates-only−off 纯门禁代价）；三档差值带分层 bootstrap 95% CI，跨零时固定表述「在 n=X 次运行下未检测到显著差异；最小可检测效应 Y pp」 |
| `forge eval report [--quarter 2026-Q3]` | 季度自评测报告（汇编已落盘证据；缺失如实标注，绝不补造） |
| `forge eval otel [--out <file>] [--limit N]` | checklog → OTLP/JSON 导出（OpenTelemetry 通道：审计行进企业 SIEM/APM；scope=forge.checklog versioned mapper，`--limit` 只导最新 N 条） |
| `forge eval aat [--out <file>] [--limit N]` | checklog → IETF agent-audit-trail 形状 JSONL（链式 prev_hash + trust_level L0-L3；versioned mapper，meta 头声明全部有意偏离——标准卡位，见 docs/compliance/standards-crosswalk.md） |
| `forge skills inventory [--lock\|--verify] [--json]` | AST10 对齐的 skill 清单与内容指纹：枚举 canonical + pack 树、每个 SKILL.md 的 sha256；`--lock` 钉基线（skills-inventory.lock 提交进仓），`--verify` 对照锁文件核对（漂移/未知/缺项 exit 2——OWASP AST07 "immutable pinning, hash verification" 机械落地） |
| `forge gate push [--ref <branch>] [--dry-run]` | git 推送边界门禁：merge-base...HEAD 重跑确定性 cheat-scan + 本分支未消解 BLOCKED 任务（不依赖本地 hook 是否生效——云端 agent 分支同受治理）；证据快照落 DataDir/pushes/，CI 里复跑同套判定兜底；阻断 exit 2 |
| `forge gate hooks install [--uninstall]` | 安装 git pre-push 钩子（core.hooksPath=.forge/git-hooks，调 `forge gate push`；forge 不在 PATH 时 fail-open 放行）——治理随 git 走的本地接线 |
| `forge task scope add <glob> [--ref <ref>]` | 追加计划改动文件到白名单（支持中途迭代；--ref 指定任务，不依赖活跃任务检测） |
| `forge task scope show` | 查看声明的白名单 + 实时 scope-drift（advisory，不阻塞） |
| `forge task override [--work-activity\|--test-coverage\|--acceptance-gate\|--skill-decisions\|--doc-gate] disable` | per-task 逃生舱：关闭指定门禁检查（如批量重构时关 read-before-edit）；使用落 checklog 审计。验证类（test-coverage/acceptance-gate/skill-decisions/doc-gate）evidence 强度 cap 到 Weak（重证据任务按证据缩放豁免）；work-activity 是节奏门禁，只审计不降强度。doc-gate 的放行须在 doc-review 轮次上限后经人工确认再走 |
| `forge task complete` | 标记任务完成（自动评分） |
| `forge task abort [--ref <ref>] [--cascade\|--detach-deps]` | 中止并删除任务（清理 ghost/卡住任务，不评分；存在反向依赖时默认仅提示，`--cascade` 递归中止所有依赖它的任务，`--detach-deps` 从依赖它的任务移除该依赖边） |
| `forge task score` | 查看任务质量评分 |
| `forge task resume [--ref <ref>]` | 拉回任务接续上下文（目标/计划/决策/阻塞/参与工具+门禁进度+git 已改），跨会话/跨工具秒级恢复；同时把当前 session 锚定到任务（锚定只能显式做：没有 hook 捕获源码写入，未 resume 的 session 不会被自动补锚） |
| `forge task resume --match "<关键词>" [--workspace\|--all-projects] [--json]` | 模糊匹配未完成任务（ref/标题/分支/目标；容忍错拼、乱序词、缩写、中文片段），返回至多 10 个带得分的候选。只读：不锚定、不认领——agent 让用户确认后再 `resume --ref` 接手 |
| `forge task context [--ref <ref>]` | 只读查看接续上下文（resume 的不改 state 别名） |
| `forge task decide --content` | 记录已确认决策（持久化进 task，跨会话/跨工具不再推翻） |
| `forge task decide --rejected --content --rationale` | 记一条已证伪方案（死路记忆，docs/plans/loop-contract-2026-10-proposal.md）：`--content` 写方案、`--rationale` 写失败证据；resume 单独成节【已证伪方案】并进 tl;dr，不列入已确认决策。验收同因复发时 forge 会自动记一条 |
| `forge task next <step>` | 追加下一步（可多条） |
| `forge task block --content/--resolve <id>` | 登记阻塞或解决阻塞（open→resolved） |
| `forge task finding --content/--resolve <id>` \| `--reset-loop --note` | 记录跨工具发现（带来源工具）、标 fixed（指纹入 ResolvedPrints 记忆）；**审查回环**（回边语义，docs/design/artifact-chain-workflow.md）：open finding 跨过 schema 回边 max_rounds 次复核、或已解决 finding 同指纹再登记（复活）→ 回环耗尽，complete 被拦升级人工；人工裁决 `--reset-loop --note` 重开（审计行落地）。耗尽后唯一出口是人——终止权外置 |
| `forge task attach --ref --tool` | 锚定 session+工具到 task（跨工具多向锚定：pi 起、claude-code 接） |
| `forge task assign --ref <ref> --to <agent> [--role] [--by]` | 把任务分派给指定 agent（offered 起步，编排器侧；未知 agent 警告但接受） |
| `forge task claim --ref <ref> [--as <agent>]` | 工作方认领分派给自己的任务（offered→claimed，自动锚定 session） |
| `forge task deliver --ref <ref> [--as <agent>]` | 工作方交付任务（claimed→delivered，交回编排器；--as 与 claim 对齐，与分派 agent 不符仅提醒不阻断） |
| `forge task question --ref <ref> --content <text>` | 工作方回抛问题（claimed→input-required，暂停等编排器/人答复） |
| `forge task answer --ref <ref> [--content <text>]` | 编排器答复回抛（input-required→claimed，答复记入 Decisions；空答复仅恢复 claimed） |
| `forge task fail --ref <ref> --reason <text>` | 工作方标记任务失败（claimed→failed，记录原因） |
| `forge task cancel --ref <ref> --reason <text>` | 编排器撤回分派（offered/claimed/input-required→canceled，记录原因） |
| `forge task reopen --ref <ref> --reason <text>` | 交付后重开（delivered→claimed，交付后发现 bug） |
| `forge task reclaim [--dry-run] [--json]` | 回收 claimed 僵尸任务（claimed>TTL 默认 7d 无 checklog 活动）回 offered、重置认领时钟（补齐设计 §3 TTL 回收触发；复用 task health 的 IsClaimedStale 同一真相源，故 health 报告与本命令目标永远一致）。回收保留 Agent 不变 → 只有原认领 agent 崩溃/重启后能重新认领；改派用 cancel+assign。`--dry-run` 只列出不改状态 |
| `forge task export --ref <ref> [-o\|--output file] [--include-checklog] [--redact]` | 把任务导出为跨机器 JSON Bundle（task state 存于用户级 DataDir 不随仓库走，跨机器交接需此载体；--include-checklog 附带证据链；--redact 抹除 issue/agent/commit/证据供对外分享） |
| `forge task import --file <bundle> [--force\|--merge]` | 从 Bundle 导入任务到本地（导入 session 标记幽灵仅溯源；默认同 ref 拒绝，--force 覆盖，--merge 按 ID 并集协作记录；含 checklog 则回放进本地 trace） |
| `forge task health [--json]` | 扫描全 project 上浮僵尸/死锁/长期未答复任务（只读告警，不改状态）：offered>7d / claimed>TTL（无 checklog 活动）/ input-required>7d / abandoned_count≥2 标黄，DependsOn 指向 failed/canceled/缺失的死锁链与环主动报；与 mine/看板共享同一检测真相源 |
| `forge selfcheck pairing` / `forge selfcheck scope` | 门禁镜像自检（discipline-first-gates）：对当前活跃任务跑与 task-verify **同一计算路径**的秒级自检——`pairing` 镜像 test-coverage 配对检查、`scope` 镜像 PlanScope 漂移；发现项逐文件列出 + 修复指引（exit 1 供脚本感知，非阻断），每次运行落 `selfcheck-pairing`/`selfcheck-scope` 观察条目——其后失败的 verify 门禁按事实级交集归 confirmation（agent 已被告知这批文件）而非 discovery。逃生激活时显式标注「门禁已豁免、以下为事实」。`forge next` 在验收待跑且未自检时提示先行自检 |

</details>

<details>
<summary><b>🔍 代码审查 / 高危命令 / Act 反馈（自动挡）</b></summary>

**代码审查门禁**：`forge review` 让 code-review-gate 从"靠手动唤起"变成可判定——task 流程下 task-complete 门禁强制 ReviewPassed 前置（提交前必审）；非 task 流程没有自动拦截（没有 Stop hook），`forge review gate` 是显式判定命令：exit 0=放行、1=需审，可以接进自建 git 钩子或 CI。误触发已防护：纯文档/配置/生成物变更、无变更、commit 后干净工作区不触发。审查由独立只读子 agent 执行（防自审盲区），见 `code-review-gate` skill。

| 命令 | 说明 |
|------|------|
| `forge review pass [--ref <ref>] [--note <文本>] [--acknowledge-changes]` | 标记当前变更已通过 code-review-gate（task 模式写任务状态，否则写分支 stamp）；--note 审查结论文本记入 ReviewRound/stamp 与 checklog 审计留痕；距上次基线有源码变更时裸 pass 会被拒——须 --note 记复审结论，或 --acknowledge-changes 显式自我承担（记 WARN 级 self-refresh 审计） |
| `forge review gate` | 判定当前是否需要审查（显式运行，agent 或 CI 调用；exit 0=放行，1=需审 block；评估本身失败 fail-open 放行） |
| `forge review status` | 显示当前审查状态 |


**Act 反馈臂（证据驱动结论）**：`forge task complete` 时把本任务的证据驱动结论（评分 + 证据强度 + 验收通过率 + 低分维度）落盘到 `~/.forge/projects/<项目key>/act/conclusions.jsonl`，供回顾流程（`session-retrospective` skill）消费，`forge act show` 可查。证据弱（Unverified/Weak）或低分（<70）的结论标 RetrospectiveNudge——对冲"高分但没真验证"的 LLM-judge 盲区。

| 命令 | 说明 |
|------|------|
| `forge act show [--ref <ref>]` | 查看最新（或指定）任务结论（含 skill 触达画像——该 task 期间触发了哪些 skill） |
| `forge act list [--json]` | 列出所有任务结论 |
| `forge act nudge` | 最新结论有回顾 nudge 时输出一行（否则静默）——显式调用（任务收尾或会话回顾前），把 Act 信号带到回顾真正发生的检查点 |
| `forge act retro-done --ref <ref> --carrier <memory/skill/claudemd/code/hook/ci/none> [--lesson <教训>]` | 证据式 ack：session-retrospective 沉淀完经验后记录「回顾已发生」，被 ack 的 nudge 退出面板告警（`--lesson` 一句话教训；忘调用则 nudge 留面板——错过的回顾可见） |
| `forge act rebuild` | 从 tasks/*.json 重建 conclusions.jsonl（迁移 act 上线前的旧任务） |

</details>

<details>
<summary><b>🧠 Skill 治理</b></summary>

管理内置 canonical skill 库（阶段 D 起不再向宿主目录分发：没有 install/adapters/drift-check）。

`forge skills list` 与 `--help` 直接读编译期嵌入的库，**不落盘**；只有需要真实路径的子命令（audit/validate/eval/decide…）才把内置库解压到系统临时目录（`<tmp>/forge-skills-cache/embedded`），不再写 `~/.forge`。有真实源码树时用 `--canonical` 或 `$FORGE_SKILLS_CANONICAL` 指过去。

| 命令 | 说明 |
|------|------|
| `forge skills list` | 列出 canonical skill 库中的 skill（无显式源时直读内置 embed，不落盘）|
| `forge skills audit` | 21 条安全规则审查（prompt 注入/数据外发/危险代码/供应链执行向量；任一 CRITICAL finding 即阻断） |
| `forge skills validate` | R1-R18 规范校验 |
| `forge skills usage` | 使用度量分析（热门 skill + undertrigger 候选） |
| `forge skills usage --by-keyword` | per-keyword 触发分析：命中/加载/抑制切片（v2 Meta 的 matched_keyword）+ 死关键词检测（声明未命中；窗口无 v2 证据条目时自动停用并说明）。加载列带宿主偏差标注（注入型宿主无工具事件信号） |
| `forge skills effectiveness` | 技能命中×任务成效关联（命中数/task数/avg分/弱占比，agent-neutral） |
| `forge skills --dir <path>` | eval 命令族公共 flag：指定 eval 数据目录（默认 `~/.forge/evals`，`FORGE_EVAL_DIR` 可覆盖；首次默认解析自动从旧 `~/.pi/research/skill-eval` 一次性迁移。仓库内 `evals/` 或 CI 用） |
| `forge skills eval-gen [--save] [--cases-only]` | 生成 eval 清单；`--save`/`--cases-only` 额外落结构化 case 集（回归闭环基准） |
| `forge skills eval-cases --skill X [--blind]` | 输出 case 集 JSON（agent dispatch 跑 prompt 的输入）；`--blind` 盲测模式：每条 prompt 前置全库 name+description 清单（各截断 200 rune，渐进披露 L1 模拟），问「该触发哪个」而非「是否触发 X」，actual ≠ target 的行即误路由混淆数据 |
| `forge skills eval-record --skill X --from <file/->` | 回填一次 eval run（agent dispatch 跑完每个 prompt 后整批提交，归一化+判定+算 health） |
| `forge skills eval-report --skill X` | latest run vs baseline 回归报告（regression 三态 + pass-rate delta + 可比性） |
| `forge skills eval-baseline --skill X` | 标记 baseline run（回归基准，显式人工决策） |
| `evals/golden/<skill>/cases.json`（数据约定，非命令） | 人工策展黄金 case 集（真实话语改写，非 description 派生；进 VCS 可评审）。eval-cases/eval-record 加载时 golden 优先、派生补充，同 ID golden 胜出；策展 ID 前缀 `g-`，`origin: "curated"`，不带 desc_hash（description 变更不过期） |
| `forge skills battery [--gate]` | 全库回归电池：逐 skill latest vs baseline 判回归（JudgeSkillAccept 单一判源），`--gate` 任一回归即阻断（exit 4）；无 baseline/陈旧锚点降级 advisory |
| `forge skills mutex-gen` | 生成跨 skill 互斥 case 集（从 description SKIP 段的（用 X）让渡边派生：B 域 prompt 必须路由到 B、不得路由回声明让渡的 A；落盘 mutex/cases.json） |
| `forge skills mutex-record --from <file/->` | 回填一次互斥集 run（actual==Positive 才 pass；`--agent-model`/`--forge-version` 盖章防跨模型/版本假回归） |
| `forge skills mutex-report [--gate] [--json]` | 互斥集混淆矩阵（actual==Negative 为头号混淆行；`--gate` 任一混淆即 BLOCKED(stderr)+exit 4） |
| `forge skills decide [<skill>] --diagnosis ... --prediction <p> --warrant <k:v>` | 记录 skill 决策四元组（skill 名可用位置参数或 --skill；在带 skills/ 树的仓库内默认写仓库 canonical 而非 embed 缓存）；`--prediction` 声明可检验预测（哪个可观测信号应改善），供验证闭环回扣；`--warrant` 给出可核验的理由锚点（`run:<eval-run-id>` 与 baseline 有效比对无回归为强，`commit:<sha>` / `doc:<仓库内路径>` 为弱）；accept 决策缺锚点、或任一决策的锚点核验失败 / run 回归时 advisory 告警，决策照常记录；每次核验在 forge 项目内落 checklog `decision-warrant` 行（强为 pass、其余 warn）。`--evidence` / `--rationale` 含百分比、倍数或 arXiv 编号（不论内外）时落 `decision-evidence` 行：有来源（URL、正文引用 `run-<id>`，或 `--warrant run:`）为 pass，缺来源为 warn 并 advisory 告警 |
| `forge skills verify --skill X --decision <id> --result <r>` | 回填决策验证结果（预测→验证闭环第二步；`--at` 指定时间、`--history`/`--history-json` 查全量可证伪台账） |
| `forge skills revert --skill X --decision <id> [--edit] [--dry-run]` | Scoped revert：按 decisions.md 的 CommitHash 撤销某条决策关联的 commit（决策闭环的撤销臂） |
| `forge skills integration [<skill>]` | 查看 skill 的 forge 集成笔记（零反向依赖契约的 forge 侧承接：命令语法/门禁/逃生口；无参数列出有笔记的 skill） |

</details>

<details>
<summary><b>📊 可观测与维护</b></summary>

| 命令 | 说明 |
|------|------|
| `forge health [--json]` | 项目级质量趋势——聚合所有任务结论（分数走势/证据盲区率/复发低分维度，task→project 粒度联动） |
| `forge enforcement [--json] [--sample N]` | 执法健康报告（只读）：聚合 task-guard 的 advisory/blocked 计数、无视升档会话、测试编辑计数与野外申报，给出双环（升档会话超阈）与降格（zombie rule 复审）两个治理信号；`--sample N` 抽 N 个已完成任务 join 其 session 的 ignores/wild 遥测 |
| `forge trace <task-ref> [--window <chars>]` | 查看任务的完整质量事件时间线（checklog + toollog + token）；`--window` 输出分段监控窗口（每窗 ≤ 预算字符、头部周期性守卫重注入——下游 LLM judge/取证按窗取输入，禁止全量轨迹塞上下文，Classifier Context Rot 缓解） |
| `forge dashboard [--port <n>] [--no-open]` | 本地全局质量看板（Pulse 面板）——在任意目录运行都聚合 `~/.forge/projects.json` 登记的全部项目（`forge registry add` 显式登记），渲染事件流（任务/gate/skill 触发/结论）、任务评分与证据链、skills 聚合（localhost 只读，自动开浏览器，Ctrl+C 退出，面板内按项目过滤）；项目目录被移走/删除后注册表条目自动淡出（读时惰性精简），不留幽灵路径 |
| `forge bridge verify <dir> [--json]` | 对一个 dsh 插件包跑静态检查（#1884 四大事故模式：inject 一致性 / 同名注册 / @deepseek-ai vendored 耦合 / 未知事件），error → exit 2——dsh 插件作者发布前自检用（`docs/design/forge-dsh-provider-roadmap.md` 为阶段 D 前的历史记录） |

</details>

---

## ⚙️ 配置文件：`spec/forge.yaml`

仓库内的成员资格声明：带契约的仓库不需要隐式登记，全局视图需要显式 `forge registry add`。

查找位置：只认 **git 仓库根**的 `spec/forge.yaml`。放好之后跑一次 `forge status`，应看到 `spec: spec/forge.yaml (仓库声明式成员资格)`；`forge policy state` 应输出 managed（declined 的仓库例外：`forge off`、`policy yield`、或 clone 到带 `.forge-decline` 的仓库都会让这里报 declined）。子目录里的同名文件不会激活整仓；非 git 目录向上查找，走到用户 home 就停（home 本身不算项目根）。符号链接路径按真实路径判定。

| 字段 | 说明 |
|------|------|
| `version` | 契约版本，按主版本兼容（`"1"`） |
| `profile` | 契约档位（mini/standard/full），影响产物与检查深度：`mini`＝最小集（产物与检查都收窄，含观测项、无 advisory）；`standard`＝缺省完整集；`full`＝预留超集位（当前与 `standard` 等同）。缺省 `standard` |
| `hooks` | 兼容字段：合法值 `on`/`off`，缺省 `off`。宿主层已删除，没有东西消费它——`forge status` 只把值显示出来；写错键名（`hook`）会报错而不是静默按缺省跑 |


## 🚦 工作流程与进阶形态

每个开发任务自动走 3 道门禁。下面是 `task start` 的完整形态（worktree 隔离 / 分派 / 依赖 / 方案档位 / scope 白名单）：

```bash
forge task start --ref feat/add-login --branch --accept "go test -v ./... :: PASS"   # 创建任务+分支+登记验收标准（--accept 可重复；Expected 是输出子串匹配；go test 带 Expected 或 contains/not-contains 断言忘加 -v 时 start 自动补——无 -v 无 PASS 行永不匹配）
forge task start --ref feat/login --worktree [--base main] [--wt-dir <父目录>]   # 在 repo 树外为此任务创建独立 worktree+分支+绑定（多任务并发隔离形态；<repo 父目录>/<repo 名>-wt/<分支> 默认）
forge task start --ref feat/add-login --scope "internal/auth/*.go"                # 声明计划改动白名单（规划前置→可度量契约，advisory 检测 scope-drift）
forge task start --ref feat/add-login --tier mini                                 # 方案档位：none 直接改 / mini 小方案 / full 完整方案；与硬触发器（迁移/schema/接口契约/鉴权/依赖/跨模块）推导档位取较高者，下调须 --tier-reason 写明理由（advisory）
#   规则表可在 protocol.yml 的 design_tiers 段覆盖：rules（按 name 追加或替换内置规则，tier 取 mini/full，paths 支持 seg:<目录名> / 含 / 的路径 glob 或目录前缀 / 文件名 glob）、standard_changes（追加不触发档位的例行改动）、replace_defaults（弃用内置规则与内置例行改动）
forge task start --ref feat/frontend --assignee kimi --role frontend --depends-on feat/api   # 创建即分派给 kimi（offered），声明上游依赖 feat/api（DAG 环检测；task-verify/task-complete 在 feat/api 交付前阻断）
forge task start --ref feat/hotfix --assignee kimi --ttl 24h   # per-task TTL 覆盖全局 7d 僵尸窗口：短时效分派 24h 无活动即标僵尸（offered/claimed/input-required 通用；0=用全局默认）
# AI 自动完成工作...
forge task gate task-implement    # ✅ 代码实现（advisory：编译/断言提醒，agent 自检）
forge task verify-acceptance      # ✅ 实跑验收标准，记 deterministic 证据（spec-as-gate）
forge task gate task-verify       # ✅ 测试验证
forge task scope show             # 查看声明白名单 + 实时 scope-drift（advisory，不阻塞）
forge task gate task-complete     # ✅ 完成确认（第三道门禁；门禁全过 ≠ 任务完结）
forge task complete               # 🏁 任务完结：MarkComplete + 自动评分 + Act 结论 + 清 active ref（2026-08-18 死锁修复后完成标记归属本命令——门禁只过不 complete 会留僵尸任务）
forge task score                  # 查看质量评分
```

<details>
<summary><b>📖 门禁细节：退出码契约 / PlanScope / Cheat-scan</b></summary>

门禁之间有时间和活动检查，防止 AI 跳过阶段直接提交。`task-implement` 的编译/断言检查为 advisory 提醒（由 agent 自检，不阻塞）——forge 技术栈无关，适配 loop engineering。`forge task verify-acceptance` 实跑 `task start --accept` 登记的验收标准（`Run :: Expected`），把 dev-workflow Plan 的验收条件从 plan 文本变成不可伪造的 deterministic 证据——对冲 agent 自述"满足验收"却没真跑的盲区。`forge task complete` 对非 generic 任务硬前置至少一条登记验收标准（零验收 = 考卷缺位拒绝完成；考卷来源分级盖章：spec-extract ＞ start ＞ conventions 兜底 ＞ manual 补登，交付验收单如实披露各层级计数）。

**门禁退出码契约**：`forge task gate` 非 0 退出（输出 `BLOCKED:` 前缀）= 硬阻断，必须修复后重跑；零退出但见 `ADVISORY:` 前缀 = 软信号（gate 仍过、已记 checklog，应修不阻断）。按退出码而非文案行动——硬错误的散文易被误读成提醒而跳过。

**PlanScope 白名单（规划前置）**：`task start --scope <glob>`（可重复，或中途 `forge task scope add <glob>` 追加）声明"打算改哪些文件"。`task-verify` 比对实改源码与声明的差集，记一条 `scope-drift` 证据（deterministic，`forge trace` 可见）并 stderr 提醒。全程 **advisory 不阻塞**——变更影响分析召回率仅 ~44%，scope 是 prediction 非 contract，偏差是常态信号而非异常；它把"规划前置"变成可度量、可回顾的契约，正堵在 review 反复出问题的根因上。

**Cheat-scan（机械作弊模式扫描）**：`task-verify` 扫任务新增行（`+` 行），机械检测 7 类 AI 作弊模式——`type-suppression`（`@ts-ignore`/`eslint-disable`/`#[allow]`/`type: ignore`）、`error-swallow`（空 `catch{}`/`except:pass`）、`dead-branch`（`if(false)`/`if(1===2)`）、`comment-only-fix`（某文件新增行全注释零逻辑）、`comment-as-debt`（新增债务注释标记不解决）、`phantom-import`（相对 import 解析不到磁盘文件）、`path-assumption`（OS 路径分隔符被当内容匹配器，如 `HasPrefix(line, string(filepath.Separator))`——跨平台崩溃指纹）——记一条 `cheat-scan` 证据（deterministic，`forge trace` 可见）并 stderr 列出命中。全程 **advisory 不阻塞**：这些模式此前全靠 code-review-gate 的 LLM 子 agent 判断，LLM 每轮对同一 diff 重新采样抓不同子集，是"每轮 review 冒新问题"的体感来源；抽到 deterministic 后，机械模式一次判准，LLM-reviewer 退到只做语义判断（设计/架构/mock 是否幻觉）。`comment-only-fix` 是启发式（severity=low，纯文档任务可能误报）。

</details>

## change 子命令补充

- `change verify <id>`：在仓库根按顺序执行 acceptance.md 里的 ```bash/```sh 围栏（每条围栏一次 shell 调用），首个非零退出即停，其余记 skipped；全部通过才封印。判据写法受限（见 AGENTS.md 协议的红线段），被拒绝的写法会直接报错而不是静默跑。
- `change status <id>`：给出 stage、验证结论、节点状态、此前失败的尝试与下一步。`blocked` 时 `next_steps` 不含任何可执行命令，恢复命令在 `handoff`；`--json` 的字段是 snake_case。`prior_attempts` 只在「未封印且存在失败记录」时出现（omitempty），最多 10 条、新→旧；每条字段：`evidence`（`evidence/` 下的时间戳目录名；字段值本身不含 `evidence/` 前缀，不能直接当路径用）、`task`（非零表示单节点 gate）、`failed_command`、`exit`、`shell`、`timed_out`（命令被超时终止时为 true，此时 `exit` 省略——那个退出码不是命令自己的结果）、`exit: -1`（命令连启动/收回退出码都没做到，比如写 stderr 文件失败；文本输出会写「没有拿到退出码」）、`stdout_file`、`stderr_file`、`reason`（没有可定位的失败命令时说明原因），除 `evidence` 外都是 omitempty。文本输出把多行判据压成一行（` ; ` 连接，超过 160 字符截断成 `…`），证据路径按仓库根打印，完整原文与字段只在 JSON 里。返工前先读这些证据，别重试已证伪的路；已封印后是终态，历史失败只作记录。
- `change agents [--pointers claude|cursor|all|none]`：同步 `AGENTS.md` 的托管块，并维护指向它的 agent 入口指针（单源多指针）——`CLAUDE.md` 的块里是 `@AGENTS.md` 导入，`.cursor/rules/forge.mdc` 的 frontmatter 保证 `alwaysApply: true`（新建或无 frontmatter 时补一份；已有 frontmatter 缺这一键时补这一行、其余键不动——这处修复发生在托管块外），并让 agent 先读 `AGENTS.md`。默认行为：`CLAUDE.md` 只在其已存在时维护（不存在不建）；`.cursor/rules/` **目录已存在**时补 `forge.mdc`（说明仓库已在用 Cursor，这是「默认不创建」的唯一例外）；`--pointers claude`/`cursor`/`all` 点名则凭空创建，`none` 表示完全不动指针。合并已有文件时：有托管块只改块内；没有块就在末尾追加，原有字节与行尾不动；标记不成对或多对报错且不动文件；二次运行字节不变。`change init` 在首次声明契约时也调用它，规则相同（因此会追加已存在的 `CLAUDE.md`、在已有 `.cursor/rules/` 目录里补文件）；指针写入失败时 init 返回「AGENTS.md 已写入；修好后跑 `forge change agents`」。`spec/forge.yaml` 已存在时 init 会拒绝重跑，此时用 `change agents` 重同步；没有 `spec/forge.yaml` 时 agents 也拒绝同步，先跑 `change init`（理由：块内正文首句断言这个仓库用 `spec/` 记录交付）。
- `change gate <id> --task <N>`：前置条件是 tasks.md 里任务 N 存在、未勾选、写了判据编号（`#1`、`#1 和 #2`、`#1 或 #2` 均可）、四份产物已补齐并删掉 `forge:fill`、引用的围栏有可执行语句且写法通过判据检查；只跑这些围栏，证据记在 `evidence/<时间戳>/`，summary 里带 `task: N`，**不封印整条 change**。fast change 没有 tasks.md，用 verify。
- `change check [--staged] [--json]`：每个未归档 change 必须有一份与当前代码和判据一致的封印证据，否则列出理由并退出 2（先说最接近真相的一条：`failed` > `stale` > 「没有封印的证据」；每条理由都带修复命令）。`--json` 在所有路径上输出 `{"checked":[…],"problems":[…],"skipped":…}`（可选 `note`）——跳过时 `checked`/`problems` 是 `[]` 而不是 `null`，`skipped` 写原因，退出码 0；`--staged` 退化为全量检查时 `note` 写退化的原因，此时 `skipped` 为空、`checked`/`problems` 是数组（可为 `[]`，但不是 `null`）。`--staged` 默认只检查 `git diff --cached` 碰到的 change；暂存区一旦出现 `spec/changes/` 之外的路径就退化为全量（代码改动可能让已封印的证据失效），空暂存区或没碰到 change 时跳过并以 0 退出；非 git 仓库同样按全量检查。`spec/changes/` 不存在则跳过——CI 不该因为仓库还没用 change 流程而变红。随仓库下发两份接线示例：`.githooks/pre-commit`（`git config core.hooksPath .githooks` 启用；POSIX 上要有可执行位，clone 后丢失用 `chmod +x .githooks/pre-commit`；机器上没有 `forge` 或版本太旧时打印原因并放行，且它看的是工作区——`git add` 后又改回文件属已知缺口）与 `.github/workflows/change-verify.yml`（只在 PR 上跑，`fetch-depth: 0` 且检 PR head sha；有 `spec/changes/` 才跑；从源码构建 forge（仅本仓库的 Dogfood 形态；换仓库要改成安装固定版本的 forge），并在仓库设置里设为 required status check 才真正拦住合并）。归档即跳过：把未封印的 change 手工 `git mv` 进 `archive/` 等于人宣布它已交付，工具不再检查——这是信任边界，不是机器能替你判断的事。
- 「同一个原因」= 每次 verify/gate 的第一条失败命令；换命令或中间通过一次就重新计数，连续 3 次同因失败即 `blocked`。
- 退出码：用法错误、产物缺失、判据写法被拒 → 1；判据实跑未通过 → 2；`change check` 的未封印/失效/失败同样 → 2。
