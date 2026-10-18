import { closeSync, fstatSync, lstatSync, openSync, readdirSync, readFileSync, readlinkSync, readSync, realpathSync, statSync } from "node:fs";
import { basename, dirname, join, relative, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { inflateRawSync } from "node:zlib";

// These package scripts are Node entry points. Starting Node directly keeps
// paths and arguments out of cmd.exe quoting, including trailing backslashes,
// embedded quotes and shell metacharacters in checkout paths.
export function runBuildScript(directory, script, args = [], env = {}) {
  const result = spawnSync(process.execPath, [join(directory, "scripts", script), ...args], {
    cwd: directory,
    env: { ...process.env, ...env },
    stdio: "inherit",
    shell: false,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${script} exited with ${result.status ?? result.signal}`);
}

export const PRODUCT = Object.freeze({
  name: "Reasonix",
  executable: "Reasonix",
  // The Wails-era CFBundleIdentifier (com.wails.<wails.json name>). LaunchServices,
  // saved-state and the macOS update swap key off it, so it survives the shell change.
  bundleId: "com.wails.reasonix-desktop",
  serviceExecutable: "reasonix-desktop",
  cliExecutable: "reasonix",
  windowsCliExecutable: "reasonix-cli",
  category: "public.app-category.developer-tools",
});

const TARGET_TABLE = {
  "darwin/arm64": { os: "darwin", arch: "arm64", packagerPlatform: "darwin", packagerArch: "arm64" },
  "darwin/amd64": { os: "darwin", arch: "amd64", packagerPlatform: "darwin", packagerArch: "x64" },
  "darwin/universal": { os: "darwin", arch: "universal", packagerPlatform: "darwin", packagerArch: "universal" },
  "windows/amd64": { os: "windows", arch: "amd64", packagerPlatform: "win32", packagerArch: "x64" },
  "windows/arm64": { os: "windows", arch: "arm64", packagerPlatform: "win32", packagerArch: "arm64" },
  "linux/amd64": { os: "linux", arch: "amd64", packagerPlatform: "linux", packagerArch: "x64" },
  "linux/arm64": { os: "linux", arch: "arm64", packagerPlatform: "linux", packagerArch: "arm64" },
};

export function parseTarget(spec) {
  const target = TARGET_TABLE[spec];
  if (!target) throw new Error(`unsupported target ${JSON.stringify(spec)}; expected one of ${Object.keys(TARGET_TABLE).join(", ")}`);
  return { ...target, spec, key: `${target.os}-${target.arch}` };
}

const TAG = /^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$/;

export function versionTag(tag) {
  if (!TAG.test(tag)) throw new Error(`version must look like v1.2.3 or v1.2.3-rc.1, got ${JSON.stringify(tag)}`);
  return tag;
}

export function releaseVersions(tag) {
  const canonical = versionTag(tag);
  const display = canonical.slice(1);
  return Object.freeze({ canonical, display, resource: display.split("-")[0] });
}

// Windows version resources, CFBundleVersion and the NSIS VIProductVersion only
// accept X.Y.Z; the full tag identifies the build through build.json and the
// Go -X main.version ldflag. Packager also writes appVersion to package.json.
export function numericVersion(tag) {
  return releaseVersions(tag).resource;
}

export function displayVersion(tag) {
  return releaseVersions(tag).display;
}

// The product identity lived in wails.json while the Wails shell was the build
// entry point; with the shell retired it is a constant here.
export function readProductIdentity() {
  return {
    projectName: PRODUCT.serviceExecutable,
    companyName: "Reasonix",
    productName: PRODUCT.name,
    copyright: "Copyright © 2026 Reasonix Contributors",
  };
}

export function shellIgnore(path) {
  if (path === "" || path === "/package.json" || path === "/dist") return false;
  if (path.startsWith("/dist/")) return path.endsWith(".map");
  return true;
}

export function sanitizeShellPackageJson(pkg, { version, productName }) {
  const keep = ["name", "description", "main", "type"];
  const out = {};
  for (const key of keep) if (key in pkg) out[key] = pkg[key];
  out.productName = productName;
  out.version = numericVersion(version);
  return out;
}

export function buildInfo({ version, channel, commit, electronVersion, target, buildTime }) {
  return {
    schemaVersion: 1,
    version: versionTag(version),
    channel,
    commit,
    buildTime,
    electron: electronVersion,
    platform: `${target.os}/${target.arch}`,
  };
}

export function packagerOptions({ target, version, identity, root, electronVersion, extraResources, icon }) {
  const numeric = numericVersion(version);
  const options = {
    dir: join(root, "electron"),
    out: join(root, "build", "electron", ".packager"),
    name: PRODUCT.name,
    executableName: PRODUCT.executable,
    platform: target.packagerPlatform,
    arch: target.packagerArch,
    electronVersion,
    appBundleId: PRODUCT.bundleId,
    appVersion: numeric,
    buildVersion: numeric,
    appCopyright: identity.copyright,
    appCategoryType: PRODUCT.category,
    asar: true,
    prune: true,
    overwrite: true,
    junk: true,
    darwinDarkModeSupport: true,
    extraResource: extraResources,
    ignore: shellIgnore,
  };
  if (icon) options.icon = icon;
  if (target.packagerPlatform === "win32") {
    options.win32metadata = {
      CompanyName: identity.companyName,
      FileDescription: identity.productName,
      ProductName: identity.productName,
      InternalName: PRODUCT.executable,
      OriginalFilename: `${PRODUCT.executable}.exe`,
    };
  }
  return options;
}

export function nsisProjectDefines(identity, version) {
  const versions = releaseVersions(version);
  const lines = [
    "; Generated by desktop/packaging/package.mjs - do not edit or commit.",
    `!define INFO_PROJECTNAME "${identity.projectName}"`,
    `!define INFO_COMPANYNAME "${identity.companyName}"`,
    `!define INFO_PRODUCTNAME "${identity.productName}"`,
    `!define INFO_PRODUCTVERSION "${versions.resource}"`,
    `!define REASONIX_DISPLAY_VERSION "${versions.display}"`,
    `!define INFO_COPYRIGHT "${identity.copyright}"`,
    `!define REASONIX_VERSION_TAG "${versions.canonical}"`,
  ];
  // makensis only decodes an include as UTF-8 when it carries a BOM; the copyright sign needs it.
  return "﻿" + lines.join("\r\n") + "\r\n";
}

export function normalizeEntry(name) {
  let out = name.replace(/\\/g, "/");
  while (out.startsWith("./")) out = out.slice(2);
  return out;
}

export function walkFiles(dir, base = dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...walkFiles(path, base));
    else out.push(normalizeEntry(relative(base, path)));
  }
  return out.sort();
}

const PE_SUFFIX = /\.(exe|dll)$/i;

export function signingFileList(entries) {
  return [...new Set(entries.map(normalizeEntry).filter((name) => PE_SUFFIX.test(name)))].sort();
}

export function parseSigningFileList(text) {
  return text.split(/\r?\n/).map((line) => line.trim()).filter((line) => line !== "" && !line.startsWith("#"));
}

export const WINDOWS_FLAT_PAYLOAD = Object.freeze([
  "reasonix-desktop.exe",
  "reasonix-guard.exe",
  "reasonix-launcher.exe",
  "reasonix-update-helper.exe",
  "reasonix-cli.exe",
  "reasonix-uninstall.exe",
]);

const APP_RESOURCES = ["resources/app.asar", "resources/app/index.html", "resources/build.json", "resources/mcp-bridge.js", "resources/icons/appicon.png", "resources/icons/trayTemplate.png", "resources/icons/trayTemplate@2x.png"];

function darwinBundleMembers() {
  const helper = (kind) => `Contents/Frameworks/${PRODUCT.name} Helper (${kind}).app/Contents/MacOS/${PRODUCT.name} Helper (${kind})`;
  return [
    "Contents/Info.plist",
    `Contents/MacOS/${PRODUCT.executable}`,
    `Contents/MacOS/${PRODUCT.serviceExecutable}`,
    `Contents/Resources/service/${PRODUCT.serviceExecutable}`,
    // The CLI sidecar lives in Resources/service/, never Contents/MacOS/: on
    // case-insensitive APFS "reasonix" there collides with the Electron main
    // executable "Reasonix" and cp would clobber it.
    `Contents/Resources/service/${PRODUCT.cliExecutable}`,
    "Contents/Resources/app.asar",
    "Contents/Resources/app/index.html",
    "Contents/Resources/build.json",
    "Contents/Resources/mcp-bridge.js",
    "Contents/Resources/icons/appicon.png",
    "Contents/Resources/icons/trayTemplate.png",
    "Contents/Resources/icons/trayTemplate@2x.png",
    "Contents/Frameworks/Electron Framework.framework/Electron Framework",
    helper("Renderer"),
    helper("GPU"),
  ];
}

const VERSION_DIR = "versions/v[^/]+";
const PACKAGING_JUNK = /(^|\/)(?:[^/]+\.map|__tests__|testdata|\.cache|coverage|npm-debug\.log|pnpm-debug\.log|yarn-error\.log)(?:$|\/)/;

const MEMBERS = {
  "darwin-app-dir": { required: darwinBundleMembers(), forbidden: ["Contents/MacOS/reasonix-guard"] },
  "darwin-zip": {
    required: darwinBundleMembers().map((name) => `${PRODUCT.name}.app/${name}`),
    forbidden: [`${PRODUCT.name}.app/Contents/MacOS/reasonix-guard`],
  },
  "windows-app-dir": {
    required: [`${PRODUCT.executable}.exe`, "ffmpeg.dll", "libEGL.dll", "libGLESv2.dll", "resources.pak", "icudtl.dat", "locales/en-US.pak", ...APP_RESOURCES],
    forbidden: [],
  },
  "windows-portable-zip": {
    required: [
      `${PRODUCT.executable}.exe`,
      `${PRODUCT.windowsCliExecutable}.exe`,
      "current.json",
      new RegExp(`^${VERSION_DIR}/reasonix-desktop\\.exe$`),
      new RegExp(`^${VERSION_DIR}/reasonix-update-helper\\.exe$`),
      new RegExp(`^${VERSION_DIR}/reasonix-cli\\.exe$`),
      new RegExp(`^${VERSION_DIR}/app/${PRODUCT.executable}\\.exe$`),
      new RegExp(`^${VERSION_DIR}/app/resources/bin/reasonix-cli-launcher\\.exe$`),
      new RegExp(`^${VERSION_DIR}/app/resources/app\\.asar$`),
      new RegExp(`^${VERSION_DIR}/app/resources/app/index\\.html$`),
      new RegExp(`^${VERSION_DIR}/app/resources/build\\.json$`),
      new RegExp(`^${VERSION_DIR}/app/resources/mcp-bridge\\.js$`),
    ],
    forbidden: ["reasonix-guard.exe", "reasonix-desktop.exe"],
  },
  "linux-app-dir": {
    required: [PRODUCT.executable, "chrome-sandbox", "chrome_crashpad_handler", "libffmpeg.so", "resources.pak", "locales/en-US.pak", ...APP_RESOURCES],
    forbidden: [],
  },
  "linux-tar": {
    required: ["reasonix-desktop", "reasonix-launcher", "reasonix-guard", "reasonix", `app/${PRODUCT.executable}`, "app/chrome-sandbox", ...APP_RESOURCES.map((name) => `app/${name}`)],
    forbidden: [],
  },
  "linux-deb": {
    required: [
      "usr/bin/reasonix-desktop",
      "usr/bin/reasonix-launcher",
      "usr/bin/reasonix",
      "usr/lib/reasonix/reasonix-update-helper",
      `usr/lib/reasonix/app/${PRODUCT.executable}`,
      "usr/lib/reasonix/app/chrome-sandbox",
      ...APP_RESOURCES.map((name) => `usr/lib/reasonix/app/${name}`),
      "usr/share/polkit-1/actions/io.reasonix.desktop.update.policy",
      "usr/share/applications/reasonix.desktop",
    ],
    forbidden: ["usr/bin/reasonix-guard"],
  },
};

export const ARTIFACT_KINDS = Object.freeze(Object.keys(MEMBERS));

export const WINDOWS_PORTABLE_LAYOUTS = Object.freeze(["canonical", "legacy-dual"]);

function memberSpec(kind, portableLayout) {
  if (!WINDOWS_PORTABLE_LAYOUTS.includes(portableLayout)) throw new Error(`unknown Windows portable layout ${JSON.stringify(portableLayout)}`);
  const spec = MEMBERS[kind];
  if (!spec) throw new Error(`unknown artifact kind ${JSON.stringify(kind)}`);
  if (kind !== "windows-portable-zip") return spec;
  return portableLayout === "legacy-dual"
    ? { required: [...spec.required, "reasonix-launcher.exe"], forbidden: spec.forbidden }
    : { required: spec.required, forbidden: [...spec.forbidden, "reasonix-launcher.exe"] };
}

export function requiredMembers(kind, portableLayout = "canonical") {
  return memberSpec(kind, portableLayout).required;
}

export function checkMembers(entries, kind, portableLayout = "canonical") {
  const spec = memberSpec(kind, portableLayout);
  const names = new Set(entries.map(normalizeEntry).filter((name) => name !== "" && !name.endsWith("/")));
  const matches = (rule) => (rule instanceof RegExp ? [...names].some((name) => rule.test(name)) : names.has(rule));
  const forbidden = [...spec.forbidden, PACKAGING_JUNK].filter((rule) => matches(rule)).map(String);
  if (kind === "windows-portable-zip") {
    const rootEntries = new Set(["Reasonix.exe", "reasonix-cli.exe", ...(portableLayout === "legacy-dual" ? ["reasonix-launcher.exe"] : [])]);
    for (const name of names) {
      if (!name.includes("/") && /\.(exe|dll)$/i.test(name) && !rootEntries.has(name) && !forbidden.includes(name)) forbidden.push(name);
    }
  }
  return {
    missing: spec.required.filter((rule) => !matches(rule)).map(String),
    forbidden,
  };
}

// GNU tar -tv and dpkg-deb -c share this column layout; bsdtar does not, so an
// unrecognised line fails instead of silently dropping the mode check.
const VERBOSE_LISTING = /^([-dl][rwxsStT-]{9})\s+(\S+)\s+\d+\s+\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}(?::\d{2})?\s+(.*)$/;

export function parseVerboseListing(lines) {
  return lines.map((line) => {
    const match = VERBOSE_LISTING.exec(line);
    if (!match) throw new Error(`unrecognised listing line: ${JSON.stringify(line)}`);
    const [, mode, owner, rest] = match;
    const name = mode.startsWith("l") ? rest.split(" -> ")[0] : rest;
    return { mode, owner, name };
  });
}

const DIRECTORY_MODE = /^drwxr-xr-x$/;

export function checkEntryModes(rows, kind) {
  const errors = [];
  for (const { mode, owner, name } of rows) {
    if (mode.startsWith("l")) continue;
    if (mode.startsWith("d") && !DIRECTORY_MODE.test(mode)) errors.push(`${name} has mode ${mode}; directories must be drwxr-xr-x`);
    if (mode.startsWith("-") && mode[7] !== "r") errors.push(`${name} has mode ${mode}; files must be world-readable`);
    if (kind === "linux-deb" && owner !== "root/root") errors.push(`${name} is owned by ${owner}; package members must be root/root`);
  }
  return errors;
}

const PT_INTERP = 3;

// Returns the program interpreter an ELF executable names, or null for a static
// one. Only the Electron shell may carry a glibc floor: a Go binary that names
// ld.so inherits the build runner's glibc and fails to start on older systems.
export function elfInterpreter(bytes) {
  if (bytes.length < 52 || bytes.readUInt32BE(0) !== 0x7f454c46) throw new Error("not an ELF file");
  const wide = bytes[4] === 2;
  const little = bytes[5] === 1;
  if (bytes[4] !== 1 && !wide) throw new Error(`unknown ELF class ${bytes[4]}`);
  if (bytes[5] !== 2 && !little) throw new Error(`unknown ELF data encoding ${bytes[5]}`);
  const u16 = (at) => (little ? bytes.readUInt16LE(at) : bytes.readUInt16BE(at));
  const u32 = (at) => (little ? bytes.readUInt32LE(at) : bytes.readUInt32BE(at));
  const word = (at) => (wide ? Number(little ? bytes.readBigUInt64LE(at) : bytes.readBigUInt64BE(at)) : u32(at));
  if (wide && bytes.length < 64) throw new Error("truncated ELF header");
  const phoff = word(wide ? 32 : 28);
  const phentsize = u16(wide ? 54 : 42);
  const phnum = u16(wide ? 56 : 44);
  for (let i = 0; i < phnum; i++) {
    const header = phoff + i * phentsize;
    if (header + phentsize > bytes.length) throw new Error("truncated ELF program header table");
    if (u32(header) !== PT_INTERP) continue;
    const offset = word(header + (wide ? 8 : 4));
    const size = word(header + (wide ? 32 : 16));
    if (offset + size > bytes.length) throw new Error("truncated ELF interpreter path");
    return bytes.toString("latin1", offset, offset + size).replace(/\0+$/, "");
  }
  return null;
}

const LINUX_GO_MEMBERS = {
  "linux-tar": ["reasonix-desktop", "reasonix-launcher", "reasonix-guard", "reasonix"],
  "linux-deb": ["usr/bin/reasonix-desktop", "usr/bin/reasonix-launcher", "usr/bin/reasonix", "usr/lib/reasonix/reasonix-update-helper"],
};

export function checkStaticGoMembers(kind, readMember) {
  const errors = [];
  for (const name of LINUX_GO_MEMBERS[kind] ?? []) {
    const interpreter = elfInterpreter(readMember(name));
    if (interpreter !== null) errors.push(`${name} is dynamically linked (interpreter ${interpreter}); Linux Go binaries must be built with CGO_ENABLED=0`);
  }
  return errors;
}

export function validateMacServiceLink(appDir) {
  const link = join(appDir, "Contents", "MacOS", PRODUCT.serviceExecutable);
  const expectedTarget = `../Resources/service/${PRODUCT.serviceExecutable}`;
  const errors = [];
  let stat;
  try {
    stat = lstatSync(link);
  } catch (error) {
    return [`service compatibility link is unavailable: ${error.message}`];
  }
  if (!stat.isSymbolicLink()) return ["service compatibility path is not a symbolic link"];
  const target = readlinkSync(link);
  if (target !== expectedTarget) errors.push(`service compatibility link target is ${JSON.stringify(target)}, want ${JSON.stringify(expectedTarget)}`);
  if (resolve(dirname(link), target) !== resolve(appDir, "Contents", "Resources", "service", PRODUCT.serviceExecutable)) {
    errors.push("service compatibility link does not resolve to the package service entity");
  }
  try {
    const realApp = realpathSync(appDir);
    const realTarget = realpathSync(link);
    const rel = relative(realApp, realTarget);
    if (rel === "" || rel === ".." || rel.startsWith(`..${process.platform === "win32" ? "\\" : "/"}`)) {
      errors.push("service compatibility link resolves outside the application bundle");
    }
    if (!statSync(realTarget).isFile()) errors.push("service compatibility link target is not a regular file");
  } catch (error) {
    errors.push(`service compatibility link is dangling or cyclic: ${error.message}`);
  }
  return errors;
}

export function inferArtifactKind(pathname, isDirectory, entries = []) {
  const name = basename(pathname);
  if (isDirectory) {
    if (name.endsWith(".app")) return "darwin-app-dir";
    if (entries.includes(`${PRODUCT.executable}.exe`)) return "windows-app-dir";
    if (entries.includes("chrome-sandbox")) return "linux-app-dir";
    throw new Error(`cannot infer the artifact kind of directory ${pathname}`);
  }
  if (/^Reasonix-darwin-.*\.zip$/.test(name)) return "darwin-zip";
  if (/^Reasonix-windows-.*\.zip$/.test(name)) return "windows-portable-zip";
  if (name.endsWith(".tar.gz")) return "linux-tar";
  if (name.endsWith(".deb")) return "linux-deb";
  throw new Error(`cannot infer the artifact kind of ${pathname}`);
}

const EOCD = 0x06054b50;
const EOCD64_LOCATOR = 0x07064b50;
const EOCD64 = 0x06064b50;
const CENTRAL_HEADER = 0x02014b50;

// Only the central directory is read, so a 300 MB bundle costs a few reads;
// zip64 records are honoured because ditto emits them for large archives.
export function listZipEntries(file) {
  return scanZip(file);
}

// Read a bounded member without extracting the archive or requiring a native
// unzip tool. Used only for small pointers and the stable launcher entries.
export function readZipMember(file, member) {
  return scanZip(file, member);
}

function scanZip(file, member) {
  const fd = openSync(file, "r");
  try {
    const size = fstatSync(fd).size;
    const tailLength = Math.min(size, 22 + 65535);
    const tail = Buffer.alloc(tailLength);
    readSync(fd, tail, 0, tailLength, size - tailLength);
    let eocd = -1;
    for (let i = tailLength - 22; i >= 0; i--) {
      if (tail.readUInt32LE(i) === EOCD) {
        eocd = i;
        break;
      }
    }
    if (eocd < 0) throw new Error(`${file}: not a zip archive (no end-of-central-directory record)`);
    let count = tail.readUInt16LE(eocd + 10);
    let directorySize = tail.readUInt32LE(eocd + 12);
    let directoryOffset = tail.readUInt32LE(eocd + 16);
    const locator = eocd - 20;
    if ((count === 0xffff || directorySize === 0xffffffff || directoryOffset === 0xffffffff) && locator >= 0 && tail.readUInt32LE(locator) === EOCD64_LOCATOR) {
      const record = Buffer.alloc(56);
      readSync(fd, record, 0, 56, Number(tail.readBigUInt64LE(locator + 8)));
      if (record.readUInt32LE(0) !== EOCD64) throw new Error(`${file}: corrupt zip64 end-of-central-directory record`);
      count = Number(record.readBigUInt64LE(32));
      directorySize = Number(record.readBigUInt64LE(40));
      directoryOffset = Number(record.readBigUInt64LE(48));
    }
    const directory = Buffer.alloc(directorySize);
    readSync(fd, directory, 0, directorySize, directoryOffset);
    const names = [];
    let contents;
    let offset = 0;
    for (let i = 0; i < count; i++) {
      if (directory.readUInt32LE(offset) !== CENTRAL_HEADER) throw new Error(`${file}: corrupt central directory at entry ${i}`);
      const nameLength = directory.readUInt16LE(offset + 28);
      const extraLength = directory.readUInt16LE(offset + 30);
      const commentLength = directory.readUInt16LE(offset + 32);
      const name = directory.toString("utf8", offset + 46, offset + 46 + nameLength);
      names.push(name);
      if (member !== undefined && normalizeEntry(name) === member) {
        if (contents !== undefined) throw new Error(`duplicate zip member ${member}`);
        let compressed = directory.readUInt32LE(offset + 20);
        let uncompressed = directory.readUInt32LE(offset + 24);
        let localOffset = directory.readUInt32LE(offset + 42);
        const extraStart = offset + 46 + nameLength;
        for (let pos = extraStart; pos + 4 <= extraStart + extraLength;) {
          const tag = directory.readUInt16LE(pos), length = directory.readUInt16LE(pos + 2);
          if (pos + 4 + length > extraStart + extraLength) throw new Error("invalid zip extra field");
          if (tag === 1) {
            let field = pos + 4;
            const next = () => {
              if (field + 8 > pos + 4 + length) throw new Error("invalid zip64 entry");
              const value = Number(directory.readBigUInt64LE(field)); field += 8;
              if (!Number.isSafeInteger(value)) throw new Error("zip64 value is too large");
              return value;
            };
            if (uncompressed === 0xffffffff) uncompressed = next();
            if (compressed === 0xffffffff) compressed = next();
            if (localOffset === 0xffffffff) localOffset = next();
          }
          pos += 4 + length;
        }
        const limit = 32 * 1024 * 1024;
        if (compressed > limit || uncompressed > limit || localOffset + 30 > size) throw new Error(`zip member ${member} exceeds bounds`);
        if (directory.readUInt16LE(offset + 8) & 1) throw new Error("encrypted zip members are unsupported");
        const local = Buffer.alloc(30);
        readSync(fd, local, 0, local.length, localOffset);
        if (local.readUInt32LE(0) !== 0x04034b50) throw new Error("invalid zip local header");
        const method = directory.readUInt16LE(offset + 10);
        if (local.readUInt16LE(8) !== method || (local.readUInt16LE(6) & 1)) throw new Error("zip local/central header mismatch");
        const localName = Buffer.alloc(local.readUInt16LE(26));
        readSync(fd, localName, 0, localName.length, localOffset + 30);
        if (localName.toString("utf8") !== name) throw new Error("zip local/central name mismatch");
        const start = localOffset + 30 + local.readUInt16LE(26) + local.readUInt16LE(28);
        if (start + compressed > directoryOffset) throw new Error("zip member overlaps central directory");
        const packed = Buffer.alloc(compressed);
        readSync(fd, packed, 0, packed.length, start);
        if (method !== 0 && method !== 8) throw new Error(`unsupported zip compression ${method}`);
        contents = method === 0 ? packed : inflateRawSync(packed, { maxOutputLength: limit });
        if (contents.length !== uncompressed) throw new Error(`zip member ${member} size mismatch`);
      }
      offset += 46 + nameLength + extraLength + commentLength;
    }
    if (member !== undefined && contents === undefined) throw new Error(`zip member ${member} is missing`);
    return member === undefined ? names : contents;
  } finally {
    closeSync(fd);
  }
}

export function isDirectory(path) {
  try {
    return statSync(path).isDirectory();
  } catch {
    return false;
  }
}
