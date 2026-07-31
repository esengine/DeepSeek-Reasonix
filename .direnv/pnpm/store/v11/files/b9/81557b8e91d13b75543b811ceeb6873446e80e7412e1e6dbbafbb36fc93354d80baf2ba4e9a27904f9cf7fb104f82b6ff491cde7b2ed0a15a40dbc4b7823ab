import { warnOnce } from "./warn.js";
import { installPackage } from "@antfu/install-pkg";
import { styleText } from "node:util";
let pending;
const tasks = {};
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function tryInstallPkg(name, autoInstall) {
	if (pending) await pending;
	if (!tasks[name]) {
		console.log(styleText("cyan", `Installing ${name}...`));
		if (typeof autoInstall === "function") tasks[name] = pending = autoInstall(name).then(() => sleep(300)).finally(() => {
			pending = void 0;
		});
		else tasks[name] = pending = installPackage(name, {
			dev: true,
			preferOffline: true
		}).then(() => sleep(300)).catch((e) => {
			warnOnce(`Failed to install ${name}`);
			console.error(e);
		}).finally(() => {
			pending = void 0;
		});
	}
	return tasks[name];
}
export { tryInstallPkg };
