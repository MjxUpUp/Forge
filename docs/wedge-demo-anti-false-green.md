# Forge 演示：识破「假绿」

状态：现行（2026-11 按 3.0 core-only 重写；演示命令全部在 3.0 命令面上，旧版的 mutation / trace / eval 演示已删除）。

这份文档给第一次接触 Forge 的人照着跑。三个小演示说明一件事：**命令退出 0，不等于这件事被验证过**。Forge 把这类「假绿」变成可执行的判据。全程在终端里操作，不需要 AI agent。

标签：**【已实跑】** 表示 2026-11-23 在 3.0 二进制（v3.0.1-19）上照本文命令跑过，输出逐字相同；**【源码原文】** 表示未实跑，文案与源码核对过；**【推算】** 表示按源码推断，以实跑为准。

## 前置

- Go 工具链（演示一、二要跑 `go test`）、git 2.28+（要支持 `git init -b`）。
- Forge ≥ 3.0.1：`npm install -g @agent_forge/forge@latest`，装完 `forge version` 确认。

两个术语：**判据**是 acceptance.md 里 bash 围栏中的命令 + 期望子串；**证据封印**指 `forge change verify` 实跑判据后写进 `spec/changes/<id>/evidence/<UTC>/` 的 stdout/stderr/exit 与 summary.json——它是「已验证」的唯一来源。

## 演示一：`-run` 指向不存在的测试，`go test` 退 0 也算失败

`go test -run NoSuchName ./...` 的退出码是 **0**，输出里只有一句 `no tests to run`。旧版 `change verify` 只看退出码，于是这种判据永远绿——它什么也没测。3.0 把它判负。

1. 建一个带测试的示例仓库：

```bash
mkdir wedge-demo && cd wedge-demo && git init -q -b main && go mod init example.com/wedge
printf 'package wedge\n\nimport "testing"\n\nfunc TestPresent(t *testing.T) {}\n' > wedge_test.go
git add -A && git commit -qm init
forge init
```

2. 立一个 change，把判据故意写成不存在的测试：

```bash
forge change new bad --title "判据指向不存在的测试"
```

把 `spec/changes/bad/acceptance.md` 写成：

```bash
go test -run TestNoSuchName ./... :: ok
```

然后：

```bash
forge change clarify bad --by-human --note "演示：判据选择器写错"
forge change phase bad
forge change verify bad
```

预期：【已实跑】退出码 2，输出里有

```
❌ [1] go test -run TestNoSuchName ./... — 选择器未匹配任何测试（go test 退出 0，但 no tests to run）
```

证据不封印，change 停在未验证状态。判定依据是 `internal/criteria/criteria.go` 的三类信号（`no tests to run` / `no test files` / `-v` 下没有 `=== RUN` 却像 go test 输出），`internal/change/verify.go` 的 `runPlan` 判 `Exit != 0 || SelectorEmpty`。

## 演示二：证据绑内容，改了文件就失效

证据不是一张永久通行证：summary 记着判据的哈希、封印时的 commit（或工作区文件的 sha256）。之后改动被验证的文件、或改 acceptance，证据自动失效。

1. 把 acceptance 改成真实存在的测试，重跑 verify：

```bash
printf 'go test -run TestPresent ./... :: ok\n' > spec/changes/bad/acceptance.md
forge change verify bad
forge change list
```

预期：【源码原文】verify 通过并封印出 `spec/changes/bad/evidence/<UTC>/`；`change list` 里这条显示绑定 commit、无「证据已失效」。

2. 改一下被验证的测试文件，再看列表：

```bash
printf '\nfunc TestExtra(t *testing.T) {}\n' >> wedge_test.go
forge change list
```

预期：【源码原文】同一条 change 被标「证据已失效」。此时「已验证」不再成立，必须重跑 `forge change verify bad`，或者把改动提交后重跑——把证据文件自己提交上去不算。

## 演示三：`review pass` 绑定代码快照

评审不是一段留言：`forge review pass` 记下当时的 HEAD / 变更哈希；之后再改源码，`task gate task-complete` 会以「审查后源码已变更」拒绝，必须由同一个 reviewer 复审。

【源码原文】最小复现骨架（演示一、二之后在同一个仓库里）：

```bash
forge task start --ref feat/wedge --branch --accept "go test -run TestPresent ./..." --agent demo
# 提交一处实现改动后：
forge task verify-acceptance
forge review pass --reviewer demo-reviewer --criteria-reviewed 1
# 现在再改一处源码，然后：
forge task gate task-complete
```

预期：门禁 BLOCKED，提示 review 快照与当前源码不一致（`review_rounds[].change_hash` / `head_commit` 对不上），要先让同一 `--reviewer demo-reviewer` 复审。未闭合项用 `--findings` 承接；换人必须 `--rotate-reviewer --reason` 留痕。

## 为什么这算「反假绿」

- **判据可以被命令证明**：退出码之外还看「这条命令到底跑了什么」——空匹配、无测试文件、`-run` 未命中都判负。
- **证据绑内容**：证据记判据哈希与代码快照，改了任何一边就失效，不存在「一次验证、长期复用」。
- **评审绑快照**：评审结论与当时的 diff 绑定，评审后改码必须复审，不能拿旧结论过关。

三条都不依赖人去「相信」：读 `summary.json`、读 `change list` 的失效标记、读门禁的 BLOCKED 文案即可复核。

## 核对记录

| 断言 | 出处 |
|---|---|
| 空匹配判负（no tests to run / no test files / -v 无 RUN） | `internal/criteria/criteria.go`、`internal/criteria/evidence_test.go` |
| `change verify` 判 `Exit != 0 || SelectorEmpty`，输出「选择器未匹配任何测试」 | `internal/change/verify.go` 的 `runPlan`、`internal/cli/change.go` 的两处 switch |
| 证据失效判定（判据哈希 / commit / 工作区 sha256） | `internal/change/verify.go` 的 summary 与 `change list` 渲染；`AGENTS.md`「一个 change 的生命周期」第 6 条 |
| review 快照绑定与复审要求 | `forge review pass --help`、`AGENTS.md` Task 流程第 6 条、`internal/review` |
