#!/bin/sh
# forge-ruthless：被删掉的检查/命令不许在文档或协议模板里复活。
# 命中即 exit 1；这是守卫，不是语义检查——只钉「这些名字不该再出现」。
set -eu
cd "$(dirname "$0")/../.."

targets="internal/change/templates docs/reference README.md"
for dir in $targets; do
  if [ ! -e "$dir" ]; then
    echo "no-removed-checks: 扫描目标不存在：$dir" >&2
    exit 1
  fi
done

pat="task doc-review|forge task mutation|forge selfcheck|task gate task-verify|--acknowledge-changes|artifact-chain|unused-scan|test-coverage-gate|self-refresh"
if grep -RIn -E "$pat" $targets 2>/tmp/forge-removed.err >/tmp/forge-removed.txt; then
  echo "✗ 仍出现已删除项的引用：" >&2
  head -10 /tmp/forge-removed.txt >&2
  exit 1
fi
if [ -s /tmp/forge-removed.err ]; then
  echo "no-removed-checks: grep 失败：" >&2
  cat /tmp/forge-removed.err >&2
  exit 1
fi
echo "no-removed-checks: ok"