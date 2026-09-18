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

## P0-C:zcode 双挂载(查因不改码)

**实证**:toollog 228/456 精确成对;本会话 SessionStart 注入双份;两个接线源
同时生效(~/.zcode/cli/config.json 用户级 batch + ~/.zcode/cli/plugins/cache/
forge/forge/0.0.0/hooks.json 全量 per-hook,标 `--agent copilot`)。
**候选修法**(P1 落地前需确认 zcode 插件 hooks 语义):a) 插件载荷按目标宿主
剥离 hooks.json;b) zcode translator 检测插件存在时跳过用户级接线;
c) dispatch 层 (session,event,tool,input-hash) 短窗幂等(风险:并行同参调用
误去重)。**spec 结论:未钉死 zcode 行为前不动码。**

## P1(预告,不在本轮)

- task-drift BLOCK ratchet(≥2 minor 预告);
- task-verify Stop 有界阻断(参照 CC 8-consecutive cap;Forge 起点
  MaxReviewRounds=3 先例,自设上限低于宿主强制上限);
- hazard HITL 等人态(block → 暂停 + 上报,不再留给 agent 绕行);
- P0-C 落地。

## P2(方向)

- 规则分层暴露:执法判定逻辑不进 agent 上下文(METR 43× 警示);
- AC-3(10) 式受审计逃生:`forge task wild` 补结构化理由 + 限额
  (业界无人做,Forge 可首创)。

## 度量(回测口径)

- M1(修 L2 生效):天花板场景下 checklog 不再断流——同任务 tier3 打满后
  每累计 20 次源码写入有 ≥1 行 test-nudge-state。
- M2(修 L1 生效):漂移场景首次 git 边界动词即有 task-drift 行(0 漏报于
  判定前提成立时)。
- M3(无误报):任务分支上的 commit/无任务会话/forge CLI 命令 → 零行。

## 总验收(verify-acceptance 实跑口径)

```accept
go test ./internal/hookdispatch/ -run 'TestRunTestNudgeHook_CeilingAuditRows|TestRunTaskDriftHook'
go test ./internal/checklog/ -run TestEscapeHatchHardeningChecksInRoster
go test ./internal/agentbridge/ -run 'TestOpencodePlugin_CarriesTaskDrift|TestOpencodePluginWiring|TestDshPluginSpecMirrorsSpec|TestKimiPluginManifestMirrorsSpec|TestPluginPack_Committed'
go test ./internal/skillgen/ -run TestClaudeMDDocumentsTaskDriftHook
go test ./internal/hooks/ -run 'TestForgeHookSpecForProfile_Tiers|TestHookWiring_FreezeRatchet'
go test ./internal/compat/ ./internal/cli/ -run 'TestAllCheckNamesSorted|TestCompatSnapshotMatchesGolden'
go vet ./...
```
