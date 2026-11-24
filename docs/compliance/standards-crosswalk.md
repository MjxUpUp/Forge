# Forge 能力 × 新兴标准对照表（standards crosswalk）

用途：把 Forge 现有能力映射到 2025-2026 涌现的 agent 治理标准与合规框架——企业采购问卷、SOC 2 审计、信通院评估应答的起点。口径是「映射到什么程度」而非「天然合规」；凡映射处给出**现存的**命令或工件作为证据指针。

依据：发展方向调研（2026-09-05，dive_04 标准与合规卡位）。标准状态快照：IETF AAT -02 个人草案；OTel GenAI semconv 无 stable；OWASP ACS v0.1；AST10 v1.0 2026 版；EU AI Act 高风险义务延期至 2027-12 且 coding agent 非高风险类目；信通院评估是国内投标事实门槛。

## 零、3.0 之后的证据面（先看这段，避免按旧导出器回答）

core-only 批次把评测与导出体系整体去掉了：`forge eval` 命令组、`forge skills`、`forge doctor` 均已删除，导出包 `internal/aatout`、`internal/otelout` 也已删除。因此本表**不再承诺** OTel/AAT 形状的机器导出；继续成立的证据是：

- `checklog.jsonl`（append-only，行内含 ed25519 签名字段与 `forge_version`）——审计链的原始事实源；
- `spec/changes/<id>/evidence/<UTC>/`——`forge change verify` 封印的判据实跑记录（stdout/stderr/exit + summary.json）；
- `forge review pass` 的评审快照绑定（`review_rounds[].change_hash` / `head_commit`）——评审时点的代码快照；
- `forge test ledger`——测试四态与 Skip 审批（`spec/test-skips.yaml`）；
- `forge compat snapshot`——七面兼容盘点（命令/检查/逃生舱/载荷/schema/阻断位点/桥）。

## 一、OWASP Agentic Skills Top 10（AST01-AST10）

OWASP 对 skill 供应链的缓解措施钦定了 ed25519 签名、skill inventories、hash pinning。Forge 当前只保留其中与「仓库内证据」相关的部分：

| AST 条目 | 缓解要求（OWASP 口径） | Forge 对应（3.0） | 满足度 |
|---|---|---|---|
| AST01 Malicious Skills | 签名与来源验证 | checklog 行的 ed25519 签名字段（`sig`/`key_id`），校验入口是导入侧验签（`bundle-verify` 检查） | 部分（SKILL.md 本体签名不在本仓库范围） |
| AST02 Supply Chain | 安装源验证 | 分发面由 `TestNpmPlatformVersionsAligned` 守卫 wrapper 与平台子包对齐 | 部分（原 `forge doctor` 漂移检测已删除） |
| AST03 Over-Privileged | 权限最小化 | 无宿主接线：forge 不注入 hook、不持有宿主权限，只在命令内检查 | 部分（机制已随宿主层移除） |
| AST05 Untrusted Instructions | 不可信指令隔离 | task 门禁的 skill-decisions 检查（改 SKILL.md 必须记决策） | 部分 |
| AST07 Update Drift | immutable pinning / hash verification | `skills/` 与 `skills-forge/` 内容目录由 `TestSkillsContentDirs` 钉住结构（SKILL.md、name、description）；无 inventory 命令 | 部分（原 `skills inventory --lock/--verify` 已删除） |
| AST08 Poor Scanning | 发布/安装期扫描 | `go test ./...` 中的安全规则测试已随 `internal/skillsqa` 删除 | 未覆盖（如实记录） |
| AST09 No Governance | skill inventories / audit logging | checklog 审计链 + `spec/changes/*/decisions` 决策留痕 | 机制落地（审计面）；无 skill 清单命令 |
| AST10 Cross-Platform Reuse | 跨平台一致性 | 单源只读内容目录（`skills/`、`skills-forge/`），不分发到宿主 | 部分 |

## 二、OWASP Agent Control Standard（ACS v0.1）

三支柱 Inspectable / Traceable / Instrumentable 与 Forge 的对应：checklog 是**可检查、可追溯**的事实层（JSONL、append-only、逐行带版本与签名），`forge change verify` 的证据目录是**可复核**的判据实跑记录。事件通道不再提供 OTel 导出（原 `forge eval otel` 已删除，core-only 批次）——消费方直接读 checklog JSONL；需要 OTLP 形状时由消费方自行映射。

## 三、IETF draft-sharif-agent-audit-trail（-02）

原 `forge eval aat` 导出器（`internal/aatout`）已删除（core-only 批次），**当前不产 AAT 形状 JSONL**。可用的等价物是 checklog 原始行（`seq`/`ts_hlc`/`sig`/`task_ref` 等字段已满足「可排序、可验签、可归属」的最低要求）；需要 AAT 形状时按草案字段从 checklog 直接映射，映射器需自建。

## 四、OpenTelemetry GenAI semconv

原 `forge eval otel` 导出器（`internal/otelout`）已删除，**当前不产 OTLP/JSON**。semconv 至今无门禁裁决语义，Forge 也不伪称 `gen_ai.*`；需要遥测时以 checklog 为源自行转换。

## 五、NIST CAISI 评估作弊示例库（叙事背书）

Forge 的证据强度分级（deterministic > agent-claim）与 CAISI「agent 评测可被作弊绕过、需要 transcript 取证」的结论同构：**cheat-scan / held-out gap / self-report 一致性**三件套仍是 task 门禁的一部分（`internal/taskpipeline/cheatscan.go`、`heldout`、`self_report`），这正是该叙事的产品化。对外引用 NIST 叙事时锚定其官方示例库：https://www.nist.gov/caisi/cheating-ai-agent-evaluations/2-examples-cheating-caisis-agent-evaluations

## 六、信通院「可信AI智能编码工具」评估（16 能力项）

评估对象是编码工具而非治理层——Forge 定位是**厂商侧评估佐证**（「可信放大器」）：审计链（checklog 签名 + 版本字段）、门禁证据分级、变更全链路留痕映射「应用成熟度/安全」维度的材料。对应关系按能力项逐条：操作可追溯 ↔ toollog / checklog / `forge task report`；交付可复核 ↔ `forge change verify` 证据目录；测试卫生 ↔ `forge test ledger`。**原映射里的 hazard-guard / file-sentinel / bash-guard 已随宿主层与 core-only 移除**，代码安全维度不再由本仓库提供证据。注：具体能力项编号以评估现行文件为准，本表不预设编号对应。

## 七、使用方式（采购/审计应答）

1. `forge change verify <id>`——交出 `spec/changes/<id>/evidence/<UTC>/` 目录（判据、命令、退出码、时间戳）；
2. `forge test ledger` + `forge test record`——测试现状与实跑记录（skipped 必须带理由与审批）；
3. `forge docs closure --strict`——命令面与活文档一致性的可复核快照；
4. `forge compat snapshot`——七面兼容盘点，作为「能力面只增不删」的审计锚点。

红线（诚实呈现）：全部标准处于早期——本表是「映射与卡位」而非「合规认证」；企业应答话术用「取证/降险/问卷通关」，不用「强制/认证」（EU 延期与 coding agent 非高风险类目已核实）。**能力已移除的行如实写「已删除」，不拿旧导出器充证据。**
