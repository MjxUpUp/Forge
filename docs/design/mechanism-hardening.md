# 机制加固 P0-P2 落地设计（2026-09）

依据：机制史调研报告（~/.forge/research/forge-mechanisms-20260905-1752/report.md §八）七项建议。每项目标/设计/落点/验收如下。

通用约束：全部走仓库既有纪律（测试配对、read-before-edit、门禁流程）；P0/P1/P2 七项各带测试；`forge compat` 系新命令进 docsconsistency 反查。

## P0-1 bundle 版本偏移感知（对侧兼容，K8s 偏移窗口语义）

- **现状**：Manifest.ForgeVersion 已存在且导出侧已填充（project_sync.go:183 cleanVersion）——调研称"无版本号"不准确，**缺的是导入侧感知**：旧二进制读新 bundle 时 Go json 静默丢弃未知字段、re-export 无声裁剪。
- **设计**：`projectsync.VersionSkew(local, bundle string) string`（util.CompareVersions；local ≥ bundle → ""；bundle > local → 警示文案：前向偏移，本机 re-export 会裁剪较新字段，建议先升 forge）。接入点：project_import.go 的 Unpack 之后（sync pull 走同一 `project import` 核心）。落 checklog 新名 `sync-version-skew`（LevelWarn，观察类）+ stderr 警示。**不硬拒**（账本幂等、重复 pull 免费的体验保持）。
- **验收**：单测三态（同版/新导旧/旧导新）；e2e 式：构造 ForgeVersion 更大的 manifest 导入出警示行。

## P0-2 npm 版本对齐守卫（分发纪律）

- **现状修正**：release.yml 发布时用 jq 注入正确版本到平台子包（L104/132）——**发布态是对齐的**；仓内 platforms/*/package.json 停在 1.28.2 是过期提交态（调研的"22 minor 偏差"是仓内漂移非发布漂移）。风险：审计误导 + 若有人绕过 workflow 手工发布即真漂移。
- **设计**：①仓内五个平台包版本对齐到 npm/package.json（1.50.0）；②守卫测试 `TestNpmPlatformVersionsAligned`（读六处 package.json，版本必须相等——进 CI，防再漂移/绕过发布流）。
- **验收**：守卫绿；故意改一个平台版本守卫红（临时验证后还原）。

## P1-3 兼容承诺表 + BLOCKED 文案契约（渐进收紧，PEP 387/K8s 形态）

- **设计**：docs/design/compat-commitments.md——三向（对上宿主/对下数据与命令/对侧多机）× 三档（强/弱/不承诺）矩阵（调研 dive_06 的义务矩阵为底）+ advisory→blocked 的时间承诺（≥2 个 minor 版本预告，PEP 387 口径）+ 文案契约（新 BLOCKED 门禁的拒绝文案必须含"预告版本"或指向承诺表）。契约的机械执法点：compat snapshot 的 blocking-sites 面（见 P1-1）让新增 BLOCKED 位点必须在 diff 里被显式审阅。
- **生效口径**：契约自首个含本设计的发版起生效；本批（held-out/self-report 等新门禁）随同版本首次发布，无存量暴露（1.50.0 已发布版不含它们）。
- **验收**：docs lint 过；承诺表引用调研证据链。

## P1-2 逃生舱升级（豁免治理学落地）

- **设计**（三件）：
  1. **reason+owner 入行**：Entry.Meta 已有——新助手 `checklog.EscapeHatchEntry(gate, reason, owner, detail)` 构造行（Meta["reason"]/["owner"]）；taskpipeline 各 escape 记录点迁移（acceptance/docgate/selfreport/heldout/skill-decisions/pushgate）：per-task override → owner=TaskRef、reason="per-task override"；env → owner="env"、reason="env"。`forge task override` 加 `--reason <text>`（存 Overrides.Reason，escape 行携带——人给理由的通道）。**v1 落地注记**：`--reason` flag 未实现——reason 通道先以机制枚举值（override/env）落地，自由文本理由为后续项（对抗复审确认；承诺表 §二.3 已同步注记）。
  2. **unfulfilled-waiver 复查**（expect 语义 v1）：`forge eval dashboard` 新节"逃生舱库存"——按 gate 聚合 escape 行：①每 gate 总数+涉及任务数；②**永久化信号**（同一 gate 在 ≥3 个不同任务被豁免）；③**unfulfilled 候选**（某任务 escape 后，同任务后续存在该 gate 相关 check 的 pass 行且无新 escape——豁免可能已不需要）。命名诚实："候选"非判定（Rust expect 误报先例）。
  3. **库存进任务报告**：task complete 输出加一行"逃生舱使用：N 次（gate 清单）"。
- **验收**：单测——escape 行带 reason/owner；dashboard 聚合含永久化/unfulfilled 候选两个信号（构造夹具）；complete 打印。

## P1-1 forge compat snapshot/report（可执行兼容工件，API Extractor 模型）

- **设计**：新包 `internal/compat`：
  - `forge compat snapshot [--out]`：**确定性**快照（排序键、无时间戳）——六面：①命令树（walk rootCmd：每命令的 path+flags，docsconsistency 树已注册）；②CheckName roster（checklog 侧新增 `AllCheckNames()`——需枚举：检查 types.go 常量的收集方式，若无常量枚举器则用反射/列表——实现时以显式列表 + guard 对齐）；③逃生舱 roster（FORGE_* env 清单——显式列表+守卫）；④内嵌载荷（skills 两棵树 name+sha256——复用 inventory 逻辑轻量版）；⑤schema 形状（seeded TaskState/Entry/ToolCall 序列化后的键集合，不含值）；⑥blocking 位点（扫 internal/**/*.go 中 `GateBlocked(` 与 `LevelBlocked` 出现的 file:count——源扫描，确定性）。
  - `forge compat report --base <ref>`：`git show <ref>:compat.snapshot.json` vs 当前实算——逐面分类 added/removed/changed；**破坏性判级**：命令/flag/schema 键/CheckName/载荷项的 removed 或 changed → BLOCKED 级（exit 2，errHardExit 式哨兵）；added → warn 级（提示"新面，建议 README/承诺表同步"）；blocking-sites 增加 → warn 级（"新 BLOCKED 位点，须按文案契约附预告版本"）。输出每面附**检测边界**（诚实呈现：工件 diff 已知盲区按调研写明）。
  - golden：`compat.snapshot.json` 提交仓根，守卫测试 `TestCompatSnapshotMatchesGolden`（当前实算 == 入库 golden——任何面变更必须显式重生成，棘轮哲学）。
- **验收**：确定性测试（两次实算字节一致）；golden 守卫；report 对"删一个 CheckName/加一个 flag"的夹具快照能正确分级；exit code 契约（0/2）。

## P2-1 指纹分流 v1（声明 + 回读；GODEBUG 骨架的轻量版）

- **设计**：`forge config get/set compat <version>`（userconfig 增字段，缺省=空=跟最新）；`forge status` 增一行 compat 声明与落后 minor 数（util.CompareVersions）；**遥测回读**：dashboard 的逃生舱库存（P1-2）就是 escape 使用的回读面——本项再补 `forge config get compat` 输出联动提示（声明落后 >0 minor 时即提示（≥2 时加承诺窗警示——实现比初稿更保守））。"按版本给默认行为"的完整 GODEBUG 机制**明确不做**（写入承诺表"不做"档 + 设计注记：等门禁数量增长后再评估）。
- **验收**：config 往返；status 行；落后计算单测。

## P2-2 push 门禁生产者声明（门禁第五代：归因入证据）

- **设计**：RunPushGate 增 `Producers []string`——从本分支未完成任务的 TaskState（OriginTool/InitiatedBy 字段，实现时确认字段名）与该任务 checklog 行的 NodeID 去重聚合（"node:<id>"/"tool:<name>"）；写入推送证据快照 + gate-push checklog Detail 首段（"producers: …"）+ CLI 输出一行。OTel 侧已有 forge.check.node_id 属性族（天然对齐）。
- **验收**：单测——两任务不同 OriginTool/NodeID → 去重聚合入快照与 Detail。

## 执行顺序

P0-1 → P0-2 → P1-3（文档）→ P1-2 → P1-1（最大件，含 CheckName 枚举基建）→ P2-1 → P2-2 → 收尾（README、docsconsistency、全量测试、门禁、审查、合并）。
