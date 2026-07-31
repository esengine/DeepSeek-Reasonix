import { tryInstallPkg } from "./install-pkg.js";
import { resolvePathAsync } from "./resolve.js";
import { promises } from "fs";
import { pathToFileURL } from "node:url";
const _collections = Object.create(null);
/** Check if full package exists, per cwd value */
const isLegacyExists = Object.create(null);
/**
* Asynchronously loads a collection from the file system.
*
* @param name {string} the name of the collection, e.g. 'mdi'
* @param autoInstall {AutoInstall} [autoInstall=false] - whether to automatically install
* @param scope {string} [scope='@iconify-json'] - the scope of the collection, e.g. '@my-company-json'
* @param cwd {string} [cwd=process.cwd()] - current working directory for caching
* @return {Promise<IconifyJSON | undefined>} the loaded IconifyJSON or undefined
*/
async function loadCollectionFromFS(name, autoInstall = false, scope = "@iconify-json", cwd = process.cwd()) {
	const cache = _collections[cwd] || (_collections[cwd] = Object.create(null));
	if (!await cache[name]) cache[name] = task();
	return cache[name];
	async function task() {
		const packageName = scope.length === 0 ? name : `${scope}/${name}`;
		let jsonPath = await resolvePathAsync(`${packageName}/icons.json`, cwd);
		if (scope === "@iconify-json") {
			if (isLegacyExists[cwd] === void 0) isLegacyExists[cwd] = !!await resolvePathAsync(`@iconify/json/collections.json`, cwd);
			const checkLegacy = isLegacyExists[cwd];
			if (!jsonPath && checkLegacy) jsonPath = await resolvePathAsync(`@iconify/json/json/${name}.json`, cwd);
			if (!jsonPath && !checkLegacy && autoInstall) {
				await tryInstallPkg(packageName, autoInstall);
				jsonPath = await resolvePathAsync(`${packageName}/icons.json`, cwd);
			}
		} else if (!jsonPath && autoInstall) {
			await tryInstallPkg(packageName, autoInstall);
			jsonPath = await resolvePathAsync(`${packageName}/icons.json`, cwd);
		}
		if (!jsonPath) {
			const packagePath = await resolvePathAsync(packageName, cwd);
			if (packagePath) {
				const { icons } = await import(pathToFileURL(packagePath).href);
				if (icons) return icons;
			}
		}
		let stat;
		try {
			stat = jsonPath ? await promises.lstat(jsonPath) : void 0;
		} catch (err) {
			return;
		}
		if (stat?.isFile()) return JSON.parse(await promises.readFile(jsonPath, "utf8"));
		else return;
	}
}
export { loadCollectionFromFS };
