#!/bin/sh
# 阶段 D 守卫：cli.md 与 README.md 不得再引用已删除的 `forge hook` 命令。
if grep -rq "forge hook " docs/reference/cli.md README.md; then
  echo "found stale forge hook reference"
  exit 1
fi
exit 0
