#!/usr/bin/env node
"use strict";
// npx launcher for the native kemory CLI.
//
// `npx kemory <args>` downloads the native build for this platform from
// the public homebrew-s9n release (the same archives the Homebrew formula
// installs), checks it against the release's .sha256 sidecar, caches it per
// version, and runs it with the given arguments. No kemory source ships in
// the npm package; this file is the whole package.
//
// stdout belongs to the CLI: `npx kemory mcp serve` is an MCP stdio server, so
// every launcher message goes to stderr.

const { spawn, spawnSync } = require("node:child_process");
const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const RELEASES = "https://github.com/SeKondBrainAILabs/homebrew-s9n/releases/download";

// `${process.platform}-${process.arch}` -> release asset. Mirrors the
// build-binaries matrix in release-cli.yml.
const TARGETS = {
  "darwin-arm64": { name: "macos-arm64", ext: "tar.gz" },
  "darwin-x64": { name: "macos-x64", ext: "tar.gz" },
  "linux-x64": { name: "linux-x64", ext: "tar.gz" },
  "linux-arm64": { name: "linux-arm64", ext: "tar.gz" },
  "win32-x64": { name: "windows-x64", ext: "zip" },
};

function resolveTarget(platform, arch) {
  return TARGETS[`${platform}-${arch}`] || null;
}

function cacheRoot(env, platform, home) {
  if (env.KEMORY_NPX_CACHE) return env.KEMORY_NPX_CACHE;
  if (platform === "win32") {
    return path.join(env.LOCALAPPDATA || path.join(home, "AppData", "Local"), "kemory", "npx");
  }
  return path.join(env.XDG_CACHE_HOME || path.join(home, ".cache"), "kemory", "npx");
}

// The sidecar holds the bare hex digest. The Windows one is written by
// PowerShell, so tolerate a BOM and surrounding whitespace.
function parseSha256(text) {
  const digest = text.replace(/^﻿/, "").trim().split(/\s+/)[0].toLowerCase();
  if (!/^[0-9a-f]{64}$/.test(digest)) throw new Error(`malformed sha256 sidecar: ${JSON.stringify(text.slice(0, 80))}`);
  return digest;
}

function binaryName(platform) {
  return platform === "win32" ? "kemory.exe" : "kemory";
}

async function fetchBuffer(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`GET ${url} returned HTTP ${res.status}`);
  return Buffer.from(await res.arrayBuffer());
}

async function install(version, target, dir, platform) {
  const asset = `kemory-${target.name}.${target.ext}`;
  const base = `${RELEASES}/cli-v${version}/${asset}`;
  process.stderr.write(`kemory: downloading ${asset} (v${version})...\n`);
  const [archive, sidecar] = await Promise.all([fetchBuffer(base), fetchBuffer(`${base}.sha256`)]);

  const expected = parseSha256(sidecar.toString("utf8"));
  const actual = crypto.createHash("sha256").update(archive).digest("hex");
  if (actual !== expected) {
    throw new Error(`checksum mismatch for ${asset}: expected ${expected}, got ${actual}. Nothing was run.`);
  }

  // Unpack next to the final location, then rename into place, so a crash or a
  // concurrent first run never leaves a half-extracted binary at `dir`.
  fs.mkdirSync(path.dirname(dir), { recursive: true });
  const tmp = fs.mkdtempSync(`${dir}.tmp-`);
  try {
    const archivePath = path.join(tmp, asset);
    fs.writeFileSync(archivePath, archive);
    const unpacked = path.join(tmp, "out");
    fs.mkdirSync(unpacked);
    // bsdtar ships with macOS and Windows 10+ and reads zip as well as tar.gz.
    const args = target.ext === "zip" ? ["-xf", archivePath, "-C", unpacked] : ["-xzf", archivePath, "-C", unpacked];
    const tar = spawnSync("tar", args, { stdio: ["ignore", 2, 2] });
    if (tar.error || tar.status !== 0) {
      throw new Error(`could not extract ${asset}: ${tar.error ? tar.error.message : `tar exited ${tar.status}`}`);
    }
    if (!fs.existsSync(path.join(unpacked, binaryName(platform)))) {
      throw new Error(`${asset} does not contain ${binaryName(platform)}`);
    }
    try {
      fs.renameSync(unpacked, dir);
    } catch (err) {
      // Another launcher finished first; its copy is identical.
      if (!fs.existsSync(path.join(dir, binaryName(platform)))) throw err;
    }
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

// launcherFor names how this package was started, from where it is installed:
// "npm" for a global install, which leaves `kemory` on PATH, and "npx" for
// npx's cache or a project's node_modules, where nothing called `kemory` lasts.
function launcherFor(dir) {
  const p = dir.split(path.sep).join("/");
  if (p.includes("/_npx/")) return "npx";
  if (/\/lib\/node_modules\/kemory(\/|$)/.test(p) || /\/npm\/node_modules\/kemory(\/|$)/i.test(p)) return "npm";
  return "npx";
}

async function main() {
  const { version } = require("../package.json");
  const target = resolveTarget(process.platform, process.arch);
  if (!target) {
    process.stderr.write(
      `kemory: no prebuilt binary for ${process.platform}-${process.arch}. Install with \`uvx kemory\` instead.\n`,
    );
    return 1;
  }

  const dir = path.join(cacheRoot(process.env, process.platform, os.homedir()), version, target.name);
  const bin = path.join(dir, binaryName(process.platform));
  if (!fs.existsSync(bin)) await install(version, target, dir, process.platform);

  // KEMORY_LAUNCHER tells the CLI how it was started: through npx, the MCP
  // host entry it writes launches through npx too, since nothing called
  // `kemory` is on PATH; from a global npm install, `kemory upgrade` knows to
  // upgrade with npm.
  const child = spawn(bin, process.argv.slice(2), {
    stdio: "inherit",
    env: { ...process.env, KEMORY_LAUNCHER: launcherFor(__dirname) },
  });
  for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"]) {
    process.on(signal, () => child.kill(signal));
  }
  return new Promise((resolve) => {
    child.on("error", (err) => {
      process.stderr.write(`kemory: could not start ${bin}: ${err.message}\n`);
      resolve(1);
    });
    child.on("exit", (code, signal) => resolve(code ?? (signal ? 1 : 0)));
  });
}

if (require.main === module) {
  main().then(
    (code) => process.exit(code),
    (err) => {
      process.stderr.write(`kemory: ${err.message}\n`);
      process.exit(1);
    },
  );
}

module.exports = { resolveTarget, cacheRoot, parseSha256, binaryName, install, launcherFor };
