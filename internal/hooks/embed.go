package hooks

// Embedded hook scripts for forge init.
// These are written to .forge/hooks/ during project initialization.
//
// Protocol: bash scripts output plain text to stdout.
// - Line starting with "PASS" = check passed, rest of line is optional detail.
// - Line starting with "FAIL" = check failed, rest of line is the reason.
// - If multiple lines, the LAST PASS/FAIL line determines the result.
// - Any output to stderr is captured for debugging.
// Go wraps the result into structured JSON for Claude Code.
//
// Go extracts tool_input fields into env vars (FORGE_FILE_PATH, FORGE_CONTENT,
// FORGE_TOOL_NAME) so bash scripts don't need to parse JSON.

const AutoCompileHook = `#!/bin/bash
# auto-compile.sh — PostToolUse hook for Write|Edit.
# Runs all applicable compilers independently (no elif chain).
set -euo pipefail

ROOT="${1:-.}"
cd "$ROOT" 2>/dev/null || exit 0

# No recognized build system — nothing to check.
if [ ! -f "go.mod" ] && [ ! -f "Cargo.toml" ] && ! { [ -f "package.json" ] && [ -f "tsconfig.json" ]; }; then
  exit 0
fi

RESULTS=""
PASS=true

# Check each build system independently — polyglot projects need all of them.
# head -20 limits compiler output to prevent ARG_MAX overflow.
if [ -f "go.mod" ]; then
  MSG=$(go build ./... 2>&1 | head -20) || { PASS=false; RESULTS="${RESULTS}[go] FAILED: ${MSG}"$'\n'; }
fi

if [ -f "Cargo.toml" ]; then
  MSG=$(cargo check 2>&1 | head -20) || { PASS=false; RESULTS="${RESULTS}[cargo] FAILED: ${MSG}"$'\n'; }
fi

if [ -f "package.json" ] && [ -f "tsconfig.json" ]; then
  MSG=$(npx tsc --noEmit 2>&1 | head -20) || { PASS=false; RESULTS="${RESULTS}[tsc] FAILED: ${MSG}"$'\n'; }
fi

if $PASS; then
  # Self-bootstrap: if this is the forge project itself, update the running binary
  if [ -f "go.mod" ] && head -1 go.mod | grep -q "github.com/Harness/forge" 2>/dev/null; then
    FORGE_CMD=$(command -v forge 2>/dev/null || true)
    FORGE_BIN=""
    if [ -n "$FORGE_CMD" ]; then
      FORGE_DIR=$(dirname "$FORGE_CMD")
      # npm wrapper: binary is at <dir>/node_modules/@agentfare/forge/bin/forge[.exe]
      for candidate in "${FORGE_DIR}/node_modules/@agentfare/forge/bin/forge.exe" "${FORGE_DIR}/node_modules/@agentfare/forge/bin/forge"; do
        if [ -f "$candidate" ]; then FORGE_BIN="$candidate"; break; fi
      done
    fi
    # Fallback: GOPATH/bin
    if [ -z "$FORGE_BIN" ]; then
      GOBIN=$(go env GOPATH 2>/dev/null)/bin/forge
      [ -f "$GOBIN.exe" ] && FORGE_BIN="$GOBIN.exe"
      [ -f "$GOBIN" ] && FORGE_BIN="$GOBIN"
    fi
    if [ -n "$FORGE_BIN" ]; then
      go build -o "$FORGE_BIN" ./cmd/forge 2>/dev/null || true
    fi
  fi
  echo "PASS [auto-compile] All builds passed."
else
  echo "FAIL [auto-compile] Build failures detected:"
  printf '%s' "$RESULTS"
  exit 1
fi
`

const AssertionCheckHook = `#!/bin/bash
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
`

const ExperienceCheckHook = `#!/bin/bash
# experience-check.sh — PreToolUse hook for Write|Edit.
# Checks file content against known gotcha patterns from knowledge base.
set -eo pipefail

ROOT="${1:-.}"
cd "$ROOT" 2>/dev/null || exit 0

KNOWLEDGE_DIR="${HOME}/.forge/knowledge/gotchas"
[ ! -d "$KNOWLEDGE_DIR" ] && exit 0

FILE_PATH="${FORGE_FILE_PATH:-}"
CONTENT="${FORGE_CONTENT:-}"

# Only check source code files
printf '%s' "$FILE_PATH" | grep -qE '\.(go|rs|ts|tsx|js|jsx|py|java|rb|zig|nim)$' || exit 0

[ -z "$CONTENT" ] && exit 0

VIOLATIONS=""
for f in "$KNOWLEDGE_DIR"/*.md; do
  [ -f "$f" ] || continue
  patterns=$(sed -n 's/.*Patterns:\*\* //p' "$f" 2>/dev/null | tr ',' '\n' | sed 's/^ *//;s/ *$//')
  [ -z "$patterns" ] && continue
  while IFS= read -r pattern; do
    [ -z "$pattern" ] || continue
    # Use -F for fixed string matching — patterns are not regex.
    if printf '%s' "$CONTENT" | grep -qF "$pattern" 2>/dev/null; then
      VIOLATIONS="${VIOLATIONS}Pattern '${pattern}' found in ${FILE_PATH}. "
    fi
  done <<< "$patterns"
done

if [ -n "$VIOLATIONS" ]; then
  echo "FAIL [experience-check] Known gotcha patterns detected: ${VIOLATIONS}"
  exit 1
else
  echo "PASS"
fi
`

const TaskVerifyHook = `#!/bin/bash
# task-verify.sh — Stop hook with blocking behavior.
# Prevents session end when quality issues are found.
set -eo pipefail

ROOT="${1:-.}"
cd "$ROOT" 2>/dev/null || exit 0

MESSAGES=""

# Task gate check
GATE_OUTPUT=$(forge task gate task-verify --silent 2>&1) || {
  MESSAGES="${MESSAGES}[task-gate] Task verify gate failed. "
}

# Check for pending mandatory reviews
if REVIEW_OUTPUT=$(forge experience list 2>/dev/null); then
  printf '%s' "$REVIEW_OUTPUT" | grep -qF "mandatory" && printf '%s' "$REVIEW_OUTPUT" | grep -qF "pending" && {
    MESSAGES="${MESSAGES}Pending mandatory review detected. Run 'forge experience list'. "
  }
fi

# Check: code changes on main/master without active task
BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
if [ "$BRANCH" = "master" ] || [ "$BRANCH" = "main" ]; then
  TASK_STATUS=$(forge task status 2>&1 || true)
  if printf '%s' "$TASK_STATUS" | grep -qF "No active task"; then
    CODE_CHANGES=$(git diff --name-only 2>/dev/null | grep -E '\.(go|rs|ts|tsx|js|jsx|py|java|rb)$' || true)
    STAGED_CHANGES=$(git diff --cached --name-only 2>/dev/null | grep -E '\.(go|rs|ts|tsx|js|jsx|py|java|rb)$' || true)
    if [ -n "$CODE_CHANGES" ] || [ -n "$STAGED_CHANGES" ]; then
      MESSAGES="${MESSAGES}Code changes on ${BRANCH} without active task. Start one: forge task start --ref <type>/<desc> --branch "
    fi
  fi
fi

# Self-bootstrap: warn if forge binary is stale (forging forge itself)
if [ -f "go.mod" ] && head -1 go.mod | grep -q "github.com/Harness/forge" 2>/dev/null; then
  FORGE_CMD=$(command -v forge 2>/dev/null || true)
  FORGE_BIN=""
  if [ -n "$FORGE_CMD" ]; then
    FORGE_DIR=$(dirname "$FORGE_CMD")
    for candidate in "${FORGE_DIR}/node_modules/@agentfare/forge/bin/forge.exe" "${FORGE_DIR}/node_modules/@agentfare/forge/bin/forge"; do
      if [ -f "$candidate" ]; then FORGE_BIN="$candidate"; break; fi
    done
  fi
  if [ -n "$FORGE_BIN" ]; then
    INSTALLED_HASH=$(go version -m "$FORGE_BIN" 2>/dev/null | grep vcs.revision | awk -F= '{print $2}' | head -c 7 || echo "")
    SOURCE_HASH=$(git rev-parse --short HEAD 2>/dev/null || echo "")
    if [ -n "$SOURCE_HASH" ] && [ -n "$INSTALLED_HASH" ] && [ "$INSTALLED_HASH" != "$SOURCE_HASH" ]; then
      MESSAGES="${MESSAGES}Forge binary is stale (installed: $INSTALLED_HASH, source: $SOURCE_HASH). Run: go install ./... then forge init. "
    fi
  fi
fi

if [ -n "$MESSAGES" ]; then
  echo "FAIL [task-verify] Issues found: ${MESSAGES}"
  exit 1
else
  echo "PASS"
fi
`

const ToolTrackHook = `#!/bin/bash
# tool-track.sh — PostToolUse hook for non-write tools.
# Records tool usage for scoring. Always passes (non-blocking).
set -eo pipefail
echo "PASS"
`
