#!/bin/bash
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
