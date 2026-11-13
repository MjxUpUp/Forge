#!/bin/sh
# 阶段 D 守卫：deadcode 必须零发现。
# 先看退出码：go run 失败（模块解析/网络/编译错）时 stdout 为空，旧写法会把它
# 当「零发现」放行（静默假绿）。工具真跑通且零输出才算通过。
if ! out=$(go run golang.org/x/tools/cmd/deadcode@v0.49.0 -test ./... 2>&1); then
  printf 'deadcode-zero: `go run deadcode` 执行失败（不是零发现）\n%s\n' "$out"
  exit 1
fi
if [ -n "$out" ]; then
  printf '%s\n' "$out"
  exit 1
fi
exit 0
