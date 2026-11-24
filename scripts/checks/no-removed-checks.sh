#!/usr/bin/env bash
# no-removed-checks: 禁止在用户可见文本里重新引入已删除的门禁/命令。
#
# 用法：no-removed-checks.sh [path...]
# 不带参数时扫描默认目标集合；带参数时只扫给定路径（目录会递归）。
# 退出码：0 = 干净；1 = 命中；2 = 目标不存在或扫描失败（不静默通过）。
#
# 范围：用户**当前**可见的指引——README、docs/reference、模板、skills、notes。
# `docs/design`、`docs/adr`、`docs/plans` 是设计档案：它们记录的是「当时怎么设计的」，
# 里面有大量用现在时描述已删机制的段落，改写它们等于篡改历史记录。这些目录不在
# 本守卫范围内，也不假装别处有检查兜底——它们由评审按「历史文档」读。
#
# 例外：同一行里出现**明确的移除措辞**（已删除/已移除/已下线/已退役/已随）才免检——
# 只出现泛泛的「删除」二字不算（那可能是「删除这个文件」之类的正常操作说明）；
# 也**不**把「不再」「不再支持」算作豁免（那是文档高频词，会把「运行 forge task
# mutation，失败后不再重试」这种当前指引放过去）。判定逐行做（awk）。

set -euo pipefail

PATTERN='task doc-review|forge task doc-review|task mutation|forge task mutation|forge selfcheck|task-verify|task gate task-verify|doc gate|--doc-gate|--test-coverage|--acknowledge-changes|artifact-chain|artifact-drill|unused-scan|test-coverage-gate|self-refresh|FORGE_ARTIFACT_CHAIN|FORGE_UNUSED_SCAN|FORGE_DOC_GATE|task artifact --extract|task artifact --set|task artifact --check|task artifact --verify|chain-init|plan-first'

if [ "$#" -eq 0 ]; then
  set -- README.md READMEs/README.zh-CN.md npm/README.md CONTRIBUTING.md RELEASE.md skills \
    docs/reference internal/change/templates skills-forge
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
    # 逐行判定：行内出现明确的移除措辞即免检（只豁免这一行，不是整段/整文件）。
    # 用 grep 而不是 awk：gawk 在 LC_ALL=C 下对多字节中文正则可能匹配不到（实测
    # 「已随」被判不匹配），grep 按字节比较稳定。
    hits="$(printf '%s\n' "$hits" | grep -vE 'decisions\.md:|已删除|已移除|已下线|已退役|已随')" || true
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