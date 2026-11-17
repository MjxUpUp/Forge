#!/usr/bin/env bash
# 发布物行为级冒烟：在一个隔离的临时仓库里，用**待发布的二进制**真实走一遍
# 「建 change → 填产物 → verify 封印 → change check」。
#
# 为什么需要它：`forge change check` 只校验「本仓库未归档 change 的证据」——
# 在本仓库（change 全归档后）它会输出「没有未归档的 change」并成功退出，等于
# 空跑，不能证明发布物能跑。真正的发布冒烟必须自己造一个 change 再验证。
#
# 用法：sh scripts/checks/release-smoke.sh <forge 二进制路径或 PATH 上的命令名>
# 退出码：0 = 发布物能建 change、能封印证据、check 认账；非 0 = 发布物坏。
set -euo pipefail

BIN="${1:-./forge}"
# `forge` 这类裸命令名按 PATH 解析（CI 里 npm i -g 装出来的就在 PATH 上）；
# 带路径的仍按文件判断。
if ! resolved="$(command -v -- "$BIN" 2>/dev/null)"; then
  echo "release-smoke: 找不到可执行文件: $BIN" >&2
  exit 2
fi
BIN="$resolved"
if [ ! -x "$BIN" ]; then
  echo "release-smoke: 找不到可执行文件: $BIN" >&2
  exit 2
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cd "$WORK"

git init -q .
git config user.email smoke@example.com
git config user.name "release smoke"

"$BIN" change init >/dev/null
"$BIN" change new smoke --title "release smoke" >/dev/null

cat > spec/changes/smoke/goal.md <<'EOF'
# smoke

## Objective

Prove the released binary can create and verify a change.

## Out of Scope

Nothing outside this temp repo.

## Check

The acceptance command exits 0 after verify seals evidence.
EOF

cat > spec/changes/smoke/change.md <<'EOF'
# smoke

## 改动

This file exists only so the change has a filled change.md.

## 影响

No product code: the smoke runs in a throwaway temp repo.

## 验证

`forge change verify smoke` runs the acceptance fence below.
EOF

cat > spec/changes/smoke/acceptance.md <<'EOF'
# smoke

```bash
echo smoke-ok :: smoke-ok
```
EOF

"$BIN" change clarify smoke --by-human --note "release smoke: throwaway temp repo, no product behavior" >/dev/null

phase="$("$BIN" change phase smoke 2>&1 || true)"
case "$phase" in
  *ready*|*verified*) ;;
  *)
    echo "release-smoke: change 没走到 ready，phase 输出:" >&2
    echo "$phase" >&2
    exit 1
    ;;
esac

"$BIN" change verify smoke >/dev/null
"$BIN" change check >/dev/null
"$BIN" change status smoke | grep -q "verified" || {
  echo "release-smoke: verify 后 status 未显示 verified" >&2
  exit 1
}

echo "release-smoke: ok（建 change → 填产物 → verify 封印 → change check）"