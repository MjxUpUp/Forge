# 任务：命令面收敛：去掉宿主层残留文案与死入口

change: `cmd-surface-convergence`

完成标准只有一条：`forge change verify cmd-surface-convergence` 封印通过。下面的勾选项是给人的
进度记录，不是判据——勾选不产生证据，也不参与封印。

- [x] 1. 删 task resume 的三个 hook 模式与 resume-stale 实现，build 与 clitask/taskpipeline 测试绿（判据：acceptance #1 和 #2）
- [x] 2. 两条静态守卫就位并通过（判据：acceptance #3 和 #4）
- [x] 3. cli 文档/兼容守卫通过（判据：acceptance #5）
- [x] 4. deadcode 保持零（判据：acceptance #6）