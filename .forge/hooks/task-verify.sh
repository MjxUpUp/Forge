#!/bin/bash
# task-verify.sh — runs task-level verification on session stop.
# No-op if no task is active.
set -eo pipefail
forge task gate task-verify --silent 2>/dev/null || true
exit 0
