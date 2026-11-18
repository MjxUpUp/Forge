#!/usr/bin/env sh
# 红先绿 e2e：在临时仓库里证明「先失败证明」与「评审者复用」两条语义都真的生效。
#
# 覆盖：
#   1. task start --accept-red 接受一条开工时失败的判据（红快照）；
#   2. 红判据开工时已通过 → 拒绝（先失败证明不成立）；
#   3. 判据转绿后 verify-acceptance 通过、task complete 放行（红→绿闭环）；
#   4. 复审必须由同一个 reviewer 承接：换 id 被拒，同 id 放行；
#   5. report 披露「先失败证明」与「评审连续性」；
#   6. 红判据一直没转绿 → task complete 被拒（负例，守住 complete 真的调用了检查）。
#
# 用法：sh scripts/checks/red-first-e2e.sh <forge 二进制|PATH 上的命令名>
set -eu

BIN=${1:-forge}
if ! command -v -- "$BIN" >/dev/null 2>&1 && [ ! -x "$BIN" ]; then
  echo "red-first-e2e: 找不到可执行文件: $BIN" >&2
  exit 2
fi

work=$(mktemp -d)
data=$(mktemp -d)
# Windows 上临时目录偶尔有句柄未释放，清理失败不能把 e2e 判成失败。
trap 'rm -rf "$work" "$data" 2>/dev/null || true' EXIT
# 数据目录必须在仓库外：写进仓库会被 `git add -A` 收进去，切分支时报「local changes
# would be overwritten」，e2e 自己就把自己绊倒。
export FORGE_DATA_HOME="$data"

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

git checkout -q master

# 负例（review-rfe-1 P1-b）：红判据一直没转绿 → task complete 必须被拒。
# 没有这一条，一个「complete 根本不检查红先绿」的二进制也能让整卷验收全绿。
"$BIN" task start --ref feat/red-unclosed --branch \
  --accept "test -f feature.txt ::" \
  --accept-red "test -f never.txt" >/dev/null
printf 'more\n' >> feature.txt
git add -A
git commit -qm "feat: more"
"$BIN" task gate task-implement >/dev/null
"$BIN" task verify-acceptance >/dev/null
"$BIN" review pass --ref feat/red-unclosed --reviewer e2e-reviewer2 --score 80 >/dev/null
"$BIN" task gate task-complete >/dev/null
if out=$("$BIN" task complete 2>&1); then
  echo "red-first-e2e: 红判据未转绿时 task complete 应被拒绝，却通过了：$out" >&2
  exit 1
fi
case "$out" in
  *未闭合*) : ;;
  *) echo "red-first-e2e: complete 的拒绝理由不对：$out" >&2; exit 1 ;;
esac

report=$("$BIN" task report --ref feat/red 2>&1)
case "$report" in
  *"先失败证明：1 条"*) : ;;
  *) echo "red-first-e2e: report 缺少先失败证明：$report" >&2; exit 1 ;;
esac
case "$report" in
  *"评审连续性：e2e-reviewer"*) : ;;
  *) echo "red-first-e2e: report 缺少评审连续性：$report" >&2; exit 1 ;;
esac


# go test 判据的红→绿闭环：start 会把带 expected 的 `go test ./...` 改写成
# `go test -v ./...`，而 --accept-red 原样记录——不归一化就会在 complete 处死锁
# （review-rfe-1 P1-c）。这里真跑一遍，保证归一化不是单测里的自说自话。
gwork="$work/gotest"
mkdir -p "$gwork"
(
  cd "$gwork" || exit 1
  git init -q . && git config user.email e2e@example.com && git config user.name e2e
  printf 'module example.com/gv\n\ngo 1.21\n' > go.mod
  printf 'package gv\n' > gv.go
  printf 'package gv\n\nimport "testing"\n\nfunc TestBroken(t *testing.T) { t.Fatal("red on purpose") }\n' > gv_test.go
  FORGE_DATA_HOME="$data" "$BIN" init >/dev/null
  git add -A && git commit -qm red
  FORGE_DATA_HOME="$data" "$BIN" task start --ref feat/gv --branch --accept "go test ./... :: PASS" --accept-red "go test ./..." >/dev/null
  printf 'package gv\n' > gv.go
  printf 'package gv\n\nimport "testing"\n\nfunc TestBroken(t *testing.T) {}\n' > gv_test.go
  git add . && git commit -qm "green"
  FORGE_DATA_HOME="$data" "$BIN" task gate task-implement >/dev/null
  FORGE_DATA_HOME="$data" "$BIN" task verify-acceptance >/dev/null
  FORGE_DATA_HOME="$data" "$BIN" review pass --ref feat/gv --reviewer e2e-gv >/dev/null
  FORGE_DATA_HOME="$data" "$BIN" task gate task-complete >/dev/null
  FORGE_DATA_HOME="$data" "$BIN" task complete >/dev/null
) || { echo "red-first-e2e: go test 判据的红→绿闭环失败（-v 归一化）"; exit 1; }

echo "red-first-e2e: ok（红→绿闭环、换人拒绝、同人复审放行、report 两行披露）"