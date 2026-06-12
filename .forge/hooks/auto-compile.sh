#!/bin/bash
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
  # Task context warning: coding on master/main without an active task.
  # Lightweight check (no forge invocation) — only warns, never blocks.
  BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
  if [ "$BRANCH" = "master" ] || [ "$BRANCH" = "main" ]; then
    TASK_COUNT=$(find .forge/tasks -name '*.json' 2>/dev/null | wc -l | tr -d ' ')
    if [ "$TASK_COUNT" = "0" ] 2>/dev/null; then
      CHANGED=$(git diff --name-only HEAD 2>/dev/null | grep -cE '\.(go|rs|ts|tsx|js|jsx|py|java|rb)$' || echo "0")
      if [ "$CHANGED" -gt 3 ]; then
        echo "WARN [task-context] ${CHANGED} code files changed on ${BRANCH} without active task. Start one: forge task start --ref <type>/<desc> --branch" >&2
      fi
    fi
  fi
  echo "PASS [auto-compile] All builds passed."
else
  echo "FAIL [auto-compile] Build failures detected:"
  printf '%s' "$RESULTS"
  exit 1
fi
