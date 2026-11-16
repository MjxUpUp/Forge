# 任务：dogfood-fixes

- [x] 1. next 干净路由：spec 仓库无任务且工作区干净时给 task start（behindRemote 优先给 pull），NextHint 同吃 spec 上下文，并更新 NextContext 注释（判据：acceptance #2）
- [x] 2. 评分回退：抽出 loadScoringConfig(root) 并接管 BuildEvaluateInput，契约缺文件静默、坏文件与非契约仓库告警，补生产路径与坏文件测试（判据：acceptance #2）
- [x] 3. 模板 Task 流程重写为 8 步（验收登记、切分方式、doc gate、提交→审查→完结、report --ref），扩展 TestAgentsProtocol_IsActionable（判据：acceptance #3）
- [x] 4. cli.md 三处补丁（next 行、task start --accept 行、task report 行）（判据：acceptance #4）
- [x] 5. forge change agents 同步 AGENTS.md；gofmt 与 deadcode 守卫（判据：acceptance #5、#6）
