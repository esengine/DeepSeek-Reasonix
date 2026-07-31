import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { exactRegex, makeIdFiltersToMatchWithQuery } from "@rolldown/pluginutils";
import { reactRefreshWrapperPlugin } from "vite/internal";
//#region ../common/refresh-utils.ts
const runtimePublicPath = "/@react-refresh";
const preambleCode = `import { injectIntoGlobalHook } from "__BASE__${runtimePublicPath.slice(1)}";
injectIntoGlobalHook(window);
window.$RefreshReg$ = () => {};
window.$RefreshSig$ = () => (type) => type;`;
const getPreambleCode = (base) => preambleCode.replace("__BASE__", base);
function virtualPreamblePlugin({ name, isEnabled }) {
	return {
		name: "vite:react-virtual-preamble",
		resolveId: {
			order: "pre",
			filter: { id: exactRegex(name) },
			handler(source) {
				if (source === name) return "\0" + source;
			}
		},
		load: {
			filter: { id: exactRegex("\0" + name) },
			handler(id) {
				if (id === "\0" + name) {
					if (isEnabled()) return preambleCode.replace("__BASE__", "/");
					return "";
				}
			}
		}
	};
}
//#endregion
//#region ../common/warning.ts
const silenceUseClientWarning = (userConfig) => ({ rollupOptions: { onwarn(warning, defaultHandler) {
	if (warning.code === "MODULE_LEVEL_DIRECTIVE" && (warning.message.includes("use client") || warning.message.includes("use server"))) return;
	if (warning.code === "SOURCEMAP_ERROR" && warning.message.includes("resolve original location") && warning.pos === 0) return;
	if (userConfig.build?.rollupOptions?.onwarn) userConfig.build.rollupOptions.onwarn(warning, defaultHandler);
	else defaultHandler(warning);
} } });
//#endregion
//#region src/reactCompilerPreset.ts
const reactCompilerPreset = (options = {}) => ({
	preset: () => ({ plugins: [["babel-plugin-react-compiler", options]] }),
	rolldown: {
		filter: { code: options.compilationMode === "annotation" ? /['"]use memo['"]/ : /\b[A-Z]|\buse/ },
		applyToEnvironmentHook: (env) => env.config.consumer === "client",
		optimizeDeps: { include: options.target === "17" || options.target === "18" ? ["react-compiler-runtime"] : ["react/compiler-runtime"] }
	}
});
//#endregion
//#region src/index.ts
const refreshRuntimePath = join(dirname(fileURLToPath(import.meta.url)), "refresh-runtime.js");
const defaultIncludeRE = /\.[tj]sx?$/;
const defaultExcludeRE = /\/node_modules\//;
function viteReact(opts = {}) {
	const include = opts.include ?? defaultIncludeRE;
	const exclude = opts.exclude ?? defaultExcludeRE;
	const jsxImportSource = opts.jsxImportSource ?? "react";
	const jsxImportRuntime = `${jsxImportSource}/jsx-runtime`;
	const jsxImportDevRuntime = `${jsxImportSource}/jsx-dev-runtime`;
	let runningInVite = false;
	let isProduction = true;
	let skipFastRefresh = true;
	let base;
	let isBundledDev = false;
	const viteBabel = {
		name: "vite:react-babel",
		enforce: "pre",
		config(_userConfig, { command }) {
			if (opts.jsxRuntime === "classic") return { oxc: {
				jsx: {
					runtime: "classic",
					refresh: command === "serve"
				},
				jsxRefreshInclude: makeIdFiltersToMatchWithQuery(include),
				jsxRefreshExclude: makeIdFiltersToMatchWithQuery(exclude)
			} };
			else return {
				oxc: {
					jsx: {
						runtime: "automatic",
						importSource: opts.jsxImportSource,
						refresh: command === "serve"
					},
					jsxRefreshInclude: makeIdFiltersToMatchWithQuery(include),
					jsxRefreshExclude: makeIdFiltersToMatchWithQuery(exclude)
				},
				optimizeDeps: { rolldownOptions: { transform: { jsx: { runtime: "automatic" } } } }
			};
		},
		configResolved(config) {
			runningInVite = true;
			base = config.base;
			if (config.experimental.bundledDev) isBundledDev = true;
			isProduction = config.isProduction;
			skipFastRefresh = isProduction || config.command === "build" || config.server.hmr === false;
		},
		options(options) {
			if (!runningInVite) {
				options.transform ??= {};
				options.transform.jsx = {
					runtime: opts.jsxRuntime,
					importSource: opts.jsxImportSource
				};
				return options;
			}
		}
	};
	const viteRefreshWrapper = {
		name: "vite:react:refresh-wrapper",
		apply: "serve",
		async applyToEnvironment(env) {
			if (env.config.consumer !== "client" || skipFastRefresh) return false;
			return reactRefreshWrapperPlugin({
				cwd: process.cwd(),
				include: makeIdFiltersToMatchWithQuery(include),
				exclude: makeIdFiltersToMatchWithQuery(exclude),
				jsxImportSource,
				reactRefreshHost: opts.reactRefreshHost ?? ""
			});
		}
	};
	const viteConfigPost = {
		name: "vite:react:config-post",
		enforce: "post",
		config(userConfig) {
			if (userConfig.server?.hmr === false) return { oxc: { jsx: { refresh: false } } };
		}
	};
	const viteReactRefreshBundledDevMode = {
		name: "vite:react-refresh-fbm",
		enforce: "pre",
		transformIndexHtml: {
			handler() {
				if (!skipFastRefresh && isBundledDev) return [{
					tag: "script",
					attrs: { type: "module" },
					children: getPreambleCode(base)
				}];
			},
			order: "pre"
		}
	};
	const dependencies = [
		"react",
		"react-dom",
		jsxImportDevRuntime,
		jsxImportRuntime
	];
	return [
		viteBabel,
		viteRefreshWrapper,
		viteConfigPost,
		viteReactRefreshBundledDevMode,
		{
			name: "vite:react-refresh",
			enforce: "pre",
			config: (userConfig) => ({
				build: silenceUseClientWarning(userConfig),
				optimizeDeps: { include: dependencies }
			}),
			resolveId: {
				filter: { id: exactRegex(runtimePublicPath) },
				handler(id) {
					if (id === "/@react-refresh") return id;
				}
			},
			load: {
				filter: { id: exactRegex(runtimePublicPath) },
				handler(id) {
					if (id === "/@react-refresh") return readFileSync(refreshRuntimePath, "utf-8").replace(/__README_URL__/g, "https://github.com/vitejs/vite-plugin-react/tree/main/packages/plugin-react");
				}
			},
			transformIndexHtml() {
				if (!skipFastRefresh && !isBundledDev) return [{
					tag: "script",
					attrs: { type: "module" },
					children: getPreambleCode(base)
				}];
			}
		},
		virtualPreamblePlugin({
			name: "@vitejs/plugin-react/preamble",
			isEnabled: () => !skipFastRefresh && !isBundledDev
		})
	];
}
viteReact.preambleCode = preambleCode;
function viteReactForCjs(options) {
	return viteReact.call(this, options);
}
Object.assign(viteReactForCjs, {
	default: viteReactForCjs,
	reactCompilerPreset
});
//#endregion
export { viteReact as default, viteReactForCjs as "module.exports", reactCompilerPreset };
