#!/usr/bin/env node

const { spawn, execSync } = require("child_process");
const path = require("path");
const fs = require("fs");

const binaryName = process.platform === "win32" ? "forge.exe" : "forge";
const binaryPath = path.join(__dirname, "bin", binaryName);
const oldPath = binaryPath + ".old";

// --- Windows crash recovery (from lark-cli pattern) ---
// If forge.exe.old exists, we're recovering from an interrupted or failed update.
function recoverOldBinary() {
  if (!fs.existsSync(oldPath)) return;

  if (!fs.existsSync(binaryPath)) {
    // forge.exe missing but .old exists — rename first, then probe.
    // On Windows, .old is not a recognized executable extension,
    // so we must restore the .exe name before testing.
    try {
      fs.renameSync(oldPath, binaryPath);
      execSync(`"${binaryPath}" --version`, { timeout: 5000, stdio: "pipe", env: { ...process.env, FORGE_SKIP_UPDATE_CHECK: "1" } });
      console.error("[forge] Recovered binary from .old backup");
    } catch (e) {
      // Restored binary is broken — put it back as .old so we don't lose it
      try { fs.renameSync(binaryPath, oldPath); } catch (_) {}
      console.error("[forge] WARNING: .old binary is also broken");
    }
  } else {
    // Both exist — verify current binary works, then clean up .old
    try {
      execSync(`"${binaryPath}" --version`, { timeout: 5000, stdio: "pipe", env: { ...process.env, FORGE_SKIP_UPDATE_CHECK: "1" } });
      fs.unlinkSync(oldPath);
    } catch (e) {
      // Current binary broken — replace with .old (rename first, then probe)
      try { fs.unlinkSync(binaryPath); } catch (_) {}
      try {
        fs.renameSync(oldPath, binaryPath);
        execSync(`"${binaryPath}" --version`, { timeout: 5000, stdio: "pipe", env: { ...process.env, FORGE_SKIP_UPDATE_CHECK: "1" } });
        console.error("[forge] Recovered binary from .old backup");
      } catch (e2) {
        console.error("[forge] WARNING: both binary and .old are broken");
      }
    }
  }
}

recoverOldBinary();

if (!fs.existsSync(binaryPath)) {
  // Binary not available (e.g., mid npm upgrade). Silently approve to avoid
  // blocking Claude Code hooks during installation.
  console.log('{"decision":"approve"}');
  process.exit(0);
}

const child = spawn(binaryPath, process.argv.slice(2), {
  stdio: "inherit",
  env: process.env,
});

child.on("exit", (code) => {
  process.exit(code || 0);
});
