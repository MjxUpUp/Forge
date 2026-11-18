#!/usr/bin/env sh
# 红先绿 e2e：在临时仓库里证明「先失败证明」与「评审者复用」两条语义都真的生效。
#
# 覆盖：
#   1. task start --accept-red 接受一条开工时失败的判据（红快照）；
#   2. 红判据开工时已通过 → 拒绝（先失败证明不成立）；
#   3. 判据转绿后 verify-acceptance 通过、task complete 放行（红→绿闭环）；
#   4. 复审必须由同一个 reviewer 承接：换 id 被拒，同 id 放行；
#   5. report 披露「先失败证明」与「评审连续性」。
#
# 用法：sh scripts/checks/red-first-e2e.sh <forge 二进制|PATH 上的命令名>
set -eu

BIN=${1:-forge}
if ! command -v -- "$BIN" >/dev/null 2>&1 && [ ! -x "$BIN" ]; then
  echo "red-first-e2e: 找不到可执行文件: $BIN" >&2
  exit 2
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export FORGE_DATA_HOME="$work/forge-data"

cd "$work"
git init -q
git config user.email "e2e@example.com"
git config user.name "e2e"
git config commit.gpgsign false

"$BIN" change init >/dev/null

git add -A
git commit -qm init

# 红线：判据开工时已经通过 → 拒绝。
if out=$("$BIN" task start --ref feat/bad-red --branch --accept "true ::" --accept-red "true" 2>&1); then
  echo "red-first-e2e: 开工即绿的 --accept-red 应被拒绝，却通过了：$out" >&2
  exit 1
fi
case "$out" in
  *先失败证明不成立*) : ;;
  *) echo "red-first-e2e: 拒绝理由不对：$out" >&2; exit 1 ;;
esac
# 失败的 start 已经切过分支（分支留着、任务没建），回到主干再继续。
git checkout -q - 2>/dev/null || git checkout -q master
git branch -D feat/bad-red >/dev/null 2>&1 || true

# 正路：判据 `test -f feature.txt` 开工时失败（红），补齐文件后转绿。
"$BIN" task start --ref feat/red --branch \
  --accept "test -f feature.txt ::" \
  --accept-red "test -f feature.txt" >/dev/null

printf 'red-first e2e\n' > feature.txt
git add -A
git commit -qm "feat: add feature"
"$BIN" task gate task-implement >/dev/null
"$BIN" task verify-acceptance >/dev/null

# 评审连续性：首轮绑定评审者，源码变更后换 id 被拒、同 id 放行。
"$BIN" review pass --ref feat/red --reviewer e2e-reviewer --score 80 >/dev/null
printf '// touched\n' >> feature.txt
git add -A
git commit -qm "fix: touch"
"$BIN" task verify-acceptance >/dev/null
if out=$("$BIN" review pass --ref feat/red --reviewer e2e-other 2>&1); then
  echo "red-first-e2e: 换 reviewer 应被拒绝，却通过了：$out" >&2
  exit 1
fi
"$BIN" review pass --ref feat/red --reviewer e2e-reviewer --score 82 >/dev/null

"$BIN" task gate task-complete >/dev/null
"$BIN" task complete >/dev/null

report=$("$BIN" task report --ref feat/red 2>&1)
case "$report" in
  *"先失败证明：1 条"*) : ;;
  *) echo "red-first-e2e: report 缺少先失败证明：$report" >&2; exit 1 ;;
esac
case "$report" in
  *"评审连续性：e2e-reviewer"*) : ;;
  *) echo "red-first-e2e: report 缺少评审连续性：$report" >&2; exit 1 ;;
esac

echo "red-first-e2e: ok（红→绿闭环、换人拒绝、同人复审放行、report 两行披露）"