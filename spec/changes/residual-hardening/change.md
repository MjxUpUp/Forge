# change — residual-hardening

## 改动

1. **删 act ack 死面**：`git rm internal/act/disposition.go internal/act/disposition_test.go`；
   删 `internal/forgedata/paths.go` 的 `ActDispositionsPath`（唯一调用者就是被删的文件）；
   `internal/health/summary.go` 把 `SummarizeAtAcked` 的 ack 参数折叠进 `SummarizeAt`（删掉导出函数），
   `summary_test.go` 删 `TestSummarizeAtAcked`，注释里对 `forge act retro-done` 的指向一并清掉。
2. **死提示守卫扩面**（`internal/cli/dead_hint_guard_test.go`）：遍历非测试 .go 的全部 `*ast.BasicLit`
   STRING（不再要求外层是 fmt 调用），并新增模板 md 扫描（`internal/**/templates/*.md`）；
   豁免 = 行内含 benign 词（已删除/已移除/已随 core-only 移除/命令不存在/removed）或该行有
   `deadhint:allow` 注释。
3. **空匹配判定加证**（`internal/criteria/criteria.go`）：`-run` 存在时，除 `no tests to run` 外，
   `no test files` 也算空匹配；`-v`（或 `-test.v`）存在且输出没有任何 `=== RUN` 且输出像 go test
   （含 `ok ` / `PASS` / `?   `）、且不含 `(cached)` 时，判为空匹配。注释写明这是结构性证据
   与已知边界（测试被 build tag 全排除、非 go 工具）。
4. **跳过判定补边界**（`internal/testledger/testledger.go`）：`skipInfo` 的 For/Range/Switch/
   TypeSwitch/Select 体与 Case/Comm 分支按条件上下文遍历（原来的 `inIf` 改成 `inCond`）；
   `ScanDecls` 改为「先按目录收集文件 → 收集该目录内带 Skip 的 helper（不动点迭代）→ 再判每个测试」，
   测试调用 helper 时 `HasSkip=true, SkipGuarded=true, SkipReason="经 helper 跳过"`。
5. **skills 内容守卫**（新增 `internal/compat/skills_content_test.go`）：`skills/`、`skills-forge/`
   下每个子目录必须有 SKILL.md，frontmatter 必须有 `name` 与 `description`，且 `name` 等于目录名；
   空目录/缺文件/名字不符都失败。

## 影响

- 删除面：`internal/act` 少 2 个文件（约 200 行 + 测试）、`forgedata` 少 1 个方法、`health` 少 1 个导出函数与 1 个测试。
- `forge status` 的 health 段语义不变（`SummarizeAt` 行为与原来 `SummarizeAtAcked(cs, now, nil)` 完全一致）。
- compat 快照：`internal/act/disposition.go` 是阻断文件（`count: 2` 那条），删除后载荷面会变，
  需要重钉 `compat.snapshot.json`。
- 文档：`docs/design/test-ledger.md` 补「switch/循环分支与 helper 间接跳过」；`docs/reference/cli.md` 无命令面变化。
- 测试：新增 criteria 用例、testledger 边界用例、compat skills 守卫；删 health 的 ack 用例。

## 验证

`forge task verify-acceptance` 实跑 6 条判据；其中 #1 在实现前是红的（`TestSelectorEmptySignals`
尚不存在，且新判定会把「-run 空匹配」判负），作为先失败证明登记。另跑 `go vet` 与
`forge docs closure --strict` 确认命令面漂移仍为 0。
