#!/bin/bash
# assertion-check.sh — blocks commits where test assertions were weakened.
# Only scans source code files to avoid false positives from docs/configs.
set -euo pipefail
ROOT="${1:-.}"
cd "$ROOT" 2>/dev/null || exit 0

git rev-parse --git-dir 2>/dev/null || exit 0

# Only check staged source code files
CODE_FILES=$(git diff --cached --name-only 2>/dev/null | grep -E '\.(go|rs|ts|tsx|js|jsx|py|java|rb|zig|nim)$' || true)
[ -z "$CODE_FILES" ] && exit 0

DIFF=$(git diff --cached -- $CODE_FILES 2>/dev/null || true)
[ -z "$DIFF" ] && exit 0

VIOLATIONS=""

echo "$DIFF" | grep -qE '^\-.*\bt\.Fatal(f)?\(' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[Go] t.Fatal/t.Fatalf removed\n"

echo "$DIFF" | grep -qE '^\+.*\bt\.Skip(f)?\(' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[Go] t.Skip added\n"

echo "$DIFF" | grep -qE '^\-.*\bassert(_eq|_ne)?!\(' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[Rust] assert! removed\n"

echo "$DIFF" | grep -qE '^\+.*#\[ignore\]' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[Rust] #[ignore] added\n"

if [ -n "$VIOLATIONS" ]; then
  echo "Assertion weakening detected:" >&2
  printf "%b" "$VIOLATIONS" >&2
  echo "Fix the code, not the tests." >&2
  exit 1
fi
exit 0
