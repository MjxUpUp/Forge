#!/bin/sh
# forge-ruthless：被删掉的检查/命令不许在文档或代码里复活。
# 命中即 exit 1；这是守卫，不是语义检查——只钉「这些名字不该再出现」。
set -eu
cd "$(dirname "$0")/../.."

bad=0
for pat in "task doc-review" "forge task mutation" "artifact-chain" "unused-scan" "test-coverage-gate" "self-refresh" "forge selfcheck"; do
  if grep -RIn -e "$pat" internal/change/templates docs/reference README.md 2>/dev/null \
      | grep -v "scripts/checks/no-removed-checks.sh" \
      | grep -v "spec/changes/archive/" >/tmp/forge-removed.txt; then
    echo "✗ 仍出现 [$pat]：" >&2
    head -5 /tmp/forge-removed.txt >&2
    bad=1
  fi
done
if [ "$bad" -ne 0 ]; then
  echo "no-removed-checks: 失败（forge-ruthless 删除项复活）" >&2
  exit 1
fi
echo "no-removed-checks: ok"