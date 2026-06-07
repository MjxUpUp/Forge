#!/bin/bash
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
