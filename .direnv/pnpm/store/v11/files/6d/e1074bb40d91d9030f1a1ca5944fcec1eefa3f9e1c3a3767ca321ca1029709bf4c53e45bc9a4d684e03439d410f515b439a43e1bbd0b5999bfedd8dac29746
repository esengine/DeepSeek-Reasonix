import { spawn as e, spawnSync as t } from "node:child_process";
import { cwd as n } from "node:process";
import { basename as r, delimiter as i, dirname as a, normalize as o, resolve as s } from "node:path";
import { pipeline as c } from "node:stream/promises";
import { PassThrough as l } from "node:stream";
import u from "node:readline";
import { closeSync as d, openSync as f, readSync as p, statSync as m } from "node:fs";
//#region src/env.ts
const h = /^path$/i;
const g = {
	key: "PATH",
	value: ""
};
function _(e) {
	for (const t in e) {
		if (!Object.prototype.hasOwnProperty.call(e, t) || !h.test(t)) continue;
		const n = e[t];
		if (!n) return g;
		return {
			key: t,
			value: n
		};
	}
	return g;
}
function v(e, t) {
	const n = t.value.split(i);
	const r = [];
	let o = e;
	let c;
	do {
		r.push(s(o, "node_modules", ".bin"));
		c = o;
		o = a(o);
	} while (o !== c);
	r.push(a(process.execPath));
	const l = r.concat(n).join(i);
	return {
		key: t.key,
		value: l
	};
}
function y(e, t, n = true) {
	const r = {
		...process.env,
		...t
	};
	if (!n) return r;
	const i = v(e, _(r));
	r[i.key] = i.value;
	return r;
}
//#endregion
//#region src/stream.ts
const b = (e) => {
	let t = e.length;
	const n = new l();
	const r = () => {
		if (--t === 0) n.end();
	};
	for (const t of e) c(t, n, { end: false }).then(r).catch(r);
	return n;
};
//#endregion
//#region src/normalize.ts
const x = /([()\][%!^"`<>&|;, *?])/g;
const S = /^#!\s*(.+)/;
const C = /\.(?:com|exe)$/i;
const w = /node_modules[\\/]\.bin[\\/][^\\/]+\.cmd$/i;
const T = process.platform === "win32";
const E = [
	".EXE",
	".CMD",
	".BAT",
	".COM"
];
/**
* Normalizes the command and arguments to work cross-platform.
* On Windows, this basically handles things like shebangs, calling
* `node_modules/.bin` commands, and escaping meta characters.
* On other platforms, it just returns the command and arguments as-is.
*/
function D(e, t = [], n = {}) {
	if (n.shell === true || !T) return {
		command: e,
		args: t,
		options: n
	};
	let i = O(e, n);
	let a = null;
	if (i !== null) {
		const e = 150;
		const t = Buffer.alloc(e);
		let n = null;
		try {
			n = f(i, "r");
			p(n, t, 0, e, 0);
		} catch {} finally {
			if (n !== null) d(n);
		}
		const o = t.toString().match(S);
		if (o !== null) {
			const e = o[1].trim();
			const t = e.indexOf(" ");
			const n = t !== -1 ? e.slice(0, t) : e;
			const i = t !== -1 ? e.slice(t + 1) : "";
			const s = r(n);
			a = s === "env" ? i || null : s;
		}
	}
	if (a !== null && i !== null) {
		t = [i, ...t];
		e = a;
		i = O(e, n);
	}
	if (i === null || !C.test(i)) {
		const r = i !== null && w.test(i);
		e = o(e);
		e = e.replace(x, "^$1");
		t = t.map((e) => {
			e = e.replace(/(?=(\\+?)?)\1"/g, "$1$1\\\"");
			e = e.replace(/(?=(\\+?)?)\1$/, "$1$1");
			e = `"${e}"`;
			e = e.replace(x, "^$1");
			if (r) e = e.replace(x, "^$1");
			return e;
		});
		t = [
			"/d",
			"/s",
			"/c",
			`"${[e, ...t].join(" ")}"`
		];
		e = n.env?.comspec ?? "cmd.exe";
		n = {
			...n,
			windowsVerbatimArguments: true
		};
	}
	return {
		command: e,
		args: t,
		options: n
	};
}
/**
* Resolves the command to an absolute path if possible.
* Handles things like traversing PATH and adding extensions from PATHEXT
*/
function O(e, t) {
	const r = (t.cwd ?? n()).toString();
	const a = t.env ?? process.env;
	const o = _(a).value;
	const c = e.includes("/") || e.includes("\\") ? [""] : [r, ...o.split(i)];
	const l = a.PATHEXT ? a.PATHEXT.split(i) : E;
	if (e.includes(".") && l[0] !== "") l.unshift("");
	for (const t of c) {
		const n = s(r, t.startsWith("\"") && t.endsWith("\"") && t.length > 1 ? t.slice(1, -1) : t, e);
		for (const e of l) {
			const t = n + e;
			try {
				if (m(t).isFile()) return t;
			} catch {}
		}
	}
	return null;
}
//#endregion
//#region src/non-zero-exit-error.ts
var k = class extends Error {
	result;
	output;
	get exitCode() {
		if (this.result.exitCode !== null) return this.result.exitCode;
	}
	constructor(e, t) {
		super(`Process exited with non-zero status (${e.exitCode})`);
		this.result = e;
		this.output = t;
	}
};
//#endregion
//#region src/main.ts
const A = /\r?\n/;
const j = {
	timeout: void 0,
	persist: false
};
const M = { timeout: void 0 };
const N = { windowsHide: true };
function P(e) {
	const t = new AbortController();
	for (const n of e) {
		if (n.aborted) {
			t.abort();
			return n;
		}
		const e = () => {
			t.abort(n.reason);
		};
		n.addEventListener("abort", e, { signal: t.signal });
	}
	return t.signal;
}
async function F(e) {
	let t = "";
	try {
		for await (const n of e) t += n.toString();
	} catch {}
	return t;
}
var I = class {
	_process;
	_aborted = false;
	_options;
	_command;
	_args;
	_resolveClose;
	_processClosed;
	_thrownError;
	get process() {
		return this._process;
	}
	get pid() {
		return this._process?.pid;
	}
	get exitCode() {
		if (this._process && this._process.exitCode !== null) return this._process.exitCode;
	}
	constructor(e, t, n) {
		this._options = {
			...j,
			...n
		};
		this._command = e;
		this._args = t ?? [];
		this._processClosed = new Promise((e) => {
			this._resolveClose = e;
		});
	}
	kill(e) {
		return this._process?.kill(e) === true;
	}
	get aborted() {
		return this._aborted;
	}
	get killed() {
		return this._process?.killed === true;
	}
	pipe(e, t, n) {
		return z(e, t, {
			...n,
			stdin: this
		});
	}
	async *[Symbol.asyncIterator]() {
		const e = this._process;
		if (!e) return;
		const t = [];
		if (this._streamErr) t.push(this._streamErr);
		if (this._streamOut) t.push(this._streamOut);
		const n = b(t);
		const r = u.createInterface({ input: n });
		for await (const e of r) yield e.toString();
		await this._processClosed;
		e.removeAllListeners();
		if (this._thrownError) throw this._thrownError;
		if (this._options?.throwOnError && this.exitCode !== 0 && this.exitCode !== void 0) throw new k(this);
	}
	async _waitForOutput() {
		const e = this._process;
		if (!e) throw new Error("No process was started");
		const [t, n] = await Promise.all([this._streamOut ? F(this._streamOut) : "", this._streamErr ? F(this._streamErr) : ""]);
		await this._processClosed;
		const { stdin: r } = this._options;
		if (r && typeof r !== "string") await r;
		e.removeAllListeners();
		if (this._thrownError) throw this._thrownError;
		const i = {
			stderr: n,
			stdout: t,
			exitCode: this.exitCode
		};
		if (this._options.throwOnError && this.exitCode !== 0 && this.exitCode !== void 0) throw new k(this, i);
		return i;
	}
	then(e, t) {
		return this._waitForOutput().then(e, t);
	}
	_streamOut;
	_streamErr;
	spawn() {
		const t = n();
		const r = this._options;
		const i = {
			...N,
			...r.nodeOptions
		};
		const a = [];
		this._resetState();
		if (r.timeout !== void 0) a.push(AbortSignal.timeout(r.timeout));
		if (r.signal !== void 0) a.push(r.signal);
		if (r.persist === true) i.detached = true;
		if (a.length > 0) i.signal = P(a);
		i.env = y(t, i.env, r.nodePath);
		const o = D(this._command, this._args, i);
		const s = e(o.command, o.args, o.options);
		if (s.stderr) this._streamErr = s.stderr;
		if (s.stdout) this._streamOut = s.stdout;
		this._process = s;
		s.once("error", this._onError);
		s.once("close", this._onClose);
		if (s.stdin) {
			const { stdin: e } = r;
			if (typeof e === "string") s.stdin.end(e);
			else e?.process?.stdout?.pipe(s.stdin);
		}
	}
	_resetState() {
		this._aborted = false;
		this._processClosed = new Promise((e) => {
			this._resolveClose = e;
		});
		this._thrownError = void 0;
	}
	_onError = (e) => {
		if (e.name === "AbortError" && (!(e.cause instanceof Error) || e.cause.name !== "TimeoutError")) {
			this._aborted = true;
			return;
		}
		this._thrownError = e;
	};
	_onClose = () => {
		if (this._resolveClose) this._resolveClose();
	};
};
function L(e, r, i) {
	const a = {
		...M,
		...i
	};
	const o = n();
	const s = {
		windowsHide: true,
		...a.nodeOptions
	};
	if (a.timeout !== void 0) s.timeout = a.timeout;
	s.env = y(o, s.env, a.nodePath);
	const c = D(e, r ?? [], s);
	const l = t(c.command, c.args, c.options);
	if (l.error) throw l.error;
	const u = l.stdout?.toString() ?? "";
	const d = l.stderr?.toString() ?? "";
	const f = l.status ?? void 0;
	const p = l.signal != null;
	const m = {
		stdout: u,
		stderr: d,
		get exitCode() {
			return f;
		},
		get pid() {
			return l.pid;
		},
		get killed() {
			return p;
		},
		*[Symbol.iterator]() {
			for (const e of [u, d]) {
				if (!e) continue;
				const t = e.split(A);
				if (t[t.length - 1] === "") t.pop();
				yield* t;
			}
		}
	};
	if (a.throwOnError && f !== 0 && f !== void 0) throw new k(m, m);
	return m;
}
const R = (e, t, n) => {
	const r = new I(e, t, n);
	r.spawn();
	return r;
};
const z = R;
const B = L;
//#endregion
export { I as ExecProcess, k as NonZeroExitError, z as exec, B as execSync, D as normalizeSpawnCommand, R as x, L as xSync };
