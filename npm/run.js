#!/usr/bin/env node

const { spawn } = require("child_process");
const path = require("path");
const fs = require("fs");

const binaryName = process.platform === "win32" ? "forge.exe" : "forge";
const binaryPath = path.join(__dirname, "bin", binaryName);

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
