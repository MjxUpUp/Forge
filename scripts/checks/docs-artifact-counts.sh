#!/bin/sh
# 守卫：change 产物数量的文档口径。
#
# 背景：goal.md 成为第五份产物后，文档里「四份产物」「四份模板」的旧说法会让
# agent 少填 goal.md；只 grep 某个新词（如「五份模板」）证明不了其余文件没说错。
# 这里同时做正向与反向检查：关键文件必须写到「五份」，且任何被检查文件都不得
# 再出现「四份产物 / 四个产物 / 四份模板」。
set -eu

files="README.md docs/reference/cli.md internal/change/templates/goal.md internal/change/templates/agents-protocol.md internal/change/templates/proposal.md internal/change/templates/design.md internal/change/templates/tasks.md internal/change/templates/acceptance.md"

for f in $files; do
  if [ ! -f "$f" ]; then
    printf 'docs-artifact-counts: 缺文件 %s\n' "$f"
    exit 1
  fi
done

# 正向：README 与 cli.md 是入口文档，必须明确写「五份」。
for f in README.md docs/reference/cli.md; do
  if ! grep -q '五份' "$f"; then
    printf 'docs-artifact-counts: %s 未提到「五份」产物\n' "$f"
    exit 1
  fi
done

# 入口文档必须点名 goal.md（只写「五份」而不说 goal，读者仍不知道第五份是什么）。
for f in README.md docs/reference/cli.md; do
  if ! grep -q 'goal.md' "$f"; then
    printf 'docs-artifact-counts: %s 未点名 goal.md\n' "$f"
    exit 1
  fi
done

# 反向：不得再出现四份产物的旧口径。
if grep -n -E '四份产物|四个产物|四份模板' $files; then
  printf 'docs-artifact-counts: 上面这些行仍是旧口径（应为五份）\n'
  exit 1
fi

exit 0