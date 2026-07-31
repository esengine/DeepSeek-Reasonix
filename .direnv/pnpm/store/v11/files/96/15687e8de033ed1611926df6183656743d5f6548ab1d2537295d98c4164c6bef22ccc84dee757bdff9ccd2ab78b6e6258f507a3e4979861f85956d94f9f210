import { fileURLToPath, pathToFileURL } from "node:url";
import fs from "node:fs/promises";
import path from "node:path";
import { resolve } from "import-meta-resolve";
/**
* Resolve path to package
*/
async function resolvePathAsync(packageName, cwd) {
	if (!await pathExists(cwd)) return;
	if (!path.isAbsolute(cwd)) return;
	const parent = pathToFileURL(path.join(cwd, "_parent.mjs")).href;
	try {
		const resolvedPath = fileURLToPath(resolve(packageName, parent));
		if (await pathExists(resolvedPath)) return resolvedPath;
	} catch {}
}
async function pathExists(path) {
	try {
		await fs.access(path);
		return true;
	} catch {
		return false;
	}
}
export { resolvePathAsync };
