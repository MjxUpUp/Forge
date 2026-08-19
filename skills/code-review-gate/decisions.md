# code-review-gate — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮
agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。
append-only：新决策追加到末尾。

> 用 `forge skills decide --skill code-review-gate ...` 追加新决策，勿手编。
> 下面是一条示例决策（基于真实历史 8e00456），展示四元组写法。

## [d-1778921400123456789-a1b2c3d4] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-07-18T09:30:00Z
- **By**: claude-code
- **Commit**: 8e00456

### Diagnosis

`forge review pass` 是声明式——只标记「审查通过」不实际执行审查。空 pass 的任务
（agent 没真派 code-reviewer 就 pass）会漏检；更隐蔽的是 pass 之后、task-complete
之前 agent 还能改代码，pass 的「通过」承诺与实际代码脱节。诊断线索：审查发现的
修复引入的副作用第一轮看不到（projectName 末两段拼接→Project 含测试名误触发
session 断言），说明「审查→修复」链路里修复阶段无人复审，pass 形同虚设。

### Revision

feat/review-snapshot（8e00456）：review pass 绑定代码快照——ReviewedHeadCommit +
ReviewedChangeHash 写进 TaskState。task-complete 门禁加硬前置 ReviewPassed，且比对
当前 HEAD == ReviewedHeadCommit：审查后改码即拒（drift 检测）。

### Evidence

回归探针：构造「pass 后改码」场景 → task-complete gate BLOCKED（HEAD 漂移），exit
非 0；「未改码」场景 → 放行。两条路径断言相反结果，覆盖 drift 检测分支（accept
的依据不是断言而是实跑的 BLOCKED/放行对比）。

### Rationale

pass 是 agent 自律最薄弱的环节（声明成本极低），绑定快照把「声明」锚到不可伪造
的 git 状态上，与 file-sentinel 同源思路（git diff = deterministic 证据）。drift
检测让「审查后偷改」从隐性行为变成硬阻断，比加更多审查 checklist 更治本。

## [d-18c771eb72121754-8d797f6a] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-07-31T18:03:53Z

### Diagnosis

整体审查发现 :126/:132/:233 三处裸方括号 [references/xxx.md] 缺链接目标，Markdown 渲染不成链接

### Revision

三处补齐 (references/xxx.md) 链接目标

### Evidence

改后 grep 'references/.*\.md\]' 无未带 ( 的残留

## [d-18c77baa46f23fa4-886a12ef] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-07-31T21:02:28Z

### Diagnosis

behavior-probe 维度全库仅本 skill 的 probes.yaml 一个消费方（约 500 行服务单点），决策拆除不推广

### Revision

删除 probes.yaml 资产；skillseval 的 probes.go/judgeBehavior/behaviorPassRate 通路、cli eval 命令的 probe 相关输出与脱敏机制同步拆除

### Evidence

audit 确认 53 个 skill 中唯一消费方；probe 字段不参与 caseID/DescHash，拆除对存量 case 集 hash 零影响

## [d-18c7e5a6381ad110-43d8bf25] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-02T05:24:39Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

建「改名/删符号后的调用方检查」canonical 节(D4)；forge 段落下沉 references/forge-integration.md；补叠加专项审查输出协议(block 以下也须显式回应)；86%/72% 统计补 Veracode 2025 来源；双语注释规范迁入(maintainability-and-readability 合并)

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e620ad442c3c-2679d4f1] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cd113414153e18-c97623dd] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-19T01:39:02Z

### Diagnosis

cheat-scan 从 4 类扩到 6 类（comment-as-debt 早已是第 5 类，文档滞后；phantom-import 本次新增）——skill 文档的预扫清单与实现漂移，子 agent 会重复判断已被机械判过的模式

### Revision

SKILL.md 与 references/forge-integration.md 的 cheat-scan 预扫节更新为 6 类枚举，注明 phantom-import 只覆盖相对路径、外部包仍归语义审查；plugins/forge 镜像同步

### Evidence

internal/taskpipeline/cheatscan.go ScanCheatPatterns 实跑 6 个检测器；TestDetectPhantomImport + TestExecuteTaskGate_CheatScan_PhantomImport 通过

## [d-18cd18075a8b76d0-63946ae4] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-19T03:44:06Z

### Diagnosis

cheat-scan 扩到 7 类（新增 path-assumption：OS 分隔符当内容匹配器——2026-08-19 Windows CI 事故的指纹），文档预扫清单需同步

### Revision

SKILL.md + forge-integration.md 预扫节更新为 7 类枚举

### Evidence

internal/taskpipeline/cheatscan.go detectPathAssumption + TestDetectPathAssumption 通过
