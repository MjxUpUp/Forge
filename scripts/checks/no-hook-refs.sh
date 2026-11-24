#!/bin/sh
# 阶段 D 守卫：cli.md 与 README 的「当前用法」不得再引用已删除的 `forge hook` 命令。
# 例外：讲历史迁移的段落本来就该出现 `forge hook`（教用户去旧配置里搜这个字符串），
# 不算过期引用。迁移段按标题里的版本号识别（如「从 1.84 升级」「1.85–1.9x 升级到 2.0」
# 「升级到 4.0」）——用 ASCII 锚点而非中文词，避免 awk 在 C locale 下匹配不了多字节。
# 迁移手册本身在 docs/migration/，不在本脚本的扫描面内。
set -eu
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
awk '
  /^## / {
    skip = ($0 ~ /[0-9]\.[0-9]/)
  }
  !skip { print }
' README.md > "$tmp"

if grep -q "forge hook " docs/reference/cli.md "$tmp"; then
  echo "found stale forge hook reference (outside the versioned migration sections)"
  exit 1
fi
exit 0
