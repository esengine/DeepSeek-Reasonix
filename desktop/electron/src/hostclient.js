"use strict";
const http = require("node:http");

// Must match serve.TokenCookie.
const TOKEN_COOKIE = "reasonix_token";
const TIMEOUT_MS = 5000;

// StudioHost reaches the kernel from the process that holds the credential.
// Routing this through the renderer would put the token within reach of the
// page for no reason beyond convenience, and the tray is main's surface anyway.
class StudioHost {
  constructor(origin, token) {
    this.origin = origin;
    this.token = token;
  }

  trayPrefs() {
    return this.json("GET", "/tray/prefs");
  }

  setTrayPrefs(icon, closeToTray) {
    return this.json("PUT", "/tray/prefs", { icon, closeToTray });
  }

  trayState() {
    return this.json("GET", "/tray/state");
  }

  // A refusal and an unreachable kernel both answer null here: every caller of
  // this is a surface that has to keep working when the kernel is going down.
  async json(method, path, body) {
    try {
      const res = await this.request(method, path, body);
      return res.status >= 200 && res.status < 300 ? JSON.parse(res.body) : null;
    } catch {
      return null;
    }
  }

  request(method, path, body) {
    return new Promise((resolve, reject) => {
      const url = new URL(path, this.origin);
      const payload = body === undefined ? null : Buffer.from(JSON.stringify(body));
      const headers = { cookie: `${TOKEN_COOKIE}=${this.token}` };
      if (payload) {
        // The gate refuses a write that does not name this listener, and it is
        // right to: nothing else tells its own page from a page that guessed
        // the port.
        headers.origin = this.origin;
        headers["content-type"] = "application/json";
        headers["content-length"] = payload.length;
      }
      const req = http.request(
        { hostname: url.hostname, port: url.port, path: url.pathname + url.search, method, headers, timeout: TIMEOUT_MS },
        (res) => {
          let text = "";
          res.setEncoding("utf8");
          res.on("data", (chunk) => (text += chunk));
          res.on("end", () => resolve({ status: res.statusCode, body: text }));
        },
      );
      req.on("timeout", () => req.destroy(new Error("the kernel did not answer")));
      req.on("error", reject);
      if (payload) req.write(payload);
      req.end();
    });
  }
}

module.exports = { StudioHost, TOKEN_COOKIE };
