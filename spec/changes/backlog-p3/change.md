# change：收口 P3 口径分叉

## 改动

1. **幻影路径判定收敛到一个地方**：新增 `internal/ghostpath/porcelain.go`——`porcelainPaths`（解析 `git status --porcelain` 的路径，重命名取两侧）、`LineHasReservedPath`（整行是否涉及保留名）、`FilterReserved`（批量过滤，磁盘存在即保留）。
   - `internal/taskpipeline/complete_advisory.go` 的未提交改动计数、`internal/taskpipeline/next.go` 的 dirty 判定、`internal/taskpipeline/pushgate.go` 的 `gitStatusDirty` 都改为逐行跳过保留名路径；
   - `internal/scoring/git.go`、`internal/cli/docs.go`、`internal/review/stamp.go`、`internal/change/verify.go` 统一走 `IsPhantomPath`/`FilterReserved`：只有「名字是保留名、磁盘上没有、HEAD 里也没有跟踪」才当幻影。真实存在的同名文件保留；已提交后被删除的同名文件也保留（`trackedInHead` 查 HEAD），否则「删掉一个真实保留名文件」会被当成干净工作区。
2. **preflight 顺序对齐**：`internal/change/gate.go` 改成与 `internal/change/verify.go` 相同——先报「产物没填完」，再报「没人确认边界（clarify）」；原来 gate 先查 clarify，同一个 change 在两条命令下会得到不同提示。
3. **残留注释**：`internal/taskpipeline/trust.go` 的 `.forge migrate` 说明还指向已删除的 `cli/sync.go`，改为现存入口 `cli/migrate.go`。

## 影响

- 行为：`nul/` 这类幻影目录不再让「工作区有改动」的判定为真（完整门禁链此前会因此误报「审查通过后源码变更」）；gate 的错误提示顺序变化（先缺产物、后缺 clarify），文案不变。
- 测试：新增 `internal/ghostpath/porcelain_test.go`（解析、平台语义、FilterReserved 顺序）；既有 `TestChangedFiles_PinsGhostpathFilter`、`TestChangedMarkdownSince_PinsGhostpathFilter` 保留。
- 文档：无用户可见变化（`docs/reference/cli.md` 已在前一轮写明幻影与容差口径）。

## 验证

`forge change verify backlog-p3` 实跑 7 条判据（见 acceptance.md）。