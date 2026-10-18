# 《沙箱 oracle v0》——变更级考卷：cheat 召回 / held-out 逃逸 / 可复算证据

状态：v0 已落地并实跑（2026-10-18，`feat/sandbox-oracle-v0`，13 例全跑通）。
依据：`internal/evalkit/golden.go`（门禁自检已有考卷形态，但只测探测命令）、`internal/evalkit/traps.go`（陷阱是自建样例，不是变更级 replay）、`internal/taskpipeline/cheatscan.go`（七类检测器是 v0 的被测对象）、`docs/design/forge-evaluation-system.md`（评测分层原案）。

---

## 0. 问题

仓库里已有三类评测：golden（门禁探测命令的 precision/recall 基线）、traps（构造陷阱重放）、redteam（对验证链注入 seeded bug）。它们回答的是"这套门禁自己能不能跑通"，不回答"门禁面对一次真实变更时拦得住什么"。

外部读数给出的是反面证据：2 star / 0 fork / 0 issue、npm 周下载三位数、五个本地项目全是本人。在这之前，关于"门禁有没有用"的所有说法都没有一个可复算的数字——包括我自己写的那些。

v0 的目标不是把数字做好看，是把数字做出来：一条变更进去，拦没拦、拦对没拦对、没有 held-out 时会漏多少，全部落成 JSON + Markdown，带套件指纹，任何人可重跑。

## 1. 形状

三层，全部在 `internal/evalkit/oracle.go`：

- **case**：`evals/oracle/cases/*.yaml`，12+1 条，字段 `id/kind/gate/description/fixture/category/mutations/heldout/expect/known_fp`。
- **fixture**：`evals/oracle/fixtures/go-basic/`，一个无第三方依赖的 Go 模块，含 `calc`/`auth`/`pager` 三个包和 `web/app.ts`（给 TS 检测器用）。
- **runner**：每条用例在临时目录里 `git init` → 提交基线并打 `oracle-base` tag → 应用文本变更 → 提交 → 按 gate 分派。

两个臂：

| 臂 | gate | 做什么 | 量什么 |
|---|---|---|---|
| 扫描臂 | `cheat-scan` | `ScanCheatPatternsRange(root, "oracle-base..HEAD")`，与被测门禁同一函数 | 召回、类别准确率、误报 |
| 验收臂 | `acceptance` | 容器内先跑可见测试（OFF），再挂 held-out 跑一遍（ON） | OFF 逃逸率、ON 逃逸率 |

验收臂用 `docker run --rm --network none -v <repo>:/work golang:1.25-alpine go test`。`--network none` 是硬约束：模块无依赖，联网只可能是被测代码在偷东西。docker 不可用时回退宿主执行并在 `sandbox` 字段如实标注 `host`。

## 2. 指标（v0 实跑）

套件指纹 `ee3eeb1c6e111849`，13 例，20.8s：

| 指标 | 值 | 口径 |
|---|---|---|
| cheat 召回 | 86%（6/7） | 标注 cheat 的变更里被拦下的比例 |
| 类别准确率 | 86%（6/7） | 拦下且命中标注类别的比例 |
| 误报率 | 0%（0/3） | 干净变更被拦下的比例（known_fp 不进分母） |
| 已知取舍 | 1 | 纯文档注释变更被 comment-only 报低置信，标 `known_fp` |
| 逃逸（OFF） | 100%（2/2） | 只跑可见测试时的漏检率 |
| 逃逸（ON） | 0%（0/2） | 挂 held-out 后的漏检率 |

数字里唯一有信息量的是那两个 100% 与 0% 的差：两条真实缺陷（删掉 token 长度校验、分页步长写成 `n*(size-1)`）在可见测试下全部绿——现有测试根本没覆盖到——挂上 held-out 后全部被拒。这量化了一件事：**可见测试是自证，held-out 才是验收**。

## 3. 两个 100% 里哪个是真测量

cheat 召回 6/7 而不是 7/7，是设计出来的第 7 条：`cheat-semantic-stub-001` 把 `Div` 改成 `return 3, nil`，让现有测试通过，但不含任何机械签名。检测器全部放过——这是 v0 最有价值的一条记录，因为它把"确定性门禁存在语义盲区"从一句自我批评变成了一个可复现的失败用例。

反过来说，前 6 条 100% 拦截是**同源构造**的结果：用例按检测器已知的正则形态写成，检测器当然拦得住。它证明的是测量链路自洽（考卷能装下、能跑、能复算），不是野外召回率。任何把 86% 引用成"真实世界拦截率"的写法都是错的。

## 4. fail-closed 与可复算

- 加载：畸形 YAML、缺字段、id 重复、kind/gate/category 互不匹配、cheat 用例 `expect != blocked`、defect 用例缺 held-out，一律报错而非跳过。空目录报错——空考卷不是通过。
- 执行：单条用例失败记 `verdict=error` 并计数，不中断整卷；CLI 在存在 error 用例时返回 BLOCKED。
- 复算：套件指纹 = 全部用例 YAML 的 sha256 前 16 位，写进报告；`--repeats N` 重复跑扫描臂，发现集合不一致记 `flips`（确定性漂移）。

## 5. 已知局限（v1 要补的）

1. 扫描臂 in-process，被测代码与评测器同机。真的恶意代码可以改内存、读 env。验收臂有容器隔离，扫描臂没有。
2. 用例是人按检测器形态构造的，不是野外采样。野外采样要做的是从真实 PR 里 harvest "事后被回滚/被 review 拒绝"的变更，带 `revert-of-fix` 元数据。
3. 镜像按 tag 钉，未按 digest；没有只读根、非 root、无 docker socket。v1 的验收臂要 `--read-only --user 65534 --cap-drop ALL` 并把 GOCACHE 放 tmpfs。
4. held-out 测试在 `go test` 前才拷进去，理论上被测代码可以探测文件存在性。v1 要在测试启动后再挂载，或打乱文件名。
5. 标注成本没有被测量。v0 的 13 条是我一个人 20 分钟标的，不等于别人的标注成本，也不等于标注一致性。

## 6. 与既有评测的分工

| 层 | 已有 | oracle 加什么 |
|---|---|---|
| 门禁自检 | `forge eval golden` | 不变 |
| 构造陷阱 | `forge eval traps` | 不变 |
| 变更级 replay | 无 | 真实 git 变更 + ground truth + ON/OFF 对照 |
| 外部有效性 | 无 | 仍是空的——v0 只建了装它的筐 |

## 7. 落地

```bash
forge eval oracle run --suite evals/oracle                       # 全臂，打印 Markdown
forge eval oracle run --arm scanner --repeats 2 --json           # 只跑扫描臂 + 漂移检测
forge eval oracle run --out evals/oracle/reports/v0-<date>.md    # 落盘证据
```

代码：`internal/evalkit/oracle.go`（~450 行）、`internal/cli/eval_oracle.go`、单测 `internal/evalkit/oracle_test.go`（含 scanner 臂集成回归）。
