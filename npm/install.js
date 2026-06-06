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
  const res = await new Promise((resolve, reject) => {
    https
      .get(url, { timeout: 30000 }, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          return download(res.headers.location, dest).then(resolve).catch(reject);
        }
        if (res.statusCode !== 200) {
          return reject(new Error(`Download failed: HTTP ${res.statusCode}`));
        }
        resolve(res);
      })
      .on("error", reject);
  });

  await pipeline(res, createWriteStream(dest));
}

async function main() {
  const binDir = path.join(__dirname, "bin");
  fs.mkdirSync(binDir, { recursive: true });

  const { goos, goarch } = getPlatform();
  const ext = goos === "windows" ? "zip" : "tar.gz";
  const archiveName = `forge_${VERSION}_${goos}_${goarch}.${ext}`;
  const url = `https://github.com/Harness/forge/releases/download/v${VERSION}/${archiveName}`;

  const archivePath = path.join(binDir, archiveName);
  console.log(`Downloading forge v${VERSION} for ${goos}/${goarch}...`);

  await download(url, archivePath);
  console.log(`Downloaded to ${archivePath}`);

  // Extract
  if (ext === "zip") {
    if (process.platform === "win32") {
      execSync(`powershell -Command "Expand-Archive -Path '${archivePath}' -DestinationPath '${binDir}' -Force"`, { stdio: "inherit" });
    } else {
      execSync(`unzip -o "${archivePath}" -d "${binDir}"`, { stdio: "inherit" });
    }
  } else {
    execSync(`tar xzf "${archivePath}" -C "${binDir}"`, { stdio: "inherit" });
  }

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
  console.error("You can download forge manually from https://github.com/Harness/forge/releases");
  process.exit(1);
});
