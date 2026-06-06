#!/usr/bin/env node

const { spawn } = require("child_process");
const path = require("path");
const fs = require("fs");

const binaryName = process.platform === "win32" ? "forge.exe" : "forge";
const binaryPath = path.join(__dirname, "bin", binaryName);

if (!fs.existsSync(binaryPath)) {
  console.error(
    "forge binary not found. Run `npm install` to download it, or install from https://github.com/MjxUpUp/forge/releases"
  );
  process.exit(1);
}

const child = spawn(binaryPath, process.argv.slice(2), {
  stdio: "inherit",
  env: process.env,
});

child.on("exit", (code) => {
  process.exit(code || 0);
});
