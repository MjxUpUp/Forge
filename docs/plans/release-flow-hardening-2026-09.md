# 发布流程硬化（2026-09-18 立项，v1.63.0 发布复盘）

> 复盘证据（v1.63.0 发布全链实录）：① 发布者本机停在 1.62.0 直到人工质疑
> ——update 通知只打印命令不代执行；② Release PR 每版必 `--admin` 绕过
> （GITHUB_TOKEN 路径 PR 无 checks）+ release 分支上 45 分钟幽灵失败 run；
> ③ 5 个未格式化文件穿过开发期全链（门禁/验收/双轮审查/lint——golangci v2
> 默认不启格式 linter），直到 make premerge 的 gofmt 才拦；④ main CI Windows
> 2/3 概率撞 flaky（`TestHook_ReadBeforeEdit/double-fire`：串行复放间隔 = hook
> 进程启动耗时，慢 runner 超 3s 生产去重窗）。

## Proposal（决策记录）

四项按伤害排序落地；PAT secret 项需用户凭据，代码侧三项 + flaky 修复本次交付。
方向：把检查/动作从终点搬到发生当下（discipline-first 原则在发布域的镜像），
消除"打印 ≠ 执行"的断层。

## P-A `forge update --apply`（断层修复）

npm 通道 `--apply` 代跑包管理器安装命令（`npmUpdateCommand` 单一真相源构造，
shell-free 切分执行）+ 装后 `--version` 自验（warning 级——PATH 层假阴只警示）；
Windows 上运行中二进制被文件锁挡住 npm 替换，提示手动命令不代跑。GitHub 通道
默认即执行安装，`--apply` 无附加作用。

## P-B auto-compile hook 的写入时刻 gofmt 检查（断层修复）

hook 已在每次源码 Write/Edit 的 PostToolUse 触发：`.go` 文件且 gofmt 在 PATH 时
跑单文件 `gofmt -l`（毫秒级），未格式化在 advisory 追加点名修复命令。非 Go
文件/无 gofmt 环境零影响（技术栈无关契约保持）。

## P-C double-fire 去重窗口 env 旋钮（flaky 修复）

生产默认 3s（2026-08-24 证据 0.5~1.9s 间隔）不变；`FORGE_BLOCK_DEDUP_WINDOW_MS`
旋钮钳制 [1s, 60s]（调宽只少记重复投递——审计去噪方向，不隐藏阻断判定）。
e2e double-fire 场景注入 60s 宽窗：串行复放的两次投递间隔 = hook 进程启动
耗时，不再依赖 runner 速度。

## P-D RELEASE_PLEASE_TOKEN secret（待用户凭据）

配置后 Release PR 自动跑 CI、tag push 直接触发 release.yml——消除每版
`--admin` 绕过与幽灵失败 run（RELEASE.md 既定路径）。需用户创建 PAT
（contents:write + pull-requests:write）设置 repo secret，非代码交付。

## 验收（verify-acceptance 实跑口径）

```accept
go test ./internal/hooks/ -run TestAutoCompileHook_GofmtAdvisory
go test ./internal/cli/ -run TestUpdateApply
go test ./internal/hookdispatch/ -run TestBlockDedupWindowEnvOverride
go test ./internal/e2e/ -run TestHook_ReadBeforeEdit
go test ./...
go vet ./...
```

lint 口径：对 main 零新增（存量债务不在本任务范围，复算命令同
discipline-first spec）。
