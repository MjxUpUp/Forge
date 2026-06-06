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
}

async function main() {
  const binDir = path.join(__dirname, "bin");
  fs.mkdirSync(binDir, { recursive: true });

  const { goos, goarch } = getPlatform();
  const archiveName = `forge_${VERSION}_${goos}_${goarch}.tar.gz`;
  const url = `https://github.com/MjxUpUp/forge/releases/download/v${VERSION}/${archiveName}`;

  const archivePath = path.join(binDir, archiveName);
  console.log(`Downloading forge v${VERSION} for ${goos}/${goarch}...`);

  await download(url, archivePath);
  console.log(`Downloaded to ${archivePath}`);

  // Extract (tar.gz works on all platforms: Linux, macOS, Windows 10+)
  // Normalize backslashes to forward slashes for tar compatibility on Windows
  // --force-local prevents GNU tar from treating "X:/path" as a remote host
  const normArchivePath = archivePath.replace(/\\/g, "/");
  const normBinDir = binDir.replace(/\\/g, "/");
  execSync(`tar xzf "${normArchivePath}" -C "${normBinDir}" --force-local`, { stdio: "inherit" });

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
  console.error("You can download forge manually from https://github.com/MjxUpUp/forge/releases");
  process.exit(1);
});
