#!/bin/bash
# task-verify.sh — runs task-level verification on session stop.
# Also hints about pending mandatory reviews and missing task context.
set -eo pipefail
forge task gate task-verify --silent 2>/dev/null || true

# Check for pending mandatory reviews
forge experience list 2>/dev/null | grep -q "mandatory.*pending" && \
  echo "⚠ Pending mandatory review detected. Run 'forge experience list' for details." >&2 || true

# Check: code changes on master without active task?
BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
MASTER="master"
MAIN="main"
if [ "$BRANCH" = "$MASTER" ] || [ "$BRANCH" = "$MAIN" ]; then
  TASK_STATUS=$(forge task status 2>&1)
  if echo "$TASK_STATUS" | grep -q "No active task"; then
    # Check if there are uncommitted code changes
    CODE_CHANGES=$(git diff --name-only 2>/dev/null | grep -E '\.(go|rs|ts|tsx|js|jsx|py|java|rb)$' || true)
    STAGED_CHANGES=$(git diff --cached --name-only 2>/dev/null | grep -E '\.(go|rs|ts|tsx|js|jsx|py|java|rb)$' || true)
    if [ -n "$CODE_CHANGES" ] || [ -n "$STAGED_CHANGES" ]; then
      echo "⚠ Code changes on $BRANCH without active task. Start one: forge task start --ref <name>" >&2
    fi
  fi
fi
exit 0
