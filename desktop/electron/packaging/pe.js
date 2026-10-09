"use strict";
const fs = require("node:fs");
const path = require("node:path");

const MACHINE = { amd64: 0x8664, arm64: 0xaa64 };
// Written by the NSIS installer rather than built by us, and 32-bit on every
// architecture; the release pins it by content (windows-signing-lib.ps1).
const INSTALLER_OWNED = { "resources/elevate.exe": 0x14c, "Uninstall Reasonix Studio.exe": 0x14c };
const EXTENSIONS = new Set([".exe", ".dll", ".node"]);

// The machine field of a PE image's COFF header. An emulated x64 binary and a
// native one are both valid programs on ARM64 Windows, so the architecture a
// package claims has to be read from the files themselves.
function peMachine(bytes) {
  if (bytes.length < 0x40 || bytes.readUInt16LE(0) !== 0x5a4d) throw new Error("not a PE image");
  const header = bytes.readUInt32LE(0x3c);
  if (header + 6 > bytes.length || bytes.readUInt32LE(header) !== 0x00004550) throw new Error("no PE signature");
  return bytes.readUInt16LE(header + 4);
}

function isPeCandidate(file) {
  if (EXTENSIONS.has(path.extname(file).toLowerCase())) return true;
  const fd = fs.openSync(file, "r");
  try {
    const head = Buffer.alloc(2);
    return fs.readSync(fd, head, 0, 2, 0) === 2 && head.readUInt16LE(0) === 0x5a4d;
  } finally {
    fs.closeSync(fd);
  }
}

function walk(dir, into) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(full, into);
    else if (entry.isFile() && isPeCandidate(full)) into.push(full);
  }
  return into;
}

// Every PE image under root with the machine it was built for.
function machines(root) {
  return walk(root, []).sort().map((file) => {
    const name = path.relative(root, file).split(path.sep).join("/");
    try {
      return { file: name, machine: peMachine(fs.readFileSync(file)) };
    } catch (err) {
      return { file: name, machine: null, error: err.message };
    }
  });
}

// The images under root that are not built for arch; none means the tree is
// uniformly that architecture. An empty tree is refused, not passed.
function foreign(root, arch) {
  const want = MACHINE[arch];
  if (want === undefined) throw new Error(`unknown architecture ${arch}`);
  const found = machines(root);
  if (found.length === 0) throw new Error(`no PE images under ${root}`);
  return found.filter((f) => f.machine !== (INSTALLER_OWNED[f.file] ?? want));
}

module.exports = { peMachine, machines, foreign, MACHINE };

if (require.main === module) {
  const [root, arch] = process.argv.slice(2);
  if (!root || !arch) {
    console.error("usage: node packaging/pe.js <dir> <amd64|arm64>");
    process.exit(2);
  }
  for (const f of machines(root)) console.log(`${f.machine === null ? f.error : "0x" + f.machine.toString(16)} ${f.file}`);
  const wrong = foreign(root, arch);
  for (const f of wrong) {
    console.error(f.error ? `::error::${f.file} cannot be read as a PE image: ${f.error}` : `::error::${f.file} is machine 0x${f.machine.toString(16)}, not ${arch}`);
  }
  process.exit(wrong.length === 0 ? 0 : 1);
}
