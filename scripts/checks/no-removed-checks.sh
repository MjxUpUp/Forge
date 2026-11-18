#!/usr/bin/env bash
# no-removed-checks: 禁止在用户可见文本里重新引入已删除的门禁/命令。
#
# 用法：no-removed-checks.sh [path...]
# 不带参数时扫描默认目标集合；带参数时只扫给定路径（目录会递归）。
# 退出码：0 = 干净；1 = 命中；2 = 目标不存在或扫描失败（不静默通过）。
#
# 范围：用户**当前**可见的指引——README、docs/reference、模板、skills、notes。
# `docs/design`、`docs/adr`、`docs/plans` 是设计档案（记录当时机制、不改写历史），
# 不在此守卫范围内；其中若出现「照抄就会误导」的命令，由评审与 cli 契约测试兜住。
#
# 例外：同一行里出现**明确的移除措辞**（已删除/已移除/已下线/已退役/已随/不再）
# 才算解释历史——只出现泛泛的「删除」二字不算（那可能是「删除这个文件」之类的
# 正常操作说明）。判定逐行做（awk），不接受「整段因为出现过一次删除就全部豁免」。

set -euo pipefail

PATTERN='task doc-review|forge task doc-review|task mutation|forge task mutation|forge selfcheck|task-verify|task gate task-verify|doc gate|--doc-gate|--test-coverage|--acknowledge-changes|artifact-chain|artifact-drill|unused-scan|test-coverage-gate|self-refresh|FORGE_ARTIFACT_CHAIN|FORGE_UNUSED_SCAN|FORGE_DOC_GATE|task artifact --extract|task artifact --set|task artifact --check|task artifact --verify|chain-init|plan-first'

if [ "$#" -eq 0 ]; then
  set -- README.md READMEs/README.zh-CN.md npm/README.md CONTRIBUTING.md RELEASE.md skills \
    docs/reference internal/change/templates internal/skillintegrate/notes skills-forge
  # decisions.md 是历史决策日志，允许记录当时存在的机制。
fi

status=0
for target in "$@"; do
  if [ ! -e "$target" ]; then
    echo "no-removed-checks: 目标不存在: $target" >&2
    status=2
    continue
  fi
  # grep 退出码：0=命中，1=无命中，>1=真错误（权限/坏路径）。要把 >1 当失败，
  # 不能 `|| true` 吞掉——否则扫描失败会静默通过。
  set +e
  hits="$(grep -rnE -- "$PATTERN" "$target" 2>/dev/null)"
  grep_rc=$?
  set -e
  if [ "$grep_rc" -gt 1 ]; then
    echo "no-removed-checks: 扫描失败（grep exit $grep_rc）: $target" >&2
    status=2
    continue
  fi
  if [ -n "$hits" ]; then
    # 逐行判定：删除词 + 历史语境同时出现才算「解释历史」（AND，不是 OR）。
    hits="$(printf '%s\n' "$hits" | awk '
      /decisions\.md:/ { next }
      {
        removal = ($0 ~ /已删除|已移除|已下线|已退役|已随|不再/)
        if (removal) next
        print
      }')" || true
  fi
  if [ -n "$hits" ]; then
    printf '%s\n' "$hits"
    echo "no-removed-checks: 上面这些位置还在引用已删除的门禁/命令（$target）" >&2
    status=1
  fi
done

if [ "$status" -eq 0 ]; then
  echo "no-removed-checks: ok"
fi
exit "$status"