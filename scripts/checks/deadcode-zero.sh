#!/bin/sh
# 阶段 D 守卫：deadcode 必须零发现。
out=$(go run golang.org/x/tools/cmd/deadcode@v0.49.0 -test ./...)
if [ -n "$out" ]; then
  printf '%s\n' "$out"
  exit 1
fi
exit 0
