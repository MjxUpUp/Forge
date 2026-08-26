# multi-repo workspace：多仓分组 + 跨仓影响声明 + 跨仓依赖设计

> 本文记录多 repo workspace 特性的完整设计决策与依据，代码注释只保留局部 rationale，全局权衡在这里。机制参照生产级多仓协调 workspace 实践——借其「单一清单 / 强制 cross-repo-impact 声明 / 规则即代码门禁 / doctor drift 检出 / 三态」五件，不借其静态长文档层与外部仓库编排工具依赖。

## 1. 问题

- Forge 的项目模型是**一 repo 一 key**（`internal/forgedata/key.go`），全部治理（任务门禁、注册表、dashboard）以单仓为界。共同交付的多仓（app + 后端 + infra）没有任何 grouping 概念：
  - 改了 A 仓忘了 B 仓接口，没有任何机制强迫 agent 在 verify 前显式想一遍影响面；
  - 任务依赖图（`TaskState.DependsOn`）只认本仓 ref，跨仓「等上游交付」表达不了；
  - 多仓状态没有一个只读聚合视图。
- 全局注册表（`~/.forge/projects.json`）已有 key ↔ path 映射与原子写/惰性精简契约，可直接托底，不需要第二套身份。

## 2. 清单层：workspace = 一组 project key 的逻辑分组

### 决策

- 用户级 `~/.forge/workspaces.json`（与 projects.json 平级，同随 `FORGE_DATA_HOME` 隔离）是**唯一真相源**；`internal/workspace` 包承载 store。刻意不做静态长文档层——source 仓的多处纠偏疤痕证明多真相源必然漂移，Forge 侧清单即全部。
- **成员引用 key 不引 path**：`RepoRef{Key, Path}` 里 Path 仅是 add 时刻的展示缓存，一切解析（状态聚合、依赖寻址、环检测）走 `forgedata.RootDir(key)`。path 会漂移（移动、worktree、大小写变体），key 不会——registry 已经踩过 path 漂移的坑，不重复。
- workspace 是 registry **之上的聚合层**：不喂 `projectroot.Find` / `IsMember` 等 hook 热路径，hook 热路径零成本。
- **同一 key 允许属于多个 workspace**：全局工具无法假设互斥（共享库仓服务两个产品是常态）；重叠由 doctor 检出为 advisory，绝不硬性拒绝。
- 文件契约复刻 registry：写走 `util.AtomicWrite`（temp+fsync+rename）；损坏文件写路径（`LoadForWrite`）备份为 `.corrupt-<ts>` 后从空重建，读路径（`Load`）返回显式错误让只读调用方 fail-open。

### 命令面

| 命令 | 职责 |
|---|---|
| `forge workspace create/add/remove/list` | 清单 CRUD；add/remove 默认当前项目、`--path` 指定他仓；remove 另有 `--key` 逃生口（repo 已删除/搬走的僵尸成员无法再按 path 推导 key） |
| `forge workspace status <name>` | 读侧聚合：按 key 扫各成员 DataDir 的活跃任务（ref/gate 进度/branch）；成员故障告警跳过，绝不整视图空白 |
| `forge workspace doctor [--json]` | drift 检出（全部 advisory 不阻断）：not-registered / path-missing / path-mismatch / multi-workspace / empty / dep-cycle |

## 3. 跨仓影响声明：task-verify 的 cross-repo-impact 门禁

### 决策

- `TaskState.CrossRepoImpact`（nil = 未声明，向后兼容；`forge task impact --level none|multi [--repo <key>]... [--note]` 写入）。**单仓改动也必须显式声明 none**——声明动作本身即意义（规则即代码模式：强迫开工前想清楚影响面）。
- 默认 **advisory**（fail-open 哲学：清单是全局用户级 store，任何机器都可能缺失/损坏，基建故障一律降级 warn 级 advisory，绝不阻断）；protocol.yml 配 `cross_repo_impact: required` 把「未声明」升级为 HARD stop。
- 报错**四段式** WHAT/WHY/HOW/REF（规则即代码的报错契约；REF 指回本文档）。
- 声明畸形（multi 空 repos / 越界 key / 未知 level）只给 advisory 修正提示——声明意图在，因笔误阻断会惩罚配合的 agent。
- 无多仓成员资格的 repo **整体跳过且不记日志**（对占绝大多数的单仓场景保持静默）。
- checklog 里该检查（`CheckCrossRepoImpact`）标为 **observation 类，不进 BuildEvidenceChain 的证据强度**——它是声明状态的观测，不是验证证据；计入会虚涨 Strength。

### 接续卡片上下文行

`forge task resume`/`task context`/SessionStart hook 卡片与 `forge task status` 在多仓成员 repo 上加单行 `Workspace: <name>（N repos）· 跨仓影响: 未声明/none/multi(...)`。fail-open：`workspace.Load` 出错静默省略该行，绝不污染卡片。

## 4. 跨仓 DependsOn（key:ref 语法）

### 决策

- `DependsOn` 条目支持 `<key>:<ref>`（`SplitDepRef` 按第一个冒号拆；无前缀 = 本仓，零行为变化）。边界：branch 推导的 ref 永不含冒号（git refname 禁 `:`），显式 `--ref` 可以——CLI 校验拒绝「前缀不是本仓所属 workspace 成员 key」的含冒号 ref，引导用户去掉冒号，而不是让门禁在误读上死锁。
- 解析读路径 `LoadDepState`：按 KEY 寻址 `forgedata.RootDir(key)/tasks`，只读无锁（依赖方只关心 IsDelivered，读到旧值无非下次门禁重查）。
- **保守 pending 语义**：目标 task 缺失/不可读、key 无数据目录，一律计 pending——断裂的依赖边绝不静默放行。
- 写入侧校验（`forge task start --depends-on`）反向 fail-open：清单不可读降级 advisory 放行（基建故障绝不阻断建任务）；越界 key 与「经本仓 key 指回自己」硬拒绝；目标暂缺容忍（前向引用合法）但给 advisory。
- **环的处置分两层**：本仓环照旧由 `AddDependency` 写入时拒绝（既有 DFS）；跨仓环**不做实时 DFS**（需要跨 DataDir 的全局图锁，复杂度高）——由 `forge workspace doctor` 周期性检出 `dep-cycle` advisory，点名完整 key:ref 环序列供人工摘边。检出刻意放在 CLI 层（`workspace_depcycle.go`）：taskpipeline 已 import workspace，反向 import 即成环。
- `IsDelivered` 语义跨仓一致：有分派看 Assignment.Status==delivered，无分派看 IsComplete——无需新定义。

### 已知盲区（接受，不阻断 v1）

- **abort 跨仓盲区**：`forge task abort` 的反向依赖提示/级联只扫本仓 DataDir；他仓任务依赖被 abort 的任务时不被提示，等 verify 时报 pending（保守语义兜底，死锁可见）。
- **`forge task mine --blocked` 显示限制**：annotateDep 对跨仓依赖显示为 missing（只按本仓 DataDir 注解）——阻断清单里的 key:ref 原样仍正确，仅状态展开缺失。
- 环上任务的死锁只能靠 doctor 发现——无实时检出（见上，全局图锁不值得）。

## 5. 与 project-sync 的边界

- workspaces.json 是**机器本地配置**（本机有哪些仓、怎么分组），**不进 bundle allowlist**（allowlist 默认拒绝——未枚举即不带），也不进 sync 通道。各机各自 `forge workspace add`，与 projects.json 的机器本地性一致。
- 跨仓依赖的目标寻址按 key，天然兼容 adopt 后的 ID key 与路径 key——只要两仓身份在本机推导一致，跨仓边就稳定。

## 6. follow-up（v1 范围外）

- workspace 级 protocol 中间层（不动 `protocol/loader.go:pathFor` 的项目级契约）；
- 规则即代码的通用 grep 检查配置化（等真实多仓规则积累 3+ 条再抽象，不提前泛化）；
- PR 链 / 跨仓 e2e 编排（强业务耦合，属用户侧 tooling）；
- workspaces.json 跨机同步（待 project sync 有用户级通道再议）；
- `task mine --blocked` 的 annotateDep 跨仓显示（§4 已知盲区）；
- abort 的跨仓反向依赖提示（同 §4）。
