---
name: release-readiness
description: "发布/上线前的 readiness 门禁清单（能安全上线吗）。Use when: 准备发布/上线/打 tag/发版/灰度时、用户问'能上线了吗'/'可以发布了吗'/'发布前检查'/'上线前 check 一遍'时、gate-8 发布门禁产出 checklist.md 时、版本号已 bump 准备 tag 时、Release 截止前的 go/no-go 评审时。SKIP: 项目功能验收/PRD 对比/完整度审查（用 project-acceptance）、文档 vs 代码一致性专项（用 docs-consistency-guard）、单次 diff 代码审查（用 code-review-gate）、编码任务交付纪律（用 implementation-discipline）、运行时 bug 排查（用 systematic-debugging）。"
metadata:
  pattern: gate
  domain: release-engineering
  composes: docs-consistency-guard
---

# 发布 Readiness 门禁

发布/上线前的强制 go/no-go 清单。"代码写完了"（project-acceptance 管的）不等于"能安全上线"（本 skill 管的）——发布是**不可逆动作**（tag 推上去、镜像发出去、迁移跑了），门禁必须在按下按钮前逐项过完。

## 核心原则

- **门控而非建议**：mandatory 项任一未过 = 🚫 NO-GO，没有"差不多能上"。recommended 项未过需在发布说明里显式记录风险，不能默默跳过。
- **不可逆动作前是成本最低的拦截点**：tag/镜像/迁移一旦发出，回滚成本 >> 发布前查 30 分钟。同 code-review-gate 的逻辑，发布门比 commit 门更硬。
- **每项配可执行命令 + 量化通过标准**：不靠"应该没问题"打勾，凭印象发版是事故第一来源。防注水（见 §防注水自检）。
- **前序门禁必须先过**：本 skill 不重审代码质量、不重做 PRD 验收——它们各自有专门 skill。本 skill 只管"上线这条线"特有的风险（版本/迁移/回滚/观测/secrets）。
- **独立上下文审查**：派只读子 agent 过清单，主 agent 刚写完代码有"它能跑"的确认偏误。同 code-review-gate 的子 agent 化逻辑。

## When to Use / When NOT to Use

**Use when:**
- 用户说"准备发布" / "上线" / "打 tag" / "发版" / "灰度" / "能上线了吗" / "发布前 check 一遍" / "go/no-go"
- forge-pipeline 走到 gate-8-release，需要产出 `checklist.md`
- 版本号已 bump，准备推 tag 或发 Release
- 即将执行不可逆动作（生产迁移、镜像 push、DNS 切换）

**SKIP（路由到更专业的 skill）:**
- **PRD 功能覆盖度 / 设计方案对比** → `project-acceptance`（管"功能做完了没"）
- **文档 vs 代码漂移专项守卫** → `docs-consistency-guard`（本 skill 第 6 项委托给它）
- **单次 diff 代码质量审查** → `code-review-gate`
- **编码期交付纪律**（先读再改/测试伴随/聚焦变更） → `implementation-discipline`
- **运行时 bug 根因排查** → `systematic-debugging`
- **API/库签名查证** → `dev-lookup`

## 流程

1. **确认范围**：发什么（CLI / 服务 / SDK / 镜像）？发到哪（npm / Docker Hub / 生产 K8s / 应用商店）？谁来决策 go/no-go？范围未定 → 先问，不要猜。
2. **过 mandatory 清单**（下文 §Mandatory 硬门禁）：每项跑命令、记结果。任一未过 → 直接 NO-GO，列出阻断项。
3. **过 recommended 清单**（下文 §Recommended 建议）：未过项进发布说明的"已知风险"段，明确降级/缓解策略。
4. **决策**：按 §决策树 给 go / no-go / go-with-risk 三档结论。
5. **产出 checklist.md**：清单 + 命令输出 + 决策结论，作为 gate-8 产出物归档。

## Mandatory 硬门禁（任一未过 = NO-GO）

每项格式：**检查什么** + **怎么查（命令）** + **通过标准** + **不通过怎么办**。

### M1. 版本号一致性（代码 / 包文件 / tag / changelog 头）

- **检查什么**：本次发布版本号在所有出处一致——源码常量、`package.json`/`Cargo.toml`/`go.mod`、CHANGELOG 头、即将打的 git tag。
- **怎么查**：
  ```bash
  grep -nE 'version\s*[:=]\s*"?<NEW_VER>' package.json Cargo.toml go.mod pyproject.toml
  grep -nE '(Version|VERSION)\s*=\s*"<NEW_VER>"' $(grep -rl 'Version\s*=' --include='*.go' --include='*.rs' .)
  git tag -l | grep -E '^v?<NEW_VER>$'          # 期望空（未推）；推完后期望唯一
  head -20 CHANGELOG.md | grep -E '^##\s+\[?<NEW_VER>\]?'
  ```
- **通过标准**：所有源出处 = `<NEW_VER>`；CHANGELOG 有对应章节；tag 推前不存在、推后唯一。
- **不通过怎么办**：补齐缺的出处（漏改源码常量是高频事故）；CHANGELOG 缺章节先补再发；tag 已存在 → 删除远程/本地 tag 重打（**绝不可复用已 push 的 tag**，见 Gotchas）。

### M2. CHANGELOG 更新且破坏性变更标注

- **检查什么**：CHANGELOG 已更新到 `<NEW_VER>`；Breaking Changes 显式标注；迁移步骤写在用户能看到的位置（不只是 commit 里）。
- **怎么查**：
  ```bash
  # CHANGELOG 存在且含本次版本
  test -f CHANGELOG.md && head -50 CHANGELOG.md | grep -E '^##\s+\[?<NEW_VER>\]?'
  # 破坏性变更标注（Keep a Changelog 风格 ### Breaking / ⚠ 或类似）
  awk '/^##\s+\[?<NEW_VER>\>?/{flag=1} /^##\s+\[?v?[0-9]/{if(flag&&!seen)exit} flag' CHANGELOG.md | grep -iE 'breaking|破坏|不兼容|迁移'
  # 自上次发布后的 commit 是否都有归属（无孤儿 commit）
  git log --oneline $(git describe --tags --abbrev=0 2>/dev/null)..HEAD | wc -l
  ```
- **通过标准**：CHANGELOG 含 `<NEW_VER>` 章节；若有破坏性变更，章节内显式标 `Breaking`/`破坏性`/`不兼容` 字样并写迁移步骤；自上次 tag 以来的实质 commit 都被 CHANGELOG 覆盖。
- **不通过怎么办**：补 CHANGELOG；破坏性变更未标 → **必须补**（用户踩坑就是你背锅）；commit 未覆盖 → 评估是漏写还是 commit 该 revert。

### M3. 构建产物校验（能产 / 可运行 / 体积合理 / 已签名）

- **检查什么**：发布产物在干净环境能重新产、能启动、体积无明显膨胀、按要求签名/校验和。
- **怎么查**：
  ```bash
  # 干净重建
  rm -rf dist/ target/ build/ out/ 2>/dev/null
  <build-cmd>                                    # npm run build / cargo build --release / go build ./...
  test $? -eq 0                                  # 期望 0

  # 烟测：产物能跑（CLI 跑 --version，服务起 --port）
  ./<binary-or-entry> --version 2>&1 | grep -E '<NEW_VER>'
  # 服务类：./server & ; sleep 2 ; curl -sf http://127.0.0.1:PORT/health ; kill %1

  # 体积对比上次发布（膨胀 >30% 要解释）。跨平台用 wc -c（Linux/macOS/Windows Git Bash 通用）；
  # 勿用 stat -c%s（仅 GNU）/ stat -f%z（仅 BSD/macOS）——平台不匹配时 fallback echo 0 会假通过
  PREV_SIZE=$(wc -c < "$(git describe --tags --abbrev=0 2>/dev/null | xargs -I{} echo dist/{}-artifact)" 2>/dev/null | tr -d ' ')
  PREV_SIZE=${PREV_SIZE:-0}     # wc 读取失败时为空；|| echo 0 不生效（tr 恒 exit 0），改用参数默认值兜底
  CURR_SIZE=$(wc -c < dist/artifact 2>/dev/null | tr -d ' ')
  CURR_SIZE=${CURR_SIZE:-0}
  echo "prev=$PREV_SIZE curr=$CURR_SIZE"

  # 签名/校验和（npm publish 自动 / Docker push 需 cosign / Release 资产 sha256sum）
  sha256sum dist/* > checksums.txt
  # cosign verify --key <key> <image>:<tag>    # 镜像类
  ```
- **通过标准**：构建 exit 0、产物 `--version` 报 `<NEW_VER>`、体积膨胀 <30%（或已解释）、签名/校验和文件存在。
- **不通过怎么办**：构建失败先修；`--version` 不对 → 版本号没贯穿到产物（回 M1）；体积暴涨 → 排查是否引入大依赖（`npm ls`/`cargo tree`/`go mod why`）；未签名 → 补签名流程再发。

### M4. 数据库迁移：前向 + 回滚均存在并演练过

- **检查什么**：本版本涉及的 DB schema 变更，前向迁移和回滚迁移都存在、在 staging 跑过、回滚路径已验证。
- **怎么查**：
  ```bash
  # 迁移文件存在
  ls migrations/ | tail -10
  # 前向 + 回滚成对（按工具：prisma migrate revert / goose down / knex rollback / alembic downgrade）
  grep -lE 'up|down|forward|rollback|revert' migrations/*${NEW_VER}*

  # 在 staging 实跑：前向 → 验证 → 回滚 → 验证
  <migrate-up-cmd> --env staging && <verify-cmd>
  <migrate-down-cmd> --env staging && <verify-cmd>

  # 破坏性 SQL 扫描（DROP/TRUNCATE/无 WHERE DELETE）——code-review-gate 的 sql-safety 同源
  grep -rnE 'DROP\s+(TABLE|DATABASE)|TRUNCATE|DELETE\s+FROM\s+\w+\s*;|GRANT\s+ALL' migrations/
  ```
- **通过标准**：成对的 up/down 文件存在；staging 跑过前向+回滚两次都成功；破坏性 SQL 每条都被审过（不是"看起来 OK"，是有书面评审记录或被双签）。
- **不通过怎么办**：缺回滚迁移 → **必补**（"前向可逆"是幻觉，DROP 了就没了）；staging 没跑过 → 必跑；破坏性 SQL 未审 → 暂停发版，补评审。

### M5. 配置 / Secrets：无硬编码、env 有默认值与文档

- **检查什么**：本次发布的代码无新增硬编码 secret；新增配置项有默认值或 fail-fast；env 变量在 README/.env.example/部署文档有记录。
- **怎么查**：
  ```bash
  # 硬编码 secret 扫描（同 project-acceptance 维度 3）
  grep -rnE '(sk-[a-zA-Z0-9]{20,}|ghp_[a-zA-Z0-9]{36}|AKIA[A-Z0-9]{16}|-----BEGIN.*PRIVATE KEY-----)' \
      --include='*.go' --include='*.ts' --include='*.js' --include='*.py' --include='*.rs' --include='*.java' \
      --exclude-dir=vendor --exclude-dir=node_modules .

  # 新增 env 变量在 .env.example / 部署文档同步
  NEW_ENVS=$(grep -rnoE 'os\.(Getenv|Environ)|process\.env\.[A-Z_]+|std::env::var\(' src/ lib/ app/ | \
      grep -oE '"[A-Z_]+"' | sort -u)
  for e in $NEW_ENVS; do
      grep -q "$e" .env.example README.md docs/deploy.md 2>/dev/null || echo "MISSING: $e"
  done

  # 配置项有默认值或显式 fail-fast（不是 nil 解引用/空字符串崩）
  grep -nE 'config\.[A-Z][a-zA-Z]+\s*\|\|\s*("|"' "'"')' src/ lib/ app/   # 占位空字符串模式 → 警告点
  ```
- **通过标准**：无硬编码 secret 命中；新增 env 在 `.env.example` 和部署文档都有；新增配置项要么有合理默认值要么有显式 `if v == "" { fatal(...) }`。
- **不通过怎么办**：硬编码 secret → **立即阻断**（即使是要"以后改"也不能发，撤下用 env 替换）；env 文档缺 → 补；无默认值且不 fail-fast → 补一个或明确必填。

### M6. 文档一致性（composes docs-consistency-guard）

- **检查什么**：本次发布涉及的衍生文档（README 命令表/hook 表/配置表/feature 列表/API 示例/平台支持表/版本徽章）与代码真相源一致。
- **委托执行**：**REQUIRED: 用 docs-consistency-guard 流程逐项核对**。本 skill 不重复其检查表，只消费结论。
- **怎么查（最小自检）**：
  ```bash
  # 跑既有守卫测试（docs-consistency-guard 建的）
  <test-cmd> -- --grep "Readme_|Docs_|Consistency"
  # 无守卫测试时的兜底：逐表核对（参考 docs-consistency-guard §配对参考表）
  ```
- **通过标准**：docs-consistency-guard 建的守卫测试全绿（= ✅ GO）。**无守卫测试、仅人工核对签字 ≠ GO**——"人工校验/肉眼对比"正是 §防注水自检 点名的弱校验措辞，必须降级为 ⚠ GO-WITH-RISK：在发布说明"已知风险"段注明"文档一致性无自动化守卫，人工核对 N 表"，并要求下一次发布前建立 docs-consistency-guard 守卫，不能长期依赖人工签字。
- **不通过怎么办**：守卫测试红 → 补文档到一致；手核对发现漂移 → 补文档并建议后续建守卫测试（防止下次再漂）。**多包/多 README 项目每份副本都要核**（Forge 踩过根 README 对、npm/README 滞后）。

### M7. Smoke Check（关键路径真跑一遍）

- **检查什么**：在 staging 或 production-like 环境真跑一次完整用户路径，不是单元测试绿，是端到端用户能完成核心动作。
- **怎么查**：
  ```bash
  # 部署到 staging
  <deploy-staging-cmd> --tag <NEW_VER>

  # 关键路径逐条验证（按项目 README "快速开始" 段）
  # CLI: 装上 → init → 主要命令 → 看到预期输出
  curl -sf https://staging.example.com/health | grep -E '"status"\s*:\s*"ok"'
  ./<binary> <primary-command> --input fixture | grep -E '<expected-output>'

  # 灰度 1% 流量 5 分钟，错误率/延迟正常
  curl -sf 'https://grafana.example.com/api/datasources/proxy/api/v1/query?query=rate(http_errors_total[5m])'
  ```
- **通过标准**：staging 部署成功；关键路径全部跑通（非 200/非 0 exit code = 不过）；灰度窗口内错误率与基线持平或下降。
- **不通过怎么办**：staging 部署失败 → 修部署脚本或产物；关键路径任意一步失败 → 修代码或回退；灰度错误率飙升 → 立即回滚（按 R3）。

## Recommended 建议（未过项进发布说明"已知风险"段）

### R1. 回滚预案存在且演练过

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

### R2. 已知问题与降级策略

- **检查什么**：本版本已知 bug/限制列在发布说明；高风险变更有 feature flag 可关；降级路径在 RUNBOOK 标注。
- **怎么查**：
  ```bash
  grep -iE 'known issues|已知问题|limitations|限制' CHANGELOG.md docs/release-notes-*.md
  grep -rnE 'feature.?flag|FEATURE_FLAG|FF_' src/ lib/ app/   # 关键开关存在
  ```
- **不通过怎么办**：补"Known Issues"段；高风险变更无 flag → 评估补 flag 的成本 vs 不发版的成本（破坏性变更强烈建议补 flag）。

### R3. 发布后观测（指标 / 告警 / 日志保留）

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

### R4. 通知与公告

- **检查什么**：用户/干系人知道这次发版——发版窗口通知、breaking change 公告、文档站更新。
- **怎么查**：
  ```bash
  # Release notes 草稿存在
  test -f docs/release-notes-<NEW_VER>.md
  # breaking change 是否提前公告（邮件/Slack/issue）
  # 文档站是否随发布更新（CI 部署 docs 站）
  ```
- **不通过怎么办**：补 Release Notes；breaking change 未提前告知 → 推迟一个版本公告后再发。

### R5. 灰度计划（高频发布/重大变更）

- **检查什么**：非 100% 一次性放出，有灰度策略（1% → 10% → 50% → 100%）和每档的回退判断点。
- **怎么查**：
  ```bash
  grep -iE 'canary|灰度|rolling.out|ramp' docs/deploy.md RUNBOOK.md
  # 灰度配置实际存在（feature flag service / load balancer rule）
  ```
- **不通过怎么办**：补灰度计划；无法灰度的服务（如 CLI 二进制）→ 至少做内部 dogfood 一周。

## 决策树：什么算 Ready 可放行

```
Start
  │
  ├── 任一 Mandatory（M1-M7）未过？
  │     └── YES → 🚫 NO-GO. 列出阻断项 + 不通过怎么办的方案，回到修复。
  │              禁止"差不多能发先发了再说"。
  │
  ├── M6 仅人工核对通过（无 docs-consistency-guard 守卫）？
  │     └── YES → ⚠ GO-WITH-RISK（强制，不能升 GO）。
  │              即使其他 M 全过、R 全过——M6 无自动化守卫 = 文档一致性是弱校验（§防注水自检 点名的反模式）。
  │              "已知风险"段注明"M6 无自动化守卫，人工核对 N 表"，并要求下次发布前建立守卫，不能长期依赖人工。
  │
  ├── 全部 Mandatory 过（M6 经守卫测试绿）+ 全部 Recommended 过？
  │     └── YES → ✅ GO. 产出 checklist.md，按发布流程执行。
  │
  └── 全部 Mandatory 过（M6 经守卫测试绿）+ 部分 Recommended 未过？
        │
        ├── 未过的 Recommended 项影响范围可控（用户无感知 / 有降级路径）？
        │     └── YES → ⚠ GO-WITH-RISK.
        │              在发布说明"已知风险"段逐项记录：
        │              - 未过项 / 影响范围 / 降级策略 / 触发回滚的条件
        │              并取得干系人 explicit ack（不是默认同意）。
        │
        └── 未过的 Recommended 项影响用户数据/安全/收入？
              └── YES → 🚫 NO-GO. 升级为阻断（Recommended 不等于可忽略）。
```

**禁止模糊结论**：不说"差不多能发""应该没问题""先发了看看"。给明确的 GO / NO-GO / GO-WITH-RISK 三档之一，附阻断项或风险项。

## checklist.md 产出格式

```markdown
# Release Readiness: <project> <NEW_VER>

**决策结论**：✅ GO / ⚠ GO-WITH-RISK / 🚫 NO-GO
**发版窗口**：YYYY-MM-DD HH:MM ~ HH:MM (TZ)
**决策人**：<name>

## Mandatory（必须全绿才 GO）
| 项 | 命令 | 结果 | 状态 |
|---|---|---|---|
| M1 版本号一致性 | `grep ...` | 4/4 出处一致 | ✅ |
| M2 CHANGELOG | `head -20 ...` | 含 [X.Y.Z] + Breaking 段 | ✅ |
| M3 构建产物 | `npm run build` | exit 0, 1.2MB (+5%) | ✅ |
| M4 迁移 + 回滚 | staging up+down | 双向 OK | ✅ |
| M5 secrets | grep sk-/ghp_ | 0 命中 | ✅ |
| M6 文档一致性 | docs-consistency-guard | 守卫全绿 | ✅ |
| M7 smoke | staging e2e | 关键路径 8/8 | ✅ |

## Recommended
| 项 | 状态 | 说明 |
|---|---|---|
| R1 回滚预案 | ✅ | RUNBOOK.md 已演练 |
| R2 已知问题 | ⚠ | issue #123 未修，发布说明已列 |
| R3 观测 | ✅ | 仪表盘 + 错误率告警 |
| R4 通知 | ✅ | 邮件已发 |
| R5 灰度 | ⚠ | CLI 二进制无法灰度，已内部 dogfood |

## 已知风险（GO-WITH-RISK 时必填）
- R2: issue #123 ... — 影响 ... — 降级策略 ... — 回滚触发条件 ...

## 签字
- 发布人：<name>
- 干系人 ack：<name>（GO-WITH-RISK 必填）
```

## Gotchas（从实际事故积累——最高信号）

- **复用已 push 的 tag**：tag 推到远程后被强制删除重打 → 用户/CI 缓存了旧 commit，部分人拉到旧版部分拉到新版，定位极慢。**铁律：tag 一旦 push，永不复用**，要修就发新版本号。Forge npm 旧版不可撤回的事故就是这个坑。
- **迁移只测了前向没测回滚**：上线后出 bug 想回滚，发现回滚迁移半年没人跑、语法错了 / 数据格式不兼容前版本代码 → 卡在中间态。**M4 强制 staging 双向跑**。
- **破坏性 SQL 被审过但没人当回事**：审阅者"看起来 OK"签字，实际是 `DELETE FROM users WHERE id = ANY($1)` 漏了 `AND deleted_at IS NULL`——code-review-gate 的 sql-safety-checklist 同源。破坏性 SQL 必须双签。
- **secrets 硬编码进镜像层**：源码里 `apiKey := os.Getenv("KEY")` 但 Dockerfile `ENV KEY=sk-xxx` 把生产 key 烤进镜像层，推到公共 registry 泄露。M5 不只扫源码，要扫 Dockerfile/CI yaml。
- **CHANGELOG 漏 Breaking Change**：commit message 写了 BREAKING 但 CHANGELOG 只列在 Improvements，用户没看到 → 升级后生产崩。M2 强制 grep `breaking|破坏|不兼容`。
- **多包/多 README 漂移**：monorepo 根 README 对了，发包的 `npm/README.md` 滞后——发出去用户看到的还是旧 hook 表/旧版本号。M6 必须覆盖每份派生副本。Forge 反复踩过（见 memory skillgen-asset-sync-discipline）。
- **回滚预案写了没演练**：RUNBOOK.md 写得很漂亮，真出事时发现"上一版本镜像 7 天清理策略已删了 N-1" / "数据库 schema 不兼容 N-1 代码"。R1 强制演练。
- **体积暴涨没人在意**：新依赖引入 50MB，CI 都绿，发布后用户安装时间翻倍 / 镜像 pull 超时。M3 强制对比上次发布的体积。
- **发布窗口与并发变更冲突**：发版进行中有人 merge 了新 commit，构建产物混了半新半旧。发布窗口期间 freeze main，或从特定 commit 切出 release branch 构建。
- **"灰度 1% 五分钟没事就全放"**：1% 流量可能根本没触达关键路径（夜间、低峰）。灰度要确认**关键路径流量真的流过新版本**，不是看总错误率，看新版本标签的错误率。

## Rationalizations（堵借口）

| 借口 | 现实 |
|---|---|
| "代码都测过了能上线" | 代码测过 ≠ 发布风险覆盖。迁移/版本号/回滚是发布特有的坑，单测管不到 |
| "回滚以后再说" | "以后"= 永不。出事时慌乱回滚比预先演练贵 100 倍 |
| "Breaking change 用户应该会看 commit" | 用户不看 commit，看 CHANGELOG。Breaking 必须在 CHANGELOG 显式段 |
| "CHANGELOG 等发完再补" | 发完就忘。Tag 推上去 CHANGELOG 还没更新 = 用户拉到无文档版本 |
| "体积涨一点没事" | 涨 50% 用户安装时间翻倍，CI 镜像 pull 超时。>30% 要解释 |
| "staging 跑过就行不用回滚演练" | staging 前向跑过不代表回滚能跑。回滚迁移必须实测 |
| "secret 先硬编码下个版本改" | 一次都不能发。硬编码 secret 进镜像 = 公开泄露 |
| "Recommended 项可以跳" | Recommended 不等于可忽略。未过项必须进"已知风险"段，取得干系人 explicit ack |
| "tag 推错了 force push 修一下" | tag 永不复用。force push 后部分缓存仍指向旧 commit，定位极慢 |
| "灰度五分钟没报错就全放" | 1% 流量可能没触达关键路径。看新版本标签的错误率，不是全局 |

## Red Flags（看到这些想法 = STOP，你在 rationalize 发布）

- "M4 回滚先跳过，前向测过应该没问题" → NO-GO，回滚迁移必须实测
- "M5 secret grep 出一个，先发了再改" → 立即阻断，撤下用 env 替换
- "CHANGELOG 等发完一起写" → 必须先写，tag 一上就不可逆
- "体积涨了 50% 但应该没事" → 必须解释，否则 NO-GO
- "回滚 RUNBOOK 写了，演练下次吧" → 至少 staging 走一次
- "破坏性 SQL 我看了应该 OK" → "看起来 OK"不是评审，要有书面记录或双签
- "Recommended 项先跳过，发完再补风险说明" → 风险说明在发版前，不是发版后
- "差不多能发" → 门控不是建议，NO-GO 就是 NO-GO
- "上个版本发的时候也没出事" → 上次没出事 ≠ 这次没风险，每项都要查
- "灰度跑了五分钟没报错" → 看新版本标签的错误率，确认流量真的过

## 防注水自检（避免清单写得比实际做法松）

同 skill-authoring-standard §防注水自检——发布清单最易注水的三类：

| 类型 | 特征 | 修复 |
|---|---|---|
| 弱校验措辞 | "人工校验/肉眼对比/大致核对/应该通过" | 换可执行命令 + 量化阈值（`grep sk- 命中 0`/`体积涨 <30%`）|
| 门控无方法 | 有"门控/必跑/强制"但无具体命令 | 每项配命令 + 通过标准（M1-M7 每项都给了）|
| checklist 无命令 | `- [ ] 版本号一致` 无配套命令 | 每项配命令（本 skill 已配）|

发布前对 checklist.md 跑一次自检：每个 ✅ 旁边必须有命令输出，不是印象。

## 子 agent 化：独立上下文审查

同 code-review-gate 的子 agent 化逻辑——主 agent 刚写完代码/刚刚 bump 了版本，有"它能上"的确认偏误，单行硬编码 secret、漏改的版本号出处、漏配的回滚迁移就在自审盲区。

- **小发布**（patch / 单服务）→ 派 1 个只读子 agent 跑 M1-M7
- **大发布**（minor/major / 多服务 / 含迁移）→ 派 2 个并行子 agent：
  - `release-risks-auditor`：M1 版本 / M2 CHANGELOG / M5 secrets / M6 文档一致性
  - `runtime-readiness-auditor`：M3 构建 / M4 迁移 / M7 smoke / R1 回滚演练
- 子 agent **只读不写**——审查与修复分离，避免"边审边放行"妥协
- 子 agent prompt 注入本 skill 的清单 + 决策树 + checklist.md 格式，要求结构化输出

## 与其他 skill 的分工

- **docs-consistency-guard**：M6 文档一致性专项**委托给它**。本 skill 不重复其检查表，消费其守卫测试结论。
- **project-acceptance**：项目级验收（PRD 对比、功能完整度、设计一致性）。前者管"功能做完了没"，本 skill 管"能安全上线吗"——发布是 project-acceptance 之后的独立门禁。
- **code-review-gate**：单次 diff 代码质量审查（AI 作弊 / SOLID / 安全）。前者管代码层质量，本 skill 管发布层风险（版本/迁移/回滚/观测），不重审代码。
- **implementation-discipline**：编码任务的交付纪律（先读再改/测试伴随/聚焦变更）。前者管开发期，本 skill 管发布期。
- **systematic-debugging**：smoke check 失败或灰度报错时，用它排查根因，不要在发布窗口边猜边改。
- **forge-pipeline gate-8-release**：本 skill 是 gate-8 的内容载体——gate-8 的 subagent 加载本 skill 跑清单、产出 `checklist.md` 作为 gate 产出物。
- **session-retrospective**：发布事故复盘后，决定经验进什么载体（守卫测试 / RUNBOOK / 本 skill 的 Gotchas）。当它判定"进发布清单"时，转交本 skill 加具体项。
