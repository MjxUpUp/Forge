#!/bin/bash
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
