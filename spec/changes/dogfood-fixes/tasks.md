# 任务：dogfood-fixes

- [ ] 1. next 干净路由：spec 仓库无任务且工作区干净时给 task start（behindRemote 优先给 pull），并更新 NextContext 注释（判据：acceptance #2）
- [ ] 2. 评分回退：抽出 loadScoringConfig(root)，契约仓库静默默认、非契约仓库保留告警，补两例 stderr 捕获测试（判据：acceptance #2）
- [ ] 3. 模板 Task 流程重写为 8 步（验收登记、切分方式、doc gate、提交→审查→完结、report --ref），扩展 TestAgentsProtocol_IsActionable（判据：acceptance #3）
- [ ] 4. cli.md 三处补丁（next 行、task start --accept 行、task report 行）（判据：acceptance #4）
- [ ] 5. forge change agents 同步 AGENTS.md；gofmt 与 deadcode 守卫（判据：acceptance #5、#6）
