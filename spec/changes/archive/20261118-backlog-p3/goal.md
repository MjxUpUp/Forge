# goal：backlog-p3

## Objective

把上一轮（backlog-closeout）评审留下的 P3 口径分叉收干净：工作区状态探测对 Windows 保留名幻影路径统一走同一套判定（不只 review/change 两侧），`gate` 与 `verify` 的 preflight 顺序一致，并清掉指向已删除代码的残留注释。完成后工作区里不存在「同一件事两处口径不同」的已知 P3。

## Out of Scope

不改门禁语义、不新增命令、不改 `ghostpath` 的平台规则（非 Windows 上仍不把 `aux.go` 当保留名）；不重写历史文档里已经标注「已删除」的设计档案。

## Check

指向 acceptance.md 的 7 条围栏：`go build`；ghostpath/taskpipeline/change 单测；整包 cli 与 scoring；docs lint；gofmt；deadcode；守卫脚本。