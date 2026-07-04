#!/usr/bin/env bash
# Forge installer — install the forge binary (macOS / Linux).
#
# Curl-pipe usage:
#   curl -fsSL https://raw.githubusercontent.com/MjxUpUp/Forge/main/plugins/forge/install.sh | bash
#
# Environment:
#   FORGE_VERSION   Pin forge version (default: latest). e.g. FORGE_VERSION=v0.30.0
#   FORGE_PKG       Override npm package (default: @agent_forge/forge)
#
# This is a FALLBACK installer. The recommended path is `npm install -g
# @agent_forge/forge`. This script does npm install first, and prints the
# plugin-install next-steps (which differ per agent and need to be run inside
# the agent CLI itself, not scripted).
#
# Modeled after https://github.com/Egonex-AI/Understand-Anything/blob/main/install.sh
# (one-step symlink-style installer) but Forge ships a real npm binary + per-agent
# plugin marketplace, so there's less to script and more to document.

set -euo pipefail

REPO="MjxUpUp/Forge"
PKG="${FORGE_PKG:-@agent_forge/forge}"

say()  { printf '\033[1;36mforge-installer\033[0m: %s\n' "$*"; }
warn() { printf '\033[1;33mforge-installer\033[0m: %s\n' "$*" >&2; }
err()  { printf '\033[1;31mforge-installer\033[0m: %s\n' "$*" >&2; }

case "$(uname -s)" in
  Linux|linux|Darwin|darwin) ;;
  *) err "unsupported OS: $(uname -s). On Windows use install.ps1."; exit 1 ;;
esac

# Already installed — print guidance and exit.
if command -v forge >/dev/null 2>&1; then
  say "forge already installed: $(forge --version 2>/dev/null || echo unknown)"
  say "For plugin install see: https://github.com/${REPO}/blob/main/plugins/forge/README.md"
  exit 0
fi

# Standard path: npm. Pin version if FORGE_VERSION is set.
if command -v npm >/dev/null 2>&1; then
  if [[ -n "${FORGE_VERSION:-}" ]]; then
    say "installing ${PKG}@${FORGE_VERSION} via npm"
    npm install -g "${PKG}@${FORGE_VERSION}"
  else
    say "installing ${PKG} via npm"
    npm install -g "${PKG}"
  fi
else
  err "npm not found. Install Node.js >= 18 (https://nodejs.org), then re-run, or use the PowerShell installer on Windows."
  exit 1
fi

if ! command -v forge >/dev/null 2>&1; then
  err "forge binary not on PATH after install. Check 'npm bin -g' output and add the directory to your PATH."
  exit 1
fi

say "✓ forge installed: $(forge --version 2>/dev/null || echo unknown)"

cat <<'NEXT'

Next step: install the plugin inside your agent CLI (interactive; not scriptable):

  Claude Code:
    /plugin marketplace add MjxUpUp/Forge
    /plugin install forge@forge

  Codex (CLI / App):
    codex plugin marketplace add MjxUpUp/Forge
    codex plugin install forge@forge

  Cursor:
    /plugin marketplace add MjxUpUp/Forge
    /plugin install forge@forge

  GitHub Copilot CLI:
    copilot plugin marketplace add MjxUpUp/Forge
    copilot plugin install forge@forge

After plugin install, opening any git project in your agent triggers the
init-suggest SessionStart hook to ask whether to enable forge (one-shot
prompt). On a Windows machine, run install.ps1 instead.
NEXT
