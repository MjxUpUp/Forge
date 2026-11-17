import io
p = "docs/reference/cli.md"
s = io.open(p, encoding="utf-8").read()
old = "基线之后又有源码变更时必须派**新的只读评审者**并换一个 `--reviewer`：同一 reviewer 自证、或用 `--note` 刷新基线都会被拒（记 WARN 审计）"
new = "基线之后又有源码变更时必须派**新的只读评审者**并换一个 `--reviewer`：同一 reviewer 自证、或用 `--note` 刷新基线都会被拒（记 WARN 审计）。`--score 0` 是合法结论；审查基线 commit 不可达（amend/rebase+gc 改写了历史）时 `task gate task-complete` 与 `task complete` 都拒绝——请重新审当前 diff，而不是靠历史不可达绕过"
assert old in s
s = s.replace(old, new)
io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("ok")