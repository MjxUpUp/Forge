const { execSync } = require("child_process");
const fs = require("fs");
const path = require("path");
const https = require("https");
const { createWriteStream } = require("fs");
const { pipeline } = require("stream/promises");

const VERSION = require("./package.json").version;

function getPlatform() {
  const platform = process.platform;
  const arch = process.arch;
  const osMap = { darwin: "darwin", linux: "linux", win32: "windows" };
  const archMap = { x64: "x86_64", arm64: "aarch64" };

  const goos = osMap[platform];
  const goarch = archMap[arch];

  if (!goos || !goarch) {
    throw new Error(`Unsupported platform: ${platform}/${arch}`);
  }

  return { goos, goarch };
}

function getBinaryName() {
  return process.platform === "win32" ? "forge.exe" : "forge";
}

async function download(url, dest) {
  let lastError;
  for (let attempt = 0; attempt < 3; attempt++) {
    if (attempt > 0) {
      const delay = Math.min(1000 * Math.pow(2, attempt), 8000);
      console.log(`Retrying (${attempt + 1}/3) after ${delay / 1000}s...`);
      await new Promise((r) => setTimeout(r, delay));
    }

    try {
      let currentUrl = url;
      while (true) {
        const res = await new Promise((resolve, reject) => {
          https
            .get(currentUrl, { timeout: 30000 }, resolve)
            .on("error", reject);
        });

        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          res.destroy();
          currentUrl = res.headers.location;
          continue;
        }
        if (res.statusCode !== 200) {
          res.destroy();
          throw new Error(`Download failed: HTTP ${res.statusCode}`);
        }

        await pipeline(res, createWriteStream(dest));
        return;
      }
    } catch (err) {
      lastError = err;
      // Clean up partial download
      try { fs.unlinkSync(dest); } catch (_) {}
    }
  }
  throw lastError || new Error("Download failed after 3 attempts");
}

/**
 * Check if an existing binary matches the target version.
 * Runs `forge --version` and parses the version string.
 */
function existingBinaryMatches(binaryPath) {
  try {
    const output = execSync(`"${binaryPath}" --version`, {
      timeout: 5000,
      stdio: "pipe",
      env: { ...process.env, FORGE_SKIP_UPDATE_CHECK: "1" },
    }).toString().trim();
    // Output format: "forge version 0.14.4 (commit: ...)" or "forge version dev"
    const match = output.match(/forge version (\S+)/);
    return match && match[1] === VERSION;
  } catch (_) {
    return false;
  }
}

async function main() {
  const binDir = path.join(__dirname, "bin");
  fs.mkdirSync(binDir, { recursive: true });

  const binaryName = getBinaryName();
  const binaryPath = path.join(binDir, binaryName);

  // Skip download if existing binary already matches the target version.
  // This avoids "file busy" errors on Windows when upgrading while
  // forge hooks are actively running in another Claude Code session.
  if (existingBinaryMatches(binaryPath)) {
    console.log(`forge v${VERSION} already installed.`);
    return;
  }

  const { goos, goarch } = getPlatform();
  const archiveName = `forge_${VERSION}_${goos}_${goarch}.tar.gz`;

  // Support custom binary host for regions with poor GitHub connectivity.
  // Usage: FORGE_BINARY_HOST=https://mirror.example.com npm install -g @agentfare/forge
  const baseUrl = process.env.FORGE_BINARY_HOST
    || "https://github.com/MjxUpUp/forge/releases/download";
  const url = `${baseUrl}/v${VERSION}/${archiveName}`;

  const archivePath = path.join(binDir, archiveName);
  console.log(`Downloading forge v${VERSION} for ${goos}/${goarch}...`);

  await download(url, archivePath);
  console.log(`Downloaded to ${archivePath}`);

  // Extract using relative path (cwd=binDir) to avoid Windows tar
  // interpreting "X:/path" as a remote host connection.
  //
  // On Windows, if forge.exe is currently running (e.g. another Claude Code
  // session has hooks active), tar cannot overwrite it. Handle this by:
  // 1. Renaming the old binary to .old (preserves running process)
  // 2. Extracting the new binary
  // 3. Cleaning up archive
  // The run.js wrapper will recover from .old on next launch if needed.
  let renamedOld = false;
  if (process.platform === "win32" && fs.existsSync(binaryPath)) {
    const oldPath = binaryPath + ".old";
    try { fs.unlinkSync(oldPath); } catch (_) {}
    try {
      fs.renameSync(binaryPath, oldPath);
      renamedOld = true;
    } catch (_) {
      // Cannot rename either — will try extract anyway
    }
  }

  try {
    execSync(`tar xzf "${archiveName}"`, { cwd: binDir, stdio: "pipe", timeout: 30000 });
  } catch (err) {
    // Extract failed — likely file busy on Windows
    if (renamedOld) {
      // Restore old binary so the package isn't left broken
      const oldPath = binaryPath + ".old";
      try {
        if (!fs.existsSync(binaryPath)) {
          fs.renameSync(oldPath, binaryPath);
          console.error(`[forge] Extract failed, restored previous binary. Will upgrade on next launch.`);
        }
      } catch (_) {}
    }
    throw new Error(
      `Failed to extract binary (file may be in use). ` +
      `Close other Claude Code sessions and run: npm install -g @agentfare/forge`
    );
  }

  // Make executable (Unix)
  if (process.platform !== "win32") {
    fs.chmodSync(binaryPath, 0o755);
  }

  // Cleanup archive and .old backup
  fs.unlinkSync(archivePath);
  if (renamedOld) {
    try { fs.unlinkSync(binaryPath + ".old"); } catch (_) {}
  }

  console.log(`forge v${VERSION} installed successfully.`);
}

main().catch((err) => {
  console.error("Installation failed:", err.message);
  console.error("");
  console.error("If GitHub is unreachable, set a mirror:");
  console.error("  FORGE_BINARY_HOST=https://your-mirror.com npm install -g @agentfare/forge");
  console.error("");
  console.error("Or download manually: https://github.com/MjxUpUp/forge/releases");
  process.exit(1);
});
