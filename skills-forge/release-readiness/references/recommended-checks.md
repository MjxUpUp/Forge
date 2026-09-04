# Recommended 建议清单（R1-R5 完整版）

未过项进发布说明"已知风险"段，明确降级/缓解策略。每项格式同 Mandatory：检查什么 + 怎么查 + 不通过怎么办。

## R1. 回滚预案存在且演练过

- **检查什么**：发布失败时如何回退到上一版本——回滚步骤明确、上一版本 tag/镜像保留、数据回滚路径可行。
- **怎么查**：
  ```bash
  test -f docs/rollback.md || test -f RUNBOOK.md
  # 上一版本 tag/镜像仍在
  git tag -l | grep "$(git describe --tags --abbrev=0 HEAD~ 2>/dev/null | sed 's/v//')"
  docker pull <image>:<PREV_VER>   # 镜像类
  # 演练：staging 上来回滚一次（与 M4 回滚迁移配合）
  ```
- **不通过怎么办**：补 `RUNBOOK.md` 含具体回滚命令；上一版本不可达 → 立即补建保留策略（N-1 必留）；未演练 → 至少在 staging 走一次回滚流程。

## R2. 已知问题与降级策略

- **检查什么**：本版本已知 bug/限制列在发布说明；高风险变更有 feature flag 可关；降级路径在 RUNBOOK 标注。
- **怎么查**：
  ```bash
  grep -iE 'known issues|已知问题|limitations|限制' CHANGELOG.md docs/release-notes-*.md
  grep -rnE 'feature.?flag|FEATURE_FLAG|FF_' src/ lib/ app/   # 关键开关存在
  ```
- **不通过怎么办**：补"Known Issues"段；高风险变更无 flag → 评估补 flag 的成本 vs 不发版的成本（破坏性变更强烈建议补 flag）。

## R3. 发布后观测（指标 / 告警 / 日志保留）

- **检查什么**：发布后关键指标有仪表盘、告警阈值已设、日志保留期覆盖回滚调查窗口。
- **怎么查**：
  ```bash
  # 仪表盘/告警存在
  curl -sf 'https://grafana.example.com/api/dashboards/uid/<release-overview>'
  curl -sf 'https://alertmanager.example.com/api/v2/alerts' | grep '<service>-error-rate'

  # 日志保留期 ≥ 7 天（足够发版后 24-48h 调查 + buffer）
  # 按日志栈查保留策略（ELK/CloudWatch/Loki）
  ```
- **不通过怎么办**：无仪表盘 → 发布前建一个最小可观测盘（错误率/p95/部署标记）；告警未设 → 至少设错误率告警；日志保留不足 → 调长。

## R4. 通知与公告

- **检查什么**：用户/干系人知道这次发版——发版窗口通知、breaking change 公告、文档站更新。
- **怎么查**：
  ```bash
  # Release notes 草稿存在
  test -f docs/release-notes-<NEW_VER>.md
  # breaking change 是否提前公告（邮件/Slack/issue）
  # 文档站是否随发布更新（CI 部署 docs 站）
  ```
- **不通过怎么办**：补 Release Notes；breaking change 未提前告知 → 推迟一个版本公告后再发。

## R5. 灰度计划（高频发布/重大变更）

- **检查什么**：非 100% 一次性放出，有灰度策略（1% → 10% → 50% → 100%）和每档的回退判断点。
- **怎么查**：
  ```bash
  grep -iE 'canary|灰度|rolling.out|ramp' docs/deploy.md RUNBOOK.md
  # 灰度配置实际存在（feature flag service / load balancer rule）
  ```
- **不通过怎么办**：补灰度计划；无法灰度的服务（如 CLI 二进制）→ 至少做内部 dogfood 一周。

## R6. 质量门禁基线回归（Forge 项目适用）

- **检查什么**：本次发布未让门禁质量回归——golden 标注集 precision/fpr 基线全绿，审计时间线无伪造行。
- **怎么查**：
  ```bash
  # 在 Forge 仓库根（golden 资产 evals/forge/golden/）：
  forge eval golden run        # precision/fpr/确定性重放；missed 或 false_positive 即回归
  forge eval audit-verify      # 伪造审计行 >0 会 BLOCKED（exit 2）
  forge status                 # "自评测" 行快速看最近基线摘要与告警
  ```
- **判定**：golden run 无 missed/false_positive finding 且 findings 为空；audit-verify 退出 0。
- **不通过怎么办**：missed/false_positive 说明门禁行为变了——先修门禁（或确属契约变更则轮换 golden 并 `--rewrite-manifest` 显式钉新指纹），不带着回归发版；audit-verify 非零按其 BLOCKED 指引溯源。非 Forge 仓库（无 eval 资产）跳过本项并在发布说明记录"不适用"。
