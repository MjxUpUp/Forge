# 澄清：边界与非目标

change: `red-first-evidence`

## 边界（做什么 / 不做什么）

用户 2026-11-18 明确：①判据先于实现、红先于绿要做成机制证据；②子 agent 必须复用——同一角色复审由同一 reviewer 连续承接，换人显式留痕；不做节点图、不新增门禁，全部挂在 task start/complete、review pass、report 上

## 非目标

不做 TDD 硬门禁（不要求实现前必须写测试文件，也不猜 git 提交顺序）；不恢复已删的 artifact-chain / doc-review / task-verify / coverage 门禁；不引入节点图；不改两道门禁的数量；机器只证明「红过」，不证明「红的判据有意义」——后者仍归评审。

人确认：2026-11-18T04:32:02Z
