# 功能聚焦三批落地设计（2026-09）

依据：docs/plans/feature-focus-2026-09.md（决策）与发展方向调研（~/.forge/research/forge-direction-20260905-0956/report.md，P0/P1）。本文是三批的工程设计记录——每特性给目标/设计/落点/验收；实现按批次开任务走门禁。

通用约束（适用全部特性）：

- **No Free Lunch 预算**：每道新门禁必须给出误报控制与逃生舱（沿既有 `FORGE_*` + checklog escape-hatch 留痕模式），无预算的门禁不合入（调研 dive_03 约束 1）。
- **观察类优先**：新 CheckName 一律先 observation/advisory 落地，BLOCKED 仅用于"证据明确造假"形态（与红线对照：BLOCKED 只出现在评测/证据自身不合法）。
- **本地门禁 = 证据生产者**：一切新判定写 checklog，为 git/PR 收口与 OTel 导出供料（方向 A/D1）。
- 监控分段约束（Classifier Context Rot，arXiv 2605.12366）：禁止把全量轨迹塞给任何 LLM 判定环节；Go 侧提供确定性分段工具，下游 judge 消费按窗口取输入。

---

## 批次一 · 线 1（P0 功能）

### 1a. OTel GenAI exporter（方向 D1）

- **目标**：checklog 审计行可导出为 OTLP/JSON，进入企业既有 SIEM/APM（Datadog 原生 ingest GenAI semconv v1.37+）；审计格式不再私有孤岛。
- **设计**：新包 `internal/otelout`——纯转换器（checklog []Entry → OTLP export request JSON），零第三方依赖（不引 otel SDK，输出手写 OTLP/JSON 1.1 形状，versioned mapper 可摘除）。映射：Resource（service.name=forge、service.version、forge.project.key）→ Scope（name=forge.checklog）→ Span（每 session 一条，name=forge.session，traceId/spanId 由 session/task key 稳定散列）→ Events（每条 checklog 一条 event，name=Check 名，attributes：forge.check.passed/level/evidence_source/task_ref/node_id + detail 截断）。gen_ai.* 语义留给上游 semconv 演进（semconv 无门禁/裁决事件），forge.* 命名空间占位——与调研 dive_04 判断一致（"缺位处用自定义属性占位并向上游提案"）。
- **落点**：internal/otelout/otelout.go + 测试（golden JSON）；CLI `forge eval otel [--out <file>] [--limit N]`（internal/cli/eval.go 注册）。
- **验收**：golden 测试钉 JSON 形状（resource/scope/span/event 四级 + 关键属性）；--out 落盘 AtomicWrite；README 命令表同步（docsconsistency 过）。

### 1b. 自报一致性门禁（方向 B · arXiv 2605.29442）

- **目标**：task-complete 前置检查"agent 声称执行过的验证命令"与 toollog 实测命令的一致性；差集非空 → advisory，涉及测试/验收类命令且任务全程零匹配 → BLOCKED（inaccurate self-reporting 的架构级回答）。
- **设计**：从任务三段工件的 checklist 已勾选项提取"声称执行的命令"（反引号内或测试类命令前缀：go test/npm test/pytest/cargo test/make test/go build 等）；与 `toolusage.LoadForTaskAll` 的 Bash ToolInput 集合比对（规范化：去 cd 前缀/空白，取首 token+首参数为匹配键）。输出差集。逃生舱 `FORGE_SELF_REPORT=disable` 记 escape-hatch 行。
- **落点**：internal/taskpipeline/selfreport.go（纯函数 + 接线）+ clitask/task_complete.go 前置调用 + CheckName `self-report-consistency` + 测试（声称 go test 而零 Bash 记录 → blocked；全部匹配 → pass；非测试类差集 → advisory）。
- **监控分段落地（同批）**：`internal/segmenter`——事件窗口切片（按字符预算切 checklog/toollog 条目序列，周期性插入 guardrail 重注入头），接入 `forge trace --window <chars>`：为下游 judge（transcript-forensics / doc-review 消费方）产出预分段轨迹。验收：800K 级合成轨迹被切成 N 窗且每窗含重注入头；trace 默认行为不变。

### 1c. git/PR 收口（方向 A v1）

- **目标**：门禁终裁从本地会话扩到 git 推送边界——所有产出（含云端 agent 分支）在 push 时过同一套确定性检查。
- **设计**：新命令族 `forge gate`：
  - `forge gate push [--ref <branch>] [--dry-run]`：①工作树未提交变更（warn）；②本分支相对 base 的累积 diff 跑 cheat-scan 批量模式（复用 ScanCheatPatterns）；③本分支关联任务的未决 BLOCKED 行检查；④写 checklog `gate-push` 行 + 推送证据快照 DataDir/pushes/<ts>.json（AtomicWrite）。命中阻断项 exit 2（BLOCKED 契约）。
  - `forge gate hooks install [--uninstall]`：写 `.forge/git-hooks/pre-push`（bash，调 `forge gate push`）并 `git config core.hooksPath .forge/git-hooks`；`--ci` 用法文档化（CI 复跑形态）。
- **落点**：internal/cli/gate.go + hooks 脚本资产（internal/hooks 或 cli 内嵌字符串）+ CheckName `gate-push` + 测试（临时 git 仓库夹具：dirty/干净/cheat 命中/未命中四态）。
- **验收**：pre-push 钩子在真实 push 前触发且阻断可复现；checklog 留痕；README 同步。

## 批次一 · 线 2（聚焦清理，不动 Go 核心）

### 1d. 设计族拆包

- 12 个 skill（frontend-feature-development/frontend-stack-selection/frontend-aesthetics-execution/frontend-code-review/ai-generated-ui-review/ai-ui-generation-workflow/design-system-workflow/design-system-migration/design-review-snapshot/design-artifact-standards/design-audit/ui-iteration-feedback-loop）从 `skills/` 迁至 `plugins/forge-design/skills/`，附 plugin 壳（README + marketplace manifest），核心 skills/ 缩至通用集。
- 同步：skills/CONVENTIONS §3 存废记录、embed 不变（skills.FS 只嵌 skills/，设计包不进二进制——按需 `forge skills install <path>` 或 marketplace）、守卫测试（计数类断言更新）、README skill 清单描述。
- 验收：go test 全绿；plugins/forge-design 12 skill 完整迁移（git mv 保留历史）；skillsqa audit 通过。

### 1e. 教科书瘦身 + maintainability 合并

- system-architecture（→~120 行，保带阈值判断规则，删 ADR 模板双头）；backend-development（删教科书节保 Gotchas/负向约束）；resilience-and-observability（删 Hystrix、修 SLO 数值、OTel-first）；secure-coding（重写：只保 OWASP 2025 映射要点 + 新增 agent/LLM 威胁节——prompt injection/MCP/skill 供应链）；maintainability-and-readability 撤销，§2.7 双语注释规范并入 code-review-gate references。
- 验收：`go run ./cmd/forge skills validate`/audit 通过；触发词与互斥组引用修复（audit 清单第 4 条路由冲突顺带修复 test-discipline/systematic-debugging 撞车）。

### 1f. 死机制清理

- session-continuity：删 session-history.jsonl 段（无工具强制的死约定），指向 forge task resume/context。
- cross-tool-context：重构为"forge task 双向锚定"为主、AI_CONTEXT.md 降级方案。
- on-demand-guards：/freeze 段改指 `forge freeze`（真 hook 已存在），删 prompt 纪律虚构。
- 验收：skills validate + 引用扫描（grep AI_CONTEXT/session-history 残留）。

## 批次二

### 2a. held-out gap 门禁（方向 B · SpecBench arXiv 2605.21384）

- **目标**：验收双套件——可见测试（agent 可见）+ held-out 测试（只在 DataDir 任务记录里，verify-acceptance 执行时才展开）；可见过而 held-out 挂 → cheat-suspect BLOCKED；gap 数字进 checklog。
- **设计**：`forge task start --heldout <file>`（命令清单导入 DataDir/heldout/<ref>.json 侧车——刻意不进 TaskState，task status/trace 不展示，结构上与 agent 常读的任务状态分离）；`forge task verify-acceptance` 同时跑两套，记录 `acceptance`（可见）与 `acceptance-heldout-gap`（gap=visiblePass∧¬heldoutPass → fail；无 heldout → Checked=false 跳过）。gap 阈值：v1 二值判定（任一 heldout 失败即 fail），分桶阈值留给 eval 校准。
- **落点**：tasktypes TaskState 字段 + taskpipeline/acceptance.go + clitask 导入/执行 + 测试。
- **验收**：golden——可见过+heldout 挂 → BLOCKED 且 detail 含 gap 描述；无 heldout 声明 → 不新增检查负担。

### 2b. safe-halt 语义（方向 B · ASE 2026）

- **目标**：hazard 连续拦截 ≥3 次的会话进入 safe-halt：停止自修复、要求人审解锁（failure transparency）。
- **设计**：hazard 包 halted 判定（events.jsonl 自最近 confirm/halt-release 的连续 EventBlock 计数，阈值 3）；task gate 推进时输出 safe-halt advisory；`forge hazard halt status` / `forge hazard halt release --yes` 人审解锁（记 halt-release 审计事件）。刻意无 env 逃生舱——解锁是人工审阅决策（agent 不得自我解锁），与既有 hazard confirm 同 philosophy。
- **落点**：internal/hazard/halt.go + clitask/hazard 子命令 + task-verify 接线 + 测试。

### 2c. issue-tracker 镜像（方向 C）

- **目标**：Forge 台账为主真相、GitHub Issues 为组织可见面（Symphony 的入口需求）。
- **设计**：`forge task mirror github [--dry-run] [--repo owner/name]`——offered→建 issue（label forge:offered），claimed/delivered/failed→label 同步；映射存 DataDir/mirror-gh.json；经 `gh` CLI 执行（无 gh 或未登录 → 明确报错）。--dry-run 只打印计划（测试用 fake gh 脚本，沿 evalkit 假二进制先例）。
- **落点**：internal/taskpipeline/mirror.go（纯计划函数）+ clitask/task_mirror.go + 测试。

### 2d. task watchdog（方向 C · always-on 治理）

- **目标**：长时任务停滞检测与租约释放（$6k 过夜/僵尸会话的 forge 侧对策）。
- **设计**：`forge task watchdog [--stall 45m] [--release]`——对每个 active/claimed 任务取"最后活动"（checklog/toollog 该任务最新时间戳），超阈 → checklog `task-stalled` advisory（marker 节流：每任务每小时至多一条）；`--release` 调既有租约释放。token 熔断已有（TaskTokenBreaker），watchdog 输出顺带展示。
- **落点**：internal/clitask/task_watchdog.go + 测试（伪造旧时间戳任务 → stalled；--release 后租约释放）。

### 2e. eval 升级（方向 E）

- harness 披露清单：Scorecard 增加 HarnessDisclosure 段（scaffold 版本/profile/宿主/sandbox/预算——arXiv 2605.23950 呼吁的披露协议字段），渲染进 scorecard 报告。
- judge MVVP：judge-audit 分数 schema 扩展可选字段（retest 轮/位置交换轮/cue 扰动轮）→ 计算 test-retest 一致率、position bias、cue 翻转率（>10% → finding "judge prompt 冻结"）；向后兼容。
- **落点**：internal/evalkit/report.go + judgeaudit.go + 测试。

## 批次三

### 3. open core 商业化设计（单独立项级，本文只出设计）

- docs/design/open-core-monetization.md：定位（模型中立证据与签核层）、付费墙边界（核心永不开收费：策略引擎/门禁/本地审计；付费：SSO/RBAC、审计导出与留存、fleet 管理、合规报告模板、私有化支持）、定价（$20-40/governed dev/mo + 审计事件超额 + 私有化年订阅）、GTM 顺序（欧美企业合规 → 个人漏斗 → 中国期权式）、里程碑与度量。引用调研 dive_05 结论，不写代码。

---

## 执行与验收总表

| 批 | 特性 | 新 CheckName/命令 | 测试形态 |
|---|---|---|---|
| B1L1 | 1a OTel | `forge eval otel` | golden JSON |
| B1L1 | 1b 自报一致性 | `self-report-consistency` | 表驱动三态 |
| B1L1 | 1b 分段 | `forge trace --window` | 合成长轨迹切片 |
| B1L1 | 1c git 收口 | `forge gate push/hooks`、`gate-push` | 临时 git 仓库四态 |
| B1L2 | 1d-1f | — | skills validate/audit + grep 残留 |
| B2 | 2a held-out | `acceptance-heldout-gap` | golden 双态 |
| B2 | 2b safe-halt | （task gate advisory + halt-release 事件） | 计数/解锁 |
| B2 | 2c mirror | `forge task mirror github` | fake gh |
| B2 | 2d watchdog | `task-stalled` | 旧时间戳夹具 |
| B2 | 2e eval | — | 披露渲染/MVVP 计算 |
| B3 | 3 设计 | — | docs lint |
