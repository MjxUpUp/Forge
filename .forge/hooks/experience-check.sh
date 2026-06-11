#!/bin/bash
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
