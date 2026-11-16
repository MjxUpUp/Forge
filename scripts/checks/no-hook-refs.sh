#!/bin/sh
# 阶段 D 守卫：cli.md 与 README 的「当前用法」不得再引用已删除的 `forge hook` 命令。
# 例外：「从 1.84 及更早版本升级」一节讲的是清理旧残留，本来就该出现 `forge hook`
# （它教用户去旧配置里搜这个字符串），不算过期引用。
set -eu
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
awk '
  /^## .*1\.84/ { skip = 1; next }
  /^## / { skip = 0 }
  !skip { print }
' README.md > "$tmp"

if grep -q "forge hook " docs/reference/cli.md "$tmp"; then
  echo "found stale forge hook reference (outside the 1.84 migration section)"
  exit 1
fi
exit 0