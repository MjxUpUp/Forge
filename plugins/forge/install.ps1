# Forge installer — Windows PowerShell.
#
# Curl-pipe (PowerShell one-liner):
#   iwr -useb https://raw.githubusercontent.com/MjxUpUp/Forge/main/plugins/forge/install.ps1 | iex
#
# Environment:
#   $env:FORGE_VERSION = "v0.30.0"   # pin version
#   $env:FORGE_PKG = "@agent_forge/forge"   # override package
#
# Modeled after install.sh — this is a FALLBACK. Recommended path is
# `npm install -g @agent_forge/forge`. The plugin install commands differ per
# agent and must be run interactively in the agent CLI (cannot be scripted).

$ErrorActionPreference = "Stop"

$repo = "MjxUpUp/Forge"
$pkg = if ($env:FORGE_PKG) { $env:FORGE_PKG } else { "@agent_forge/forge" }

function Say([string]$msg)  { Write-Host "forge-installer: $msg" -ForegroundColor Cyan }
function Warn([string]$msg) { Write-Host "forge-installer: $msg" -ForegroundColor Yellow }
function Err([string]$msg)  { Write-Host "forge-installer: $msg" -ForegroundColor Red }

if (Get-Command forge -ErrorAction SilentlyContinue) {
  $v = (& forge --version 2>$null) -join "`n"
  Say "forge already installed: $v"
  Say "For plugin install see: https://github.com/$repo/blob/main/plugins/forge/README.md"
  exit 0
}

if (Get-Command npm -ErrorAction SilentlyContinue) {
  if ($env:FORGE_VERSION) {
    Say "installing $pkg@$env:FORGE_VERSION via npm"
    npm install -g "$pkg@$env:FORGE_VERSION"
  } else {
    Say "installing $pkg via npm"
    npm install -g $pkg
  }
} else {
  Err "npm not found. Install Node.js >= 18 (https://nodejs.org) and re-run, or use the bash install.sh on macOS / Linux."
  exit 1
}

if (-not (Get-Command forge -ErrorAction SilentlyContinue)) {
  Err "forge binary not on PATH after install. Check 'npm bin -g' output and add the directory to your PATH."
  exit 1
}

Say "✓ forge installed: $((& forge --version) -join ' / ')"

@"

Next step: install the plugin inside your agent CLI (interactive; not scriptable):

  Claude Code:
    /plugin marketplace add $repo
    /plugin install forge@forge

  Codex (CLI / App):
    codex plugin marketplace add $repo
    codex plugin install forge@forge

  Cursor:
    /plugin marketplace add $repo
    /plugin install forge@forge

  GitHub Copilot CLI:
    copilot plugin marketplace add $repo
    copilot plugin install forge@forge

After plugin install, opening any git project in your agent triggers the
init-suggest SessionStart hook to ask whether to enable forge (one-shot
prompt). On macOS / Linux, run install.sh instead.
"@
