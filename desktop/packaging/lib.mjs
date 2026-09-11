import { closeSync, fstatSync, openSync, readdirSync, readFileSync, readSync, statSync } from "node:fs";
import { basename, join, relative } from "node:path";
import { spawnSync } from "node:child_process";

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

// Windows version resources, CFBundleVersion and the NSIS VIProductVersion only
// accept X.Y.Z; the full tag identifies the build through build.json and the
// Go -X main.version ldflag. Packager also writes appVersion to package.json.
export function numericVersion(tag) {
  return versionTag(tag).slice(1).split("-")[0];
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
  const lines = [
    "; Generated by desktop/packaging/package.mjs - do not edit or commit.",
    `!define INFO_PROJECTNAME "${identity.projectName}"`,
    `!define INFO_COMPANYNAME "${identity.companyName}"`,
    `!define INFO_PRODUCTNAME "${identity.productName}"`,
    `!define INFO_PRODUCTVERSION "${numericVersion(version)}"`,
    `!define INFO_COPYRIGHT "${identity.copyright}"`,
    `!define REASONIX_VERSION_TAG "${versionTag(version)}"`,
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

const APP_RESOURCES = ["resources/app.asar", "resources/app/index.html", "resources/build.json", "resources/icons/appicon.png"];

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
    "Contents/Resources/icons/appicon.png",
    "Contents/Frameworks/Electron Framework.framework/Electron Framework",
    helper("Renderer"),
    helper("GPU"),
  ];
}

const VERSION_DIR = "versions/v[^/]+";

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
      "reasonix-launcher.exe",
      `${PRODUCT.windowsCliExecutable}.exe`,
      "current.json",
      new RegExp(`^${VERSION_DIR}/reasonix-desktop\\.exe$`),
      new RegExp(`^${VERSION_DIR}/reasonix-update-helper\\.exe$`),
      new RegExp(`^${VERSION_DIR}/reasonix-cli\\.exe$`),
      new RegExp(`^${VERSION_DIR}/app/${PRODUCT.executable}\\.exe$`),
      new RegExp(`^${VERSION_DIR}/app/resources/app\\.asar$`),
      new RegExp(`^${VERSION_DIR}/app/resources/app/index\\.html$`),
      new RegExp(`^${VERSION_DIR}/app/resources/build\\.json$`),
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

export function requiredMembers(kind) {
  const spec = MEMBERS[kind];
  if (!spec) throw new Error(`unknown artifact kind ${JSON.stringify(kind)}`);
  return spec.required;
}

export function checkMembers(entries, kind) {
  const spec = MEMBERS[kind];
  if (!spec) throw new Error(`unknown artifact kind ${JSON.stringify(kind)}`);
  const names = new Set(entries.map(normalizeEntry).filter((name) => name !== "" && !name.endsWith("/")));
  const matches = (rule) => (rule instanceof RegExp ? [...names].some((name) => rule.test(name)) : names.has(rule));
  return {
    missing: spec.required.filter((rule) => !matches(rule)).map(String),
    forbidden: spec.forbidden.filter((rule) => matches(rule)).map(String),
  };
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
    let offset = 0;
    for (let i = 0; i < count; i++) {
      if (directory.readUInt32LE(offset) !== CENTRAL_HEADER) throw new Error(`${file}: corrupt central directory at entry ${i}`);
      const nameLength = directory.readUInt16LE(offset + 28);
      const extraLength = directory.readUInt16LE(offset + 30);
      const commentLength = directory.readUInt16LE(offset + 32);
      names.push(directory.toString("utf8", offset + 46, offset + 46 + nameLength));
      offset += 46 + nameLength + extraLength + commentLength;
    }
    return names;
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
