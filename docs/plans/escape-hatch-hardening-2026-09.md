# 门禁逃生口加固(2026-09-18 立项)

> 背景:sess_7e05f7e1 取证(76 分钟全自主会话)+ 业界/学界调研(run_dir:
> ~/.forge/research/agent-gate-enforcement-20260918-1508)。四层失守定性:
> L1 执法挂在 agent 不会用的动词上(gate CLI)、L2 advisory 天花板后审计静默、
> L3 zcode 双挂载、L4 HITL 无等人态。本 spec 收敛 P0 落地项与其契约。
>
> 理论锚点:参考监视器三属性(always invoked / tamper-proof / verifiable,
> NIST);业界共识 = 逃生口不消灭而 sudo 化(显式/必经中介/必留痕/可禁绝/有界);
> 学界警示 = prompt 层禁令实证无效(METR、ImpossibleBench),执法逻辑对 agent
> 过度暴露反升作弊率。

## 原则(本 spec 全部改动不得违反)

1. **审计不可静默**:任何"降低打断"的改动不得删除 checklog 落盘——原则源自
   discipline-first-gates 原则 2,2026-09-18 取证实证违反后果(checklog 断流
   35 分钟,失守不可见)。
2. **advisory 先行,block 需承诺表**:P0 一律 advisory(stderr + checklog),
   BLOCK 化走 ≥2 minor 预告 ratchet(gate-cmd-form 先例)。
3. **choke point 优先于新检查**:新执法点必须挂在 agent 必经动词上
   (git 边界动词 / Stop),不做"再多一个无人理会的 advisory"。
4. **不污染既有度量口径**:新观察用新 Check 名(test-nudge-state / task-drift),
   不混入 test-nudge / test-coverage-gate 的 D1-D3 分母分子。
5. **失败开放但可观察**:判定依赖缺失(git 不可用 / 任务态读失败)→ 静默放行,
   但正常路径必须有审计行兜底。

## P0-A:test-nudge 天花板审计行(修 L2)

**病灶**:hook_track.go 非升档路径 `if !fire { return nil }`——tier3 打满后
150+ 次源码写入零记录,违反原则 1。

**契约**:
1. 新 Check 名 `test-nudge-state`(checklog.types)。Passed=true、
   Checked=true、Level=advisory、Delivered 显式 false(审计行,非注入)、
   Source=deterministic。
2. 触发节流:非升档源码写入评估累计每 20 次(`testNudgeStateRowEvery`)
   落一行;发射(升档)时计数器清零。计数器存于既有 testNudgeState
   (新增字段 `SuppressedWrites`),随 TaskRef 边界重置。
3. Meta:`unpaired_files` / `tier` / `fired_tier` / `suppressed_writes`
   (与 test-nudge 行同键同序,便于 funnel 关联)。
4. 度量护栏不变:D2/D3 与防伪护栏均按 check 名 `test-nudge` 统计,新行
   不入其分母(原则 4)。

## P0-B:task-drift choke point(修 L1 最小可用)

**病灶**:任务存在时,task-guard `echo PASS`、bash-guard 无 git 语义——
commit/branch/merge 全程无中介,agent 越出任务分支狂奔不可见。

**契约**:
1. 新 in-process hook `task-drift`(hookdispatch,无 bash embed),挂
   PreToolUse Bash matcher(ForgeHookSpec,batch 自动并入)。
2. 触发词:命令含 git 边界动词——`git commit`、`git merge`、`git branch <name>`、
   `git checkout -b`、`git switch -c`(建分支/提交/合并 = 任务边界动作)。
3. 判定(均需活跃任务,taskAttributionForSession):当前 git 分支
   (`git -C root rev-parse --abbrev-ref HEAD`)≠ state.Branch → 漂移。
   git 失败/非仓库 → 静默放行(原则 5)。任务分支上的 commit **不是**漂移
   (commit-before-complete 是既定合法顺序)。
4. 输出:stdout allow-with-detail 通道(`EmitAdvisoryRouted`,文案以
   `[task-drift]` 谓词开头——choke point 的价值在 agent 看得见,故弃
   gate-cmd-form 的 stderr 通道而取 test-nudge 的上下文注入通道;kimi 宿主
   入队攒发,Delivered/Channel 经 AdvisoryEmissionChannel 如实盖章)+
   checklog warn 行(Check=`task-drift`,Passed=false,Meta:verb/branch/
   task_branch/gate/occurrence)。文案给出口:回任务分支 / `forge task gate`
   推进 / `forge task abort` 显式弃任务。`git branch <name>` 只认非旗标
   名字 token——`-a/-v/--list/-d/-m` 查询/删除/改名形态不是建支,不触发。
5. 节流阶梯(会话 marker 计数):第 1、2 次全量文案(第 2 次升档措辞),
   之后每第 10 次(10/20/…)落一行计数摘要——审计不静默但不刷屏(原则 1/2)。
6. 永不阻断(P0);BLOCK 化 = P1 ratchet(见分期)。不入 profileLiteHooks
   (advisory 不满足 lite 准入规则)。
7. 排除:命令本身是 `forge ` 前缀(任务 CLI 自建分支合法)。

## P0-C:zcode 双挂载(已落地,2026-09-18 P1 批)

**根因(实证钉死)**:forge 钩子经两条通道同时进入 zcode——① zcode
translator 写的用户级 `~/.zcode/cli/config.json`(batch 接线,--agent zcode);
② 用户以 Claude Code 插件格式安装的 plugins/forge 载荷
(`.claude-plugin/plugin.json` 全量 per-hook;installed_plugins.json 载明
source=./plugins/forge)。zcode 两条都执行且对 matcher 不敏感(实证:两侧
Write|Edit 组均不含 tool-track,toollog 的 Write/Edit 行仍精确 2×——钩子组按
事件全量触发)。后果:每次工具调用 2× 进程拉起、SessionStart 注入双份、
toollog 双行、抑制计数双记。

**落地修法(候选 c,审查修正后收敛)**:dispatch 层短窗幂等守卫
(`hook_idempotency.go`)——runHook 对 (hook,event,tool,session,input) 完全
一致的重复调用,窗口内(默认 3s,`FORGE_HOOK_DEDUP_WINDOW` 旋钮,0=禁用,
钳制 ≤10s)静默跳过。**仅观测型钩子**(isObservationOnlyHook 白名单:注入/
观察类)——首版全量去重被只读审查推翻:skip=allow 对阻断型钩子是执法反转
(deny 后 3s 内逐字重试被静默放行,实证打挂 hazard release 与 task-guard
阶梯 e2e)。阻断型钩子双投递无害(重复 deny 同一命令,各钩子自带去重)。
跨通道去重(key 刻意不含 agent);marker 落 DataDir/markers/hook-dedup/。
候选 a/b 弃(需卸载插件/破坏归因)。已知代价:并行同参调用的第二条少记一行
toollog(本就去重对象);竞态双读双跑=良性 fail-open 退回现状。

## P1(已落地,2026-09-18 P1 批)

- **task-drift BLOCK ratchet**:机械版本门 `util.CompareVersions(version,
  "1.66.0")`(1.64 advisory 首发 → 1.66 自动转 BLOCK,不留 gate-cmd-form 式
  文字债);逃生 `FORGE_TASK_DRIFT=0`(落 escape-hatch 行);会话阻断上限
  5 次(超限自动降级 advisory——有界)。承诺表已登记(2026-09-18 裁决记录,
  兼作「机械版本门」先例)。
- **task-verify Stop 有界阻断**:窄条件=活跃任务+未提交代码变更(**含
  untracked 新代码文件**;工作在飞未落盘,runaway 签名;gate PASS 不豁免
  ——advisory 门通过 ≠ 工作落盘)。三重界:①会话限额 3 次,超限回落
  advisory+一次性说明;②block 时清 60s 节流戳——连续阻断由限额封顶(至多
  3 连),而非节流削弱成每分钟一次的急停提示(审查披露项;段位在 kimi 早退
  之前,执法面宿主均匀);③逃生 `FORGE_TASK_VERIFY_STOP=0`。每次 block 落
  一行 checklog fail(与 task-drift block 行对称,原则 1 的 block 面)。
  历史教训(无条件阻断致死循环)以「限额」而非「永不阻断」回应。
- **hazard HITL 等人态**:拦截时刻只有 agent 收到 deny(自主会话里 agent 可
  能改写绕行,取证 9 秒绕行);Stop 面新增 HITL 提醒——本会话 block−(release
  +confirm)>0 时输出「N 个高危拦截未经人工确认」(会话维度,task-verify.sh;
  近似计数,审计走 forge hazard status)。
- **P0-C 落地**(见上节)。

## P2(wild 已落地;分层暴露仍为方向)

- **AC-3(10) 受审计逃生(已落地)**:`forge task wild` 每条申报落 checklog
  escape-hatch 行(gate=task-wild/reason=declarative/owner,进统一漏斗);
  会话限额 3 条,超额升级提示建任务(不拒绝——逃生舱永在);有活跃任务时
  申报输出更强异常信号行。
- **规则分层暴露(仍为方向,未立项)**:执法判定逻辑不进 agent 上下文
  (METR 43× 警示)——教学面(点名文件)与执法面(判定规则)分离,需要
  独立 spec 定义暴露面清单,不在本 spec 范围。

## 度量(回测口径)

- M1(修 L2 生效):天花板场景下 checklog 不再断流——同任务 tier3 打满后
  每累计 20 次源码写入有 ≥1 行 test-nudge-state。
- M2(修 L1 生效):漂移场景首次 git 边界动词即有 task-drift 行(0 漏报于
  判定前提成立时)。
- M3(无误报):任务分支上的 commit/无任务会话/forge CLI 命令 → 零行。

## 总验收(verify-acceptance 实跑口径)

```accept
go test ./internal/hookdispatch/ -run 'TestRunTestNudgeHook_CeilingAuditRows|TestRunTaskDriftHook|TestTaskDriftBlockRatchet|TestHookDedup|TestSkipDuplicate|TestRunHook_DuplicateInvocationSkippedSecond'
go test ./internal/checklog/ -run TestEscapeHatchHardeningChecksInRoster
go test ./internal/agentbridge/ -run 'TestOpencodePlugin_CarriesTaskDrift|TestOpencodePluginWiring|TestDshPluginSpecMirrorsSpec|TestKimiPluginManifestMirrorsSpec|TestPluginPack_Committed'
go test ./internal/skillgen/ -run TestClaudeMDDocumentsTaskDriftHook
go test ./internal/hooks/ -run 'TestForgeHookSpecForProfile_Tiers|TestHookWiring_FreezeRatchet'
go test ./internal/compat/ ./internal/cli/ -run 'TestAllCheckNamesSorted|TestCompatSnapshotMatchesGolden|TestTaskWild'
go test ./internal/e2e/ -run TestTaskVerifyStopHook
go vet ./...
```
