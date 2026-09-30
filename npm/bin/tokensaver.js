#!/usr/bin/env node
// Downloads the matching tokensaver release binary on first run, caches it under
// the user's home directory, then execs it with stdio passed straight through —
// tokensaver speaks MCP over stdio, so nothing here may touch stdin/stdout.
"use strict";

const { spawnSync } = require("child_process");
const fs = require("fs");
const https = require("https");
const os = require("os");
const path = require("path");

const pkg = require("../package.json");
const version = pkg.version;

const GOOS = { darwin: "darwin", linux: "linux", win32: "windows" }[process.platform];
const GOARCH = { x64: "amd64", arm64: "arm64" }[process.arch];

if (!GOOS || !GOARCH) {
  console.error(
    `tokensaver: no prebuilt binary for ${process.platform}/${process.arch}. ` +
      "Build from source: https://github.com/use-tokesaver/tokensaver#install"
  );
  process.exit(1);
}

const ext = GOOS === "windows" ? "zip" : "tar.gz";
const asset = `tokensaver_${version}_${GOOS}_${GOARCH}.${ext}`;
const url = `https://github.com/use-tokesaver/tokensaver/releases/download/v${version}/${asset}`;

const cacheDir = path.join(os.homedir(), ".cache", "tokensaver-npm", version);
const binName = GOOS === "windows" ? "tokensaver.exe" : "tokensaver";
const binPath = path.join(cacheDir, binName);

function download(fromURL, toPath, redirectsLeft = 5) {
  return new Promise((resolve, reject) => {
    https
      .get(fromURL, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          if (redirectsLeft === 0) return reject(new Error("too many redirects"));
          res.resume();
          return resolve(download(res.headers.location, toPath, redirectsLeft - 1));
        }
        if (res.statusCode !== 200) {
          res.resume();
          return reject(new Error(`GET ${fromURL}: HTTP ${res.statusCode}`));
        }
        const file = fs.createWriteStream(toPath);
        res.pipe(file);
        file.on("finish", () => file.close(resolve));
        file.on("error", reject);
      })
      .on("error", reject);
  });
}

async function ensureBinary() {
  if (fs.existsSync(binPath)) return;

  fs.mkdirSync(cacheDir, { recursive: true });
  const archivePath = path.join(cacheDir, asset);
  process.stderr.write(`tokensaver: downloading ${asset}...\n`);
  await download(url, archivePath);

  // Both macOS/Linux tar.gz and Windows zip extract with `tar` — bsdtar (macOS,
  // Windows 10 1803+) and GNU tar both handle zip via libarchive/an internal
  // shim; no extra dependency needed for either archive format.
  const tar = spawnSync("tar", ["-xf", archivePath, "-C", cacheDir], { stdio: "inherit" });
  if (tar.status !== 0) {
    throw new Error(`extracting ${asset} failed (tar exit ${tar.status})`);
  }
  fs.rmSync(archivePath, { force: true });
  if (GOOS !== "windows") fs.chmodSync(binPath, 0o755);
}

ensureBinary()
  .then(() => {
    const result = spawnSync(binPath, process.argv.slice(2), { stdio: "inherit" });
    process.exit(result.status === null ? 1 : result.status);
  })
  .catch((err) => {
    console.error(`tokensaver: ${err.message}`);
    process.exit(1);
  });
