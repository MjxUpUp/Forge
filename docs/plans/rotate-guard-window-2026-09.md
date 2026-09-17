# 提案+规格：rotate 并发守卫窗口放宽

> 状态：已实施（ada69f2，spec+代码+守卫注释一体合入 main）

## 提案

release 分支 CI（2026-09-16）慢 windows runner 上 TestRecord_ConcurrentRotateNoDeadlock
两次撞 30s 守卫（30.53s/34.92s，实录见 internal/checklog/store_test.go:285-289
守卫注释；同代码 main 三平台——linux/mac/windows——绿过）——runner 负载波动
非回归；死锁守卫价值在「拦永不完成」，延迟只换 flake 风险。

## 规格

守卫窗口 30s → 90s（internal/checklog/store_test.go:290），注释带两次实录数据。

## 验收（实测）

- `go test ./internal/checklog -run TestRecord_ConcurrentRotateNoDeadlock`
  本机 PASS（2026-09-17，8.56s——新窗口余量充足）
- 注释含实录依据（store_test.go:285-289：30.53s/34.92s，2026-09-16）
