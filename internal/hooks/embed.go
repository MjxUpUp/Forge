package hooks

// Embedded hook scripts for forge init.
// These are written to .forge/hooks/ during project initialization.

const AutoCompileHook = `#!/bin/bash
# auto-compile.sh — runs on Write/Edit to ensure code compiles.
set -euo pipefail
ROOT="${1:-.}"
cd "$ROOT" 2>/dev/null || exit 0

if [ -f "go.mod" ]; then
  echo "[auto-compile] go build ./..."
  go build ./...
elif [ -f "Cargo.toml" ]; then
  echo "[auto-compile] cargo check"
  cargo check 2>&1
elif [ -f "package.json" ]; then
  if [ -f "tsconfig.json" ]; then
    echo "[auto-compile] tsc --noEmit"
    npx tsc --noEmit 2>&1
  else
    echo "[auto-compile] No TypeScript, skipping compile check."
  fi
else
  echo "[auto-compile] No recognized build system, skipping."
fi
`

const AssertionCheckHook = `#!/bin/bash
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
`

const ExperienceCheckHook = `#!/bin/bash
# experience-check.sh — scans code for known gotcha patterns.
set -eo pipefail
ROOT="${1:-.}"
cd "$ROOT" 2>/dev/null || exit 0

KNOWLEDGE_DIR="${HOME}/.forge/knowledge/gotchas"
[ ! -d "$KNOWLEDGE_DIR" ] && exit 0

VIOLATIONS=0
for f in "$KNOWLEDGE_DIR"/*.md; do
  [ -f "$f" ] || continue
  patterns=$(sed -n 's/.*Patterns:\*\* //p' "$f" 2>/dev/null | tr ',' '\n' | sed 's/^ *//;s/ *$//')
  [ -z "$patterns" ] && continue
  while IFS= read -r pattern; do
    [ -z "$pattern" ] || continue
    matches=$(grep -rn "$pattern" --include="*.go" --include="*.rs" --include="*.ts" --include="*.ets" . 2>/dev/null | grep -v node_modules | grep -v '.git/' | grep -v '.min.' | head -3)
    if [ -n "$matches" ]; then
      echo "$matches" >&2
      VIOLATIONS=$((VIOLATIONS + 1))
    fi
  done <<< "$patterns"
done

[ $VIOLATIONS -gt 0 ] && echo "$VIOLATIONS violation(s) found" >&2 && exit 1
echo "[experience-check] All clear."
exit 0
`

const TaskVerifyHook = `#!/bin/bash
# task-verify.sh — runs task-level verification on session stop.
# Also hints about pending mandatory reviews.
set -eo pipefail
forge task gate task-verify --silent 2>/dev/null || true

# Check for pending mandatory reviews
forge experience list 2>/dev/null | grep -q "mandatory.*pending" && \
  echo "⚠ Pending mandatory review detected. Run 'forge experience list' for details." >&2 || true
exit 0
`
