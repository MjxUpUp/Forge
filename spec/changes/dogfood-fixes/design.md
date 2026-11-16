# 设计：dogfood-fixes

## TL;DR

next 的干净工作区分支补上「开工」出口；契约仓库的评分回退改为静默默认；
把 dogfood 踩出来的门禁步骤写进 AGENTS 模板与 cli.md，并用测试钉住。

## 1. next：干净工作区不再空转

`internal/taskpipeline/next.go` 的无活跃任务分支：

- `dirty` 分支保持 1.87 的四象限（ready change → task start --change；open change →
  change phase；spec 声明 → change new；否则 task start/wild）。
- 干净分支的新顺序：`behindRemote > 0` 先 `git pull --ff-only`（同步优先）；
  然后 `SpecDeclared` → `forge task start --ref <ref> --branch --title <title>`
  （理由里写清：可直接开工、`--kind task` 是小修标记、change 可后补）；
  未声明仓库仍给 `forge status`（保持旧行为）。
- 不新增状态字段：判定仍只用 `Dirty/SpecDeclared/BehindRemote`。

## 2. 评分：契约仓库没有 protocol.yml 是常态

`internal/taskpipeline/scoring.go` 抽出 `loadScoringConfig(root)`：

- `protocol.Load` 失败且是**契约仓库且文件不存在**（`specroot.Declared(root) && !protocol.Configured(root)`）→ 静默返回默认权重（阶段 C 起 init 不写 protocol.yml，缺文件是设计）。
- 文件存在但加载失败（坏 YAML/权限）→ 任何仓库都告警（否则坏配置被当成默认权重，等于无痕改分）。
- 失败且非契约仓库 → 保留原告警（可能是手误/类型错，静默改分不可接受）。
- 成功但无 `Scoring` 段 → 默认权重（原行为）。

## 3. 文档与模板

`internal/change/templates/agents-protocol.md` 的 Task 流程改为可照抄的 8 步：

1. `task start --accept ...` 登记验收；补登用 `task accept`（manual 层披露）；
   `--accept` 按空白切分不经 shell；考卷至少一条测试类命令。
2. 可选钉 change 契约。
3. 干活。
4. implement → verify-acceptance → task-verify（BLOCKED 先读证据）。
5. 含 .md 时 `docs lint` + `task doc-review --score`。
6. commit → 只读子代理审查 → `review pass` → `task-complete` → `task complete`。
7. `task report --ref`（完成后必须带）。
8. abort/override 的边界。

`docs/reference/cli.md`：`next` 行补干净工作区路由；`task start --accept` 行补切分方式；
`task report` 行补 `--ref` 必填。

## 4. 影响面

- 代码：`internal/taskpipeline/next.go`（干净分支新增一条路由 + `NextRepoContext`）、`scoring.go`（提取 `loadScoringConfig` 并接管 `BuildEvaluateInput` 的配置加载）。
- 测试：`next_test.go`（干净+spec→task start；干净+spec+behindRemote→pull；
  原「clean stays status」改为非 spec 仓库；`NextHint` 也吃 spec 上下文）、
  `scoring_config_test.go`（契约缺文件静默 / 坏文件告警 / 非契约告警 / `BuildEvaluateInput` 生产路径）、
  `agents_protocol_test.go`（新增必含句：`task accept`、不经 shell、doc-review、report --ref）。
- 文档：模板、cli.md、AGENTS.md（`forge change agents` 同步）。
- 不涉及：acceptance 语法、flag 集合、compat 快照（命令数不变）。

## 5. 风险

- `next` 在干净仓库直接建议 `task start` 可能让「只想看状态」的人多一步——理由文本
  明确写「也可以先 change new」并在标题模板里提示，可接受。
- 静默默认权重会让契约仓库的评分口径不可配——但契约仓库本来就没有 protocol.yml，
  且 `--profile` 已写进 spec/forge.yaml；需要自定义评分的用户仍可放 protocol.yml（成功后走原路径）。
  例外：文件**存在**却解析失败（坏 YAML/权限）时仍告警，否则坏配置会被当成默认权重，
  等于无痕改分（`internal/taskpipeline/scoring.go` 用 `protocol.Configured` 区分缺失与坏文件）。

## 6. 不做（Non-goals）

- 不改 `--accept` 的切分语义（仍按空白切分、不经 shell）——只在文档写清楚。
- 不给 `next` 增加交互式提问或自动开工——它只给建议，执行权仍在 agent。
- 不做 held-out 保留集、不改评分维度权重、不改门禁顺序。

## 7. 验证（Verification）

- 单元：`TestNextDecision_EntryRouting`（spec 干净→task start、落后远端先 pull、普通仓库不变）、
  `TestLoadScoringConfig_*`（缺文件静默 / 坏文件告警 / 非契约告警）、`TestConfigured`、`TestBuildEvaluateInput_*`（生产路径）。
- 模板：`TestAgentsProtocol_IsActionable` 钉 8 步关键句。
- 文档：`forge docs lint` 0 硬失败；`change verify dogfood-fixes` 6/6。
