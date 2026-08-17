# HANDOFF — Skill Eval 2.0 + CI 标准化（交接给 kimi）

> 交接日期：2026-08-17。交接方：Claude（已完成 Task 1）。接手方：kimi。
> 总目标：把 forge 的 skill eval 体系从「有机制、没资产」升级为「资产进 VCS + 分层判定 + CI 门禁」，并补齐 GitHub Actions 标准化流程。

## 已完成（Task 1，已合入 main，commit 021f89b，评分 94/A）

**EvalDir 路径治理**——eval 数据根从硬编码 `~/.pi/research/skill-eval` 迁入 forge 命名空间：

- 解析链（`internal/skillseval/dir.go`）：`--dir` flag > `FORGE_EVAL_DIR` env > `~/.forge/evals`（走 `forgedata.GlobalHome()`）
- 一次性旧路径迁移：哨兵标记 `.migrated-from-pi` 锚定完成；staging copy 防半成品；best-effort 不阻塞解析；无锁竞态复查
- `forge skills --dir <path>` persistent flag 已挂到全部 eval 子命令（eval-gen/eval-cases/eval-record/eval-baseline/eval-report/battery）
- **后续任务全部依赖这个**：仓库级 evals/ 目录 + `--dir` 是 CI 门禁的前提

## 待办任务（按依赖顺序，每个走独立 forge task）

**纪律（必须遵守）**：每个任务 `forge task start --ref <ref> --title "..." --branch`（在 main 上）→ 实现（改文件前先 Read）→ `go build ./... && go test ./... -count=1` 全绿 → `forge task gate task-implement --ref <ref>` → `forge task gate task-verify --ref <ref>` → **先 git commit** → 派只读子 agent 审查 diff → `forge review pass` → `forge task gate task-complete --ref <ref>` → `forge task complete --ref <ref>` → merge 回 main。README 命令表必须同步新增 flag/命令（`internal/cli/docs_consistency_test.go` 守卫 D 棘轮会拦）。

### Task 2: 互斥集（feat/eval-mutex-set）

**问题**：50+ skill 有明显混淆簇（review 簇：code-review-gate/rust-code-review/frontend-code-review/ai-generated-ui-review；调试簇：systematic-debugging/compile-fix-loop/test-discipline；验收簇：project-acceptance/design-audit/adversarial-verification/release-readiness），但 eval case 全是单 skill 视角，没有任何跨 skill case。

**关键资产（已存在，直接利用）**：SKILL.md 的 SKIP 边界已声明所有权让渡，形如：
```
SKIP: Rust 代码审查（用 rust-code-review）、只做格式化或 lint 建议时（...）
```
`（用 X）` 括号模式 = 有向边：A --skip--> B。

**设计**：
1. `internal/skillseval/mutex.go`：
   - `MutexEdges(canonical) ([]MutexEdge, error)`——解析全部 SKILL.md 的 SKIP 片段（复用 `ExtractTriggers` 的 skip 提取，`internal/skillseval/eval.go:38`），用正则 `[（(]用\s+([a-z0-9-]+)` 提取让渡目标，与 `skillsdist.ListSkills` 交叉验证（目标必须存在），去重排序
   - `MutexCases(canonical) ([]MutexCase, error)`——对每条边 (A→B)，取 B 的 trigger 片段渲染 prompt（复用 `renderTriggerPrompt`），生成 case：`{ID: sha1("mutex:"+A+":"+B+":"+fragment)[:12], Positive: B, Negative: A, Prompt, Source}`；每边取 ≤2 个 prompt 控制规模
   - `SaveMutexCases(dir, cases)` → `<dir>/mutex/cases.json`；`LoadMutexCases`
   - MutexRun 记录：`<dir>/mutex/runs.jsonl`（append，字段对齐 EvalRun：timestamp/forge_version/agent_model + results[{case_id, actual}]）
   - `ConfusionMatrix(latest MutexRun, cases)` → 按 (Positive, Actual) 计数 + pass 判定（actual==Positive 才 pass；actual==Negative 是头号混淆行）
2. CLI（`internal/cli/skills_mutex*.go`）：
   - `forge skills mutex-gen [--dir]`——生成 + 打印边摘要（评审 aid）
   - `forge skills mutex-record --from <file> [--dir]`——回填 `[{case_id, actual}]`
   - `forge skills mutex-report [--dir] [--gate]`——混淆矩阵；`--gate` 任一 actual==Negative 即 exit 4（对齐 battery 契约：BLOCKED 走 stderr）
3. 首批资产：对仓库 `skills/` 全量跑 mutex-gen，输出提交进 `evals/mutex/cases.json`（预期 ~40 边 × ≤2 = ~80 case）
4. 测试：边解析（含「（用 X，与门控叠加不替代）」尾缀变体）、case 生成稳定性、record/report 判定、gate 退出码

### Task 3: 黄金集资产化（feat/eval-golden-vcs）

**问题**：case 从 SKILL.md description 自我派生（近似同义反复——描述里的词命中描述里的词），且存用户家目录不进 VCS。

**设计**：
1. `evals/golden/<skill>/cases.json`——人工策展黄金集，格式复用 CaseSet 但 case 可含 `origin: "curated"` 标记；策展要求（区别于派生 case）：真实话语改写（不是 description 原词）、每 skill ≥3 正例 + ≥2 负例 + 1 边界例
2. `forge skills eval-gen` 增加 `--curated` 或直接支持把 `evals/golden/` 下文件与派生 case 合并加载（`LoadCases` 扩展：golden 优先，派生补充，ID 不冲突——curated ID 前缀 `g-`）
3. 首批策展：挑 5 个高价值 skill（code-review-gate、systematic-debugging、research-workflow、dev-lookup、skill-evolution）人工写黄金 case 提交
4. `evals/README.md` 说明目录语义（golden=人工策展进 VCS、mutex=互斥、runs/baselines=CI 提交的机器产物）
5. 测试：golden 加载合并、origin 标记、派生/策展 ID 不冲突

### Task 4: 盲测协议（feat/eval-blind-dispatch)

**问题**：现在 eval-cases 是「知道测谁」的单 skill 视角（prompt 只问「会不会触发 skill X」），真实路由是全库竞争。`internal/skillseval/eval.go` 的 EvalSkill 清单里其实已写了盲测思想：「你是新 session，收到这个 prompt，你会加载哪个 skill？为什么？」——把它形式化。

**设计**（参考社区实测范式，54 查询 × 全量元数据盲测从 93% 调到 100% 误触发 0）：
1. `forge skills eval-cases --skill X --blind`：输出 case 时 prompt 前置全库 skill 的 name+description 清单（Level-1 渐进披露模拟），问「该触发哪个」而非「是否触发 X」
2. 清单注入注意：description 很长（50+ skill 全文会爆 prompt）——截断到 name + description 前 200 rune（`eval.go` 已有 `firstNRunes` 先例）
3. `eval-record` 支持回填 `actual_triggered: "<skill名|none>"`（格式已兼容，`runs.go` SubmitResult 的归一化已按 canonical 精确匹配）——盲测只是换了 prompt 来源，record 通路不用改；确认这条再动刀，能少改就少改
4. 产出：盲测 run 天然产出混淆矩阵数据源（actual ≠ target 的行就是误路由），`mutex-report` 可复用
5. 三条迭代纪律（写进 evals/README.md + mutex-report 输出提示）：改 description 后必须全量重跑（相邻 skill 会回归）；歧义时改 eval 不改 skill（防过拟合）；borderline 误触发记录不调掉
6. 测试：--blind 输出格式、清单截断、record 通路兼容

### Task 5: CI 流水线（feat/ci-quality-pipeline，纯 YAML 无 Go 改动，但依赖 Task 2/3 提交的 evals/ 资产）

1. **ci.yml 加 `skills-qa` job**（ubuntu 一份即可，timeout 10min）：
   ```yaml
   - run: go build -o /tmp/forge ./cmd/forge/
   - run: /tmp/forge skills validate --canonical ./skills        # exit 2 = 规范失败
   - run: /tmp/forge skills audit --canonical ./skills --gate    # exit 4 = HIGH/CRITICAL
   - run: /tmp/forge plugin pack --out /tmp/pack && git diff --exit-code -- .claude-plugin .cursor-plugin
     # 生成物漂移门禁：regenerate 后 diff 非空 = 提交的 marketplace.json 落后
   - run: /tmp/forge skills battery --dir evals --gate           # Task 2/3 资产就位后生效；空电池会打 vacuous 提示
   - run: /tmp/forge skills mutex-report --dir evals --gate      # Task 2 就位后生效
   ```
   注意：`forge plugin pack` 写 cwd——先跑再 diff，diff 范围限定两个生成物目录；audit --json 可加 `> $GITHUB_STEP_SUMMARY` 顺手出摘要
2. **nightly.yml**（`schedule: cron`，off-minute 如 `37 3 * * *`，`workflow_dispatch` 手动）：
   - build forge → `forge verify --regression`（E2E 场景：fresh-install/upgrade-v040/v030——先本地实跑确认在 fresh runner 的行为，upgrade 夹具可能要钉）
   - `forge doctor`（装机后环境一致性 smoke）
   - `forge skills drift-check`（需先 `forge init` 装机才有意义——fresh runner 上先 init）
3. **release.yml 尾部加 `npm-verify` job**（`needs: [npm]`）：
   - `npm i -g @agent_forge/forge` 后 `forge --version` 断言等于 tag 版本——补上 `internal/ci/release_workflow_test.go:8` 注释承认的「发布后装机无人验证」缺口
4. 分支保护建议（写 PR 描述即可，仓库设置需人工）：`skills-qa` 与 `build` 设为必需检查
5. 注意 `.github/workflows/*.yml` 改动会触发 workflow-test-guard hook（Write/Edit 后自动跑 internal/ci 守护测试）——正常反馈，不是拦截

## 前沿调研结论（设计依据，已验证来源）

1. **四层测试范式**（github.com/timwukp/agent-skills-best-practice TESTING.md，Claude Code+Kiro 实测）：L1 静态校验（≈validate）→ L2 盲测 routing（judge 只见 name+description，ground truth 保密，指标 recall+false activations）→ L3 执行/评分分离（期望对执行者隐藏，独立 grader 逐条 expectation）→ L4 真实环境 e2e。我们 Task 4=L2，Task 5 的 verify --regression=L4。
2. **官方 `claude plugin eval`**（early access，本组织未启用，不能依赖但值得抄设计）：case 即文件（prompt.md+graders/*.md）；六种 grader（regex/tool_used/tool_order/file_exists/llm 2-of-3/baseline）；routing 判定 idiom=tool_used 打 Skill 工具 min/max；CI 契约=退出码+稳定 JSON；ablation 臂分离 skill 增量 vs 模型本征能力。
3. **指标共识**：routing 是分类任务——precision（误触发）+ recall（漏触发）必须同报；测试集必须含 out-of-scope 负例。

## 关键文件索引

| 文件 | 作用 |
|---|---|
| `internal/skillseval/dir.go` | eval 根解析+迁移（Task 1 产出，全部任务的地基） |
| `internal/skillseval/eval.go` | ExtractTriggers/renderPrompt（Task 2 复用） |
| `internal/skillseval/cases.go` | EvalCase/CaseSet/SaveCases（Task 3 扩展点） |
| `internal/skillseval/runs.go` | EvalRun/SubmitRun/CompareRuns（Task 4 通路确认） |
| `internal/skillseval/battery.go` + `judge_accept.go` | 回归电池+deterministic 判据（gate 契约对齐对象） |
| `internal/cli/skills_eval_dir.go` | --dir flag 接线模式（新命令照抄） |
| `.github/workflows/ci.yml` / `release.yml` | Task 5 改造对象 |
| `internal/cli/docs_consistency_test.go` | README 守卫棘轮（新命令必须进 README 表） |
