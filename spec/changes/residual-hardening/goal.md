# goal — residual-hardening

## Objective

把 core-cleanup 收尾时如实列出的 5 条残留风险逐条处理掉，让「不留死能力 / 判据不说假话」这两条
从「口径」变成「有守卫、有反证」：

1. `internal/act/disposition.go`（Disposition / AppendDisposition / LoadDispositions / AckMatcher /
   Carriers / IsValidCarrier）在 `forge act retro-done` 被删后已无任何生产调用者——它承载的
   「证据式 ack」通路整条死掉。删除这套 ack 面，并把 `health.SummarizeAtAcked` 折叠回
   `SummarizeAt`（ack 谓词永远为 nil，留着只是死参数）。
2. 死提示守卫 `TestNoDeadCommandHints` 只扫 fmt 系调用的字符串实参，覆盖不到
   `errors.New` 之外的常量、变量初始化、以及渲染给用户的模板 md。扩到「非测试 Go 源码里的
   全部字符串字面量 + internal/**/templates/*.md」，并给合法豁免一个显式标记 `deadhint:allow`。
3. `criteria.SelectorMatchedNothing` 只认 `no tests to run` 一条子串。补两条同类信号：
   `no test files`（包内根本没有测试文件，退出 0）与「-v 下却没有任何 `=== RUN`」——
   后者是 go test 的结构性证据，不是猜输出。
4. `testledger.skipInfo` 只把 `if` 内的 Skip 算条件跳过：switch/case 分支、for/range 循环体里的
   Skip 被误判为无条件（要理由与审批），而经 helper 间接跳过的测试完全看不见。补：分支/循环体
   算条件；识别同包内「带 Skip 的 helper」，测试调用它时标为经 helper 跳过。
5. `skills/` 与 `skills-forge/` 的 SKILL.md 内容仍被 compat 载荷面与 skill-decisions 门禁按路径读取，
   但没有任何守卫保证「目录 ⇄ SKILL.md ⇄ frontmatter name」一致。加一条内容守卫测试。

## Out of Scope

- 不改门禁语义（task/change 两道门禁的判定口径、退出码来源、证据封印规则一律不动）。
- 不新增顶层命令，不改 `forge` 命令面（compat 快照除 `internal/act` 删除导致的载荷项变化外应无差异）。
- 不重写 core-only 已删的命令组，不恢复任何已删能力。
- 不动 git 历史、不删 ADR / CHANGELOG / `spec/changes/archive`。
- 不改变异算子与 mutation 判定口径。

## Check

验收走 `spec/changes/residual-hardening/acceptance.md` 的 6 条围栏：#1 空匹配判定新信号、
#2 跳过判定边界、#3 死提示守卫（含模板）、#4 skills 内容守卫、#5 受影响包全绿（含 act/health 删除后的编译）、
#6 `go build ./...`。
