#!/bin/bash
# assertion-check.sh — PreToolUse hook for Write|Edit.
# Two modes: per-file (FORGE_FILE_PATH set) or batch (checks all git diffs).
set -eo pipefail

ROOT="${1:-.}"
cd "$ROOT" 2>/dev/null || exit 0

FILE_PATH="${FORGE_FILE_PATH:-}"
CONTENT="${FORGE_CONTENT:-}"
VIOLATIONS=""

# --- Per-file mode (hook-triggered by Claude Code) ---
if [ -n "$FILE_PATH" ]; then
# Only check source code files
printf '%s' "$FILE_PATH" | grep -qE '\.(go|rs|ts|tsx|js|jsx|py|java|rb|zig|nim)$' || exit 0

# Only check test files
printf '%s' "$FILE_PATH" | grep -qE '(_test\.|_spec\.|\.test\.|\.spec\.|test/|tests/|__tests__/)' || exit 0

# Go: t.Skip / t.Skipf added
printf '%s' "$CONTENT" | grep -qE 't\.Skip(f)?\(' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[Go] t.Skip found. "

# Rust: #[ignore] added
printf '%s' "$CONTENT" | grep -qE '#\[ignore\]' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[Rust] #[ignore] found. "

# TypeScript/JavaScript: test.skip / it.skip / describe.skip
printf '%s' "$CONTENT" | grep -qE '(test|it|describe)\.skip\(' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[TS/JS] test/it/describe.skip found. "

printf '%s' "$CONTENT" | grep -qE '\bx(it|describe)\(' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[TS/JS] xit/xdescribe found. "

# Python: unittest.skip / pytest.mark.skip
printf '%s' "$CONTENT" | grep -qE '@(unittest\.skip|pytest\.mark\.skip)' 2>/dev/null && \
  VIOLATIONS="${VIOLATIONS}[Python] skip decorator found. "
fi

# --- Diff mode (batch gate check + per-file fallback) ---
if git rev-parse --git-dir >/dev/null 2>&1; then
  check_diff() {
    local diff="$1"
    local label="$2"
    [ -z "$diff" ] && return
    printf '%s' "$diff" | grep -qE '^\-.*\bt\.Fatal(f)?\(' 2>/dev/null && \
      VIOLATIONS="${VIOLATIONS}[Go] t.Fatal removed in ${label}. "
    printf '%s' "$diff" | grep -qE '^\-.*\bassert(_eq|_ne)?!\(' 2>/dev/null && \
      VIOLATIONS="${VIOLATIONS}[Rust] assert! removed in ${label}. "
    printf '%s' "$diff" | grep -qE '^\+.*\bt\.Skip(f)?\(' 2>/dev/null && \
      VIOLATIONS="${VIOLATIONS}[Go] t.Skip added in ${label}. "
    printf '%s' "$diff" | grep -qE '^\+.*#\[ignore\]' 2>/dev/null && \
      VIOLATIONS="${VIOLATIONS}[Rust] #[ignore] added in ${label}. "
    printf '%s' "$diff" | grep -qE '^\+.*\b(test|it|describe)\.skip\(' 2>/dev/null && \
      VIOLATIONS="${VIOLATIONS}[TS/JS] .skip() added in ${label}. "
    : # always return 0 — grep misses are not errors
  }

  CODE_FILES=$( (git diff --cached --name-only 2>/dev/null; git diff --name-only 2>/dev/null) | sort -u | grep -E '(_test\.|_spec\.|\.test\.|\.spec\.|test/|tests/)' | grep -E '\.(go|rs|ts|tsx|js|jsx)$' || true)
  if [ -n "$CODE_FILES" ]; then
    STAGED_DIFF=$(git diff --cached -- $CODE_FILES 2>/dev/null || true)
    check_diff "$STAGED_DIFF" "staged diff" || true
    UNSTAGED_DIFF=$(git diff -- $CODE_FILES 2>/dev/null || true)
    check_diff "$UNSTAGED_DIFF" "unstaged diff" || true
  fi
fi

if [ -n "$VIOLATIONS" ]; then
  echo "FAIL Assertion weakening detected: ${VIOLATIONS}Fix the code, not the tests."
  exit 1
else
  echo "PASS"
fi
