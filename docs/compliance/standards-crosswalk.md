# Forge 能力 × 新兴标准对照表（standards crosswalk）

用途：把 Forge 现有能力映射到 2025-2026 涌现的 agent 治理标准与合规框架——企业采购问卷、SOC 2 审计、信通院评估应答的起点。口径是"映射到什么程度"而非"天然合规"（凡映射处给出 Forge 命令/工件作为证据指针）。

依据：发展方向调研（2026-09-05，dive_04 标准与合规卡位）。标准状态快照：IETF AAT -02 个人草案；OTel GenAI semconv 无 stable；OWASP ACS v0.1；AST10 v1.0 2026 版；EU AI Act 高风险义务延期至 2027-12 且 coding agent 非高风险类目；信通院评估是国内投标事实门槛。

## 一、OWASP Agentic Skills Top 10（AST01-AST10）

OWASP 对 skill 供应链的缓解措施钦定了 ed25519 签名、skill inventories、hash pinning——Forge 原语与之一致：

| AST 条目 | 缓解要求（OWASP 口径） | Forge 对应 | 满足度 |
|---|---|---|---|
| AST01 Malicious Skills | 签名与来源验证 | checklog 行 ed25519 签名 + 验签四态（`forge eval audit-verify`） | 部分（SKILL.md 本体签名待 skill 分发链建设） |
| AST02 Supply Chain | 安装源验证 | `forge doctor` 分发漂移检测（canonical vs 各宿主目标） | 部分 |
| AST03 Over-Privileged | 权限最小化 | hostcap 能力注册表 + 22 hook 只读/拦截分层 | 部分 |
| AST05 Untrusted Instructions | 不可信指令隔离 | conventions 指纹过期检测（注入内容可验新旧） | 部分 |
| AST07 Update Drift | "immutable pinning, hash verification" | `forge skills inventory --lock/--verify`（SKILL.md sha256 钉基线，漂移 exit 2） | **机制落地** |
| AST08 Poor Scanning | 发布/安装期扫描 | `forge skills audit`（21 条安全规则）+ skillsqa | 机制落地 |
| AST09 No Governance | "skill inventories, audit logging" | `forge skills inventory` + checklog 审计链 + decisions.md 决策留痕 | **机制落地** |
| AST10 Cross-Platform Reuse | 跨平台一致性 | 双树分发 + drift-check（canonical 单源多目标） | 部分 |

## 二、OWASP Agent Control Standard（ACS v0.1）

三支柱 Inspectable/Traceable/Instrumentable 与 Forge 架构对应：Forge 本体即 ACS 的 "Guardian"参考实现形态（叠在 12 宿主上的中间层）。事件通道：checklog → OTel（`forge eval otel`，ACS 事件用 OTel 追踪——与 Forge 导出器同通道）；AAT 形状导出（`forge eval aat`）覆盖 OCSF 之外的 JSONL 消费方。v1 计划的 ACS→OTel/OCSF mapper 落地后可补 OCSF 侧。

## 三、IETF draft-sharif-agent-audit-trail（-02）

`forge eval aat [--out] [--limit]`：checklog → AAT 形状 JSONL（链式 prev_hash、record_id、trust_level L0-L3、outcome/escalation 语义）。四项有意偏离在每份导出 meta 头声明（确定性 UUID、JCS 近似、ed25519 直通、无 TSA）——versioned mapper，-03 演进时递增 mapper_version 重写。IPR 提示：草案有专利披露，本导出器保持可摘除（独立包 internal/aatout，不改内核存储）。

## 四、OpenTelemetry GenAI semconv

`forge eval otel`：checklog → OTLP/JSON（resource→scope→span→event，forge.* 命名空间占位——semconv 尚无门禁裁决语义，不伪称 gen_ai.*）。Datadog v1.37+ 原生 ingest 通道已打通（导出格式即消费格式）。

## 五、NIST CAISI 评估作弊示例库（叙事背书）

Forge 的证据强度分级（deterministic > agent-claim）与 CAISI "agent 评测可被作弊绕过、需要 transcript 取证"结论同构：cheat-scan / held-out gap / self-report 一致性三件套正是该叙事的产品化。对外文档引用 NIST 叙事时锚定其官方示例库 URL。

## 六、信通院「可信AI智能编码工具」评估（16 能力项）

评估对象是编码工具而非治理层——Forge 定位是**厂商侧评估佐证**（"可信放大器"）：审计链（checklog 签名+验签）、门禁证据分级、全链路操作留痕映射"应用成熟度/安全"维度的材料。对应关系按能力项逐条：代码安全维度（通用/供应链漏洞自动修复、机密防泄漏）↔ hazard-guard/file-sentinel/bash-guard；操作可追溯 ↔ toollog/checklog/trace。注：具体能力项编号以评估现行文件为准，本表不预设编号对应。

## 七、使用方式（采购/审计应答）

1. `forge eval otel --out otelp.json` + `forge eval aat --out aat.jsonl`——交给审计方的两份机器可读证据包；
2. `forge skills inventory --verify`（CI 常驻）——AST10 pinning 应答；
3. `forge eval dashboard --json`——治理遥测快照（escape 率/自举通过率）；
4. 本表 + gates-card（`forge eval card --render`）——治理面声明的两份文本锚点。

红线（诚实呈现）：全部标准处于早期——本表是"映射与卡位"而非"合规认证"；企业应答话术用"取证/降险/问卷通关"，不用"强制/认证"（EU 延期与 coding agent 非高风险类目已核实）。
