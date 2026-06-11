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

async function main() {
  const binDir = path.join(__dirname, "bin");
  fs.mkdirSync(binDir, { recursive: true });

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
  execSync(`tar xzf "${archiveName}"`, { cwd: binDir, stdio: "inherit" });

  // Make executable
  const binaryName = getBinaryName();
  const binaryPath = path.join(binDir, binaryName);
  if (process.platform !== "win32") {
    fs.chmodSync(binaryPath, 0o755);
  }

  // Cleanup archive
  fs.unlinkSync(archivePath);

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
