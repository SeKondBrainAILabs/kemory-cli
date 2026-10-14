"use strict";
// Run with: node --test packaging/npm/test/launcher.test.js
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const { resolveTarget, cacheRoot, parseSha256, binaryName, install } = require("../bin/kemory.js");

test("every supported platform maps to a release asset", () => {
  assert.deepEqual(resolveTarget("darwin", "arm64"), { name: "macos-arm64", ext: "tar.gz" });
  assert.deepEqual(resolveTarget("darwin", "x64"), { name: "macos-x64", ext: "tar.gz" });
  assert.deepEqual(resolveTarget("linux", "x64"), { name: "linux-x64", ext: "tar.gz" });
  assert.deepEqual(resolveTarget("linux", "arm64"), { name: "linux-arm64", ext: "tar.gz" });
  assert.deepEqual(resolveTarget("win32", "x64"), { name: "windows-x64", ext: "zip" });
});

test("unsupported platforms have no asset", () => {
  assert.equal(resolveTarget("win32", "arm64"), null);
  assert.equal(resolveTarget("freebsd", "x64"), null);
});

test("cache lives under XDG, ~/.cache or LOCALAPPDATA, with an override", () => {
  assert.equal(cacheRoot({ KEMORY_NPX_CACHE: "/c" }, "linux", "/h"), "/c");
  assert.equal(cacheRoot({ XDG_CACHE_HOME: "/x" }, "linux", "/h"), path.join("/x", "kemory", "npx"));
  assert.equal(cacheRoot({}, "darwin", "/h"), path.join("/h", ".cache", "kemory", "npx"));
  assert.equal(cacheRoot({ LOCALAPPDATA: "C:\\L" }, "win32", "/h"), path.join("C:\\L", "kemory", "npx"));
});

test("sha256 sidecars parse with or without a BOM and trailing text", () => {
  const hex = "a".repeat(64);
  assert.equal(parseSha256(`${hex}\n`), hex);
  assert.equal(parseSha256(`\uFEFF${hex.toUpperCase()}`), hex);
  assert.equal(parseSha256(`${hex}  kemory-linux-x64.tar.gz\n`), hex);
});

test("a malformed sidecar is refused", () => {
  assert.throws(() => parseSha256("not-a-digest"), /malformed sha256/);
  assert.throws(() => parseSha256(""), /malformed sha256/);
});

test("Windows runs kemory.exe", () => {
  assert.equal(binaryName("win32"), "kemory.exe");
  assert.equal(binaryName("darwin"), "kemory");
});

test("a checksum mismatch installs nothing", async (t) => {
  const realFetch = global.fetch;
  t.after(() => {
    global.fetch = realFetch;
  });
  global.fetch = async (url) =>
    new Response(url.endsWith(".sha256") ? `${"0".repeat(64)}\n` : "not the real archive", { status: 200 });

  const dir = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "kemory-npx-")), "9.9.9", "linux-x64");
  await assert.rejects(install("9.9.9", { name: "linux-x64", ext: "tar.gz" }, dir, "linux"), /checksum mismatch/);
  assert.equal(fs.existsSync(dir), false);
});
