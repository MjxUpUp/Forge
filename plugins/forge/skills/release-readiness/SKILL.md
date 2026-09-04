---
name: release-readiness
description: "发布/上线前的 readiness 门禁清单（能安全上线吗）。Use when: 准备发布/上线/打 tag/发版/灰度时、用户问'能上线了吗'/'可以发布了吗'/'发布前检查'/'上线前 check 一遍'时、版本号已 bump 准备 tag 时、Release 截止前的 go/no-go 评审时。SKIP: 项目功能验收/PRD 对比/完整度审查（用 project-acceptance）、文档 vs 代码一致性专项（用 docs-consistency-guard）、单次 diff 代码审查（用 code-review-gate）、编码任务交付纪律（用 implementation-discipline）、运行时 bug 排查（用 systematic-debugging）。"
metadata:
  pattern: gate
  domain: operations
  # requires_forge: R6 引用 forge eval golden run/audit-verify（跨项目复用时该检查项跳过并记录）
  requires_forge: "true"
  composes: [docs-consistency-guard]
  triggers: [{"event":"PreToolUse","match":"Bash","keywords":["git tag","npm publish","goreleaser","docker push","cargo publish"],"cooldown":300}]
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
  awk '/^##\s+\[?<NEW_VER>\]?/{flag=1; next} /^##\s+\[?v?[0-9]/{if(flag)exit} flag' CHANGELOG.md | grep -iE 'breaking|破坏|不兼容|迁移'
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
- **划界**：编码期安全基线（OWASP/注入/认证等系统安全审查）归 secure-coding；本项只做发布前增量扫描——新增硬编码 secret + env 默认值/文档同步，不重复其检查表。
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
  grep -nE 'config\.[A-Z][a-zA-Z]+\s*\|\|\s*""' src/ lib/ app/   # 空双引号默认值 → 警告点
  grep -nE "config\.[A-Z][a-zA-Z]+\s*\|\|\s*''" src/ lib/ app/   # 空单引号默认值 → 警告点
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

| 项 | 检查什么 | 未过的处置 |
|---|---|---|
| R1 回滚预案 | 回滚步骤明确、上一版本 tag/镜像保留、staging 演练过 | 补 RUNBOOK 含具体回滚命令；N-1 必留；未演练至少 staging 走一次 |
| R2 已知问题与降级策略 | 已知 bug/限制列发布说明；高风险变更有 feature flag | 补 Known Issues 段；破坏性变更强烈建议补 flag |
| R3 发布后观测 | 仪表盘/告警存在；日志保留 ≥7 天覆盖回滚调查窗口 | 建最小可观测盘（错误率/p95/部署标记）+ 至少设错误率告警 |
| R4 通知与公告 | 发版窗口通知、breaking change 公告、文档站更新 | 补 Release Notes；breaking 未公告推迟一个版本 |
| R5 灰度计划 | 灰度档位（1%→10%→50%→100%）+ 每档回退判断点 | 补灰度计划；无法灰度（如 CLI 二进制）至少内部 dogfood 一周 |
| R6 质量门禁基线回归（Forge 项目适用） | `forge eval golden run` 无 missed/false-positive finding（门禁 precision/fpr 基线未回归）；`forge eval audit-verify` 伪造审计行 0 | 基线回归先修门禁再发；audit-verify 非零 = 审计时间线被污染，按 BLOCKED 指引溯源后重跑；无 eval 资产的项目跳过并记录 |

每项的完整检查命令与通过标准：见 [references/recommended-checks.md](references/recommended-checks.md)。

## 决策树：什么算 Ready 可放行

- 任一 Mandatory（M1-M7）未过 → 🚫 **NO-GO**：列出阻断项 + 修复方案，回到修复。禁止"差不多能发先发了再说"。
- M6 仅人工核对通过（无 docs-consistency-guard 守卫）→ ⚠ **GO-WITH-RISK**（强制，不能升 GO）："已知风险"段注明"M6 无自动化守卫，人工核对 N 表"，并要求下次发布前建立守卫。
- 全部 Mandatory 过（M6 经守卫测试绿）+ 全部 Recommended 过 → ✅ **GO**：产出 checklist.md，按发布流程执行。
- 全部 Mandatory 过 + 部分 Recommended 未过：影响范围可控（用户无感知/有降级路径）→ ⚠ **GO-WITH-RISK**（"已知风险"段逐项记录未过项/影响/降级策略/回滚触发条件，取得干系人 explicit ack）；影响用户数据/安全/收入 → 🚫 **NO-GO**（Recommended 不等于可忽略）。

**禁止模糊结论**：不说"差不多能发""应该没问题""先发了看看"。完整决策树：见 [references/decision-tree.md](references/decision-tree.md)。

## checklist.md 产出格式

清单 + 命令输出 + 决策结论的归档模板（gate-8 产出物）：见 [references/checklist-template.md](references/checklist-template.md)。

## Gotchas / Rationalizations / Red Flags

发布事故经验库（tag 永不复用、迁移必须双向演练、secrets 还要扫 Dockerfile/CI yaml、灰度看新版本标签错误率等）、堵借口表、STOP 信号清单：见 [references/gotchas-and-rationalizations.md](references/gotchas-and-rationalizations.md)。

## 防注水自检（避免清单写得比实际做法松）

规则唯一真相源：skill-authoring-standard「防注水自检」节（弱校验措辞 / 门控无方法 / checklist 无命令三类），此处不复制。发布前对 checklist.md 跑一次自检：每个 ✅ 旁边必须有命令输出，不是印象。

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
- **secure-coding**：编码期安全基线（OWASP/STRIDE/安全编码规范）。M5 secrets 扫描只做发布前增量（新增硬编码 secret + env 文档同步），系统性安全审查归它。
- **implementation-discipline**：编码任务的交付纪律（先读再改/测试伴随/聚焦变更）。前者管开发期，本 skill 管发布期。
- **systematic-debugging**：smoke check 失败或灰度报错时，用它排查根因，不要在发布窗口边猜边改。
- **session-retrospective**：发布事故复盘后，决定经验进什么载体（守卫测试 / RUNBOOK / 本 skill 的 Gotchas）。当它判定"进发布清单"时，转交本 skill 加具体项。
