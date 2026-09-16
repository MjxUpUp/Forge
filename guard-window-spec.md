# 提案+规格：rotate 并发守卫窗口放宽

## 提案

release 分支 CI 慢 windows runner 上 TestRecord_ConcurrentRotateNoDeadlock
两次撞 30s 守卫（30.53s/34.92s，同代码 main 三平台绿过）——runner 负载波动
非回归；死锁守卫价值在「拦永不完成」，延迟只换 flake 风险。

## 规格

守卫窗口 30s → 90s，注释带两次实录数据。

## 验收条件

- 本机测试绿
- 注释含实录依据
