# 代码普查报告：架构优化与冗余清理（2026-09）

> 普查任务：`survey/code-census-arch-redundancy`（generic）。方法：go build/vet 基线 + deadcode/staticcheck 全仓静态分析 + 三路并行代码考古（架构分层 / 冗余重复 / skills 域），关键发现均经 file:line 抽查核实。只读普查，未改任何代码。
>
> **清偿进度（2026-09 全批收尾）**：本报告是普查时点的快照。第六节 6 项任务已全部执行并合入 main：
> 1. feat/single-source-convergence（95/A）——R1-R4 + P3 前两项清偿，补 checklog↔gate、doctor 词表↔谓词两道互钉 guard。勘误：普查称 isForgeHookCommand「无任何测试钉扎」不准确——settings_test.go:998 已有契约测试（普查员检索漏报）。
> 2. feat/deadcode-staticcheck-sweep（89/B）——6 处真死删除（含普查漏网的 completeGraceWindow bash 镜像）、17 项裁决注记、staticcheck 21→0；skillgen codexConfigHome 收归 hostcap。
> 3. feat/cliskills-extraction（95/A）——25 源文件+13 测试迁 internal/cliskills；审查拦截 Version init 拷贝恒空串的阻断缺陷后改惰性 seam。
> 4. feat/tasktypes-leaf（89/B）——数据模型下沉 internal/tasktypes（零反向依赖守卫常驻测试）；skillgen 的 taskpipeline 依赖消亡。
> 5. feat/skillmetrics-split（89/B）——B 簇 6 文件迁 internal/skillmetrics；EngagedAfter 导出为 engaged 判定单一源。
> 6. feat/task-domain-sinking（96/A）+ feat/continuity-attribution-sinking（98/A）——CompleteGeneric 完成编排与 AttributedPorcelain 归属过滤组装双双下沉 taskpipeline（A1 全项闭合）、attribution.PorcelainLines porcelain 单一入口、序言/播种收敛（P3-4 实测缩幅：14 处仅 3 处纯同构，其余为刻意 UX 分化，已注记）。
>
> **缓期项（含理由）**：~~A2-3 task 簇物理搬家~~ **已于 feat/clitask-extraction（95/A）执行完毕**——函数级重测绘推翻了缓期时的预估（簇对非簇真实依赖仅 7 助手，runHook/extractDetail 系标识符级测绘噪音），16 源文件+22 测试迁 internal/clitask，CommitBestEffort 接缝 + 16 导出符号；剩余缓期：A2-2 hookdispatch（runHook dispatcher 仍住 cli，现可经接缝评估）；P7 embed.go 分文件——hash 守卫已钉内容，重组纯排版收益；P8 命名清理——用户可见改名需产品决策；P3-6 JSON 读侧——普查自评低优先。

## 总体结论

仓库纪律基本面良好：构建与 vet 全绿；写侧已统一 `util.AtomicWrite`；逃生舱口集中且带 checklog 审计；无 import 环。结构性问题集中在两处：

1. **`internal/cli` 是唯一但巨大的架构债**：102 文件 / 23,782 行（非测试），import 42 个 internal 包，且承载领域逻辑（任务完成编排、continuity 组装、分派生命周期）而非纯命令面。
2. **约定"单一事实源 + guard test"存在 3 处已证实的逐字镜像违规**，另有 1 处 `FORGE_DATA_HOME` 逃生舱口失灵（用户可感知的 bug 面）。

规模基线（非测试 LOC，Top 6）：cli 23,782 · taskpipeline 10,792 · agentbridge 4,393 · skillseval 3,258 · hooks 3,023（3 文件，embed.go 单文件 2,322 行） · dashboard 1,860。

## 一、P1 — 违反声明约定的镜像与失灵（建议第一批清偿）

**R1｜hook 命令判定谓词三处逐字镜像，无 guard test**
- 源：`internal/hooks/settings.go:487` `isForgeHookCommand`（未导出）
- 镜像：`internal/cli/cleanup.go:275` `isForgeCmd`；`internal/agentbridge/codex.go:152` `isForgeBridgeCommand`（注释自认复制）；`internal/doctor/doctor.go:737-741` 注释亦承认镜像存在
- 全仓无任何一致性测试钉扎。收敛：导出 `hooks.IsForgeHookCommand`，三处共用并删镜像；加 guard 断言。

**R2｜门禁名字面量手写 26 处（非测试），checklog 常量不完备**
- 名义源 `internal/taskpipeline/gates.go:12-24` 只在 `DefaultGates()` 内联字面量，无逐 ID 常量；`internal/checklog/types.go:18-20` 有 `CheckTaskVerify/CheckTaskComplete/CheckTaskGuard` 但**缺 task-implement**。
- 代表证据：`internal/taskpipeline/executor.go:129,163,246,358,389,398,447`、`internal/cli/next.go:101-109`、`internal/cli/task_complete.go:222,241`、`internal/hooks/settings.go:21`、`internal/hooks/embed.go:384`（bash 内字符串）。
- 收敛：`taskpipeline/gates.go` 导出 `GateImplement/GateVerify/GateComplete` 常量并由 `DefaultGates()` 消费；checklog `CheckName` 补 `CheckTaskImplement` 并反向引用 gate 常量，两侧互钉。

**R3｜FORGE:START/END 标记串镜像**
- 源：`internal/util/marksection.go:48-49`。镜像：`internal/cli/cleanup.go:37-38`（注释已过时——所指 skillgen 常量早已改别名 util）、`internal/agentbridge/windsurf.go:349-350`；`internal/agentbridge/codex.go:301-302` 与 `kimi.go:49-50` 互拷 TOML 注释变体。
- 收敛：cleanup/windsurf 直接用 `util.ForgeSectionStart/End`；agentbridge 包内一处派生 `"# " + util.ForgeSectionStart`，另一处引用。

**R4｜`FORGE_DATA_HOME` 逃生舱口失灵（bug 面）**
- `forgedata.GlobalHome()`（`internal/forgedata/key.go:371`）是声明的唯一真相源，16 个文件已采用；但 `internal/cli/system.go:35,107`、`internal/cli/update_check.go:14,153,196`、`internal/cli/suggest.go:36`（err 分支兜底）仍 UserHomeDir+`".forge"` 直拼——用户设 `FORGE_DATA_HOME` 后 `forge system`/update-check 检查错目录。
- 顺带：`update_check.go:218` 裸 `os.WriteFile` 违反 AtomicWrite 约定。

## 二、P2 — 架构优化（分批执行）

**A1｜internal/cli 上帝包，领域逻辑入住命令层**
- 证据：`internal/cli/task_gate.go:21-40` completeGenericTask 决定 generic 任务自动过三门禁+MarkComplete（完成语义住在 cli）；`task_continuity.go:994-1012` porcelain+L3 归属过滤；`task_assignment.go` 821 行分派生命周期。
- 方向：cli 只留 cobra 装配与输出；完成编排/continuity 组装下沉 taskpipeline（或新 taskflow）。

**A2｜cli 内三簇天然拆分缝隙（按收益/风险排序）**
1. **skills 簇**：26 文件约 3,450 行（`skills_*.go`+`skill_trigger.go`），import 几乎只有 skills* 域包 → 整体迁出 `internal/cliskills`，零外部纠缠，首推。
2. **hook 簇**：9 文件 3,422 行（hook.go 1,365 行为 dispatcher）→ `internal/hookdispatch`，与 `internal/hooks`（资产包）形成资产/执行两分。
3. **task 簇**：16 文件 6,204 行 → 与 A1 的下沉联动做。

**A3｜skill 域反向依赖执行器**
- skillgen/skilltrigger/datamerge/dashboard 均 import taskpipeline，实际只用 `ActiveTaskState/CurrentSessionID/DefaultGates` 等约 3 个类型。方向：仿 `scoringtypes` 先例，纯类型下沉 `internal/tasktypes` 叶子包。

**A4｜skillseval（3,258 行）双职责**
- A 簇 eval 案例生命周期（~1,200 行）与 B 簇使用度量/观测（~1,110 行：usage/funnel/keyword/effectiveness/weakness/drift）零共享，消费方也不同（dashboard/pulse 只吃 B）。拆出 `internal/skillmetrics`。mutex.go(486) 可再独立为让渡冲突子域。

## 三、P3 — 模式性重复与中型清理

| # | 发现 | 证据 | 收敛 |
|---|---|---|---|
| P1 | rune 截断 7 份实现 | `util/text.go:9`（源）；`skilltrigger/render.go:126`、`cli/task_continuity.go:554` 逐字拷贝；`cli/trace.go:136`、`forgedata/key.go:81`、`taskpipeline/acceptance.go:271`、`toolusage/store.go:307` 变体 | 全收敛 `util.TruncateRunes`（可加 suffix 参） |
| P2 | `FORGE_SKILL_TRIGGER=="0"` 判定 5 处 | `skilltrigger/noise.go:70,121,197,223` + `cli/skill_trigger.go:132` | `skilltrigger.Disabled()` 单一源 |
| P3 | "本任务改动文件"三处实现 | `cli/task_continuity.go:994` / `taskpipeline/testcoverage.go:207` / `skilltrigger/conditions.go:46`（前两处直接跑 `git status --porcelain`，skilltrigger 侧经共享包 `attribution.ChangedFiles` 委托） | 收敛 attribution 或 taskpipeline 单一入口 |
| P4 | task 状态解析序言 14 块 | `if explicitRef != ""` 同构：`task_misc.go`×6、`task_gate.go`×2、review/task_complete/task_abort/task_impact/task_continuity | 仿 `skill_trigger.go:269` `taskRefForSession` 提 `resolveTaskState` |
| P5 | 测试播种助手 7 份 | cli 各 task 测试自播；`task_depref_test.go:83` writeForeignTask 手写 `projects/<key>/tasks/<ref>.json` 存储布局（镜像两份知识） | 抽 mutate-回调式 `Seed(t,...)`（正面样本 `project_sync_test.go:38`） |
| P6 | JSON 读侧无共享助手 | "ReadFile→IsNotExist→Unmarshal" 散布 49 文件 | 低优先；`util.ReadJSONFile` |
| P7 | hooks/embed.go 单文件 2,322 行 | 18 个 bash hook 字符串常量挤一文件 | 一 hook 一文件或 embed .sh + hash guard |
| P8 | 命名撞车 | "health" 三义（health/cli health.go/task_health.go）；worktree 绑定存 `DataDir/workspaces/` vs 多仓清单 `~/.forge/workspaces.json` | 文档明示或改名，暂不动存储路径 |

## 四、死代码与静态告警（deadcode 19 项 / staticcheck 21 条）

- **确认真死（无任何调用方含测试）**：`agentbridge/detect.go:79 codexConfigHome`、`scoring/evaluator.go:410 isSourceExt` → 直接删。
- **测试可达、生产未接线**（17 项）：`hlc.Compare/Parse/Clock.Observe`（sync-convergence.md §3 已自认未接线）、`checklog.LatestByCheckForSession/Clear`、`docsconsistency.AllFlags/DanglingSkillRefs`（guard 机制本体）、`forgedatatest.ForDataDir/RealProject`、`review.MarkPassed`、`taskpipeline.VerifyArtifact`、`skillsdist.hashTree`、`protocol.MustShouldLabel`、`nodestamp.resetForTest`、`projectsync.sha256Hex`、`agentbridge/kimi_plugin.go isSkillTriggerCommand`、`dashboard/pulseCache.loadCount` → 逐项"接线或删除"，hlc 头部补同款警示注释。
- **staticcheck 21 条**：SA4006×3（其中 `skillsdist/install.go:208` 为死初始化，非 bug；其余 2 条在测试）、S1038×3、ST1005×2（`cli/task_port.go:172,177` 错误串大写）等，均为一行级修复。

## 五、查过且干净 / 判定不合并的项

- 状态名（offered/claimed/delivered）集中在 taskpipeline Assignment 方法，无手抄。
- `RetentionDays`、frontmatter 解析、`findProjectRoot`、sha256 用法均单一源。
- checklog vs health vs doctor（事件存储/质量上卷/接线审计）、scoring vs scoringtypes（防环叶子）、protocol vs registry（项目配置 vs 全局注册表）、nodeid←hlc/nodestamp（严格分层）、workspace vs worktree vs projectroot（文档已划界）——**均不合并**。
- `internal/util`（435 行、零 internal 依赖、类别清晰）不是垃圾抽屉；`forgedata` 高 fan-in 属健康底座。

## 六、建议后续任务拆分（每任务独立分支，按序清偿）

1. **feat/single-source-convergence**（P1，R1-R4 + P3-1/P3-2）：镜像收敛 + gate/标记常量化 + GlobalHome 三处修复 + truncate/Disabled 收敛。含 guard test。风险低，收益即时。
2. **feat/deadcode-staticcheck-sweep**（四）：2 真死删除 + 17 项接线/删裁决 + 21 条 staticcheck。
3. **feat/cliskills-extraction**（A2-1）：skills 簇 26 文件迁出。零纠缠，搬家不改逻辑。
4. **feat/tasktypes-leaf**（A3）：类型下沉，skillgen/dashboard 等改引叶子包。
5. **feat/skillmetrics-split**（A4）：skillseval B 簇迁出。
6. **feat/task-domain-sinking**（A1+A2-3，最大件）：完成编排/continuity 下沉，配 resolveTaskState 序言收敛（P3-4）、测试播种收敛（P3-5）与"本任务改动文件"单一入口（P3-3）。
7. 可选：hookdispatch 拆分（A2-2）、embed.go 分文件（P7）、命名清理（P8）、JSON 读侧助手 `util.ReadJSONFile`（P3-6，低优先，先覆盖 cli/hooks/taskpipeline 三个高频包）。
