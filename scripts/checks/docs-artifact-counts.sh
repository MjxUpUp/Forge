#!/bin/sh
# 守卫：change 产物数量的文档口径（1.88 激进减法后：三份）。
#
# 背景：goal 三段 + change.md + acceptance 成为默认布局，proposal/design 不再生成。
# 文档里「四份 / 五份产物」的旧说法会让 agent 少填 change.md 或去找不存在的模板；
# 只 grep 某个新词证明不了其余文件没说错。这里同时做正向与反向检查。
set -eu

files="README.md docs/reference/cli.md internal/change/templates/goal.md internal/change/templates/agents-protocol.md internal/change/templates/acceptance.md"

for f in $files; do
  if [ ! -f "$f" ]; then
    printf 'docs-artifact-counts: 缺文件 %s\n' "$f"
    exit 1
  fi
done

# 入口文档必须写「三份」，并点名三份产物是什么。
for f in README.md docs/reference/cli.md internal/change/templates/agents-protocol.md; do
  if ! grep -q '三份' "$f"; then
    printf 'docs-artifact-counts: %s 未提到「三份」产物\n' "$f"
    exit 1
  fi
done

for f in README.md docs/reference/cli.md; do
  for want in goal.md change.md acceptance.md; do
    if ! grep -q "$want" "$f"; then
      printf 'docs-artifact-counts: %s 未点名 %s\n' "$f" "$want"
      exit 1
    fi
  done
done

# 反向：不得再出现四份/五份产物的旧口径，也不得再引用已删除的模板文件。
if grep -n -E '四份产物|四个产物|五份产物|五个产物|五份模板|四份模板' $files; then
  printf 'docs-artifact-counts: 上面这些行仍是旧口径（应为三份）\n'
  exit 1
fi
if grep -n -E 'templates/(proposal|design)\.md' $files; then
  printf 'docs-artifact-counts: 上面这些行引用了已删除的模板\n'
  exit 1
fi

exit 0