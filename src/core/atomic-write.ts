import { chmodSync, copyFileSync, renameSync, unlinkSync, writeFileSync } from "node:fs";
import { chmod, copyFile, rename, unlink, writeFile } from "node:fs/promises";

export interface AtomicWriteFs {
  writeFileSync: typeof writeFileSync;
  chmodSync: typeof chmodSync;
  renameSync: typeof renameSync;
  copyFileSync: typeof copyFileSync;
  unlinkSync: typeof unlinkSync;
}

const defaultFs: AtomicWriteFs = {
  writeFileSync,
  chmodSync,
  renameSync,
  copyFileSync,
  unlinkSync,
};

/** Atomic write with EXDEV fallback — Windows OneDrive / reparse points refuse rename, fixes #1738. */
export function atomicWriteSync(
  path: string,
  body: string,
  tmp: string,
  mode = 0o600,
  fs: AtomicWriteFs = defaultFs,
): void {
  try {
    fs.writeFileSync(tmp, body, "utf8");
    try {
      fs.chmodSync(tmp, mode);
    } catch {
      /* platform without chmod */
    }
    try {
      fs.renameSync(tmp, path);
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code !== "EXDEV") throw err;
      fs.copyFileSync(tmp, path);
      try {
        fs.chmodSync(path, mode);
      } catch {
        /* platform without chmod */
      }
    }
  } catch (err) {
    try {
      fs.unlinkSync(tmp);
    } catch {
      /* tmp may already be gone or never existed */
    }
    throw err;
  }
  try {
    fs.unlinkSync(tmp);
  } catch {
    /* rename consumed it on the happy path; only present after EXDEV fallback */
  }
}

/** Async atomic write with EXDEV fallback. */
export async function atomicWrite(
  path: string,
  body: string,
  tmp: string,
  mode = 0o600,
): Promise<void> {
  try {
    await writeFile(tmp, body, "utf8");
    try {
      await chmod(tmp, mode);
    } catch {
      /* platform without chmod */
    }
    try {
      await rename(tmp, path);
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code !== "EXDEV") throw err;
      await copyFile(tmp, path);
      try {
        await chmod(path, mode);
      } catch {
        /* platform without chmod */
      }
    }
  } catch (err) {
    try {
      await unlink(tmp);
    } catch {
      /* tmp may already be gone or never existed */
    }
    throw err;
  }
  try {
    await unlink(tmp);
  } catch {
    /* rename consumed it on the happy path; only present after EXDEV fallback */
  }
}
